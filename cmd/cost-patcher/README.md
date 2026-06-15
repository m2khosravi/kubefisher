# cost-patcher

Main entrypoint for the KubeFisher cost patcher: reads `gpu-pricing` from the cluster, exposes pricing as Prometheus metrics, queries recording rules, and writes contract annotations on top-level workload objects.

Application wiring lives in **`internal/costpatcher`**. This directory should stay thin (flags + `main` only).
