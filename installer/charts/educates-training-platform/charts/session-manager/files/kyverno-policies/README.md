# Bundled Kyverno workshop policies

Two independent paths, mirroring v3's split between
`01-clusterpolicies.yaml` and `06-secrets.yaml`:

## `cluster-policies/` — applied as cluster-wide ClusterPolicy resources

Vendored from
[kyverno/policies](https://github.com/kyverno/policies)
(`origin/release-1.15`, matching v3's `vendir.yml`). Both Pod Security
Standards profiles are installed unconditionally when
`bundledKyvernoPolicies.clusterPolicies: true` — workshops don't pick a
profile, so both must be present in the cluster. Default action is
`Audit`, inherited from upstream.

| Directory | Source | What it covers |
|---|---|---|
| `cluster-policies/baseline/` | `pod-security-cel/baseline/*` | Pod Security Standards baseline |
| `cluster-policies/restricted/` | `pod-security-cel/restricted/*` | Pod Security Standards restricted |

## `workshop-policies/` — bundled into the educates-config Secret

Concatenated into the `kyverno-policies.yaml` Secret key when
`bundledKyvernoPolicies.workshopPolicies: true`. session-manager reads
the stream and clones each rule per workshop environment with a
namespace selector added (see
`session-manager/handlers/kyverno_rules.py`). They are **not** applied
cluster-wide — only as part of per-workshop
`educates-environment-<env>` ClusterPolicies once a workshop is
created.

| File pattern | Source |
|---|---|
| `disallow-cri-sock-mount.yaml`, `disallow-empty-ingress-host.yaml`, `restrict-service-external-ips.yaml`, `restrict-node-port.yaml` | `kyverno/policies` `best-practices-cel/*` |
| `disallow-ingress-nginx-custom-snippets.yaml`, `restrict-annotations.yaml`, `restrict-ingress-paths.yaml` | `kyverno/policies` `nginx-ingress-cel/*` |
| `disallow-localhost-services.yaml`, `prevent-cr8escape.yaml`, `restrict-loadbalancer.yaml` | `kyverno/policies` `other-cel/*` |
| `require-ingress-session-name.yaml` | Educates-internal (vendored from v3 `_ytt_lib/kyverno-policies/`) |

## Known CEL validation warnings

A small number of the vendored cluster-policies emit Kyverno admission
warnings at install time on Kubernetes / Kyverno versions kind ships
with today (v3 default = Kyverno 1.15.1). The CEL expressions inside
those policies use features the API server's CEL evaluator rejects —
for example `object.spec.containers + object.spec.initContainers`
(list concat), `size(volume)` on a list element, equality between an
object and a string. The policies still install (they're
`validationFailureAction: Audit`), but the affected rules are
effectively dead — they won't match anything at runtime.

Known offenders by symptom (not exhaustive):

- `disallow-capabilities` — list-concat expression.
- `disallow-host-path` — `size(volume)` on a list element.
- `restrict-sysctls` — equality of object to string.

Same warnings appear with v3's installer because it vendors the same
release. They will be revisited when we bump the Kyverno version
and/or the Kubernetes version floor — likely both at once. No action
needed for the pre-phase.

## Refreshing the bundle

1. Re-clone or fetch the matching release tag from kyverno/policies.
2. Replace the YAML files in the directories above. Keep filenames
   flat — drop the per-policy subdirs the upstream uses.
3. `helm template` the chart to confirm policies still parse and the
   `kyverno-policies.yaml` Secret-key shape is intact.
