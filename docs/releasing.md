# Release process

This document describes how to cut a KubeFisher release. Releases publish two
container images to GitHub Container Registry (ghcr.io) and a Helm-installable
chart tag.

---

## Steps

### 1. Bump chart versions

Edit [`charts/kubefisher/Chart.yaml`](../charts/kubefisher/Chart.yaml) and
update **both** fields to the new version:

```yaml
version: X.Y.Z      # Helm chart version
appVersion: "X.Y.Z" # image tag that _helpers.tpl falls back to
```

The chart's `_helpers.tpl` uses `default .Chart.AppVersion .Values.image.tag`,
so leaving `image.tag` and `operator.image.tag` empty in `values.yaml`
(the default) means the published release image is pulled automatically.

### 2. Commit the version bump

```bash
git add charts/kubefisher/Chart.yaml
git commit -m "chore: release vX.Y.Z"
```

### 3. Tag the commit and push

```bash
git tag -s vX.Y.Z -m "Release vX.Y.Z"
git push origin main
git push origin vX.Y.Z
```

The tag must match `v*.*.*` exactly (e.g. `v0.2.0`, `v1.0.0`) to trigger the
release workflow.

### 4. CI builds and pushes images

[`.github/workflows/release.yml`](../.github/workflows/release.yml) fires on
the pushed tag and produces:

| Image | Tags |
|---|---|
| `ghcr.io/m2khosravi/kubefisher/cost-patcher` | `X.Y.Z`, `X.Y`, `latest` |
| `ghcr.io/m2khosravi/kubefisher/operator` | `X.Y.Z`, `X.Y`, `latest` |

Both images are built for `linux/amd64`. Monitor the run at:
`https://github.com/m2khosravi/kubefisher/actions/workflows/release.yml`

### 5. Create a GitHub Release

After the workflow succeeds, create a GitHub Release for the tag at:
`https://github.com/m2khosravi/kubefisher/releases/new?tag=vX.Y.Z`

Include in the release notes:
- **What's new** — new commands, changed behaviour, notable fixes.
- **Upgrade steps** — any `values.yaml` key renames, CRD changes
  (`make operator-manifests` output), or manual migration actions.
- **Known issues**, if any.

---

## First-time publish: make GHCR packages public

GitHub Container Registry packages are **private by default** on first push.
This is a one-time manual step per image and cannot be automated:

1. Go to `https://github.com/m2khosravi?tab=packages`
2. Click **kubefisher/cost-patcher** → **Package settings** → change visibility
   to **Public**.
3. Repeat for **kubefisher/operator**.

After this one-time change, all subsequent pushes to those packages remain
public and no further action is needed.

---

## Edge builds (main branch)

Every push to `main` also triggers the release workflow and tags both images as
`edge`:

```
ghcr.io/m2khosravi/kubefisher/cost-patcher:edge
ghcr.io/m2khosravi/kubefisher/operator:edge
```

`edge` is intended for early testers who want the latest merged changes without
waiting for a versioned release. Do not use `edge` in production.

---

## Local dev images

Local k3d development uses a separate tag (`dev`) that is never pushed to
ghcr.io. The Makefile targets handle this:

```bash
make cost-patcher-image              # builds kubefisher/cost-patcher:dev locally
make cluster-k3d-import-cost-patcher # imports into k3d (not pushed to any registry)
make cluster-install-kubefisher-cost # helm install with --set image.tag=dev
```

See [`docs/cluster-dev.md`](cluster-dev.md) for the full local dev workflow.
