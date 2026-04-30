# Architectural Decisions Log

> One paragraph per decision. Append-only — when a decision is reversed,
> add a new entry and link back to the superseded one. Don't rewrite
> history.

Format: `### <short title>` — date — what was decided, why.

---

### Helm chart apiVersion v2

**Date:** 2026-04-27.
**Decision:** All Helm charts produced for the v4 installer use
`apiVersion: v2` in `Chart.yaml`. **Why:** v2 is required for
`dependencies`, `kubeVersion` enforcement, and library-chart support, all
of which we use or will use. v1 is legacy.

### Kubernetes version floor: 1.31

**Date:** 2026-04-27.
**Decision:** All v4 charts declare `kubeVersion: ">=1.31.0-0"`.
**Why:** Educates v4 will not support Kubernetes <1.31. The chart-level
constraint is advisory (Helm warns rather than fails on mismatch in some
versions), but it documents the contract and lets `helm install` reject
obvious mismatches. Project-level support is the source of truth; this is
the chart-level mirror of it.

### Single version across umbrella and subcharts

**Date:** 2026-04-27.
**Decision:** The `educates-training-platform` umbrella chart and its
three subcharts (`secrets-manager`, `lookup-service`, `session-manager`)
all carry the same `version` and `appVersion`, bumped together on every
release. **Why:** They ship as a set. Independent versioning would imply
the subcharts are reusable in other contexts, which they are not — they
are tightly coupled to a specific Educates runtime build. A single
version simplifies release tooling and makes "which subchart version goes
with which umbrella version" a non-question.

### Chart version tracks appVersion

**Date:** 2026-04-27.
**Decision:** `version` and `appVersion` are kept in lockstep on these
charts. **Why:** Same reasoning as the previous entry — these charts are
not reusable artifacts whose chart packaging would evolve independently
of the application they install. Decoupling the two adds bookkeeping
without buying anything.

### Umbrella chart structure: physical subcharts under `charts/`

**Date:** 2026-04-27.
**Decision:** The `educates-training-platform` umbrella chart embeds its
three subcharts as physical directories under `charts/` *and* declares
them in `Chart.yaml` `dependencies` (with `condition: <name>.enabled`).
**Why:** Physical subcharts make `helm template` and `helm install` work
without a `helm dependency update` step or a Chart.lock. Declaring them
in `dependencies` is what lets the `condition` flag toggle them on and
off cleanly. Combining both gives the local-repo ergonomics of physical
charts with the gating ergonomics of declared dependencies.

### Subchart toggling: per-subchart `enabled` flag

**Date:** 2026-04-27.
**Decision:** Each subchart can be independently enabled or disabled via
`<subchart-name>.enabled` in the umbrella values. All three default to
`true`. **Why:** The pre-phase plan calls for subcharts to be enabled or
disabled independently. The flag is the standard Helm pattern. All three
default on because the typical install is the full runtime; users who
want a subset (e.g., no lookup-service for a single-cluster install) opt
out explicitly.

### Subcharts do not vendor upstream charts

**Date:** 2026-04-27.
**Decision:** The Educates runtime subcharts (`secrets-manager`,
`lookup-service`, `session-manager`) do not depend on any upstream Helm
charts and therefore vendor nothing. **Why:** These subcharts package
Educates' own components only. Cluster services like cert-manager,
Contour, Kyverno, external-dns are installed by the operator (in Managed
mode), not by the runtime chart. Open item #2 in the development plan
(vendor upstream charts at build time) applies to those operator-driven
installs, not to these subcharts.

### Runtime chart values shape is operator-driven, not v3-driven  *(superseded 2026-04-30 — see "session-manager values are typed and standalone-friendly")*

**Date:** 2026-04-27.
**Decision:** The values shape of the `educates-training-platform` chart
and its subcharts is designed for what the v4 operator will pass in
(derived from component CRs and `EducatesClusterConfig.status`), not as a
1:1 mirror of the v3 ytt schema. **Why:** The v3 schema is shaped by the
single-blob installer config it was fed from. The v4 operator passes
narrowly scoped values per component, derived from per-component CRs.
Mirroring v3 would carry forward structural choices that no longer fit.
Translation choices will be flagged as they're made; the migration tool
(`educates migrate-config`) bridges the user-facing v3 config to v4 CR
YAML, not to chart values.

