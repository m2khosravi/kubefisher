package workload

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/cost"
)

// WorkloadStatus is a platform-agnostic view of a named GPU workload.
type WorkloadStatus struct {
	Name         string
	Namespace    string
	Kind         string
	Phase        string
	Endpoint     string
	Replicas     string
	CostPerHour  string
	CostPerToken string
	WorkloadType string
	Platform     string
	LastUpdated  string
}

// FindResource locates a workload by name across supported kinds.
func FindResource(
	ctx context.Context,
	name, namespace string,
	cl client.Client,
	dyn dynamic.Interface,
	disc discovery.DiscoveryInterface,
) (*WorkloadStatus, error) {
	cat, err := discoverResources(disc)
	if err != nil {
		return nil, err
	}

	if dyn != nil {
		for _, gvr := range cat.kserve {
			if st, err := getUnstructured(ctx, dyn, gvr, namespace, name, "InferenceService"); err == nil {
				return st, nil
			} else if !apierrors.IsNotFound(err) {
				return nil, err
			}
		}
		for _, gvr := range cat.bento {
			if st, err := getUnstructured(ctx, dyn, gvr, namespace, name, "BentoDeployment"); err == nil {
				return st, nil
			} else if !apierrors.IsNotFound(err) {
				return nil, err
			}
		}
		for _, gvr := range cat.training {
			kind := trainingKindForResource(gvr.Resource)
			if st, err := getUnstructured(ctx, dyn, gvr, namespace, name, kind); err == nil {
				return st, nil
			} else if !apierrors.IsNotFound(err) {
				return nil, err
			}
		}
		for _, gvr := range cat.ray {
			kind := rayKindForResource(gvr.Resource)
			if st, err := getUnstructured(ctx, dyn, gvr, namespace, name, kind); err == nil {
				return st, nil
			} else if !apierrors.IsNotFound(err) {
				return nil, err
			}
		}
	}

	var dep appsv1.Deployment
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &dep); err == nil {
		return statusFromDeployment(&dep), nil
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	var sts appsv1.StatefulSet
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &sts); err == nil {
		return statusFromStatefulSet(&sts), nil
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get statefulset: %w", err)
	}

	return nil, fmt.Errorf("resource %q not found in namespace %q", name, namespace)
}

func getUnstructured(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, namespace, name, kind string) (*WorkloadStatus, error) {
	u, err := dyn.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if u.GetKind() == "" {
		u.SetKind(kind)
	}
	return statusFromUnstructured(u), nil
}

func statusFromUnstructured(u *unstructured.Unstructured) *WorkloadStatus {
	kind := u.GetKind()
	labels := u.GetLabels()
	anns, _, _ := unstructured.NestedStringMap(u.Object, "metadata", "annotations")
	templateLabels, _ := templateLabelsFromUnstructured(u)

	platform, workloadType := cost.DetectPlatform(mergeMaps(labels, templateLabels), anns, kind)

	st := &WorkloadStatus{
		Name:         u.GetName(),
		Namespace:    u.GetNamespace(),
		Kind:         kind,
		Platform:     platform,
		WorkloadType: workloadType,
		Phase:        phaseFromUnstructured(u),
		Endpoint:     dashIfEmpty(endpointFromUnstructured(u, kind)),
		Replicas:     replicasFromUnstructuredStatus(u),
	}
	populateCostFields(st, anns)
	return st
}

func statusFromDeployment(dep *appsv1.Deployment) *WorkloadStatus {
	labels := mergeMaps(dep.Labels, dep.Spec.Template.Labels)
	platform, workloadType := cost.DetectPlatform(labels, dep.Annotations, "Deployment")

	want := int32(1)
	if dep.Spec.Replicas != nil {
		want = *dep.Spec.Replicas
	}
	avail := dep.Status.AvailableReplicas

	st := &WorkloadStatus{
		Name:         dep.Name,
		Namespace:    dep.Namespace,
		Kind:         "Deployment",
		Platform:     platform,
		WorkloadType: workloadType,
		Phase:        deploymentPhase(avail, want),
		Replicas:     fmt.Sprintf("%d/%d", avail, want),
		Endpoint:     "—",
	}
	populateCostFields(st, dep.Annotations)
	return st
}

