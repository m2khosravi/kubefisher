package kubefisher

import (
	"context"
	"fmt"
	"strings"

	"github.com/m2khosravi/kubefisher/internal/version"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newVersionCommand(g *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show binary version and operator version in cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("kubefisher: %s\n", version.Version)

			ns := g.Namespace
			if ns == "" {
				ns = "kubefisher-system"
			}

			cc := NewClientConfig(g.Kubeconfig, g.Context)
			cl, err := BuildControllerRuntimeClient(cc)
			if err != nil {
				// Not fatal — binary version was already printed.
				fmt.Printf("operator:    (could not connect to cluster: %v)\n", err)
				return nil
			}

			tag, err := operatorImageTag(cmd.Context(), cl, ns)
			if err != nil {
				fmt.Printf("operator:    (not installed in namespace %q)\n", ns)
				return nil
			}
			fmt.Printf("operator:    %s\n", tag)
			return nil
		},
	}
}

// operatorImageTag finds the operator Deployment in ns and returns the image
// tag portion (the part after ':') of its first container image.
func operatorImageTag(ctx context.Context, cl client.Client, ns string) (string, error) {
	var list appsv1.DeploymentList
	if err := cl.List(ctx, &list,
		client.InNamespace(ns),
		client.MatchingLabels{"app.kubernetes.io/component": "operator"},
	); err != nil {
		return "", fmt.Errorf("list operator deployments: %w", err)
	}
	for _, d := range list.Items {
		for _, c := range d.Spec.Template.Spec.Containers {
			if tag := imageTag(c.Image); tag != "" {
				return tag, nil
			}
		}
	}
	return "", fmt.Errorf("operator deployment not found")
}

// imageTag extracts the tag after ':' from a container image reference.
// Returns the full image string when no ':' is present.
func imageTag(image string) string {
	// Strip digest if present (image@sha256:...).
	if idx := strings.LastIndex(image, "@"); idx >= 0 {
		image = image[:idx]
	}
	if idx := strings.LastIndex(image, ":"); idx >= 0 {
		return image[idx+1:]
	}
	return image
}
