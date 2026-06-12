# Bundled Kyverno workshop policies

Two independent paths, mirroring v3's split between
`01-clusterpolicies.yaml` and `06-secrets.yaml`:

## `cluster-policies/` — applied as cluster-wide ClusterPolicy resources

Vendored from
[kyverno/policies](https://github.com/kyverno/policies)
(`origin/release-1.18`, matching the Kyverno version the operator
bundles — see `KyvernoAppVersion` in
`installer/operator/vendored-charts/embed.go`). Both Pod Security
Standards profiles are installed unconditionally when
`clusterSecurity.policyEngine: Kyverno` — workshops don't pick a
profile, so both must be present in the cluster. Default action is
`Audit`, inherited from upstream.

| Directory | Source | What it covers |
|---|---|---|
| `cluster-policies/baseline/` | `pod-security-cel/baseline/*` | Pod Security Standards baseline |
| `cluster-policies/restricted/` | `pod-security-cel/restricted/*` | Pod Security Standards restricted |

## `workshop-policies/` — bundled into the educates-config Secret

Concatenated into the `kyverno-policies.yaml` Secret key when
`workshopSecurity.rulesEngine: Kyverno`. session-manager reads
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

A small number of the vendored cluster-policies historically emitted
Kyverno admission warnings at install time. The CEL expressions inside
those policies use features some API-server CEL evaluators reject —
for example `object.spec.containers + object.spec.initContainers`
(list concat), `size(volume)` on a list element, equality between an
object and a string. The policies still install (they're
`validationFailureAction: Audit`), but an affected rule is effectively
dead — it won't match anything at runtime.

Known offenders by symptom under Kyverno 1.15 (not exhaustive):

- `disallow-capabilities` — list-concat expression.
- `disallow-host-path` — `size(volume)` on a list element.
- `restrict-sysctls` — equality of object to string.

The `release-1.18` refresh (2026-06-12) modernised the CEL in four
policies (`disallow-host-ports`, `disallow-capabilities-strict`,
`require-run-as-nonroot`, `restrict-seccomp-strict`) but left the
three offenders above byte-identical to `release-1.15`, so their
warnings likely persist; re-check on the next bump.

## Refreshing the bundle

Re-vendor whenever the bundled Kyverno chart is bumped in
`installer/operator/vendored-charts/` — the branch must match the new
`KyvernoAppVersion` (e.g. Kyverno `v1.18.x` → branch `release-1.18`).

1. Clone the matching release branch:
   `git clone --depth 1 --branch release-1.NN https://github.com/kyverno/policies`
2. Replace the YAML files in the directories above, keeping filenames
   flat — drop the per-policy subdirs the upstream uses
   (`pod-security-cel/baseline/<name>/<name>.yaml` →
   `cluster-policies/baseline/<name>.yaml`). For `workshop-policies/`,
   copy only the files listed in the table above;
   `require-ingress-session-name.yaml` is Educates-internal and must
   not be overwritten.
3. `helm template` the session-manager chart with
   `--set global.clusterSecurity.policyEngine=Kyverno` and confirm the
   ClusterPolicy resources and the `kyverno-policies.yaml` Secret key
   still render, and that the policy *names* inside the Secret stream
   are unchanged (session-manager's `kyverno_rules.py` clones them by
   name per workshop environment).
4. Update the branch reference at the top of this file and the CEL
   warnings section.
