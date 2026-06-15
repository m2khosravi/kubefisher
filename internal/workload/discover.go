package workload

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

type resourceCatalog struct {
	kserve   []schema.GroupVersionResource
	bento    []schema.GroupVersionResource
	training []schema.GroupVersionResource
	ray      []schema.GroupVersionResource
}

func discoverResources(disc discovery.DiscoveryInterface) (resourceCatalog, error) {
	var cat resourceCatalog
	lists, err := disc.ServerPreferredResources()
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			lists, _ = disc.ServerPreferredResources()
		} else {
			return cat, fmt.Errorf("discovery: %w", err)
		}
	}
	for _, rl := range lists {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			continue
		}
		group := gv.Group
		for _, r := range rl.APIResources {
			if r.Kind == "" || strings.Contains(r.Name, "/") {
				continue
			}
			gvr := schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: r.Name}
			switch {
			case strings.Contains(group, "serving.kserve.io") && r.Name == "inferenceservices":
				cat.kserve = append(cat.kserve, gvr)
			case isBentoDeploymentGVR(group, r.Name):
				cat.bento = append(cat.bento, gvr)
			case strings.Contains(group, "kubeflow.org") &&
				(r.Name == "trainjobs" || r.Name == "pytorchjobs" || r.Name == "tfjobs" ||
					r.Name == "xgboostjobs" || r.Name == "mpijobs" || r.Name == "paddlejobs"):
				cat.training = append(cat.training, gvr)
			case strings.Contains(group, "ray.io") && (r.Name == "rayjobs" || r.Name == "rayclusters" || r.Name == "rayservices"):
				cat.ray = append(cat.ray, gvr)
			}
		}
	}
	return cat, nil
}

func isBentoDeploymentGVR(group, resource string) bool {
	if resource != "bentodeployments" {
		return false
	}
	return strings.Contains(group, "serving.yatai.ai") || strings.Contains(group, "serving.bento.ai")
}