### CRDs shipped in each subchart's `crds/` directory  *(supersedes earlier `templates/`-based decision)*

**Date:** 2026-04-28.
**Decision:** CRDs in each runtime subchart are placed in the
subchart's `crds/` directory (Helm's special location) as pure YAML —
no Helm templating, no annotations. They are installed on first
`helm install`, left untouched on `helm upgrade`, and not deleted on
`helm uninstall`.
**Why this supersedes the earlier `templates/` decision:**
- Helm cannot install CRs and their CRDs in a single release when the
  CRDs are in `templates/`: Helm needs the CRD's REST mapping to exist
  in the API server *while* it builds the manifest, but the CRD won't
  be applied until the manifest is built. Confirmed empirically when
  scenario 01 failed with
  `resource mapping not found for ... SecretInjector ... ensure CRDs
  are installed first`. The runtime chart contains both the
  `secrets.educates.dev` CRDs *and* SecretInjector/SecretCopier custom
  resources, so this is unavoidable with `templates/`.
- Workarounds exist (pre-install hooks on each CRD; a dedicated CRDs
  subchart installed first) but each adds chart complexity and changes
  the upgrade story in ways we'd rather not commit to.
- The original concern that drove the `templates/` choice was
  upgradability during v1alpha1 schema churn. In practice the Educates
  internal CRDs (training, secrets, lookup) have been stable through v3
  and are not expected to change in v4 except in a coordinated way that
  also requires a runtime change. The operator CRDs that *will* see
  more churn (EducatesClusterConfig, SecretsManager, LookupService,
  SessionManager) are managed by the operator chart, not this runtime
  chart, and those will be handled separately.
**Consequences to be aware of:**
- A schema change in any of these CRDs requires users to apply the new
  CRD out-of-band before `helm upgrade` (or use `helm install --force`,
  which has its own caveats). This is the standard operational story
  for CRDs in Helm and is documented across the Helm ecosystem.
- `helm uninstall` leaves CRDs in place (which is actually the same
  behaviour we wanted from the previous `keep` annotation, just via a
  different mechanism).
- CRD files cannot use Helm template directives — they are pure YAML.
  Chart labels are not added to CRDs as a result; this is consistent
  with how most Helm charts ship CRDs.
**Reconsider trigger:** If we end up needing per-release CRD updates
during v4 development (e.g., we change a validation rule and want
`helm upgrade` to roll it out), revisit and consider a dedicated
`crds` subchart pattern (kube-prometheus-stack-style) that explicitly
manages the CRD lifecycle.

### Drop the v3 `educates-config` blob from the secrets-manager subchart

**Date:** 2026-04-27.
**Decision:** The v4 `secrets-manager` subchart does not create or mount
the `educates-config` Secret that the v3 installer used. **Why:** The
secrets-manager Python operator only reads `operator.namespace` from
that config blob, and even that value is overridden at runtime by
`/var/run/secrets/kubernetes.io/serviceaccount/namespace`. The mount is
dead weight for this component. The session-manager subchart's
configuration story (which is genuinely larger) is handled separately in
its own subchart values.

### Drop PodSecurityPolicy bindings; keep SCC for now

**Date:** 2026-04-27.
**Decision:** The v4 runtime subcharts do not include
PodSecurityPolicy-related ClusterRoleBindings. SCC
(SecurityContextConstraints) ClusterRoleBindings remain available,
gated by a chart value, defaulting off. **Why:** PodSecurityPolicy was
removed from Kubernetes in 1.25; the v4 floor is 1.31, so PSP code is
unreachable. SCC is OpenShift-specific and is being kept available
because the OpenShift Inline-mode story (Scenario E in the CRD draft)
hasn't been fully designed yet. Once that scenario is implemented we
will revisit whether SCC bindings belong in the runtime subcharts at
all, or are better managed by the cluster admin out-of-band.

### `remote-access` is a separate, toggleable subchart

