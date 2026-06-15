/*
Copyright 2026 KubeFisher.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"testing"
	"time"

	quotav1alpha1 "github.com/m2khosravi/kubefisher/operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPodRequestsGPU(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "cpu only",
			pod:  podWithResources(map[string]string{"cpu": "1"}, nil),
			want: false,
		},
		{
			name: "gpu in requests",
			pod:  podWithResources(map[string]string{gpuResourceName: "1"}, nil),
			want: true,
		},
		{
			name: "gpu in limits only",
			pod:  podWithResources(nil, map[string]string{gpuResourceName: "2"}),
			want: true,
		},
		{
			name: "zero gpu request",
			pod:  podWithResources(map[string]string{gpuResourceName: "0"}, nil),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, podRequestsGPU(tc.pod))
		})
	}
}

func TestPodGPUQuotaValidator_ValidateCreate(t *testing.T) {
	reset := metav1.NewTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	tests := []struct {
		name      string
		pod       *corev1.Pod
		quotas    []quotav1alpha1.TeamInferenceQuota
		wantDeny  bool
		wantSub   string
		wantEvent bool
	}{
		{
			name:     "cpu only pod allowed",
			pod:      gpuPod("cpu-pod", false),
			wantDeny: false,
		},
		{
			name:     "gpu pod no quota configured",
			pod:      gpuPod("gpu-pod", true),
			quotas:   nil,
			wantDeny: false,
		},
		{
			name: "gpu pod quota active",
			pod:  gpuPod("gpu-pod", true),
			quotas: []quotav1alpha1.TeamInferenceQuota{
				quota("llm", "team-quota", quotav1alpha1.QuotaPhaseActive, quotav1alpha1.EnforcementModeEnforce, reset),
			},
			wantDeny: false,
		},
		{
			name: "gpu pod quota warning",
			pod:  gpuPod("gpu-pod", true),
			quotas: []quotav1alpha1.TeamInferenceQuota{
				quota("llm", "team-quota", quotav1alpha1.QuotaPhaseWarning, quotav1alpha1.EnforcementModeEnforce, reset),
			},
			wantDeny: false,
		},
		{
			name: "gpu pod quota unknown allows",
			pod:  gpuPod("gpu-pod", true),
			quotas: []quotav1alpha1.TeamInferenceQuota{
				quota("llm", "team-quota", quotav1alpha1.QuotaPhaseUnknown, quotav1alpha1.EnforcementModeEnforce, reset),
			},
			wantDeny: false,
		},
		{
			name: "gpu pod exceeded audit allows with event",
			pod:  gpuPod("gpu-pod", true),
			quotas: []quotav1alpha1.TeamInferenceQuota{
				exceededQuota("llm", "team-quota", quotav1alpha1.EnforcementModeAudit, reset),
			},
			wantDeny:  false,
			wantEvent: true,
		},
		{
			name: "gpu pod exceeded enforce denied",
			pod:  gpuPod("gpu-pod", true),
			quotas: []quotav1alpha1.TeamInferenceQuota{
				exceededQuota("llm", "team-quota", quotav1alpha1.EnforcementModeEnforce, reset),
			},
			wantDeny: true,
			wantSub:  `namespace "llm" exceeded TeamInferenceQuota "team-quota"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(scheme))
			utilruntime.Must(quotav1alpha1.AddToScheme(scheme))

			var objs []client.Object
			for i := range tc.quotas {
				q := tc.quotas[i]
				objs = append(objs, &q)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			recorder := record.NewFakeRecorder(10)
			validator := &PodGPUQuotaValidator{
				Client:   fakeClient,
				Recorder: recorder,
			}

			_, err := validator.ValidateCreate(context.Background(), tc.pod)
			if tc.wantDeny {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantSub)
				return
			}
			require.NoError(t, err)

			if tc.wantEvent {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, reasonAuditGPUAllowed)
					assert.Contains(t, event, "[Audit]")
				case <-time.After(time.Second):
					t.Fatal("expected audit event to be recorded")
				}
			}
		})
	}
}

func gpuPod(name string, withGPU bool) *corev1.Pod {
	var p *corev1.Pod
	if withGPU {
		p = podWithResources(map[string]string{gpuResourceName: "1"}, nil)
	} else {
		p = podWithResources(map[string]string{"cpu": "1"}, nil)
	}
	p.Name = name
	return p
}

func podWithResources(requests, limits map[string]string) *corev1.Pod {
	req := corev1.ResourceList{}
	for k, v := range requests {
		req[corev1.ResourceName(k)] = resource.MustParse(v)
	}
	lim := corev1.ResourceList{}
	for k, v := range limits {
		lim[corev1.ResourceName(k)] = resource.MustParse(v)
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "llm",
			Name:      "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "main",
				Resources: corev1.ResourceRequirements{
					Requests: req,
					Limits:   lim,
				},
			}},
		},
	}
}

func quota(ns, name string, phase quotav1alpha1.QuotaPhase, mode quotav1alpha1.EnforcementMode, reset metav1.Time) quotav1alpha1.TeamInferenceQuota {
	return quotav1alpha1.TeamInferenceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: quotav1alpha1.TeamInferenceQuotaSpec{
			DailyTokenBudget:    1_000_000,
			MonthlyCostLimitUSD: "500.00",
			EnforcementMode:     mode,
		},
		Status: quotav1alpha1.TeamInferenceQuotaStatus{
			Phase:             phase,
			TokensUsedToday:   100,
			CostUsedThisMonth: "$10.00",
			NextResetTime:     &reset,
		},
	}
}

func exceededQuota(ns, name string, mode quotav1alpha1.EnforcementMode, reset metav1.Time) quotav1alpha1.TeamInferenceQuota {
	q := quota(ns, name, quotav1alpha1.QuotaPhaseExceeded, mode, reset)
	q.Status.TokensUsedToday = 2_000_000
	q.Status.CostUsedThisMonth = "$600.00"
	return q
}

func TestDenyMessageFormat(t *testing.T) {
	reset := metav1.NewTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	q := exceededQuota("llm", "team-quota", quotav1alpha1.EnforcementModeEnforce, reset)
	msg := denyMessage(&q)

	assert.Contains(t, msg, `namespace "llm"`)
	assert.Contains(t, msg, `TeamInferenceQuota "team-quota"`)
	assert.Contains(t, msg, "Tokens (rolling 24h): 2,000,000 of 1,000,000 daily budget")
	assert.Contains(t, msg, "Cost (month-to-date, UTC): $600.00 of $500.00 monthly limit")
	assert.Contains(t, msg, "2026-06-01 00:00")
	assert.Contains(t, msg, "2026-06-01T00:00:00Z")
	assert.Contains(t, msg, "kubectl edit teaminferencequota team-quota -n llm")
}

func TestFormatCount(t *testing.T) {
	assert.Equal(t, "0", formatCount(0))
	assert.Equal(t, "999", formatCount(999))
	assert.Equal(t, "1,000", formatCount(1000))
	assert.Equal(t, "2,000,000", formatCount(2_000_000))
}

func TestNormalizeUSD(t *testing.T) {
	assert.Equal(t, "$600.00", normalizeUSD("$600.00"))
	assert.Equal(t, "$500.00", normalizeUSD("500.00"))
}
