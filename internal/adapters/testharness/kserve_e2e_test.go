//go:build e2e

package testharness

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestKServeGPURealAnnotation deploys an InferenceService on a real GPU cluster and
// waits for the cost-patcher to write kubefisher.io/cost-per-hour (up to 60s).
func TestKServeGPURealAnnotation(t *testing.T) {
	if os.Getenv("KUBECONFIG") == "" && os.Getenv("FISHER_E2E_KUBECONFIG") == "" {
		t.Skip("set KUBECONFIG or FISHER_E2E_KUBECONFIG for GPU e2e")
	}

	kubeconfig := os.Getenv("FISHER_E2E_KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)

	cl, err := client.New(cfg, client.Options{})
	require.NoError(t, err)

	ctx := context.Background()
	ns := os.Getenv("FISHER_E2E_NAMESPACE")
	if ns == "" {
		ns = "default"
	}
	name := os.Getenv("FISHER_E2E_IS_NAME")
	if name == "" {
		name = "fisher-e2e-cost"
	}

	gvk := schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1beta1",
		Kind:    "InferenceService",
	}

	u := kserveInferenceService(name, ns)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(gvk)
	err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, existing)
	if apierrors.IsNotFound(err) {
		require.NoError(t, cl.Create(ctx, u))
		t.Cleanup(func() {
			_ = cl.Delete(ctx, u)
		})
	} else {
		require.NoError(t, err)
	}

	var got unstructured.Unstructured
	got.SetGroupVersionKind(gvk)
	key := client.ObjectKey{Namespace: ns, Name: name}

	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := cl.Get(ctx, key, &got); err != nil {
			return false, err
		}
		anns := got.GetAnnotations()
		if anns == nil {
			return false, nil
		}
		v := anns[contract.AnnCostPerHour]
		return v != "" && v != "0", nil
	})
	require.NoError(t, err, "expected %s annotation within 60s", contract.AnnCostPerHour)
}
