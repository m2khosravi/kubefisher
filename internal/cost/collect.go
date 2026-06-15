package cost

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

type rowKey struct {
	namespace string
	kind      string
	name      string
}

// CollectCostRows scans GPU workloads and reads cost annotations written by cost-patcher.
func CollectCostRows(ctx context.Context, cl client.Client, dyn dynamic.Interface, disc discovery.DiscoveryInterface, namespace string) ([]CostRow, error) {
	catalog, err := discoverCRDs(disc)
	if err != nil {
		return nil, err
	}

	byKey := map[rowKey]CostRow{}

	if err := collectDeployments(ctx, cl, namespace, byKey); err != nil {
		return nil, err
	}
	if err := collectStatefulSets(ctx, cl, namespace, byKey); err != nil {
		return nil, err
	}
	if dyn != nil {
		for _, gvr := range catalog.kserve {
			if err := collectUnstructured(ctx, dyn, namespace, gvr, byKey); err != nil {
				return nil, err
			}
		}
		for _, gvr := range catalog.bento {
			if err := collectUnstructured(ctx, dyn, namespace, gvr, byKey); err != nil {
				return nil, err
			}
		}
		for _, gvr := range catalog.training {
			if err := collectUnstructured(ctx, dyn, namespace, gvr, byKey); err != nil {
				return nil, err
			}
		}
		for _, gvr := range catalog.ray {
			if err := collectUnstructured(ctx, dyn, namespace, gvr, byKey); err != nil {
				return nil, err
			}
		}
	}

	rows := make([]CostRow, 0, len(byKey))
	for _, r := range byKey {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

type crdCatalog struct {
	kserve   []schema.GroupVersionResource
	bento    []schema.GroupVersionResource
	training []schema.GroupVersionResource
	ray      []schema.GroupVersionResource
}

func discoverCRDs(disc discovery.DiscoveryInterface) (crdCatalog, error) {
	var cat crdCatalog
	lists, err := disc.ServerPreferredResources()
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			lists, _ = disc.ServerPreferredResources()
		} else {
			return cat, fmt.Errorf("discovery: %w", err)
		}
	}
	for _, rl := range lists {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			continue
		}
		group := gv.Group
		for _, r := range rl.APIResources {
			if r.Kind == "" || strings.Contains(r.Name, "/") {
				continue
			}
			gvr := schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: r.Name}
			switch {
			case strings.Contains(group, "serving.kserve.io") && r.Name == "inferenceservices":
				cat.kserve = append(cat.kserve, gvr)
			case IsBentoDeploymentGVR(group, r.Name):
				cat.bento = append(cat.bento, gvr)
			case strings.Contains(group, "kubeflow.org") &&
				(r.Name == "trainjobs" || r.Name == "pytorchjobs" || r.Name == "tfjobs" ||
					r.Name == "xgboostjobs" || r.Name == "mpijobs" || r.Name == "paddlejobs"):
				cat.training = append(cat.training, gvr)
			case strings.Contains(group, "ray.io") && (r.Name == "rayjobs" || r.Name == "rayclusters" || r.Name == "rayservices"):
				cat.ray = append(cat.ray, gvr)
			}
		}
	}
	return cat, nil
}

func collectDeployments(ctx context.Context, cl client.Client, namespace string, byKey map[rowKey]CostRow) error {
	var list appsv1.DeploymentList
	if err := cl.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list deployments: %w", err)
	}
	for i := range list.Items {
		d := &list.Items[i]
		if isKServeChildDeployment(d) || isBentoChildDeployment(d) {
			continue
		}
		if !shouldIncludeWorkload(d.Annotations, d.Spec.Template.Spec) {
			continue
		}
		addWorkloadRow(byKey, d.Namespace, d.Name, "Deployment", d.Labels, d.Annotations,
			d.Spec.Template.Labels, replicaCount(d.Spec.Replicas), d.Spec.Template.Spec)
	}
	return nil
}

