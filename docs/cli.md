# kubefisher CLI reference

`kubefisher` is the KubeFisher command-line tool. It covers every operator concern from initial cluster setup through live cost monitoring, workload deployment, and quota management — without requiring knowledge of which serving platform (KServe, BentoML, Ray, or none) is installed.

The CLI uses the same kubeconfig resolution as `kubectl` (`KUBECONFIG`, `~/.kube/config`, current context). It does not use controller-runtime; the implementation lives in `internal/cli/kubefisher/`. Labels and annotations referenced by these commands are defined in [`docs/contract.md`](contract.md).

---

## Build and install

```bash
# From the repo root
make kubefisher-build          # produces bin/kubefisher

# Or directly
go build -o bin/kubefisher ./cmd/kubefisher
```

The binary version is embedded at build time via ldflags into `internal/version.Version`. The Makefile uses `git describe --tags --always` as the value. Run `kubefisher version` to confirm.

A short alias `kf` is also created as a symlink in `bin/` by `make kubefisher-build`:

```bash
bin/kf cost -A        # same as bin/kubefisher cost -A
bin/kf quota list -A  # same as bin/kubefisher quota list -A
```

---

## Global flags

These flags are accepted by every subcommand.

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--kubeconfig` | | `$KUBECONFIG` / `~/.kube/config` | Path to kubeconfig file |
| `--context` | | current context | Kubernetes context name |
| `--namespace` | `-n` | current context namespace | Target namespace |
| `--all-namespaces` | `-A` | false | Query all namespaces (list-style commands only; `quota get` rejects this flag) |
| `--output` | `-o` | `table` | Output format: `table`, `json`, `yaml` |
| `--log-format` | | `text` (or `$LOG_FORMAT`) | Structured log format written to stderr: `text` or `json` |

Semantic colours (phase, cost tiers) are only applied when stdout is a TTY. Redirecting output to a pipe or file disables colour automatically.

---

## Commands

### `install`

Install the KubeFisher observability stack on any Kubernetes cluster.

```
kubefisher install [flags]
```

**What it does:**

1. Checks for an existing Prometheus stack and installs `kube-prometheus-stack` if absent (skip with `--skip-prometheus`).
2. Detects GPU nodes via `nvidia.com/gpu` node capacity; installs `gpu-operator` only if GPU nodes are present and DCGM is absent.
3. Installs the `kubefisher` Helm chart (cost-patcher + optional quota operator) idempotently.
4. Applies the embedded `gpu-pricing` ConfigMap and PrometheusRule via Server-Side Apply.
5. Prints a summary of detected serving platforms (KServe, BentoML, Ray, or none).

Running `install` twice is safe — no resources are duplicated or broken.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--namespace` / `-n` | `kubefisher-system` | Namespace for the kubefisher chart |
| `--dry-run` | false | Print what would be installed without applying any changes |
| `--skip-prometheus` | false | Skip kube-prometheus-stack even if Prometheus is absent |
| `--chart-path` | `./charts/kubefisher` | Path or OCI reference for the kubefisher chart |

**Prerequisites:** `helm` must be in `PATH`.

**Example:**

```bash
kubefisher install
kubefisher install --dry-run
kubefisher install --namespace my-system --skip-prometheus
```

---

### `version`

Print the binary version and the operator image tag installed in the cluster.

```
kubefisher version
```

Output:

```
kubefisher: v0.3.1
operator:    v0.3.1
```

The operator version is read from the image tag of the Deployment labelled `app.kubernetes.io/component=operator` in the target namespace (default: `kubefisher-system`, overridden with `--namespace`). If the operator is not installed, the line reads `(not installed in namespace "...")` and the command still exits 0.

---

### `cost`

Show a cost table for all GPU workloads — cost/hr and cost/token — across platforms.

```
kubefisher cost [flags]
```

The table covers Deployments, StatefulSets, and platform CRDs when installed:

- **Inference:** KServe `InferenceService`, BentoML `BentoDeployment`, KubeRay `RayService`
- **Training:** Kubeflow Trainer `TrainJob`, legacy Kubeflow jobs (`PyTorchJob`, …), KubeRay `RayJob` / `RayCluster`

Cost values are read from annotations written by the cost-patcher (`kubefisher.io/cost-per-hour-per-replica`, `kubefisher.io/cost-per-hour-total`, `kubefisher.io/cost-per-token`, and for completed Trainer jobs `kubefisher.io/total-job-cost-usd` on the `TrainJob` object). The deprecated `kubefisher.io/cost-per-hour` key is also read for backward compatibility with older cost-patcher versions. Absent annotations display as `—` — not an error.

