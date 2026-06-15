package deploy

import (
	"testing"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/stretchr/testify/require"
)

func TestGenericStrategyBuildLabels(t *testing.T) {
	g := GenericStrategy{}
	objs, err := g.Build(DeployOptions{
		Model:     "meta-llama/Llama-3.1-8B",
		Namespace: "default",
		Team:      "team-a",
		GPU:       "a10g",
		Replicas:  1,
	})
	require.NoError(t, err)
	require.Len(t, objs, 2)
	dep := objs[0]
	labels := dep.GetLabels()
	require.Equal(t, contract.PlatformVLLM, labels[contract.AnnPlatform])
	require.Equal(t, "meta-llama/Llama-3.1-8B", labels[contract.LabelModel])
	require.Equal(t, "team-a", labels[contract.LabelTeam])
	require.Equal(t, contract.WorkloadTypeInference, labels[contract.LabelWorkloadType])
}
