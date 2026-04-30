# Scenario 06 — Bundled + user Kyverno policies, end-to-end

Validates both Kyverno-policy paths the chart supports: cluster-wide
ClusterPolicies installed directly, and the per-workshop
`educates-environment-<env>` ClusterPolicy session-manager spawns from
the `kyverno-policies.yaml` Secret feed.

## What's tested

Two independent paths land in the cluster:

- **Cluster-wide path** — `clusterSecurity.policyEngine: Kyverno`
  causes the chart to apply both Pod Security Standards profiles
  (baseline + restricted) as cluster-wide ClusterPolicy resources.
- **Per-workshop path** — `workshopSecurity.rulesEngine: Kyverno`
  writes the operational policies + the Educates-internal
  `require-ingress-session-name` into the `kyverno-policies.yaml`
  Secret feed. session-manager reads it and spawns one
  `educates-environment-<env>` ClusterPolicy per workshop, cloning
  each rule with a namespace selector.

User-supplied extras travel on both paths through
`clusterSecurity.additionalKyvernoPolicies` (installed directly) and
`workshopSecurity.additionalKyvernoPolicies` (appended to the Secret
feed).

The `post-deploy.sh` hook asserts:

- Cluster-wide ClusterPolicies present:
  - `disallow-privileged-containers` (bundled baseline)
  - `require-run-as-nonroot` (bundled restricted)
  - `scenario-06-cluster-marker` (user-supplied via
    `clusterSecurity.additionalKyvernoPolicies`)
- Per-environment `educates-environment-*` ClusterPolicy present with
  rules:
  - `no-loadbalancer-service` (bundled operational — the
    `restrict-loadbalancer.yaml` file's ClusterPolicy is named
    `no-loadbalancer-service` upstream; session-manager preserves
    that name when cloning the single-rule policy)
  - `require-ingress-session-name` (bundled Educates-internal)
  - `scenario-06-workshop-marker` (user-supplied via
    `workshopSecurity.additionalKyvernoPolicies`)

## Layout

Same as scenario 01 (HTTP, nip.io domain). The user-supplied marker
policies are CEL ClusterPolicies that always evaluate `true` (never
deny anything) — they exist only to be detectable by name.

## Out of scope

- Verifying that the policies actually deny anything in practice —
  that would require crafting offending Pod specs and checking
  Kyverno violation events. The chart-side wiring (rule names reach
  the per-env policy and ClusterPolicies appear cluster-wide) is
  what's under test here, not Kyverno itself.
- Toggle-off coverage for the bundles. With the typed-values shape,
  toggling off is `clusterSecurity.policyEngine: None` /
  `workshopSecurity.rulesEngine: None`. Add a sibling scenario for
  the disabled case if needed.
