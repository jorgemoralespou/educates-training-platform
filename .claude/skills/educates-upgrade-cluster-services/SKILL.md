---
name: educates-upgrade-cluster-services
description: Upgrade a vendored upstream cluster-service Helm chart the Educates operator installs for EducatesClusterConfig — cert-manager, Contour, Kyverno, or external-dns. Invoke when asked to "upgrade cert-manager / contour / kyverno / external-dns", "bump a vendored cluster-service chart", or "update the operator's bundled charts".
argument-hint: "[cert-manager|contour|kyverno|external-dns] (omit to check all four)"
allowed-tools: "Read, Edit, Bash, WebFetch, Glob"
---

# Educates Cluster-Service Chart Upgrade Protocol

The operator installs four upstream cluster services for `EducatesClusterConfig`
(Managed mode) from Helm chart tarballs vendored at
`installer/operator/vendored-charts/`: **cert-manager**, **Contour**,
**Kyverno**, **external-dns**. The bytes are checked into the repo and embedded
into the operator binary via `//go:embed`. This skill bumps one (or checks all)
to a new upstream version.

`installer/operator/vendored-charts/README.md` is the canonical reference for
this workflow — keep it and this skill in agreement.

If `$ARGUMENTS` names a service, upgrade only that one. With no argument, check
all four for newer upstream versions and report before changing anything.

> The same directory also holds the in-repo runtime subcharts (secrets-manager,
> lookup-service, session-manager, node-ca-injector, remote-access). Those are
> **not** in scope here — they are repackaged from source via
> `make package-local-charts`, not downloaded.

## Where each piece of version state lives

A version is pinned in **four** canonical places that must stay in lockstep (a
consistency test enforces it — see Verification):

1. `installer/operator/Makefile` → `VENDORED_CHARTS` list: `name=version=url`.
2. `installer/operator/vendored-charts/SHA256SUMS`: `<sha256>  <name>-<version>.tgz`.
3. `installer/operator/vendored-charts/embed.go`: the `//go:embed <name>-<version>.tgz`
   directive, the `<X>ChartVersion` constant, and (Contour / external-dns /
   Kyverno) the `<X>AppVersion` constant. cert-manager has a single
   `CertManagerVersion` (its chart version equals its appVersion).
4. The tarball file itself on disk.

Beyond these four, some tests **hardcode a version string** rather than
referencing the `embed.go` constant (an import cycle prevents the `helm` package's
tests from importing `vendoredcharts`). The `DirectoryConsistent` test does **not**
cover these, so they go stale silently and only surface as a CI test failure. The
known one is `installer/operator/internal/helm/load_test.go`
(`TestLoadArchive_VendoredCertManager`), which hardcodes the **cert-manager**
version twice (the `.tgz` filename and the `AppVersion` assertion). Always finish a
bump with the straggler grep below — do not rely on the four-place list alone.

## Upstream sources and version semantics

| Service | Upstream releases | Chart URL pattern | App version |
|---|---|---|---|
| cert-manager | `https://github.com/cert-manager/cert-manager/releases` | `https://charts.jetstack.io/charts/cert-manager-v<VER>.tgz` | == chart version (`v<VER>`) |
| contour | `https://github.com/projectcontour/helm-charts/releases` | `https://github.com/projectcontour/helm-charts/releases/download/contour-<VER>/contour-<VER>.tgz` | Contour binary version (e.g. `1.33.5`) |
| external-dns | `https://github.com/kubernetes-sigs/external-dns/releases` | `https://github.com/kubernetes-sigs/external-dns/releases/download/external-dns-helm-chart-<VER>/external-dns-<VER>.tgz` | external-dns binary version |
| kyverno | `https://github.com/kyverno/kyverno/releases` | `https://kyverno.github.io/kyverno/kyverno-<VER>.tgz` | Kyverno binary version (e.g. `v1.18.1`) |

Use WebFetch on the releases page to find the latest stable chart version (not a
pre-release). The chart version and the bundled app version differ for Contour,
external-dns, and Kyverno — read the new app version from the tarball's
`Chart.yaml` (`appVersion:`) after downloading.

