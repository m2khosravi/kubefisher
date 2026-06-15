package deploy

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// DetectServingPlatform returns KServeStrategy when InferenceService CRD is installed,
// otherwise GenericStrategy (plain vLLM Deployment).
func DetectServingPlatform(disc discovery.DiscoveryInterface) DeployStrategy {
	if gvr, ok := findKServeGVR(disc); ok {
		return KServeStrategy{GVR: gvr}
	}
	return GenericStrategy{}
}

func findKServeGVR(disc discovery.DiscoveryInterface) (schema.GroupVersionResource, bool) {
	lists, err := disc.ServerPreferredResources()
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			lists, _ = disc.ServerPreferredResources()
		} else {
			return schema.GroupVersionResource{}, false
		}
	}
	for _, rl := range lists {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			continue
		}
		if !strings.Contains(gv.Group, "serving.kserve.io") {
			continue
		}
		for _, r := range rl.APIResources {
			if r.Name == "inferenceservices" && !strings.Contains(r.Name, "/") {
				return schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: r.Name,
				}, true
			}
		}
	}
	return schema.GroupVersionResource{}, false
}