func collectStatefulSets(ctx context.Context, cl client.Client, namespace string, byKey map[rowKey]CostRow) error {
	var list appsv1.StatefulSetList
	if err := cl.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list statefulsets: %w", err)
	}
	for i := range list.Items {
		sts := &list.Items[i]
		if !shouldIncludeWorkload(sts.Annotations, sts.Spec.Template.Spec) {
			continue
		}
		addWorkloadRow(byKey, sts.Namespace, sts.Name, "StatefulSet", sts.Labels, sts.Annotations,
			sts.Spec.Template.Labels, replicaCount(sts.Spec.Replicas), sts.Spec.Template.Spec)
	}
	return nil
}

func collectUnstructured(ctx context.Context, dyn dynamic.Interface, namespace string, gvr schema.GroupVersionResource, byKey map[rowKey]CostRow) error {
	list, err := dyn.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list %s: %w", gvr.String(), err)
	}
	for i := range list.Items {
		u := &list.Items[i]
		kind := u.GetKind()
		if kind == "" {
			kind = gvr.Resource
		}
		podSpec := podSpecFromUnstructured(u)
		if !shouldIncludeWorkload(u.GetAnnotations(), podSpec) {
			continue
		}
		templateLabels, _ := templateLabelsFromUnstructured(u)
		addWorkloadRow(byKey, u.GetNamespace(), u.GetName(), kind, u.GetLabels(), u.GetAnnotations(),
			templateLabels, replicasFromUnstructured(u), podSpec)
	}
	return nil
}

func isKServeChildDeployment(d *appsv1.Deployment) bool {
	if d.Spec.Template.Labels != nil && d.Spec.Template.Labels["serving.kserve.io/inferenceservice"] != "" {
		return true
	}
	return false
}

func isBentoChildDeployment(d *appsv1.Deployment) bool {
	if d.Spec.Template.Labels == nil {
		return false
	}
	return d.Spec.Template.Labels["yatai.bentoml.com/bento-deployment"] != ""
}

func shouldIncludeWorkload(annotations map[string]string, podSpec corev1.PodSpec) bool {
	if hasCostAnnotation(annotations) {
		return true
	}
	return podSpecRequestsGPU(podSpec)
}

func hasCostAnnotation(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	_, okH := annotations[contract.AnnCostPerHourPerReplica]
	if !okH {
		_, okH = annotations[contract.AnnCostPerHour]
	}
	_, okT := annotations[contract.AnnCostPerToken]
	return okH || okT
}

func addWorkloadRow(byKey map[rowKey]CostRow, namespace, name, kind string,
	objLabels, annotations, templateLabels map[string]string, replicas int32, podSpec corev1.PodSpec) {
	key := rowKey{namespace: namespace, kind: kind, name: name}
	if _, exists := byKey[key]; exists {
		return
	}
	mergeLabels := map[string]string{}
	for k, v := range objLabels {
		mergeLabels[k] = v
	}
	for k, v := range templateLabels {
		mergeLabels[k] = v
	}
	platform, workloadType := DetectPlatform(mergeLabels, annotations, kind)
	gpuPerPod := gpusPerPod(podSpec)
	row := CostRow{
		Namespace:    namespace,
		Name:         name,
		Platform:     platform,
		WorkloadType: workloadType,
		WorkloadKind: kind,
		GPUCount:     int(replicas) * gpuPerPod,
		Replicas:     replicas,
	}
	cph, cpt, cphTotal, updated := costAnnotationsFromMap(annotations)
	row.CostPerHour = cph
	row.CostPerHourTotal = cphTotal
	row.CostPerToken = cpt
	row.LastUpdated = updated
	byKey[key] = row
}

func costAnnotationsFromMap(annotations map[string]string) (costPerHour, costPerToken, costPerHourTotal *float64, lastUpdated string) {
	if annotations == nil {
		return nil, nil, nil, ""
	}
	// Prefer the new per-replica key; fall back to the deprecated key for older patchers.
	cphKey := contract.AnnCostPerHourPerReplica
	if _, ok := annotations[cphKey]; !ok {
		cphKey = contract.AnnCostPerHour
	}
	if v, ok := annotations[cphKey]; ok && strings.TrimSpace(v) != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			costPerHour = &f
		}
	}
	if v, ok := annotations[contract.AnnCostPerToken]; ok && strings.TrimSpace(v) != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			costPerToken = &f
		}
	}
	if v, ok := annotations[contract.AnnCostPerHourTotal]; ok && strings.TrimSpace(v) != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			costPerHourTotal = &f
		}
	}
	lastUpdated = annotations[contract.AnnLastUpdated]
	return costPerHour, costPerToken, costPerHourTotal, lastUpdated
}

