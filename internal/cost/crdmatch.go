package cost

import "strings"

// IsBentoDeploymentGVR reports whether discovery found a BentoDeployment API resource.
// Yatai uses serving.yatai.ai; older Bento stacks may use serving.bento.ai.
func IsBentoDeploymentGVR(group, resource string) bool {
	if resource != "bentodeployments" {
		return false
	}
	return strings.Contains(group, "serving.yatai.ai") || strings.Contains(group, "serving.bento.ai")
}