**Date:** 2026-04-27.
**Decision:** The `remote-access` ServiceAccount, ClusterRole,
ClusterRoleBinding, and long-lived service-account-token Secret are
packaged as their own subchart (`remote-access`) under the umbrella,
gated by `remote-access.enabled`, defaulted to `true`. The lookup-service
subchart conditionally mounts the `remote-access-token` Secret at
`/opt/cluster-access-token` only when `remote-access.enabled=true`.
**Why:** `remote-access` grants read access to `training.educates.dev`
resources for external CLI clients (e.g., `educates` CLI used
cross-cluster). Use cases vary: some installs want it without
lookup-service (session-manager only, with external CLI access), some
want lookup-service federation without remote CLI access, some want
both. Bundling it inside lookup-service forced installs to choose
between two unrelated capabilities. A separate subchart keeps the
permission grant explicit and independently toggleable. **How to apply:**
The lookup-service Deployment uses `/opt/cluster-access-token` only when
serving a "local" ClusterConfig (no kubeconfig secretRef), per
`lookup-service/service/handlers/clusters.py`. When `remote-access` is
disabled, lookup-service still works for any ClusterConfig with an
explicit kubeconfig secretRef. This runtime nuance is documented in
chart values, not enforced at chart install time.

### session-manager hard-couples to secrets-manager; secrets-manager is standalone

**Date:** 2026-04-27.
**Decision:** The `session-manager` subchart renders SecretCopier and
SecretInjector resources unconditionally (no `secretsManagerIntegration`
toggle). Installing session-manager therefore requires the
`secrets.educates.dev` CRDs to be present, which means `secrets-manager`
must be installed alongside it. The `secrets-manager` subchart, by
contrast, is fully standalone — it can be installed on its own without
session-manager. **Why:** session-manager's runtime depends on the
secret-propagation primitives (image-pull-secrets, registry credentials,
ingress TLS replication into operator namespace) that secrets-manager
provides. Splitting them via a chart-level toggle would only paper over
a real runtime dependency. The reverse is not true — secrets-manager is
useful by itself (e.g., for cross-namespace secret propagation in a
cluster that hosts other Educates-adjacent tooling), so it stays
standalone.

### Runtime chart never creates TLS or CA Secrets

**Date:** 2026-04-27.
**Decision:** The `educates-training-platform` chart and its subcharts
do not create wildcard TLS Secrets or CA Secrets from inline value
fields. They only reference such Secrets by name (e.g.,
`ingress.tls.secretName`, `caTrust.secretName`). **Why:** v3 supported
inline `tls.crt`/`tls.key` and `ca.crt` value fields and synthesised the
backing Secrets at install time. That feature is being retired in v4 —
TLS and CA material are managed by the v4 operator (in Managed mode) or
declared by the user as pre-existing Secrets (in Inline mode), per
`EducatesClusterConfig`. The runtime chart only consumes the resulting
Secrets by name, never materialises them. Standalone chart users who
have raw cert/key material must create the Secret themselves before
`helm install`.

### Bundled Kyverno policies: two-path layout (`clusterPolicies` + `workshopPolicies`)

**Date:** 2026-04-29 (revised same day after closer reading of v3).
**Decision:** The session-manager subchart ships v3-vendored Kyverno
policies on **two independent paths**, mirroring v3's split between
`01-clusterpolicies.yaml` and `06-secrets.yaml`:

- **`bundledKyvernoPolicies.clusterPolicies`** (default `true`) —
  cluster-wide ClusterPolicy resources installed directly by the chart
  from `files/kyverno-policies/cluster-policies/{baseline,restricted}/`.
  Both Pod Security Standards profiles are installed unconditionally
  when this is on; workshops do not pick a profile, so all must be
  present. Default action is `Audit` from the upstream YAMLs.
- **`bundledKyvernoPolicies.workshopPolicies`** (default `true`) —
  operational best-practices + the Educates-internal
  `require-ingress-session-name`, concatenated into the
  `kyverno-policies.yaml` key of the `educates-config` Secret.
  session-manager reads the stream and clones each rule per workshop
  environment with a namespace selector added.

User extras live in `.Values.additionalKyvernoPolicies` (a map with
two list-valued keys, `clusterPolicies` and `workshopPolicies`,
mirroring the `bundledKyvernoPolicies` toggles). The chart applies
`additionalKyvernoPolicies.clusterPolicies` cluster-wide alongside
the bundled set, and appends
`additionalKyvernoPolicies.workshopPolicies` to the workshop-policies
Secret feed. This is a **net-new feature vs. v3** — v3 had no
operator-managed path for users to install additional Kyverno
ClusterPolicies; admins applied them out-of-band after install.
Bringing them into the chart values keeps the platform configuration
in one place and lets users version and review their policy
extensions alongside everything else.

