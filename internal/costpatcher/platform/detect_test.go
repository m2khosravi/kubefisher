package platform

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

func TestGenericDetect(t *testing.T) {
	a := Generic{}
	require.True(t, a.Detect(&corev1.Pod{}))
}

func TestKServeDetect(t *testing.T) {
	a := KServe{}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{name: "nil_pod", pod: nil, want: false},
		{name: "nil_labels", pod: &corev1.Pod{}, want: false},
		{
			name: "platform_label_kserve",
			pod:  &corev1.Pod{ObjectMeta: metav1.ObjectMeta{}},
			want: true,
		},
		{
			name: "kserve_inferenceservice_label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			want: true,
		},
		{
			name: "other_platform",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			want: false,
		},
	}

	// Fill in labels per case (keeps structs short).
	tests[2].pod.Labels = map[string]string{"kubefisher.io/platform": "kserve"}
	tests[3].pod.Labels = map[string]string{"serving.kserve.io/inferenceservice": "llama3"}
	tests[4].pod.Labels = map[string]string{"kubefisher.io/platform": "vllm"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, a.Detect(tt.pod))
		})
	}
}
