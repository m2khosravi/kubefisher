// Package adapters registers platform adapters for the cost patcher.
package adapters

import (
	"github.com/m2khosravi/kubefisher/internal/costpatcher/platform"
)

// Registry is the ordered list of platform adapters.
// First match wins; Generic must always be last (catch-all).
var Registry = []platform.Adapter{
	platform.KServe{},
	platform.KubeflowTrainer{},
	platform.RayServe{},
	platform.BentoML{},
	platform.Generic{},
}
