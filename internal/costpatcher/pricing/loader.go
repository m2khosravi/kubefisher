package pricing

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DefaultPricingKey = "pricing.yaml"

// Document matches docs/contract.md gpu-pricing.yaml schema.
type Document struct {
	Version  int            `yaml:"version"`
	Currency string         `yaml:"currency"`
	GPUs     []GPUPriceRule `yaml:"GPUs"`
}

type GPUPriceRule struct {
	Description     string  `yaml:"description,omitempty"`
	Match           Match   `yaml:"match"`
	PricePerGPUHour float64 `yaml:"price_per_gpu_hour"`
	EffectiveFrom   string  `yaml:"effective_from,omitempty"`
}

type Match struct {
	NodeLabels map[string]string `yaml:"node_labels"`
}

// LoadFromConfigMapData parses pricing YAML bytes.
func LoadFromConfigMapData(data string) (*Document, error) {
	var doc Document
	if err := yaml.Unmarshal([]byte(data), &doc); err != nil {
		return nil, fmt.Errorf("parse pricing yaml: %w", err)
	}
	if doc.Version < 1 {
		return nil, fmt.Errorf("pricing: invalid version %d", doc.Version)
	}
	if strings.TrimSpace(doc.Currency) == "" {
		return nil, fmt.Errorf("pricing: currency required")
	}
	if len(doc.GPUs) == 0 {
		return nil, fmt.Errorf("pricing: GPUs list empty")
	}
	for i, g := range doc.GPUs {
		if len(g.Match.NodeLabels) == 0 {
			return nil, fmt.Errorf("pricing: GPUs[%d] match.node_labels required", i)
		}
		if g.PricePerGPUHour <= 0 {
			return nil, fmt.Errorf("pricing: GPUs[%d] price_per_gpu_hour must be > 0", i)
		}
	}
	return &doc, nil
}

// FetchConfigMap loads the pricing.yaml (or key) from a ConfigMap.
func FetchConfigMap(ctx context.Context, c client.Client, namespace, name, dataKey string) (*Document, error) {
	if dataKey == "" {
		dataKey = DefaultPricingKey
	}
	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cm); err != nil {
		return nil, err
	}
	raw, ok := cm.Data[dataKey]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("configmap %s/%s missing data key %q", namespace, name, dataKey)
	}
	return LoadFromConfigMapData(raw)
}
