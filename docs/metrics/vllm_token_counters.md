## vLLM token counter metrics (Prometheus)

Source of truth: vLLM “Production Metrics” docs at `https://docs.vllm.ai/en/stable/usage/metrics/`.

### Canonical token counters

- **Prefill (prompt) tokens**: `vllm:prompt_tokens_total`
- **Generation (output) tokens**: `vllm:generation_tokens_total`

These are monotonic counters. For rates/throughput, always use `rate()`/`irate()` in PromQL.

### Expected labels

vLLM commonly attaches a `model_name` label (see vLLM docs). Treat labels as best-effort and avoid hard-coding them in verification logic unless required.

### Validation queries (PromQL)

- **Prompt tokens/sec**:
  - `sum(rate(vllm:prompt_tokens_total[5m]))`
- **Generation tokens/sec**:
  - `sum(rate(vllm:generation_tokens_total[5m]))`

### What “validated” means in this repo

- **Scrape validation (k3d/no-GPU)**: Prometheus returns at least one series for both metrics above (using the vLLM mock metrics exporter).\n- **Runtime validation (GPU cluster)**: after sending a few completion requests to vLLM, the counters increase over time.

