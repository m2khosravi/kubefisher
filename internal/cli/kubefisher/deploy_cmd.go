package kubefisher

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/m2khosravi/kubefisher/internal/deploy"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func newDeployCommand(g *GlobalFlags) *cobra.Command {
	var (
		model   string
		gpu     string
		team    string
		replicas int32
		image   string
		dryRun  bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a GPU model to your cluster",
		Long: `Deploy a model using the serving platform detected in the cluster.

When KServe (InferenceService CRD) is installed, creates an InferenceService.
Otherwise creates a plain vLLM Deployment + Service with contract labels.

Polls every 3s until ready, then prints endpoint URL and cost/hr annotation (— if cost-patcher has not run yet).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeploy(cmd.Context(), g, deploy.DeployOptions{
				Model:    model,
				GPU:      gpu,
				Team:     team,
				Replicas: replicas,
				Image:    image,
				DryRun:   dryRun,
			}, timeout)
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "HuggingFace model ID (required)")
	cmd.Flags().StringVar(&gpu, "gpu", deploy.DefaultGPU, "GPU type for node selector (accelerator label)")
	cmd.Flags().StringVar(&team, "team", deploy.DefaultTeam, "Team label (kubefisher.io/team)")
	cmd.Flags().Int32Var(&replicas, "replicas", 1, "Number of replicas")
	cmd.Flags().StringVar(&image, "image", "", "Container image override (generic strategy only)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print YAML to stdout without creating resources")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "How long to wait for the workload to become ready (0 = no timeout)")
	_ = cmd.MarkFlagRequired("model")

	return cmd
}

func runDeploy(ctx context.Context, g *GlobalFlags, opts deploy.DeployOptions, timeout time.Duration) error {
	cc := NewClientConfig(g.Kubeconfig, g.Context)
	ns, err := ResolveNamespace(*g, cc)
	if err != nil {
		return err
	}
	opts.Namespace = ns

	cl, err := BuildControllerRuntimeClient(cc)
	if err != nil {
		return err
	}
	disc, err := NewDiscoveryClient(cc)
	if err != nil {
		return err
	}
	dyn, err := NewDynamicClient(cc)
	if err != nil {
		return err
	}

	strategy := deploy.DetectServingPlatform(disc)
	fmt.Fprintf(os.Stderr, "deploy: using strategy %q\n", strategy.Name())

	objects, err := strategy.Build(opts)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return printDryRunYAML(objects)
	}

	for _, obj := range objects {
		if err := createObject(ctx, cl, dyn, strategy, obj); err != nil {
			return err
		}
	}

	name := strategy.PrimaryName(opts)
	endpoint, err := deploy.PollUntilReady(ctx, strategy, cl, dyn, opts.Namespace, name, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nDiagnose with:\n  kubectl get pods -n %s\n  kubectl describe pod -n %s -l app.kubernetes.io/name=%s\n  kubectl get nodes --show-labels\n", opts.Namespace, opts.Namespace, name)
		return err
	}

	cost := strategy.ReadCostPerHour(ctx, cl, dyn, opts.Namespace, name)
	deploy.PrintDeploySuccess(endpoint, cost)
	return nil
}

func createObject(ctx context.Context, cl client.Client, dyn dynamic.Interface, strategy deploy.DeployStrategy, obj client.Object) error {
	if deploy.IsKServeObject(obj) {
		u := obj.(*unstructured.Unstructured)
		if ks, ok := strategy.(deploy.KServeStrategy); ok {
			return deploy.CreateKServe(ctx, dyn, ks.KServeGVR(), u)
		}
	}
	cobj := obj
	if err := cl.Create(ctx, cobj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("%s %s/%s already exists", cobj.GetObjectKind().GroupVersionKind().Kind, cobj.GetNamespace(), cobj.GetName())
		}
		return fmt.Errorf("create %s %s/%s: %w", cobj.GetObjectKind().GroupVersionKind().Kind, cobj.GetNamespace(), cobj.GetName(), err)
	}
	return nil
}

func printDryRunYAML(objects []client.Object) error {
	for i, obj := range objects {
		if i > 0 {
			fmt.Println("---")
		}
		u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return fmt.Errorf("marshal object: %w", err)
		}
		if gvk := obj.GetObjectKind().GroupVersionKind(); gvk.Kind != "" {
			u["apiVersion"] = gvk.GroupVersion().String()
			u["kind"] = gvk.Kind
		}
		b, err := yaml.Marshal(u)
		if err != nil {
			return fmt.Errorf("yaml encode: %w", err)
		}
		fmt.Print(string(b))
	}
	return nil
}
