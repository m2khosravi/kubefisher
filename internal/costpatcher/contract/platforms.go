package contract

// Platform display names (CLI PLATFORM column and cost-patcher adapter names).
const (
	PlatformKServe           = "kserve"
	PlatformBentoML          = "bentoml"
	PlatformKubeflowTraining = "kubeflow-training"
	PlatformKubeflowTrainer  = "kubeflow-trainer"
	PlatformRay              = "ray"
	PlatformRayServe         = "ray-serve"
	PlatformVLLM             = "vllm"
	PlatformTriton           = "triton"
	PlatformUnknown          = "unknown"
)

// Workload types (CLI TYPE column).
const (
	WorkloadTypeInference = "inference"
	WorkloadTypeTraining  = "training"
)

// AnnPlatform is written/read on workloads and pod templates for platform attribution.
const AnnPlatform = "kubefisher.io/platform"

// Contract labels applied by deploy and expected by cost-patcher / PromQL joins.
const (
	LabelModel        = "kubefisher.io/model"
	LabelTeam         = "kubefisher.io/team"
	LabelWorkloadType = "kubefisher.io/workload-type"
)
