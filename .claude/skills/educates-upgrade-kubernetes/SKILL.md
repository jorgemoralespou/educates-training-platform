---
name: educates-upgrade-kubernetes
description: Upgrade Kubernetes version support across the educates-training-platform project. Invoke when asked to "upgrade kubernetes version", "support newer kubernetes version", "add kubectl version", or "drop kubernetes version". Updates kubectl binaries in the base environment Dockerfile, version mapping scripts, kind cluster constants, and Go dependencies.
argument-hint: "[<new-version>] [--drop <old-version>] (e.g. 1.35 --drop 1.31)"
allowed-tools: "Read, Edit, Glob, Bash, WebFetch"
---

# Educates Kubernetes Version Upgrade Protocol

Upgrade Kubernetes version support based on `$ARGUMENTS`.

- If `<new-version>` is provided (e.g. `1.35`), add support for that version.
- If `--drop <old-version>` is provided (e.g. `--drop 1.31`), remove that version.
- If no arguments, check `https://www.downloadkubernetes.com/` for the latest stable versions and ask the user whether to keep 3 or 4 versions before proceeding.

We support versions for **linux** on **amd64** and **arm64** only.

## Step 1: Gather Version Information

### For the new kubectl version

1. Fetch checksums for both architectures:
   - `https://dl.k8s.io/v{KUBECTL_VERSION}/kubernetes-client-linux-amd64.tar.gz.sha512`
   - `https://dl.k8s.io/v{KUBECTL_VERSION}/kubernetes-client-linux-arm64.tar.gz.sha512`

   The full version string uses semantic versioning (e.g. `1.35.1`). The short version is `major.minor` (e.g. `1.35`).

2. Find the corresponding kind node image and SHA256 digest at `https://github.com/kubernetes-sigs/kind/releases`. Image format: `kindest/node:v<K8S_VERSION>@sha256:<DIGEST>`

### Current versions

Read `workshop-images/base-environment/Dockerfile` to see which versions are currently installed and their RUN block format. Read `client-programs/pkg/constants/kubernetes.go` to see the current kind image map.

## Step 2: Update All Files

You must update **all** of the following. Do not skip any.

### `workshop-images/base-environment/Dockerfile`

**Adding a new version** — add a `RUN <<EOF` block following the existing pattern:

```dockerfile
RUN <<EOF
    KUBECTL_VERSION=1.35.1
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="<sha512-for-amd64>"
    CHECKSUM_arm64="<sha512-for-arm64>"
    # ... rest of install block (copy from adjacent version block)
EOF
```

**Removing an old version** — delete the entire `RUN <<EOF ... EOF` block for that version.

### Version mapping scripts (3 files with identical logic)

Update the `case` statement in all three files to match the currently supported versions:

- `workshop-images/base-environment/opt/kubernetes/bin/kubectl`
- `workshop-images/base-environment/opt/kubernetes/bin/kubectl-convert`
- `workshop-images/base-environment/opt/eduk8s/etc/setup.d/01-kubernetes.sh`

Pattern rules:
- `1.2*` and `1.3[012]` (or equivalent old versions) → map to the lowest supported version
- One explicit case per supported version (e.g. `1.33`, `1.34`, `1.35`, `1.36`)
- Default fallback (`*`) → the highest supported version

Example for versions 1.33–1.36:

```bash
case "$KUBECTL_VERSION" in
1.2*)
    KUBECTL_VERSION=1.33
    ;;
1.3[012])
    KUBECTL_VERSION=1.33
    ;;
1.33)
    KUBECTL_VERSION=1.33
    ;;
1.34)
    KUBECTL_VERSION=1.34
    ;;
1.35)
    KUBECTL_VERSION=1.35
    ;;
1.36)
    KUBECTL_VERSION=1.36
    ;;
*)
    KUBECTL_VERSION=1.36
    ;;
esac
```

The three files must have **identical** case statement logic.

### `client-programs/pkg/constants/kubernetes.go`

