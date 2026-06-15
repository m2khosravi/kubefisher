# kubefisher CLI

`kubefisher` is the KubeFisher command-line tool. It covers cluster setup, cost visibility, model deployment, workload status/logs, and team quota management — platform-agnostically (KServe, BentoML, Ray, or none).

## Build

```bash
make kubefisher-build          # → bin/kubefisher
# or
go build -o bin/kubefisher ./cmd/kubefisher
```

**Full documentation: [`docs/cli.md`](../../docs/cli.md)**

---

## Command quick reference

| Command | Description |
|---------|-------------|
| `kubefisher install` | Idempotent install of Prometheus, DCGM, and cost-patcher on any cluster |
| `kubefisher version` | Binary version + operator image tag in cluster |
| `kubefisher cost` | GPU workload cost table (cost/hr, cost/token) across all platforms |
| `kubefisher cost --watch` | Refresh cost table every 10 seconds |
| `kubefisher deploy --model MODEL` | Deploy a model (KServe or plain vLLM); polls until ready |
| `kubefisher status NAME` | Phase, endpoint, cost for a workload by name |
| `kubefisher logs NAME` | Stream logs from pods backing a workload (SIGINT-safe) |
| `kubefisher quota list` | List TeamInferenceQuota with phase, budget bars, and usage |
| `kubefisher quota get NAME` | Get one TeamInferenceQuota |
| `kubefisher quota set` | Create/update a TeamInferenceQuota (idempotent via SSA) |

Global flags on every command: `--kubeconfig`, `--context`, `--namespace` / `-n`, `--all-namespaces` / `-A`, `--output` / `-o` (`table`, `json`, `yaml`), `--log-format`.

---

## Common examples

```bash
# Install observability stack
kubefisher install
kubefisher install --dry-run

# Check cost across all namespaces
kubefisher cost -A
kubefisher cost -A --watch

# Deploy a model and tail its logs
kubefisher deploy --model meta-llama/Llama-3.1-8B --gpu a10g -n inference
kubefisher status llama-31-8b -n inference
kubefisher logs  llama-31-8b -n inference

# Set up a team quota (enforcement requires namespace label)
kubefisher quota set --name team-a -n team-a \
  --daily-tokens 1000000 --monthly-cost 500.00
kubectl label namespace team-a kubefisher.io/quota-enforcement=enabled
kubefisher quota list -A
kubefisher quota get team-a -n team-a -o yaml
```

Semantic colours for PHASE and cost tiers are applied only when stdout is a TTY. See [`docs/teaminferencequota-operator.md`](../../docs/teaminferencequota-operator.md) for the CRD and Prometheus queries that back the quota status fields.

Implementation lives in `internal/cli/kubefisher/`. See `internal/README.md` for how the CLI relates to cost-patcher and the operator.
