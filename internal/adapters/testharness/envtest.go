package testharness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// envtestContext holds a started envtest apiserver client.
type envtestContext struct {
	T      *testing.T
	Ctx    context.Context
	Client client.Client
	Stop   func()
}

// runEnvtest starts envtest with optional CRD directories and runs fn.
func runEnvtest(t *testing.T, crdDirs []string, fn func(etc *envtestContext)) {
	t.Helper()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     crdDirs,
		ErrorIfCRDPathMissing: len(crdDirs) > 0,
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

	fn(&envtestContext{T: t, Ctx: ctx, Client: k8s, Stop: cancel})
}
