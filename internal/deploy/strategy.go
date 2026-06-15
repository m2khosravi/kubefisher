package deploy

import (
	"context"

	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultVLLMImage = "vllm/vllm-openai:latest"
	DefaultGPU       = "a10g"
	DefaultTeam      = "default"
)

// DeployOptions configures a model deployment.
type DeployOptions struct {
	Model     string
	GPU       string
	Namespace string
	Team      string
	Replicas  int32
	Image     string
	DryRun    bool
}

// DeployStrategy builds cluster objects and waits until the workload is ready.
type DeployStrategy interface {
	Name() string
	// PrimaryName is the main resource name used for wait and cost lookup.
	PrimaryName(opts DeployOptions) string
	Build(opts DeployOptions) ([]client.Object, error)
	WaitReady(ctx context.Context, cl client.Client, dyn dynamic.Interface, namespace, name string) (endpoint string, err error)
	ReadCostPerHour(ctx context.Context, cl client.Client, dyn dynamic.Interface, namespace, name string) string
}