**Why the split, and why not collapse them:** the first iteration of
this decision used a single `profile` selector + `operationalPolicies`
toggle, all bundling into the Secret feed. That was wrong: the v3
baseline+restricted policies are applied **cluster-wide** at install
time (by `01-clusterpolicies.yaml`), not per-workshop. Workshops can't
choose a profile — they can only exclude individual rules from the
per-environment feed. Replicating v3's split is the only way to preserve
both behaviours.
**Reconsider trigger:** if the policy bundle starts churning faster
than the runtime, or we add other operational policy sources (e.g.,
admission-controller policies, Trivy admission rules) and the bundle
becomes operationally distinct from session-manager, split it into a
dedicated subchart at that point. Likely natural alongside Phase 4
operator work, when the runtime may also gain the ability to read
policies from labelled Secrets — that change would let a sibling
subchart populate its own Secrets without the Helm cross-subchart
composition problem.

### Image references exposed as repository + tag

**Date:** 2026-04-27.
**Decision:** Every container image reference in the runtime chart is
exposed in values as `image.repository` plus `image.tag` (or an
equivalent splittable shape). **Why:** This is the shape `helm dt
wrap`/`unwrap` and similar relocation tools expect. Locking the shape now
keeps the air-gapped path open without committing to a specific
relocation tool (open item #4). No digest pinning at chart level for
v0.1.0; pinning happens via published values files at release time.

### session-manager values are typed and standalone-friendly  *(supersedes "Runtime chart values shape is operator-driven, not v3-driven")*

**Date:** 2026-04-30.
**Decision:** The `session-manager` subchart exposes its inputs as a
typed, schema-validated values surface (see
`docs/architecture/session-manager-chart-values.yaml` and the
matching JSON schema). The well-known runtime fields — `clusterIngress`,
`clusterSecurity`, `workshopSecurity`, `imageRegistry`, `imageVersions`,
`trainingPortal`, `sessionCookies`, `clusterStorage`, `clusterRuntime`,
`clusterNetwork`, `dockerDaemon`, `workshopAnalytics`, and
`websiteStyling` — are top-level, typed values. The opaque `config:`
map remains as an escape hatch deep-merged on top of the typed-derived
runtime config; new fields land there first and get promoted to typed
values in a subsequent release. SecretCopiers for ingress TLS/CA are
auto-derived from `clusterIngress.{tls,ca}CertificateRef.namespace`
when those refs target a foreign namespace; the explicit
`secretPropagation.upstream.ingressTLS/ingressCA` knobs from the
earlier shape are gone.

**Why this supersedes the earlier decision:** The earlier framing was
"the operator passes typed values, so the chart just needs to accept
what the operator emits — don't mirror v3." That gave us a chart with
one well-known runtime field (`clusterIngress`) typed and the rest
flat-out opaque under `config:`. The development-plan note for the
runtime chart explicitly calls it "what the Educates project will
publish ongoing as the canonical Helm install for the runtime,"
including for users who don't want the operator. Standalone users
were therefore expected to write opaque YAML against the v3 schema
from memory, *and* duplicate the same TLS/CA inputs in two different
forms (`config.clusterIngress.tlsCertificateRef` and
`secretPropagation.upstream.ingressTLS`). Both audiences — the
operator and the standalone user — are better served by a single
typed surface.

The chart-side translation also lets us decouple presentation casing
from runtime casing: chart values use PascalCase enums (`Kyverno`,
`PodSecurityStandards`, `OpenShiftSCC`, `None`) per Kubernetes
convention, while the runtime config blob continues to use lowercase,
unchanged. No runtime change was required.

**Consequences to be aware of:**
- The chart fails fast at template time if `clusterIngress.domain` is
  empty, instead of silently falling through to the runtime's
  `educates-local-dev.test` default. This is a deliberate change — the
  default existed for the runtime's own development; production
  installs should never hit it.
- `additionalProperties: false` on every typed block in the schema
  catches typos, but means new runtime fields land in `config:` until
  promoted. Plan for cross-version drift: runtime adds a field →
  release N exposes it via `config:` → release N+1 promotes it to a
  typed key.
- Helm injects `enabled` and `global` into subchart values; both are
  whitelisted in the schema.
- The `imagePullSecrets` schema item is `[{name: ...}]` (PodSpec
  shape), not `[string]` — it flows directly into the Deployment's
  PodSpec, so the standard k8s shape applies.

**Reconsider trigger:** if the operator ever needs values the
standalone user wouldn't (or vice versa) and the typed surface starts
forking by audience, split presentation: keep the typed shape as the
canonical, expose a thinner operator-only surface that derives from
it. Not anticipated for v4.

### Image tags derive from `Chart.appVersion`; `imageVersions` defaults via helper, merged by name

**Date:** 2026-04-30.
**Decision:** The bundled Educates runtime version is `Chart.appVersion`
in `Chart.yaml` — used as the default tag for the chart's own pods
(session-manager, pause-container) AND for the Educates-published
entries in the chart's default `imageVersions` set. The default
`imageVersions` list (mirroring v3's
`carvel-packages/installer/config/images.yaml`) is built in the
`session-manager.imageVersions` template helper, with Educates-published
tags derived from `Chart.appVersion` and upstream pins
(`docker-in-docker`, `loftsh-*`, `debian-base-image`) hard-coded to
specific tags. User-supplied entries in `.Values.imageVersions` are
merged BY NAME — an override replaces just the matching default, other
defaults pass through, names not in the default list are appended.
The `Chart.appVersion` field is currently `"3.7.1"` (the v3 runtime we
ship against) while `Chart.version` is `4.0.0-alpha.1` (the chart
package); they may diverge if a chart-only patch ships, but normally
move in lock-step at release time.

**Why `Chart.appVersion`:** It's the standard Helm pattern — `appVersion`
denotes the bundled application's version, `version` denotes the chart
package. Using `appVersion` for runtime tags means a release process
that bumps `Chart.appVersion` automatically updates every runtime
image reference; no parallel knob to keep in sync. Tying runtime tags
to `Chart.version` instead, or auto-injecting it without distinction,
silently broke session-manager's spawned children (training-portal,
base-environment, etc.) with `ErrImagePull` against non-existent
`4.0.0-alpha.1` images.

