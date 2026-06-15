package cost

import (
	"strings"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

// DetectPlatform returns platform and workload type from object metadata and kind.
// labels are pod-template labels when available; annotations are workload annotations.
func DetectPlatform(labels, annotations map[string]string, kind string) (platform, workloadType string) {
	kind = strings.TrimSpace(kind)

	switch kind {
	case "InferenceService":
		return contract.PlatformKServe, contract.WorkloadTypeInference
	case "BentoDeployment":
		return contract.PlatformBentoML, contract.WorkloadTypeInference
	case "TrainJob":
		return contract.PlatformKubeflowTrainer, contract.WorkloadTypeTraining
	case "PyTorchJob", "TFJob", "XGBoostJob", "MPIJob", "PaddleJob":
		return contract.PlatformKubeflowTraining, contract.WorkloadTypeTraining
	case "RayService":
		return contract.PlatformRayServe, contract.WorkloadTypeInference
	case "RayJob", "RayCluster":
		return contract.PlatformRay, contract.WorkloadTypeTraining
	}

	if labels != nil {
		if labels["serving.kserve.io/inferenceservice"] != "" {
			return contract.PlatformKServe, contract.WorkloadTypeInference
		}
		if labels["trainer.kubeflow.org/trainjob-ancestor-step"] != "" ||
			labels["trainer.kubeflow.org/trainjob-name"] != "" {
			return contract.PlatformKubeflowTrainer, contract.WorkloadTypeTraining
		}
		if labels["training.kubeflow.org/job-name"] != "" ||
			labels["training.kubeflow.org/replica-type"] != "" {
			return contract.PlatformKubeflowTraining, contract.WorkloadTypeTraining
		}
		if labels["ray.io/serve-deployment"] != "" {
			return contract.PlatformRayServe, contract.WorkloadTypeInference
		}
		if labels["ray.io/cluster-name"] != "" || labels["ray.io/node-type"] != "" {
			return contract.PlatformRay, contract.WorkloadTypeTraining
		}
		if labels["yatai.bentoml.com/bento-deployment"] != "" {
			return contract.PlatformBentoML, contract.WorkloadTypeInference
		}
		if labels["app.kubernetes.io/managed-by"] == "bentoml-yatai" {
			return contract.PlatformBentoML, contract.WorkloadTypeInference
		}
		if labels["app"] == "vllm" || labels["app.kubernetes.io/name"] == "vllm" {
			return contract.PlatformVLLM, contract.WorkloadTypeInference
		}
	}

	if annotations != nil {
		if p := strings.TrimSpace(annotations[contract.AnnPlatform]); p != "" {
			return normalizePlatform(p), workloadTypeForPlatform(p)
		}
	}
	if labels != nil {
		if p := strings.TrimSpace(labels[contract.AnnPlatform]); p != "" {
			return normalizePlatform(p), workloadTypeForPlatform(p)
		}
	}

	return contract.PlatformUnknown, contract.WorkloadTypeInference
}

func normalizePlatform(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case contract.PlatformKServe:
		return contract.PlatformKServe
	case contract.PlatformBentoML:
		return contract.PlatformBentoML
	case contract.PlatformKubeflowTrainer:
		return contract.PlatformKubeflowTrainer
	case contract.PlatformKubeflowTraining, "kubeflow":
		return contract.PlatformKubeflowTraining
	case contract.PlatformRayServe:
		return contract.PlatformRayServe
	case contract.PlatformRay:
		return contract.PlatformRay
	case contract.PlatformVLLM:
		return contract.PlatformVLLM
	case contract.PlatformTriton:
		return contract.PlatformTriton
	default:
		return contract.PlatformUnknown
	}
}

func workloadTypeForPlatform(p string) string {
	switch normalizePlatform(p) {
	case contract.PlatformKubeflowTrainer, contract.PlatformKubeflowTraining, contract.PlatformRay:
		return contract.WorkloadTypeTraining
	default:
		return contract.WorkloadTypeInference
	}
}
