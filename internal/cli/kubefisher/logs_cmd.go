package kubefisher

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/m2khosravi/kubefisher/internal/workload"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func newLogsCommand(g *GlobalFlags) *cobra.Command {
	var (
		follow    bool
		container string
		podName   string
	)

	cmd := &cobra.Command{
		Use:   "logs NAME",
		Short: "Stream logs from pods backing a GPU workload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd.Context(), g, args[0], follow, container, podName)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", true, "Follow log output")
	cmd.Flags().StringVarP(&container, "container", "c", "", "Container name (default: first container)")
	cmd.Flags().StringVar(&podName, "pod", "", "Pod name when multiple pods match (default: first)")

	return cmd
}

func runLogs(ctx context.Context, g *GlobalFlags, name string, follow bool, container, podName string) error {
	cc := NewClientConfig(g.Kubeconfig, g.Context)
	ns, err := ResolveNamespace(*g, cc)
	if err != nil {
		return err
	}
	cl, err := BuildControllerRuntimeClient(cc)
	if err != nil {
		return err
	}
	dyn, err := NewDynamicClient(cc)
	if err != nil {
		return err
	}
	disc, err := NewDiscoveryClient(cc)
	if err != nil {
		return err
	}
	typed, err := NewTypedClient(cc)
	if err != nil {
		return err
	}

	st, err := workload.FindResource(ctx, name, ns, cl, dyn, disc)
	if err != nil {
		return err
	}

	pods, err := workload.FindPodsForResource(ctx, st, cl)
	if err != nil {
		return err
	}

	target := pods[0]
	if podName != "" {
		found := false
		for _, p := range pods {
			if p.Name == podName {
				target = p
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("pod %q not found for %s %q (candidates: %s)", podName, st.Kind, st.Name, joinPodNames(pods))
		}
	} else if len(pods) > 1 {
		fmt.Fprintf(os.Stderr, "multiple pods match; streaming %s (use --pod to choose): %s\n", target.Name, joinPodNames(pods))
	}

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := streamPodLogs(ctx, typed, ns, target.Name, container, follow); err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "Log stream ended.")
			return nil
		}
		return err
	}
	fmt.Fprintln(os.Stderr, "Log stream ended.")
	return nil
}

func streamPodLogs(ctx context.Context, cs kubernetes.Interface, namespace, podName, container string, follow bool) error {
	opts := &corev1.PodLogOptions{Follow: follow}
	if container != "" {
		opts.Container = container
	}
	req := cs.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("open log stream for pod %s/%s: %w", namespace, podName, err)
	}
	defer stream.Close()

	_, err = io.Copy(os.Stdout, stream)
	if err != nil && ctx.Err() != nil {
		return nil
	}
	return err
}

func joinPodNames(pods []corev1.Pod) string {
	names := make([]string, len(pods))
	for i, p := range pods {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}
