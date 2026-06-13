# Bundled Kyverno workshop policies

These are **Kyverno `ValidatingPolicy` resources** (`policies.kyverno.io`),
the policy type Kyverno 1.18 recommends in place of the legacy `ClusterPolicy`
(`kyverno.io`). Two independent paths, mirroring v3's split between
`01-clusterpolicies.yaml` and `06-secrets.yaml`:

## `cluster-policies/` — applied cluster-wide

Vendored from
[kyverno/policies](https://github.com/kyverno/policies)
(`origin/release-1.18`, matching the Kyverno version the operator bundles —
see `KyvernoAppVersion` in `installer/operator/vendored-charts/embed.go`),
from the `pod-security-vpol` set. Both Pod Security Standards profiles are
installed unconditionally as `ValidatingPolicy` resources when
`clusterSecurity.policyEngine: Kyverno` — workshops don't pick a profile, so
both must be present. Action is `Audit` (`validationActions: [Audit]`),
inherited from upstream.

| Directory | Source | What it covers |
|---|---|---|
| `cluster-policies/baseline/` | `pod-security-vpol/baseline/*` | Pod Security Standards baseline |
| `cluster-policies/restricted/` | `pod-security-vpol/restricted/*` | Pod Security Standards restricted |

The upstream `pod-security-vpol` baseline drops the `disallow-host-ports-range`
"Alternate" policy that the CEL set carried; it folds into `disallow-host-ports`.
We follow that curation (these are `Audit`-only cluster-wide, so no enforcement
change).

## `workshop-policies/` — bundled into the educates-config Secret

Concatenated into the `kyverno-policies.yaml` Secret key when
`workshopSecurity.rulesEngine: Kyverno`. session-manager reads the stream and
creates a **per-workshop-environment copy** of each policy, scoped to the
environment's session namespaces via `spec.matchConstraints.namespaceSelector`
(see `session-manager/handlers/kyverno_rules.py`). They are **not** applied
cluster-wide — only as `educates-environment-<env>-<policy>` ValidatingPolicies
once a workshop is created. The per-env action follows the workshop's
`session.namespaces.security.rules.action` (`Enforce` → `Deny`, `Audit` →
`Audit`), overriding the bundled file's own action.

| File pattern | Source |
|---|---|
| `disallow-cri-sock-mount.yaml`, `disallow-empty-ingress-host.yaml`, `restrict-service-external-ips.yaml`, `restrict-node-port.yaml` | `kyverno/policies` `best-practices-vpol/*` |
| `disallow-localhost-services.yaml`, `prevent-cr8escape.yaml`, `restrict-loadbalancer.yaml` | `kyverno/policies` `other-vpol/*` |
| `disallow-ingress-nginx-custom-snippets.yaml`, `restrict-annotations.yaml`, `restrict-ingress-paths.yaml` | Hand-ported to `ValidatingPolicy` from `nginx-ingress-cel/*` (no upstream `-vpol` variant exists) |
| `require-ingress-session-name.yaml` | Educates-internal; rewritten from the legacy JMESPath ClusterPolicy to `ValidatingPolicy` CEL (reads the session name from `namespaceObject`) |

## User-supplied workshop policies and the ClusterPolicy deprecation

`workshopSecurity.additionalKyvernoPolicies` entries flow through the same
per-workshop scoping. A new Policy type (`ValidatingPolicy`, etc.) is scoped
natively. A legacy `ClusterPolicy` is **still scoped but deprecated**: the
runtime logs a warning and continues. This tracks Kyverno's own timeline —
`ClusterPolicy` is deprecated in 1.18 and removed in 1.20 — and Educates will
drop support for workshop-provided ClusterPolicies on the same schedule.
Migrate them to `ValidatingPolicy`.

## Hand-ported policies

The three nginx-ingress policies and `require-ingress-session-name` have no
upstream `-vpol` variant, so they are maintained here by hand. When refreshing,
do **not** overwrite them from upstream. `disallow-ingress-nginx-custom-snippets`
matches both ConfigMap and Ingress; its two original rules became one policy
whose validations are each guarded by `request.kind.kind`. These are
security-critical CEL — validate against real resources, not just rendering.

## Refreshing the bundle

Re-vendor whenever the bundled Kyverno chart is bumped in
`installer/operator/vendored-charts/` — the branch must match the new
`KyvernoAppVersion` (e.g. Kyverno `v1.18.x` → branch `release-1.18`).

1. Clone the matching release branch:
   `git clone --depth 1 --branch release-1.NN https://github.com/kyverno/policies`
2. Replace the YAML under `cluster-policies/` and `workshop-policies/` from the
   `*-vpol` directories, keeping filenames flat — drop the per-policy subdirs
   and the `kustomization.yaml` / `.chainsaw-test` / `.kyverno-test` /
   `artifacthub-pkg.yml` helpers. Copy only the files listed in the tables
   above; leave the four hand-ported policies untouched.
3. `helm template` the session-manager chart with
   `--set global.clusterSecurity.policyEngine=Kyverno
   --set workshopSecurity.rulesEngine=Kyverno`; confirm the cluster-policies
   render as `ValidatingPolicy`, the `kyverno-policies.yaml` Secret key renders,
   and the policy *names* inside the stream are unchanged (the runtime names the
   per-env copies after them, and workshop `exclude` lists reference them).
4. `make -C installer/operator package-local-charts` to refresh the embedded
   session-manager subchart tarball the operator ships.
5. Update the branch reference and the source tables above.
