package deploy

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

// KServeStrategy deploys serving.kserve.io/v1beta1 InferenceService.
type KServeStrategy struct {
	GVR schema.GroupVersionResource
}

func (KServeStrategy) Name() string { return contract.PlatformKServe }

func (k KServeStrategy) PrimaryName(opts DeployOptions) string {
	return ModelSlug(opts.Model)
}

func (k KServeStrategy) Build(opts DeployOptions) ([]client.Object, error) {
	if err := validateDeployOptions(opts); err != nil {
		return nil, err
	}
	name := k.PrimaryName(opts)
	team := opts.Team
	if team == "" {
		team = DefaultTeam
	}

	labels := map[string]string{
		contract.AnnPlatform:         contract.PlatformKServe,
		contract.LabelModel:          opts.Model,
		contract.LabelTeam:           team,
		contract.LabelWorkloadType:   contract.WorkloadTypeInference,
	}

	obj := map[string]any{
		"apiVersion": "serving.kserve.io/v1beta1",
		"kind":       "InferenceService",
		"metadata": map[string]any{
			"name":      name,
			"namespace": opts.Namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"predictor": map[string]any{
				"model": map[string]any{
					"modelFormat": map[string]any{
						"name": "vllm",
					},
					"storageUri": fmt.Sprintf("hf://%s", opts.Model),
					"resources": map[string]any{
						"requests": map[string]any{
							"nvidia.com/gpu": "1",
						},
						"limits": map[string]any{
							"nvidia.com/gpu": "1",
						},
					},
				},
			},
		},
	}

	u := &unstructured.Unstructured{Object: obj}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1beta1",
		Kind:    "InferenceService",
	})
	return []client.Object{u}, nil
}

func (k KServeStrategy) WaitReady(ctx context.Context, _ client.Client, dyn dynamic.Interface, namespace, name string) (string, error) {
	if dyn == nil {
		return "", fmt.Errorf("dynamic client required for KServe")
	}
	u, err := dyn.Resource(k.GVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get InferenceService %s/%s: %w", namespace, name, err)
	}
	if !inferenceServiceReady(u) {
		return "", fmt.Errorf("inference service not ready")
	}
	url, _, _ := unstructured.NestedString(u.Object, "status", "url")
	if url == "" {
		return "", fmt.Errorf("inference service ready but status.url is empty")
	}
	return url, nil
}

func (k KServeStrategy) ReadCostPerHour(ctx context.Context, _ client.Client, dyn dynamic.Interface, namespace, name string) string {
	if dyn == nil {
		return "—"
	}
	u, err := dyn.Resource(k.GVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "—"
	}
	anns, _, _ := unstructured.NestedStringMap(u.Object, "metadata", "annotations")
	if anns == nil {
		return "—"
	}
	return formatCostAnnotation(anns[contract.AnnCostPerHour])
}

func inferenceServiceReady(u *unstructured.Unstructured) bool {
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

// KServeGVR returns the GVR for dynamic create (used by deploy_cmd).
func (k KServeStrategy) KServeGVR() schema.GroupVersionResource {
	return k.GVR
}

// IsKServeObject reports whether obj is an unstructured InferenceService for this strategy.
func IsKServeObject(obj client.Object) bool {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return false
	}
	return u.GetAPIVersion() == "serving.kserve.io/v1beta1" && u.GetKind() == "InferenceService"
}

// CreateKServe creates an InferenceService via the dynamic client.
func CreateKServe(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, u *unstructured.Unstructured) error {
	_, err := dyn.Resource(gvr).Namespace(u.GetNamespace()).Create(ctx, u, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create InferenceService: %w", err)
	}
	return nil
}
