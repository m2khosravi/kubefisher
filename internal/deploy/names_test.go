package deploy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelSlug(t *testing.T) {
	require.Equal(t, "meta-llama-llama-3-1-8b", ModelSlug("meta-llama/Llama-3.1-8B"))
	require.Equal(t, "model", ModelSlug(""))
}