**Kyverno is the awkward one: its chart version and binary version are on
different numbering tracks and live in different places.** The
`github.com/kyverno/kyverno/releases` page lists the **binary** version
(`appVersion`, e.g. `v1.18.1`); the **chart** version (`<VER>` in the URL above,
e.g. `3.8.1`) only exists in the Helm repo's `index.yaml`. They are *not*
aligned and you cannot derive one from the other by inspection — you must resolve
the chart version that ships a given Kyverno binary from the repo index:

```bash
# What chart version ships Kyverno binary v1.18.1? (stable, non-rc rows only)
curl -sSfL https://kyverno.github.io/kyverno/index.yaml \
  | yq -r '.entries.kyverno[]
      | select(.appVersion | test("rc")|not)
      | .version + "  =>  " + .appVersion' \
  | grep ' v1.18.1$'        # → 3.8.1  =>  v1.18.1
```

To browse the newest stable charts and the binary each one bundles (the first
row is the latest stable chart), drop the `grep`:

```bash
curl -sSfL https://kyverno.github.io/kyverno/index.yaml \
  | yq -r '.entries.kyverno[]
      | select(.appVersion | test("rc")|not)
      | .version + "  =>  " + .appVersion' \
  | head
```

Equivalently with the Helm CLI (`helm repo add kyverno
https://kyverno.github.io/kyverno/ && helm repo update kyverno && helm search
repo kyverno/kyverno --versions`), whose `CHART VERSION` / `APP VERSION` columns
are the same two fields. Either way, that chart version is what goes in
`<VER>` for the download URL, the Makefile, `SHA256SUMS`, and the `embed.go`
filename + `KyvernoChartVersion`; the matching `appVersion` is
`KyvernoAppVersion` (still confirm it from the downloaded tarball's `Chart.yaml`
per step 5).

Current versions: read them from the Makefile `VENDORED_CHARTS` line and the
`embed.go` constants.

## Update process (per chart)

Worked example: contour `0.6.0` → `<VER>`. Other charts are identical except
cert-manager has no separate `AppVersion` constant.

1. **Compute the new tarball's SHA256** from the upstream URL (the "I am
   deliberately trusting these bytes" step — `make vendor-charts` refuses to
   download anything whose hash isn't already recorded):
   ```bash
   curl -sSfL "https://github.com/projectcontour/helm-charts/releases/download/contour-<VER>/contour-<VER>.tgz" | shasum -a 256
   ```

2. **`installer/operator/Makefile`** — update the chart's `VENDORED_CHARTS`
   entry (version and URL).

3. **`SHA256SUMS`** — replace the chart's line with the new filename + hash.

4. **`make -C installer/operator vendor-charts`** — downloads, verifies against
   SHA256SUMS, and writes the new tarball into place. Then `git rm` the old
   tarball (nothing prunes it automatically).

5. **`embed.go`** — update the `//go:embed` filename, the `<X>ChartVersion`
   constant, and the `<X>AppVersion` constant. Read the new appVersion from the
   tarball:
   ```bash
   tar -xzOf installer/operator/vendored-charts/contour-<VER>.tgz contour/Chart.yaml | grep -E '^(version|appVersion):'
   ```

6. **Grep the repo for stragglers — hardcoded references to the *old* version**
   that live outside the four canonical places (notably tests). Run this with the
   OLD version string before you forget it, and update every hit:
   ```bash
   # e.g. old cert-manager version v1.20.2 → catches internal/helm/load_test.go
   git grep -n "<old-version>" -- installer/operator
   ```
   For cert-manager this always includes `internal/helm/load_test.go` (filename +
   `AppVersion` assertion). Skip the `vendored-charts/` line for the tarball you
   just `git rm`'d.

7. **Re-verify reconciler assumptions** (the load-bearing step — see next
   section). The per-service reconciler gates readiness on specific workload
   names / webhook behaviour verified against a chart version.

8. **`installer/operator/vendored-charts/README.md`** — update the "Current
   contents" version table.

9. **Kyverno only — also re-vendor the bundled policies.** See "Kyverno
   special case" below.

## Re-verify reconciler assumptions

Each cluster service has a reconciler in `installer/operator/internal/controller/config/`
that gates readiness on workload names and, sometimes, CRD/webhook behaviour.
A chart bump can rename a Deployment, change a values key, or move a CRD API
version and silently break the gate. Render the new chart and confirm:

```bash
# extract + template the new chart with the operator's values shape
helm template <name> <path-to-extracted-chart> [--set ...] | grep -E 'kind: (Deployment|DaemonSet)' -A2
```