func statusFromStatefulSet(sts *appsv1.StatefulSet) *WorkloadStatus {
	labels := mergeMaps(sts.Labels, sts.Spec.Template.Labels)
	platform, workloadType := cost.DetectPlatform(labels, sts.Annotations, "StatefulSet")

	want := int32(1)
	if sts.Spec.Replicas != nil {
		want = *sts.Spec.Replicas
	}
	ready := sts.Status.ReadyReplicas

	st := &WorkloadStatus{
		Name:         sts.Name,
		Namespace:    sts.Namespace,
		Kind:         "StatefulSet",
		Platform:     platform,
		WorkloadType: workloadType,
		Phase:        deploymentPhase(ready, want),
		Replicas:     fmt.Sprintf("%d/%d", ready, want),
		Endpoint:     "—",
	}
	populateCostFields(st, sts.Annotations)
	return st
}

func deploymentPhase(ready, want int32) string {
	if want == 0 {
		return "ScaledToZero"
	}
	if ready >= want {
		return "Available"
	}
	if ready > 0 {
		return "Progressing"
	}
	return "Unavailable"
}

func phaseFromUnstructured(u *unstructured.Unstructured) string {
	if readyConditionTrue(u) {
		return "Ready"
	}
	if ph, ok, _ := unstructured.NestedString(u.Object, "status", "phase"); ok && ph != "" {
		return ph
	}
	return "Unknown"
}

func readyConditionTrue(u *unstructured.Unstructured) bool {
	conds, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	if !found {
		return false
	}
	for _, c := range conds {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		s, _ := m["status"].(string)
		if strings.EqualFold(t, "Ready") && strings.EqualFold(s, "True") {
			return true
		}
	}
	return false
}

func endpointFromUnstructured(u *unstructured.Unstructured, kind string) string {
	if url, ok, _ := unstructured.NestedString(u.Object, "status", "url"); ok && url != "" {
		return url
	}
	if kind == "InferenceService" {
		return ""
	}
	return ""
}

func replicasFromUnstructuredStatus(u *unstructured.Unstructured) string {
	if r, ok, _ := unstructured.NestedInt64(u.Object, "status", "replicas"); ok {
		ready, _, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas")
		return fmt.Sprintf("%d/%d", ready, r)
	}
	return "—"
}

func templateLabelsFromUnstructured(u *unstructured.Unstructured) (map[string]string, bool) {
	if labels, ok, _ := unstructured.NestedStringMap(u.Object, "spec", "template", "metadata", "labels"); ok {
		return labels, true
	}
	if labels, ok, _ := unstructured.NestedStringMap(u.Object, "spec", "predictor", "metadata", "labels"); ok {
		return labels, true
	}
	return nil, false
}

func mergeMaps(parts ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range parts {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func trainingKindForResource(resource string) string {
	switch resource {
	case "trainjobs":
		return "TrainJob"
	case "pytorchjobs":
		return "PyTorchJob"
	case "tfjobs":
		return "TFJob"
	case "xgboostjobs":
		return "XGBoostJob"
	case "mpijobs":
		return "MPIJob"
	case "paddlejobs":
		return "PaddleJob"
	default:
		return "TrainJob"
	}
}

func rayKindForResource(resource string) string {
	switch resource {
	case "rayclusters":
		return "RayCluster"
	case "rayservices":
		return "RayService"
	default:
		return "RayJob"
	}
}

// ResolveDeploymentEndpoint sets Endpoint from a matching Service when possible.
func ResolveDeploymentEndpoint(ctx context.Context, cl client.Client, st *WorkloadStatus) {
	if st.Kind != "Deployment" && st.Kind != "StatefulSet" {
		return
	}
	var svc corev1.Service
	if err := cl.Get(ctx, client.ObjectKey{Namespace: st.Namespace, Name: st.Name}, &svc); err != nil {
		st.Endpoint = fmt.Sprintf("http://%s.%s.svc:8000/v1/completions", st.Name, st.Namespace)
		return
	}
	port := int32(8000)
	for _, p := range svc.Spec.Ports {
		if p.Port > 0 {
			port = p.Port
			break
		}
	}
	st.Endpoint = fmt.Sprintf("http://%s.%s.svc:%d/v1/completions", st.Name, st.Namespace, port)
}
