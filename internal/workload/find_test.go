package workload

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeploymentPhase(t *testing.T) {
	require.Equal(t, "Available", deploymentPhase(2, 2))
	require.Equal(t, "Progressing", deploymentPhase(1, 2))
	require.Equal(t, "Unavailable", deploymentPhase(0, 2))
}

func TestFormatCostPerHour(t *testing.T) {
	require.Equal(t, "—", formatCostPerHour(""))
	require.Equal(t, "$1.25/hr", formatCostPerHour("1.25"))
}
