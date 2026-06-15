// Package testharness provides a shared envtest suite for platform.Adapter implementations.
package testharness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/m2khosravi/kubefisher/internal/costpatcher/platform"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Option configures AdapterTestSuite.
type Option func(*suiteConfig)

type suiteConfig struct {
	crdDirectoryPaths []string
	nonMatchPod       *corev1.Pod
	sampleCost        platform.CostResult
}

// WithCRDDirectoryPaths registers CRDs from the given directories (envtest loads all YAML files).
func WithCRDDirectoryPaths(paths ...string) Option {
	return func(c *suiteConfig) {
		c.crdDirectoryPaths = append(c.crdDirectoryPaths, paths...)
	}
}

// WithNonMatchPod sets the pod used for Detect(non-match); default is an empty Pod.
func WithNonMatchPod(pod *corev1.Pod) Option {
	return func(c *suiteConfig) {
		c.nonMatchPod = pod
	}
}

// WithSampleCost overrides the CostResult used in WriteCost; defaults to hour + token values.
func WithSampleCost(res platform.CostResult) Option {
	return func(c *suiteConfig) {
		c.sampleCost = res
	}
}

// AdapterTestSuite runs Detect, ResolveTarget (GetOwner), and WriteCost against a real apiserver.
func AdapterTestSuite(
	t *testing.T,
	adapter platform.Adapter,
	matchPod *corev1.Pod,
	fakeOwner client.Object,
	opts ...Option,
) {
	t.Helper()

	cfg := suiteConfig{
		nonMatchPod: &corev1.Pod{},
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.sampleCost.LastUpdatedAt.IsZero() {
		hour := 1.25
		token := 0.000002
		cfg.sampleCost = platform.CostResult{
			CostPerHour:   &hour,
			CostPerToken:  &token,
			LastUpdatedAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		}
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     cfg.crdDirectoryPaths,
		ErrorIfCRDPathMissing: len(cfg.crdDirectoryPaths) > 0,
	}
	if dir := firstEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	restCfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testEnv.Stop())
	})

	k8s, err := client.New(restCfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	ns := fakeOwner.GetNamespace()
	if ns == "" {
		ns = "default"
	}
	matchPod = matchPod.DeepCopy()
	matchPod.Namespace = ns
	if matchPod.Name == "" {
		matchPod.Name = "test-pod"
	}

	t.Run("Detect/match", func(t *testing.T) {
		require.True(t, adapter.Detect(matchPod), "adapter %q should detect match pod", adapter.Name())
	})

	t.Run("Detect/no_match", func(t *testing.T) {
		if adapter.Detect(cfg.nonMatchPod) {
			t.Skipf("adapter %q is catch-all; skipping non-match Detect test", adapter.Name())
		}
		require.False(t, adapter.Detect(cfg.nonMatchPod), "adapter %q should not detect non-match pod", adapter.Name())
	})

	t.Run("GetOwner", func(t *testing.T) {
		require.NoError(t, k8s.Create(ctx, fakeOwner))

		got, err := adapter.ResolveTarget(ctx, k8s, matchPod)
		require.NoError(t, err)
		require.NotNil(t, got)

		wantGVK := fakeOwner.GetObjectKind().GroupVersionKind()
		gotGVK := got.GetObjectKind().GroupVersionKind()
		require.Equal(t, wantGVK, gotGVK, "resolved GVK")
		require.Equal(t, fakeOwner.GetName(), got.GetName())
		require.Equal(t, ns, got.GetNamespace())
	})

	t.Run("WriteCost", func(t *testing.T) {
		require.NoError(t, ensureOwnerExists(ctx, k8s, fakeOwner))

		target, err := adapter.ResolveTarget(ctx, k8s, matchPod)
		require.NoError(t, err)

		require.NoError(t, adapter.WriteCost(ctx, k8s, target, cfg.sampleCost))

		refetched := target.DeepCopyObject().(client.Object)
		require.NoError(t, k8s.Get(ctx, client.ObjectKeyFromObject(target), refetched))

		anns := refetched.GetAnnotations()
		require.NotNil(t, anns)
		if cfg.sampleCost.CostPerHour != nil {
			require.Contains(t, anns, contract.AnnCostPerHour)
			require.NotEmpty(t, anns[contract.AnnCostPerHour])
		}
		if cfg.sampleCost.CostPerToken != nil {
			require.Contains(t, anns, contract.AnnCostPerToken)
			require.NotEmpty(t, anns[contract.AnnCostPerToken])
		}
		require.Contains(t, anns, contract.AnnLastUpdated)
		require.Equal(t, cfg.sampleCost.LastUpdatedAt.UTC().Format(time.RFC3339), anns[contract.AnnLastUpdated])
	})
}

func ensureOwnerExists(ctx context.Context, c client.Client, obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	existing := obj.DeepCopyObject().(client.Object)
	err := c.Get(ctx, key, existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	return c.Create(ctx, obj)
}

func firstEnvTestBinaryDir() string {
	if assets := os.Getenv("KUBEBUILDER_ASSETS"); assets != "" {
		if abs, err := filepath.Abs(assets); err == nil {
			return abs
		}
		return assets
	}
	for _, basePath := range []string{
		filepath.Join("bin", "k8s"),
		filepath.Join("..", "..", "..", "bin", "k8s"), // repo root from internal/adapters/testharness
	} {
		if dir := firstEnvTestVersionDir(basePath); dir != "" {
			return dir
		}
	}
	return ""
}

func firstEnvTestVersionDir(basePath string) string {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		absBase = basePath
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(absBase, entry.Name())
		}
	}
	return ""
}

// PodWithControllerOwner returns a pod with a controller ownerReference to owner.
func PodWithControllerOwner(owner metav1.Object, podName string) *corev1.Pod {
	var apiVersion, kind string
	switch owner.(type) {
	case *appsv1.Deployment:
		apiVersion, kind = "apps/v1", "Deployment"
	case *appsv1.StatefulSet:
		apiVersion, kind = "apps/v1", "StatefulSet"
	case *appsv1.ReplicaSet:
		apiVersion, kind = "apps/v1", "ReplicaSet"
	default:
		co, ok := owner.(client.Object)
		if !ok {
			apiVersion, kind = "v1", "Unknown"
		} else {
			gvk := co.GetObjectKind().GroupVersionKind()
			apiVersion, kind = gvk.GroupVersion().String(), gvk.Kind
		}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: owner.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: apiVersion,
					Kind:       kind,
					Name:       owner.GetName(),
					UID:        owner.GetUID(),
					Controller: ptr(true),
				},
			},
		},
	}
}

func ptr[T any](v T) *T { return &v }
