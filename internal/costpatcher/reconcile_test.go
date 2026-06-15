package costpatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/m2khosravi/kubefisher/internal/costpatcher/platform"
	"github.com/m2khosravi/kubefisher/pkg/promclient"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestIsPodLoading(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "running_not_loading",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			},
			want: false,
		},
		{
			name: "container_creating",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "image_pull_backoff",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "pending_no_waiting_reason",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodPending},
			},
			want: true,
		},
		{
			name: "failed_not_loading",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodFailed},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isPodLoading(tt.pod))
		})
	}
}

func TestReconcilerUsesGPUCountFromPod(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "model",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("4"),
						},
					},
				},
			},
		},
	}
	require.Equal(t, int64(4), platform.GPUCountFromPod(pod))
}

// ── helpers shared by staleness tests ────────────────────────────────────────

func reconcilerTestScheme() *runtime.Scheme {
	sc := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sc))
	return sc
}

// reconcilerTestFixtures returns a Deployment, ReplicaSet and running GPU Pod wired with
// owner references so that Generic.ResolveTarget walks Pod → ReplicaSet → Deployment.
func reconcilerTestFixtures(ns string) (*appsv1.Deployment, *appsv1.ReplicaSet, *corev1.Pod) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "model-server", Namespace: ns, UID: "dep-uid"},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "model-server-rs", Namespace: ns, UID: "rs-uid",
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "apps/v1", Kind: "Deployment", Name: dep.Name, UID: dep.UID, Controller: ptrBool(true)},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "model-server-pod", Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: ptrBool(true)},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "model",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	return dep, rs, pod
}

