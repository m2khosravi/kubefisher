# Scripts

Optional build/install/analysis scripts can live here to keep the root `Makefile` small (see [project layout `/scripts`](https://github.com/golang-standards/project-layout/tree/master/scripts)).

This repository currently drives most automation via the root **`Makefile`** (`operator-*`, `fisher-build`, cluster targets, etc.).

Cost patcher checks:

- **`verify_cost_patcher.sh`** — waits for `kubefisher.io/cost-per-hour` / `cost-per-token` on a Deployment (see `make cluster-verify-cost-annotation-*`).
