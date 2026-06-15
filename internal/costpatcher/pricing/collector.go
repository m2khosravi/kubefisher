package pricing

import (
	"maps"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

const metricGPUPricePerHour = "kubefisher_gpu_price_per_hour"
const metricGPUPricePerHourByNode = "kubefisher_gpu_price_per_hour_by_node"

// GaugeCollector exposes gpu-pricing.yaml as Prometheus gauges for PromQL joins.
// All series share the same label names (union of all rule keys, plus currency).
type GaugeCollector struct {
	mu        sync.Mutex
	doc       *Document
	unionK8s  []string // sorted Kubernetes node label keys appearing in any rule
	nodePrice map[string]float64
}

func NewGaugeCollector() *GaugeCollector {
	return &GaugeCollector{}
}

func (c *GaugeCollector) SetDocument(doc *Document) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doc = doc
	c.unionK8s = mergeSortedUnique(c.unionK8s, unionKeysFromDoc(doc))
}

// SetNodePrices sets computed per-node prices (nodeName -> price_per_gpu_hour).
func (c *GaugeCollector) SetNodePrices(nodePrice map[string]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodePrice = maps.Clone(nodePrice)
}

func (c *GaugeCollector) metricLabelNames() []string {
	out := make([]string, 0, 1+len(c.unionK8s))
	out = append(out, "currency")
	for _, k := range c.unionK8s {
		out = append(out, KSMLabelName(k))
	}
	return out
}

func (c *GaugeCollector) Describe(ch chan<- *prometheus.Desc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ln := c.metricLabelNames()
	if len(ln) == 0 {
		ln = []string{"currency"}
	}
	ch <- prometheus.NewDesc(
		metricGPUPricePerHour,
		"KubeFisher GPU list price per GPU-hour from gpu-pricing ConfigMap (join keys align with kube_node_labels label_* names).",
		ln, nil,
	)

	ch <- prometheus.NewDesc(
		metricGPUPricePerHourByNode,
		"KubeFisher GPU list price per GPU-hour by node name (computed from gpu-pricing + node labels).",
		[]string{"currency", "node"}, nil,
	)
}

func (c *GaugeCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.doc == nil {
		return
	}

	// Rule-keyed series (for optional joins with kube_node_labels when available).
	if len(c.unionK8s) > 0 {
		ln := c.metricLabelNames()
		desc := prometheus.NewDesc(
			metricGPUPricePerHour,
			"KubeFisher GPU list price per GPU-hour from gpu-pricing ConfigMap (join keys align with kube_node_labels label_* names).",
			ln, nil,
		)
		for _, g := range c.doc.GPUs {
			vals := make([]string, len(ln))
			vals[0] = c.doc.Currency
			for i, k8sKey := range c.unionK8s {
				vals[i+1] = g.Match.NodeLabels[k8sKey]
			}
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, g.PricePerGPUHour, vals...)
		}
	}

	// Node-keyed series (primary join for k3d/dev where kube_node_labels may be disabled).
	if len(c.nodePrice) > 0 {
		desc := prometheus.NewDesc(
			metricGPUPricePerHourByNode,
			"KubeFisher GPU list price per GPU-hour by node name (computed from gpu-pricing + node labels).",
			[]string{"currency", "node"}, nil,
		)
		for node, price := range c.nodePrice {
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, price, c.doc.Currency, node)
		}
	}
}

// KSMLabelName exports kube-state-metrics style label_* for a Kubernetes node label key.
func KSMLabelName(k8sLabelKey string) string {
	s := strings.ReplaceAll(k8sLabelKey, "/", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return "label_" + s
}

func unionKeysFromDoc(doc *Document) []string {
	if doc == nil {
		return nil
	}
	set := map[string]struct{}{}
	for _, g := range doc.GPUs {
		for k := range g.Match.NodeLabels {
			set[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func mergeSortedUnique(existing []string, incoming []string) []string {
	m := map[string]struct{}{}
	for _, x := range existing {
		m[x] = struct{}{}
	}
	for _, x := range incoming {
		m[x] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for x := range m {
		out = append(out, x)
	}
	sortStrings(out)
	return out
}

func sortStrings(a []string) {
	for i := 0; i < len(a); i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}
