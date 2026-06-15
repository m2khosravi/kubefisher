package kubeclient

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func New() (client.Client, *rest.Config, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("kube config: %w", err)
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, fmt.Errorf("kube client: %w", err)
	}
	return cl, cfg, nil
}

// NewFromKubeconfig builds a client from an explicit kubeconfig path (tests / CI).
func NewFromKubeconfig(kubeconfigPath string) (client.Client, *rest.Config, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, nil, err
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, err
	}
	return cl, cfg, nil
}

// NewCached returns a controller-runtime client backed by a shared cache for reads
// and a direct client for writes. Callers must keep ctx alive for cache lifetime.
func NewCached(ctx context.Context) (client.Client, *rest.Config, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("kube config: %w", err)
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// Reader cache
	c, err := cache.New(cfg, cache.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, fmt.Errorf("kube cache: %w", err)
	}
	go func() {
		_ = c.Start(ctx)
	}()
	if !c.WaitForCacheSync(ctx) {
		return nil, nil, fmt.Errorf("kube cache: sync failed")
	}

	cl, err := client.New(cfg, client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader: c,
			// The pricing loader uses direct GET for a single ConfigMap; caching it would require
			// list/watch RBAC (and can attempt cluster-scope list depending on cache wiring).
			// Keep this least-privilege and avoid cache reflectors for ConfigMaps.
			DisableFor: []client.Object{&corev1.ConfigMap{}},
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("kube client: %w", err)
	}
	return cl, cfg, nil
}