func replicaCount(r *int32) int32 {
	if r == nil {
		return 1
	}
	return *r
}

func gpusPerPod(spec corev1.PodSpec) int {
	total := 0
	for _, c := range spec.Containers {
		total += gpuQty(c.Resources.Limits) + gpuQty(c.Resources.Requests)
	}
	for _, c := range spec.InitContainers {
		total += gpuQty(c.Resources.Limits) + gpuQty(c.Resources.Requests)
	}
	if total > 0 {
		return total
	}
	return 0
}

func gpuQty(rl corev1.ResourceList) int {
	if rl == nil {
		return 0
	}
	q, ok := rl["nvidia.com/gpu"]
	if !ok {
		return 0
	}
	return int(q.Value())
}

func podSpecRequestsGPU(spec corev1.PodSpec) bool {
	return gpusPerPod(spec) > 0
}

func podSpecFromUnstructured(u *unstructured.Unstructured) corev1.PodSpec {
	var spec corev1.PodSpec
	// Bento/Yatai BentoDeployment resource limits (GPU count for inclusion filter).
	if limits, ok, _ := unstructured.NestedMap(u.Object, "spec", "resources", "limits"); ok {
		spec.Containers = []corev1.Container{{Resources: corev1.ResourceRequirements{
			Limits: resourceListFromMap(limits),
		}}}
		if podSpecRequestsGPU(spec) {
			return spec
		}
	}
	for _, path := range [][]string{
		{"spec", "predictor", "model", "resources"},
		{"spec", "predictor", "resources"},
	} {
		if limits := nestedResourceLimits(u, path...); limits != nil {
			spec.Containers = []corev1.Container{{Resources: corev1.ResourceRequirements{Limits: limits}}}
			if podSpecRequestsGPU(spec) {
				return spec
			}
		}
	}
	// KServe predictor pod template paths.
	for _, path := range [][]string{
		{"spec", "predictor", "podSpec"},
		{"spec", "predictor", "containers"},
	} {
		if path[len(path)-1] == "containers" {
			if raw, ok, _ := unstructured.NestedSlice(u.Object, path...); ok && len(raw) > 0 {
				spec.Containers = containersFromSlice(raw)
				if podSpecRequestsGPU(spec) {
					return spec
				}
			}
			continue
		}
		if raw, ok, _ := unstructured.NestedMap(u.Object, path...); ok {
			_ = runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &spec)
			if podSpecRequestsGPU(spec) {
				return spec
			}
		}
	}
	if raw, ok, _ := unstructured.NestedMap(u.Object, "spec", "template", "spec"); ok {
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &spec)
	}
	return spec
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

func replicasFromUnstructured(u *unstructured.Unstructured) int32 {
	if r, ok, _ := unstructured.NestedInt64(u.Object, "spec", "replicas"); ok && r > 0 {
		return int32(r)
	}
	return 1
}

func containersFromSlice(raw []any) []corev1.Container {
	out := make([]corev1.Container, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var c corev1.Container
		if res, ok, _ := unstructured.NestedMap(m, "resources"); ok {
			limits := resourceListFromMap(nestedMap(res, "limits"))
			requests := resourceListFromMap(nestedMap(res, "requests"))
			c.Resources.Limits = limits
			c.Resources.Requests = requests
		}
		out = append(out, c)
	}
	return out
}

func nestedResourceLimits(u *unstructured.Unstructured, keys ...string) corev1.ResourceList {
	res, ok, err := unstructured.NestedMap(u.Object, keys...)
	if err != nil || !ok {
		return nil
	}
	limits := nestedMap(res, "limits")
	return resourceListFromMap(limits)
}

func nestedMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func resourceListFromMap(m map[string]any) corev1.ResourceList {
	if m == nil {
		return nil
	}
	rl := corev1.ResourceList{}
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			continue
		}
		q, err := resource.ParseQuantity(s)
		if err == nil {
			rl[corev1.ResourceName(k)] = q
		}
	}
	return rl
}
