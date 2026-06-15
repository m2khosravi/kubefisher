package pricing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFromConfigMapData(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid_single_rule",
			yaml: `
version: 1
currency: USD
GPUs:
  - match:
      node_labels:
        accelerator: "nvidia-a10g"
    price_per_gpu_hour: 1.25
`,
			wantErr: false,
		},
		{
			name: "missing_currency",
			yaml: `
version: 1
GPUs:
  - match:
      node_labels:
        accelerator: "nvidia-a10g"
    price_per_gpu_hour: 1.25
`,
			wantErr: true,
		},
		{
			name: "empty_gpus",
			yaml: `
version: 1
currency: USD
GPUs: []
`,
			wantErr: true,
		},
		{
			name: "bad_price",
			yaml: `
version: 1
currency: USD
GPUs:
  - match:
      node_labels:
        accelerator: "nvidia-a10g"
    price_per_gpu_hour: 0
`,
			wantErr: true,
		},
		{
			name: "bad_version",
			yaml: `
version: 0
currency: USD
GPUs:
  - match:
      node_labels:
        accelerator: "nvidia-a10g"
    price_per_gpu_hour: 1.25
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFromConfigMapData(tt.yaml)
			require.Equal(t, tt.wantErr, err != nil)
		})
	}
}
