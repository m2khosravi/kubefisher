package install

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed assets/*.yaml
var assetFS embed.FS

// ApplyAssets decodes each embedded YAML file and applies it to the cluster
// via server-side apply (SSA). SSA is idempotent: re-running does not create
// duplicates or cause conflicts.
func ApplyAssets(ctx context.Context, cl client.Client, namespace string) error {
	entries, err := assetFS.ReadDir("assets")
	if err != nil {
		return fmt.Errorf("read embedded assets: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := assetFS.ReadFile("assets/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read asset %s: %w", entry.Name(), err)
		}
		if err := applyYAML(ctx, cl, namespace, data, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func applyYAML(ctx context.Context, cl client.Client, namespace string, data []byte, name string) error {
	obj := &unstructured.Unstructured{}
	if err := yaml.NewYAMLOrJSONDecoder(
		bytes.NewReader(data), 4096,
	).Decode(obj); err != nil {
		return fmt.Errorf("decode asset %s: %w", name, err)
	}

	if obj.GetNamespace() == "" {
		obj.SetNamespace(namespace)
	}

	slog.Info("applying asset", "file", name, "kind", obj.GetKind(), "name", obj.GetName(), "namespace", obj.GetNamespace())

	if err := cl.Patch(ctx, obj,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("kubefisher-cli"),
	); err != nil {
		return fmt.Errorf("SSA apply %s/%s (%s): %w", obj.GetNamespace(), obj.GetName(), obj.GetKind(), err)
	}
	return nil
}

