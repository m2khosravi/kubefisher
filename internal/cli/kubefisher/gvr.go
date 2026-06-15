package kubefisher

import "k8s.io/apimachinery/pkg/runtime/schema"

// TeamInferenceQuotaGVR is the API resource for TeamInferenceQuota CRs (short name: tiq).
var TeamInferenceQuotaGVR = schema.GroupVersionResource{
	Group:    "quota.kubefisher.io",
	Version:  "v1alpha1",
	Resource: "teaminferencequotas",
}