This map is what `educates local cluster create --kubernetes-version` uses to
pick a kind node image, so the digests **must come from the kind release the
CLI vendors** (`sigs.k8s.io/kind` in `client-programs/go.mod`). The node-image
set and digests are kind-version-specific — take them from the matching release
notes at https://github.com/kubernetes-sigs/kind/releases, not an older kind
release.

Update `KubernetesVersionToKindImage` — add new version entries, remove dropped ones:

```go
var KubernetesVersionToKindImage = map[string]string{
    "1.36": "kindest/node:v1.36.x@sha256:<digest>",
    "1.35": "kindest/node:v1.35.x@sha256:<digest>",
    "1.34": "kindest/node:v1.34.x@sha256:<digest>",
    "1.33": "kindest/node:v1.33.x@sha256:<digest>",
}

const DefaultKubernetesVersion = "1.36"
```

`DefaultKubernetesVersion` is the version `local cluster create` uses when
`--kubernetes-version` is not given. It currently tracks the **newest**
supported version (`"1.36"`); if you would rather trade recency for stability,
the second-newest is a reasonable alternative — just keep it consistent with
the code, `go.mod`, and the release notes.

Whenever the supported set or the vendored kind version changes, also update any
**code comments that name a specific version or kind release** so they still
match reality — e.g. the `sigs.k8s.io/kind vX.Y.Z` note in
`pkg/constants/kubernetes.go` and the example node image in the
`Ensuring node image (...)` comment in `pkg/cluster/kindcluster.go`.

### `client-programs/go.mod`

Update `sigs.k8s.io/kind` to the version that supports the new Kubernetes versions. Update `k8s.io/*` dependencies to align with `DefaultKubernetesVersion`:

- For Kubernetes 1.36.x → use `k8s.io/* v0.36.x`
- Packages: `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/cli-runtime`, `k8s.io/client-go`, `k8s.io/kubectl`
- Also update `sigs.k8s.io/controller-runtime` to a compatible version

After editing `go.mod`, run:

```bash
cd client-programs && go mod tidy
```

## Keep the vcluster application aligned

The `vcluster` workshop application provisions Kubernetes at its own set of
versions (the `loftsh-kubernetes` images in session-manager), maintained by the
**`educates-upgrade-vcluster`** skill. Keep that set the same as the versions
supported here, and keep the vcluster `K8S_DEFAULT_VERSION` the same as
`DefaultKubernetesVersion`, as long as the vcluster release in use supports them
(check its `images-optional.txt` / `images.txt`).

After changing the supported set here, **ask the user for permission to run
`educates-upgrade-vcluster`** to bring the vcluster application's `K8S_VERSIONS`
/ `K8S_DEFAULT_VERSION` in line (or to accept a deliberate divergence, for
example when the current vcluster release cannot yet provision a newly added
version). Do not silently leave the two sets out of sync.

## Step 3: Verify

Run in order, fix any failures before proceeding:

```bash
# Build the workshop base image (verifies Dockerfile changes)
make build-base-environment

# Build the CLI (verifies go.mod and constants changes)
cd client-programs && go mod tidy && go build ./...
```

## Step 4: Post-Verification Checklist

- [ ] All three version mapping files have identical case statement logic
- [ ] SHA512 checksums are correct for both amd64 and arm64
- [ ] Removed version has no remaining RUN block in Dockerfile
- [ ] `KubernetesVersionToKindImage` contains exactly the supported versions, with digests from the vendored kind release
- [ ] `DefaultKubernetesVersion` matches the intended default (currently the newest supported version)
- [ ] `k8s.io/*` versions in `go.mod` align with `DefaultKubernetesVersion`
- [ ] Code comments naming a specific version or kind release were updated to match
- [ ] `go mod tidy` ran cleanly in `client-programs/`
- [ ] vcluster application k8s set aligned, or divergence agreed with the user (see `educates-upgrade-vcluster`)
