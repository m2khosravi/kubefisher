package platform

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const gpuResourceName = "nvidia.com/gpu"

// GPUCountFromPod returns the total GPU limit (or request) across containers.
func GPUCountFromPod(pod *corev1.Pod) int64 {
	if pod == nil {
		return 0
	}
	var total int64
	for _, c := range pod.Spec.Containers {
		n := gpuQtyFromList(c.Resources.Limits)
		if n == 0 {
			n = gpuQtyFromList(c.Resources.Requests)
		}
		total += n
	}
	if total > 0 {
		return total
	}
	for _, c := range pod.Spec.InitContainers {
		n := gpuQtyFromList(c.Resources.Limits)
		if n == 0 {
			n = gpuQtyFromList(c.Resources.Requests)
		}
		total += n
	}
	return total
}

func gpuQtyFromList(rl corev1.ResourceList) int64 {
	if rl == nil {
		return 0
	}
	q, ok := rl[gpuResourceName]
	if !ok {
		return 0
	}
	return q.Value()
}

// GPUCountFromUnstructured reads nvidia.com/gpu from nested limits map (KServe predictor.model).
func GPUCountFromUnstructured(obj map[string]interface{}, paths ...[]string) int64 {
	defaultPaths := [][]string{
		{"spec", "predictor", "model", "resources", "limits"},
		{"spec", "predictor", "resources", "limits"},
		{"spec", "predictor", "workerSpec", "containers"},
	}
	if len(paths) == 0 {
		paths = defaultPaths
	}
	for _, p := range paths {
		if len(p) == 5 && p[4] == "limits" {
			if n := gpuFromLimitsMap(nestedMap(obj, p...)); n > 0 {
				return n
			}
			continue
		}
		if len(p) == 4 && p[3] == "containers" {
			if n := gpuFromContainersSlice(nestedSlice(obj, p...)); n > 0 {
				return n
			}
		}
	}
	return 0
}

func nestedMap(obj map[string]interface{}, keys ...string) map[string]interface{} {
	cur := obj
	for i, k := range keys {
		if cur == nil {
			return nil
		}
		v, ok := cur[k]
		if !ok {
			return nil
		}
		if i == len(keys)-1 {
			m, _ := v.(map[string]interface{})
			return m
		}
		cur, _ = v.(map[string]interface{})
	}
	return nil
}

func nestedSlice(obj map[string]interface{}, keys ...string) []interface{} {
	cur := obj
	for i, k := range keys {
		if cur == nil {
			return nil
		}
		v, ok := cur[k]
		if !ok {
			return nil
		}
		if i == len(keys)-1 {
			s, _ := v.([]interface{})
			return s
		}
		cur, _ = v.(map[string]interface{})
	}
	return nil
}

func gpuFromLimitsMap(limits map[string]interface{}) int64 {
	if limits == nil {
		return 0
	}
	v, ok := limits[gpuResourceName]
	if !ok {
		return 0
	}
	return parseGPUQuantity(v)
}

func gpuFromContainersSlice(containers []interface{}) int64 {
	var max int64
	for _, item := range containers {
		cm, _ := item.(map[string]interface{})
		res, _ := cm["resources"].(map[string]interface{})
		if res == nil {
			continue
		}
		limits, _ := res["limits"].(map[string]interface{})
		if n := gpuFromLimitsMap(limits); n > max {
			max = n
		}
	}
	return max
}

func parseGPUQuantity(v interface{}) int64 {
	switch t := v.(type) {
	case string:
		q, err := resource.ParseQuantity(t)
		if err != nil {
			return 0
		}
		return q.Value()
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	default:
		return 0
	}
}
