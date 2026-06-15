package kubefisher

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GlobalFlags are persistent CLI flags (kubeconfig, context, namespace, output).
type GlobalFlags struct {
	Kubeconfig    string
	Context       string
	Namespace     string // empty: use current context default namespace
	AllNamespaces bool
	Output        string
	LogFormat     string
}

// NewClientConfig returns kubectl-compatible deferred client config (path + context only).
func NewClientConfig(kubeconfigPath, contextName string) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
}

// ResolveNamespace returns the namespace for a namespaced operation, or NamespaceAll when -A.
func ResolveNamespace(g GlobalFlags, cc clientcmd.ClientConfig) (string, error) {
	if g.AllNamespaces {
		return metav1.NamespaceAll, nil
	}
	if g.Namespace != "" {
		return g.Namespace, nil
	}
	ns, _, err := cc.Namespace()
	if err != nil {
		return "", fmt.Errorf("resolve default namespace: %w", err)
	}
	if ns == "" {
		ns = "default"
	}
	return ns, nil
}

// NewDynamicClient builds a dynamic.Interface from client config.
// Used by the quota command for unstructured TeamInferenceQuota listing.
func NewDynamicClient(cc clientcmd.ClientConfig) (dynamic.Interface, error) {
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("rest config: %w", err)
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return dyn, nil
}

// BuildControllerRuntimeClient bridges kubectl-style kubeconfig flags
// (--kubeconfig, --context) to a controller-runtime client.Client.
// Satisfies the repo architecture rule: use controller-runtime client for
// API operations; use clientcmd only for kubeconfig resolution.
func BuildControllerRuntimeClient(cc clientcmd.ClientConfig) (client.Client, error) {
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("rest config: %w", err)
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	cl, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("controller-runtime client: %w", err)
	}
	return cl, nil
}

// NewDiscoveryClient builds a discovery.DiscoveryInterface for CRD probing.
// Discovery is the one place where a specialised client is needed alongside
// controller-runtime client (ServerPreferredResources is not on client.Client).
func NewDiscoveryClient(cc clientcmd.ClientConfig) (discovery.DiscoveryInterface, error) {
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("rest config: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	return disc, nil
}

// NewTypedClient builds a kubernetes.Interface for pod log streaming and other
// CoreV1 APIs not exposed on controller-runtime client.Client.
func NewTypedClient(cc clientcmd.ClientConfig) (kubernetes.Interface, error) {
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("rest config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("typed client: %w", err)
	}
	return cs, nil
}