// promTestServer returns an httptest.Server and a *phase pointer. Set *phase=0 to return
// costPerHour and tokenPerHour values; set *phase=1 to return empty for cost_per_hour;
// set *phase=2 to return cost_per_hour but empty for cost_per_token.
func promTestServer(t *testing.T, costPerHour, costPerToken float64) (*httptest.Server, *int) {
	t.Helper()
	phase := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if q == "" {
			_ = r.ParseForm()
			q = r.FormValue("query")
		}
		switch {
		case strings.Contains(q, "cost_per_hour"):
			if phase == 1 {
				writeTestPromEmpty(w)
			} else {
				writeTestPromVector(w, costPerHour)
			}
		case strings.Contains(q, "cost_per_token"):
			if phase == 0 {
				writeTestPromVector(w, costPerToken)
			} else {
				writeTestPromEmpty(w)
			}
		default:
			writeTestPromEmpty(w)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &phase
}

func writeTestPromVector(w http.ResponseWriter, v float64) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "vector",
			"result": []map[string]any{{
				"metric": map[string]string{},
				"value":  []any{float64(1), fmt.Sprintf("%g", v)},
			}},
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func writeTestPromEmpty(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"status": "success",
		"data":   map[string]any{"resultType": "vector", "result": []map[string]any{}},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func ptrBool(b bool) *bool { return &b }

// ── staleness tests ───────────────────────────────────────────────────────────

// TestReconcilerDeletesCostHourAnnotationWhenMetricDisappears verifies that after a
// kubefisher:cost_per_hour series disappears from Prometheus, the next reconcile tick
// removes AnnCostPerHour (and AnnCostPerToken, AnnGPUCount, AnnLastUpdated) from the
// owner Deployment rather than leaving stale values.
func TestReconcilerDeletesCostHourAnnotationWhenMetricDisappears(t *testing.T) {
	const ns = "test-ns"
	dep, rs, pod := reconcilerTestFixtures(ns)
	k8s := fake.NewClientBuilder().WithScheme(reconcilerTestScheme()).WithObjects(dep, rs, pod).Build()

	srv, phase := promTestServer(t, 1.25, 0.000002)
	prom, err := promclient.NewClient(srv.URL)
	require.NoError(t, err)

	rec := &Reconciler{
		K8s:      k8s,
		Prom:     prom,
		Adapters: []platform.Adapter{platform.Generic{}},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := context.Background()

	// ── Tick 1: Prometheus returns cost_per_hour=1.25 ────────────────────────
	require.NoError(t, rec.tick(ctx))

	var got appsv1.Deployment
	require.NoError(t, k8s.Get(ctx, types.NamespacedName{Namespace: ns, Name: dep.Name}, &got))
	require.Contains(t, got.Annotations, contract.AnnCostPerHour, "cost_per_hour annotation should be written after first tick")
	require.Equal(t, "1.25", got.Annotations[contract.AnnCostPerHour])

	// ── Tick 2: Prometheus returns empty for cost_per_hour ───────────────────
	*phase = 1
	require.NoError(t, rec.tick(ctx))

	var got2 appsv1.Deployment
	require.NoError(t, k8s.Get(ctx, types.NamespacedName{Namespace: ns, Name: dep.Name}, &got2))
	require.NotContains(t, got2.Annotations, contract.AnnCostPerHour, "cost_per_hour annotation must be removed when metric is absent")
	require.NotContains(t, got2.Annotations, contract.AnnCostPerToken, "cost_per_token annotation must also be removed when cost_per_hour is absent")
	require.NotContains(t, got2.Annotations, contract.AnnGPUCount, "gpu_count annotation must also be removed when cost_per_hour is absent")
	require.NotContains(t, got2.Annotations, contract.AnnLastUpdated, "last_updated annotation must also be removed when cost_per_hour is absent")

	// ── Tick 3: Prometheus returns cost_per_hour again ───────────────────────
	*phase = 0
	require.NoError(t, rec.tick(ctx))

	var got3 appsv1.Deployment
	require.NoError(t, k8s.Get(ctx, types.NamespacedName{Namespace: ns, Name: dep.Name}, &got3))
	require.Contains(t, got3.Annotations, contract.AnnCostPerHour, "cost_per_hour annotation should be re-written when metric reappears")
}

// TestReconcilerDeletesCostTokenAnnotationWhenMetricDisappears verifies that when
// kubefisher:cost_per_token disappears from Prometheus (e.g. token rate drops to 0) while
// kubefisher:cost_per_hour is still present, only AnnCostPerToken is removed; AnnCostPerHour
// and AnnGPUCount remain untouched.
func TestReconcilerDeletesCostTokenAnnotationWhenMetricDisappears(t *testing.T) {
	const ns = "test-ns2"
	dep, rs, pod := reconcilerTestFixtures(ns)
	k8s := fake.NewClientBuilder().WithScheme(reconcilerTestScheme()).WithObjects(dep, rs, pod).Build()

	srv, phase := promTestServer(t, 1.25, 0.000002)
	prom, err := promclient.NewClient(srv.URL)
	require.NoError(t, err)

	rec := &Reconciler{
		K8s:      k8s,
		Prom:     prom,
		Adapters: []platform.Adapter{platform.Generic{}},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := context.Background()

	// ── Tick 1: both cost_per_hour and cost_per_token present ────────────────
	require.NoError(t, rec.tick(ctx))

	var got appsv1.Deployment
	require.NoError(t, k8s.Get(ctx, types.NamespacedName{Namespace: ns, Name: dep.Name}, &got))
	require.Contains(t, got.Annotations, contract.AnnCostPerHour)
	require.Contains(t, got.Annotations, contract.AnnCostPerToken, "cost_per_token annotation should be written when metric is present")

	// ── Tick 2: cost_per_hour still present, cost_per_token disappears ───────
	*phase = 2 // promTestServer: phase>=1 means empty for cost_per_token, but cost_per_hour still returns value
	require.NoError(t, rec.tick(ctx))

	var got2 appsv1.Deployment
	require.NoError(t, k8s.Get(ctx, types.NamespacedName{Namespace: ns, Name: dep.Name}, &got2))
	require.Contains(t, got2.Annotations, contract.AnnCostPerHour, "cost_per_hour must still be present")
	require.Contains(t, got2.Annotations, contract.AnnGPUCount, "gpu_count must still be present")
	require.NotContains(t, got2.Annotations, contract.AnnCostPerToken, "cost_per_token annotation must be removed when token metric disappears")
}