**Why `imageRegistry` is the prefix knob:** A chart user working
against a fork or a locally-built registry should be able to redirect
every Educates-image reference with one knob. `imageRegistry.host` /
`.namespace` (defaulting to `ghcr.io` / `educates`) compose the prefix
for: the chart-pod (when `image.repository` is empty), the pause image
(when `imagePuller.pauseImage.repository` is empty), and the Educates-
published entries in the `imageVersions` helper. Upstream pins
(`docker-in-docker`, `loftsh-*`, `debian-base-image`) are NOT
relocated by `imageRegistry` — those are public upstream images that
mirror under different names; relocate them via per-entry
`imageVersions` overrides instead.

**Why a helper, not a populated `values.yaml` default:** Chart users
typically don't need to see the full image inventory in their values
file — they just want overrides for the entries they're changing
(airgap, JDK variant, mirror). Per-key merge means a single override
doesn't require copying the whole list. The helper is the documented
inventory; the values file stays focused on the overrides.

**Why per-key merge instead of Helm's default list-replacement:** v3's
UX required overriding the entire `imageVersions` list to change one
entry. Per-key merge in the helper is strictly better — chart users
override only what they need and inherit the rest, including any new
entries the chart adds in future releases.

**Consequences to be aware of:**
- A release that bumps the runtime updates `Chart.appVersion` in the
  umbrella chart and the four subchart `Chart.yaml`s. Standard Helm
  release process; no parallel values-file edit needed.
- The full default image inventory lives in
  `templates/_helpers.tpl::session-manager.imageVersions` — keep this
  in sync with `session-manager/handlers/operator_config.py`'s
  `image_reference()` short-name list when adding new runtime images.
- A user override with a `name` not in the helper's defaults is
  appended (forward-compat). A user override matching a default `name`
  replaces just that entry's `image`.
- `imageRegistry.host` / `.namespace` affect ONLY images that fall
  through to the runtime's `image_reference()` — i.e., images NOT in
  the merged `imageVersions` list. For airgap relocation of the
  helper-defaulted set, override the `image:` field of the relevant
  entries via `.Values.imageVersions`.

**Reconsider trigger:** if the helper-default list grows large enough
that surfacing it in `values.yaml` is more useful than the per-key
override UX, revisit. Or if `Chart.appVersion` and the runtime version
need to diverge (a chart that bundles multiple runtime versions for
selection at install time), promote the runtime version to a typed
value at that point.
