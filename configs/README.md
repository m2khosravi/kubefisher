# configs

Example and template configuration files (see [project layout `/configs`](https://github.com/golang-standards/project-layout/tree/master/configs)).

Not to be confused with [`../config/`](../config/), which holds **cluster/local-dev Kubernetes manifests** (k3d, observability stack, serving demos).

The cluster-applied `gpu-pricing` ConfigMap is rendered by the Helm chart at
[`../charts/kubefisher/`](../charts/kubefisher/) from
`gpuPricing.pricing` in `values.yaml`. This directory holds a standalone YAML
example (`gpu-pricing.example.yaml`) suitable for documentation and copy-paste
into your own values overrides.
