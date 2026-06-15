package cost

import (
	"testing"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/stretchr/testify/require"
)

func TestDetectPlatform(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		anns     map[string]string
		kind     string
		platform string
		wtype    string
	}{
		{
			name:     "kserve_kind",
			kind:     "InferenceService",
			platform: contract.PlatformKServe,
			wtype:    contract.WorkloadTypeInference,
		},
		{
			name:     "bento_kind",
			kind:     "BentoDeployment",
			platform: contract.PlatformBentoML,
			wtype:    contract.WorkloadTypeInference,
		},
		{
			name:     "yatai_bento_deployment_label",
			kind:     "Deployment",
			labels:   map[string]string{"yatai.bentoml.com/bento-deployment": "llama3"},
			platform: contract.PlatformBentoML,
			wtype:    contract.WorkloadTypeInference,
		},
		{
			name:     "trainjob",
			kind:     "TrainJob",
			platform: contract.PlatformKubeflowTrainer,
			wtype:    contract.WorkloadTypeTraining,
		},
		{
			name:     "trainer_v2_pod_labels",
			kind:     "Pod",
			labels: map[string]string{
				"trainer.kubeflow.org/trainjob-ancestor-step": "trainer",
				"trainer.kubeflow.org/trainjob-name":          "my-job",
			},
			platform: contract.PlatformKubeflowTrainer,
			wtype:    contract.WorkloadTypeTraining,
		},
		{
			name:     "rayservice_kind",
			kind:     "RayService",
			platform: contract.PlatformRayServe,
			wtype:    contract.WorkloadTypeInference,
		},
		{
			name:     "ray_serve_deployment_label",
			kind:     "Pod",
			labels: map[string]string{
				"ray.io/serve-deployment": "my-app",
				"ray.io/cluster-name":     "my-svc-raycluster-abc",
			},
			platform: contract.PlatformRayServe,
			wtype:    contract.WorkloadTypeInference,
		},
		{
			name:     "deployment_vllm_label",
			kind:     "Deployment",
			labels:   map[string]string{"app": "vllm"},
			platform: contract.PlatformVLLM,
			wtype:    contract.WorkloadTypeInference,
		},
		{
			name:     "triton_platform_label",
			kind:     "Deployment",
			labels: map[string]string{
				contract.AnnPlatform: "triton",
				contract.LabelModel:  "resnet50",
			},
			platform: contract.PlatformTriton,
			wtype:    contract.WorkloadTypeInference,
		},
		{
			name:     "annotation_platform",
			kind:     "Deployment",
			anns:     map[string]string{contract.AnnPlatform: "bentoml"},
			platform: contract.PlatformBentoML,
			wtype:    contract.WorkloadTypeInference,
		},
		{
			name:     "unknown",
			kind:     "Deployment",
			platform: contract.PlatformUnknown,
			wtype:    contract.WorkloadTypeInference,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, w := DetectPlatform(tt.labels, tt.anns, tt.kind)
			require.Equal(t, tt.platform, p)
			require.Equal(t, tt.wtype, w)
		})
	}
}
