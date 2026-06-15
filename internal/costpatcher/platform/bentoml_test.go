package platform

import (
	"testing"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBentoMLDetect(t *testing.T) {
	a := BentoML{}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{name: "nil_pod", pod: nil, want: false},
		{name: "nil_labels", pod: &corev1.Pod{}, want: false},
		{
			name: "bento_deployment_label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{labelBentoDeployment: "llama3"},
				},
			},
			want: true,
		},
		{
			name: "platform_label_bentoml",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{contract.AnnPlatform: contract.PlatformBentoML},
				},
			},
			want: true,
		},
		{
			name: "other_platform",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{contract.AnnPlatform: "vllm"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, a.Detect(tt.pod))
		})
	}
}