**PLATFORM column** uses contract tokens such as `kserve`, `bentoml`, `ray-serve`, `kubeflow-trainer`, `kubeflow-training`, `ray`, `vllm` (see [`docs/contract.md`](contract.md#platform-tokens)). **WORKLOAD-TYPE** is `inference` or `training`.

**Columns:** `NAMESPACE · NAME · PLATFORM · TYPE · REPLICAS · COST/HR (REPLICA) · COST/HR (TOTAL) · COST/TOKEN · MONTHLY-EST`

- **COST/HR (REPLICA)** — GPU compute cost for a single replica, sourced from `kubefisher.io/cost-per-hour-per-replica`.
- **COST/HR (TOTAL)** — Fleet-wide GPU compute cost (`per-replica × replicas`), sourced from `kubefisher.io/cost-per-hour-total`.
- **MONTHLY-EST** — `COST/HR (TOTAL) × 720` hours.

Cost colouring: red > $5/hr, yellow > $2/hr, green otherwise (TTY only).

**Footer:** `Total: $X.XX/hr · Est. $X,XXX/mo`

**Flags:**

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--watch` | `-w` | false | Refresh every 10 seconds (clears screen between updates) |

**Examples:**

```bash
kubefisher cost
kubefisher cost -n team-a
kubefisher cost -A -o json
kubefisher cost --watch
```

---

### `deploy`

Deploy a GPU model to the cluster, routing to the appropriate serving platform.

```
kubefisher deploy --model MODEL [flags]
```

When KServe (`InferenceService` CRD) is detected, an `InferenceService` is created. Otherwise, a plain vLLM `Deployment` + `Service` is created with contract labels. The command polls every 3 seconds until the workload is ready, then prints the endpoint URL and cost/hr annotation (displayed as `—` if the cost-patcher has not yet run).

Always adds the following labels/annotations: `kubefisher.io/model`, `kubefisher.io/platform`, `kubefisher.io/team`, `kubefisher.io/workload-type`.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--model` | *(required)* | HuggingFace model ID (e.g. `meta-llama/Llama-3.1-8B`) |
| `--gpu` | `a10g` | GPU type for node selector (`accelerator` label) |
| `--team` | `platform` | Team label value (`kubefisher.io/team`) |
| `--replicas` | `1` | Number of replicas |
| `--image` | | Container image override (generic/vLLM strategy only) |
| `--dry-run` | false | Print YAML to stdout without creating any resources |

**Examples:**

```bash
kubefisher deploy --model meta-llama/Llama-3.1-8B --gpu a10g
kubefisher deploy --model facebook/opt-125m --team ml-research --replicas 2
kubefisher deploy --model meta-llama/Llama-3.1-8B --dry-run
```

After a successful deploy:

```
Endpoint: http://llama-31-8b.team-a.svc.cluster.local/v1
Cost/hr:  $3.50
Run: kubefisher cost --watch
```

---

### `status`

Show the live status of a GPU workload by name.

```
kubefisher status NAME [flags]
```

Searches for the workload across all known resource types in priority order: KServe `InferenceService`, BentoML `BentoDeployment`, Kubeflow training CRDs (`TrainJob`, `PyTorchJob`, …), KubeRay CRDs (`RayService`, `RayJob`, `RayCluster`), then `Deployment` / `StatefulSet`. The name is matched within the resolved namespace.

**Output fields:** `Phase · Endpoint · Replicas · Cost/hr · Cost/token · WorkloadType · Last-updated`

Phase is coloured on TTY (green = Ready/Active, yellow = Deploying/Warning, red = Failed/Exceeded).

**Examples:**

```bash
kubefisher status llama-31-8b
kubefisher status llama-31-8b -n team-a
kubefisher status my-job -o json
```

---

### `logs`

Stream logs from the pods backing a GPU workload, without needing to know pod names.

```
kubefisher logs NAME [flags]
```

Finds the workload via the same discovery as `status`, then selects the backing pods using label selectors. Pressing Ctrl-C closes the stream cleanly (no goroutine leak).

**Flags:**

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--follow` | `-f` | true | Follow log output (stream continuously) |
| `--container` | `-c` | *(first container)* | Container name when the pod has multiple containers |
| `--pod` | | *(first matching pod)* | Specific pod name when multiple pods back the workload |

**Examples:**

```bash
kubefisher logs llama-31-8b
kubefisher logs llama-31-8b --follow=false
kubefisher logs llama-31-8b -c model-server
kubefisher logs my-training-job --pod my-training-job-worker-0
```

---

### `quota`

Inspect and manage `TeamInferenceQuota` resources. Short alias: `tiq`.

```
kubefisher quota SUBCOMMAND [flags]
```

#### `quota list`

List all `TeamInferenceQuota` objects in the resolved namespace (or all namespaces with `-A`).

```
kubefisher quota list [flags]
kubefisher quota ls [flags]
```

**Table columns:**

| Column | Source |
|--------|--------|
| `NAMESPACE` | metadata.namespace |
| `NAME` | metadata.name |
| `PHASE` | status.phase — coloured green=Active, yellow=Warning, red=Exceeded |
| `TOKENS-USED` | status.tokensUsedToday (rolling 24 h) |
| `BUDGET` | spec.dailyTokenBudget |
| `TOKEN-REM` | ASCII bar from status.tokenBudgetRemainingPct — e.g. `[█████░░░]  62%` |
| `COST-USED` | status.costUsedThisMonth (USD) |
| `LIMIT` | spec.monthlyCostLimitUSD (USD) |
| `COST-REM` | ASCII bar from status.costBudgetRemainingPct |
| `MODE` | spec.enforcementMode (`Enforce` or `Audit`) |
| `AGE` | time since creation |

When no quotas are found: `No quotas found. Run: kubefisher quota set --help`

**Examples:**

```bash
kubefisher quota list
kubefisher quota list -A
kubefisher quota list -n team-a -o json
kubefisher quota ls -A -o yaml
```

#### `quota get`

Get a single `TeamInferenceQuota` by name.

```
kubefisher quota get NAME [flags]
```

Does not accept `--all-namespaces`; use `--namespace` or `-n` to specify the namespace.

**Examples:**

```bash
kubefisher quota get team-a -n team-a
kubefisher quota get team-a -n team-a -o yaml
```

#### `quota set`

Create or update a `TeamInferenceQuota` via Server-Side Apply. Running the command twice with the same flags produces no error and no duplicate resources.

```
kubefisher quota set --name NAME --daily-tokens N --monthly-cost AMOUNT [flags]
```

**Flags:**

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--name` | yes | | Name of the `TeamInferenceQuota` object |
| `--daily-tokens` | yes | | Daily token budget (rolling 24 h window from Prometheus) |
| `--monthly-cost` | yes | | Monthly USD cost limit, e.g. `500.00` |
| `--mode` | | `Enforce` | Enforcement mode: `Enforce` (deny GPU pods when Exceeded) or `Audit` (observe only) |
| `--alert-threshold` | | `80` | Utilisation percentage at which phase transitions to `Warning` |

The global `--namespace` / `-n` flag sets the target namespace and is required.

**Examples:**

```bash
# Create a quota for team-a: 1 million tokens/day, $500/mo, default Enforce mode
kubefisher quota set --name team-a -n team-a \
  --daily-tokens 1000000 --monthly-cost 500.00

# Switch to Audit mode (observe-only; pods are never blocked)
kubefisher quota set --name team-a -n team-a \
  --daily-tokens 1000000 --monthly-cost 500.00 --mode Audit

# Lower the warning threshold to 70%
kubefisher quota set --name team-a -n team-a \
  --daily-tokens 1000000 --monthly-cost 500.00 --alert-threshold 70
```

After applying, verify enforcement is active (requires namespace label + webhook):

```bash
kubectl label namespace team-a kubefisher.io/quota-enforcement=enabled
kubefisher quota list -n team-a
```

See [`docs/verify-quota.md`](verify-quota.md) for the full 5-command enforcement verification runbook.

---

## Typical workflows

### Greenfield cluster

```bash
# 1. Install the observability stack (Prometheus + DCGM + cost-patcher)
kubefisher install

# 2. Confirm versions
kubefisher version

# 3. Check which GPU workloads are present and what they cost
kubefisher cost -A

# 4. Deploy a model
kubefisher deploy --model meta-llama/Llama-3.1-8B --gpu a10g -n inference

# 5. Watch status and tail logs
kubefisher status llama-31-8b -n inference
kubefisher logs  llama-31-8b -n inference
```

### Setting up team quotas

```bash
# 1. Apply a quota for a team namespace
kubefisher quota set --name team-a -n team-a \
  --daily-tokens 2000000 --monthly-cost 1000.00

# 2. Enable enforcement on the namespace
kubectl label namespace team-a kubefisher.io/quota-enforcement=enabled

# 3. Monitor all quotas
kubefisher quota list -A

# 4. Watch cost changes live
kubefisher cost -A --watch
```

---

## Related documentation

| Document | Description |
|----------|-------------|
| [`docs/contract.md`](contract.md) | Authoritative labels, annotations, metrics, and pricing schema |
| [`docs/cost-patcher.md`](cost-patcher.md) | How cost-patcher writes `kubefisher.io/cost-per-hour-per-replica`, `kubefisher.io/cost-per-hour-total`, and `kubefisher.io/cost-per-token` |
| [`docs/teaminferencequota-operator.md`](teaminferencequota-operator.md) | TeamInferenceQuota CRD, Prometheus queries, reconciler, tests |
| [`docs/verify-quota.md`](verify-quota.md) | 5-command enforcement verification runbook |
| [`docs/cluster-dev.md`](cluster-dev.md) | k3d local cluster setup and dev workflows |
| [`docs/security.md`](security.md) | Webhook policy, TLS, namespace exclusions, RBAC |
