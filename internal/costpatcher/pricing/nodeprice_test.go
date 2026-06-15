package pricing

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

func TestPriceForNode(t *testing.T) {
	doc := &Document{
		Version:  1,
		Currency: "USD",
		GPUs: []GPUPriceRule{
			{
				Match:           Match{NodeLabels: map[string]string{"accelerator": "nvidia-a10g"}},
				PricePerGPUHour: 1.25,
			},
			{
				Match:           Match{NodeLabels: map[string]string{"accelerator": "nvidia-a100"}},
				PricePerGPUHour: 3.40,
			},
		},
	}

	tests := []struct {
		name      string
		labels    map[string]string
		wantOK    bool
		wantPrice float64
	}{
		{name: "match_a10g", labels: map[string]string{"accelerator": "nvidia-a10g"}, wantOK: true, wantPrice: 1.25},
		{name: "match_a100", labels: map[string]string{"accelerator": "nvidia-a100"}, wantOK: true, wantPrice: 3.40},
		{name: "no_match", labels: map[string]string{"accelerator": "nvidia-h100"}, wantOK: false, wantPrice: 0},
		{name: "missing_label", labels: map[string]string{}, wantOK: false, wantPrice: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: tt.labels}}
			price, ok := PriceForNode(doc, n)
			require.Equal(t, tt.wantOK, ok)
			require.InDelta(t, tt.wantPrice, price, 0.0000001)
		})
	}
}