| Service | Reconciler | What it asserts (verify still true) |
|---|---|---|
| cert-manager | `certmanager.go` | `cert-manager`, `cert-manager-cainjector`, `cert-manager-webhook` Deployments ready; `cert-manager.io` API discovery responds; CRDs present (deferred watch) |
| contour | `contour.go` | `contour-contour` Deployment + `contour-envoy` DaemonSet (names from the chart fullname helper); no admission webhook; `envoy.useHostPort` / `envoy.service.type` values still honoured |
| external-dns | `externaldns.go` | the external-dns Deployment is ready; provider value keys (Route53 / CloudDNS) unchanged |
| kyverno | `kyverno.go` | the four controller Deployments (`kyverno-{admission,background,cleanup,reports}-controller`) ready; `validatingpolicies.policies.kyverno.io` CRD still installed; RBAC role names unchanged |

If a name or values key changed, update the reconciler constant and its
"verified against <version>" comment in the same change.

## Kyverno special case

Bumping the Kyverno **chart** usually means bumping the Kyverno **binary**
(`KyvernoAppVersion`), and the bundled Kyverno security policies are pinned to a
matching `kyverno/policies` release branch. When `KyvernoAppVersion` changes
(e.g. `v1.18.x` → `v1.19.x`):

1. Re-vendor the policies from the matching `release-1.NN` branch — follow
   `installer/charts/educates-training-platform/charts/session-manager/files/kyverno-policies/README.md`.
2. Repackage the session-manager subchart so the operator ships the refreshed
   policies:
   ```bash
   make -C installer/operator package-local-charts
   ```
   (then restore any subchart tarball that only changed by gzip timestamp).
3. Re-check that `kyverno.go`'s RBAC / CRD assumptions and the session-manager
   `validatingpolicies` ClusterRole still match the new Kyverno.

## Verification

```bash
make -C installer/operator verify-vendored-charts   # SHA256 of every on-disk tarball
make -C installer/operator test                     # runs the consistency + load tests
```

`make test` includes:
- `TestVendoredCharts_DirectoryConsistent` — ties the Makefile `VENDORED_CHARTS`
  list, `SHA256SUMS`, `embed.go` constants, and the on-disk tarballs together; a
  half-finished bump (or a leftover old tarball) fails here.
- `Test<Service>_Embedded` — asserts each embedded chart's name/version/appVersion
  match the `embed.go` constants.

The operator image `//go:embed`s these tarballs, so to test on a cluster rebuild
the operator (`make image-operator`) and redeploy. For a real upgrade, also
deploy `EducatesClusterConfig` in Managed mode and confirm the service reaches
Ready and its reconciler condition flips (`CertificatesReady`, `IngressReady`,
`DNSReady`, `PolicyEnforcementReady`).

## Notes

- Never edit a vendored tarball or unpack-and-repack it — ship the upstream
  chart unmodified. Configuration is applied through the operator's Helm values
  (the per-service `render<Service>Values` in the reconcilers), not by patching
  the bytes.
- Major-version bumps: read the upstream chart's changelog for renamed values,
  workload names, or CRD API-version moves, and flag breaking changes.

## Post-upgrade checklist

- [ ] `VENDORED_CHARTS` (Makefile), `SHA256SUMS`, and `embed.go` all updated to the new version
- [ ] New tarball downloaded via `make vendor-charts` (hash-verified); old tarball `git rm`'d
- [ ] `embed.go` `<X>AppVersion` read from the new tarball's `Chart.yaml`
- [ ] `git grep "<old-version>"` run; straggler hardcoded refs updated (cert-manager: `internal/helm/load_test.go`)
- [ ] Reconciler workload-name / values / CRD assumptions re-verified; constants + comments updated if changed
- [ ] Fine-grained install RBAC regenerated (`make generate-installer-rbac`) + CLI copy synced (`make embed-installer-chart`); an "unmapped Kind" error means adding the Kind to `hack/generate-installer-rbac.sh`
- [ ] Kyverno: bundled policies re-vendored + session-manager subchart repackaged (if appVersion changed)
- [ ] README "Current contents" table updated
- [ ] `make verify-vendored-charts` and `make test` pass
- [ ] Major-version bumps: changelog reviewed for breaking changes and flagged
