package contract

// Annotation keys from docs/contract.md (Kubernetes resource annotations).
const (
	// AnnCostPerHourPerReplica is the per-pod/per-replica GPU compute cost rate written by the
	// cost patcher. Renamed from "kubefisher.io/cost-per-hour" in a pre-1.0 contract change;
	// the old key is still dual-written as AnnCostPerHour for migration (see deprecation note).
	AnnCostPerHourPerReplica = "kubefisher.io/cost-per-hour-per-replica"

	// AnnCostPerHourTotal is the fleet-total GPU compute cost rate:
	// AnnCostPerHourPerReplica × current replica count (status.replicas, fallback spec.replicas).
	AnnCostPerHourTotal = "kubefisher.io/cost-per-hour-total"

	// AnnCostPerHour is the deprecated predecessor of AnnCostPerHourPerReplica.
	// Dual-written alongside the new key for one release cycle; remove on the next MAJOR bump.
	AnnCostPerHour = "kubefisher.io/cost-per-hour"

	AnnCostPerToken    = "kubefisher.io/cost-per-token"
	AnnLastUpdated     = "kubefisher.io/last-updated-at"
	AnnGPUCount        = "kubefisher.io/gpu-count"
	AnnTotalJobCostUSD = "kubefisher.io/total-job-cost-usd"
)
