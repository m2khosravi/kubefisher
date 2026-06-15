package pricing

import (
	corev1 "k8s.io/api/core/v1"
)

// PriceForNode returns the first matching GPU rule price for a node.
// Match semantics: all labels in rule.match.node_labels must be present on the node with the same value.
func PriceForNode(doc *Document, node *corev1.Node) (pricePerGPUHour float64, ok bool) {
	if doc == nil || node == nil {
		return 0, false
	}
	labels := node.GetLabels()
	for _, rule := range doc.GPUs {
		if matchAll(labels, rule.Match.NodeLabels) {
			return rule.PricePerGPUHour, true
		}
	}
	return 0, false
}

func matchAll(nodeLabels map[string]string, want map[string]string) bool {
	if len(want) == 0 {
		return false
	}
	for k, v := range want {
		if nodeLabels[k] != v {
			return false
		}
	}
	return true
}
