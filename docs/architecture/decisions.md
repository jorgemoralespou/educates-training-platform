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

### Cross-cutting values promoted to umbrella `global:`; subchart-local fall-back

**Date:** 2026-04-30.
**Decision:** `imageRegistry`, `clusterIngress`, and `clusterSecurity`
are deployment-scope values that should be set once and consumed
consistently across every subchart that renders affected resources. They
move to the umbrella's `global:` block, propagated by Helm to every
subchart as `.Values.global.<key>`. Each subchart retains a local block
of the same name with sensible defaults (e.g., `imageRegistry.host:
ghcr.io`); helpers deep-merge the umbrella global over the subchart
local, with globals winning per-leaf where set. Subcharts remain
independently installable: a standalone `helm install lookup-service`
just sets the values at the top level instead of under `global:`.

**Why globals over per-subchart duplication:** Under the umbrella, a
single `global.clusterSecurity.policyEngine: OpenShiftSCC` now triggers
SCC ClusterRoleBindings in both session-manager and secrets-manager.
A single `global.imageRegistry: { host: my-fork, namespace: org }`
redirects every chart-pod, pause image, and runtime-children entry. A
single `global.clusterIngress.{tls,ca}CertificateRef` drives both the
session-manager auto-derived SecretCopier and (in step 2) the lookup-
service Ingress + chart-rendered ca-trust-store init container in both
subcharts. Without globals each user would have to set the same value in
multiple subchart blocks and keep them in sync.

**Why retain subchart locals (not globals-only):** Helm subcharts are
independently installable by design. A standalone `helm install
session-manager` user shouldn't have to know the umbrella's `global:`
convention; they should write `clusterIngress: { domain: ... }` at the
top level of their values file like any other chart. Globals are a
multiplier when the umbrella exists; subchart locals are the default
shape when it doesn't.

**Why deep-merge with globals winning:** The merge runs
`mergeOverwrite local global`: each leaf in the global block replaces
the matching leaf in the subchart local; unset globals leave subchart
locals intact. So a user can set `global.imageRegistry.host: my-fork`
without also having to set `namespace`; the subchart-local `namespace:
educates` passes through. Pathological case: an explicit empty string
in a global key (e.g., `global.imageRegistry.host: ""`) overrides the
local — document this as "explicit unset is still an override."

**Validation:** subchart schemas can no longer require keys that may
come from globals. The session-manager schema drops `clusterIngress` and
`clusterSecurity` from its top-level `required` list; helpers do the
post-merge `fail` instead. Subchart-local `clusterIngress.domain`'s
`minLength: 1` constraint is also relaxed for the same reason. The
helper enforces non-empty resolved values at template time.

**Consequences to be aware of:**
- Helm's globals propagation only works under an umbrella chart. A
  user installing a subchart standalone gets the local defaults; their
  values file uses top-level keys, not `global:`.
- The local fall-back means there are two valid paths to set the same
  thing. Document that umbrella users should prefer `global:` and only
  put cross-cutting values in subchart blocks for per-subchart
  overrides (atypical).
- Helpers across subcharts duplicate the merge logic (~10 lines each).
  Acceptable at this scale; revisit a library chart if duplication
  exceeds three places of substantial helper logic.

**Reconsider trigger:** if the umbrella is dropped (operator builds
each component from individual chart installs without the umbrella
wrapper), or if the duplicated helpers grow significantly, extract a
shared library chart that all subcharts depend on.

### node-ca-injector is its own subchart, not a flag inside session-manager

**Date:** 2026-04-30.
**Decision:** The cluster-node CA injection feature lives in its own
subchart (`installer/charts/educates-training-platform/charts/node-ca-injector/`),
sibling to session-manager / lookup-service / secrets-manager / remote-
access. The toggle is the umbrella's `node-ca-injector.enabled: false`
(default off), via Helm's standard subchart-condition mechanism. The
subchart consumes `global.clusterIngress.caCertificateRef` (with
subchart-local fall-back for standalone) and bails fast at template
time if the resolved CA ref is empty. The earlier-considered
`session-manager.clusterIngress.caNodeInjector.enabled` field is
removed entirely from the values surface.

**Why a separate subchart:**

- *Source layout match.* `node-ca-injector/` already exists as a
  top-level Go module with its own Dockerfile in this repo. The chart
  layout now mirrors the source.
- *Lifecycle independence.* It's a privileged per-node DaemonSet plus
  a controller Deployment, with its own image, RBAC, and operational
  story. Nothing about it logically belongs to session-manager's
  release.
- *Mirrors the remote-access precedent.* remote-access is also a
  small, optional, single-purpose subchart with its own toggle.
  node-ca-injector fits the same shape exactly.
- *Cleaner toggle UX.* `node-ca-injector.enabled: true` at the
  umbrella is more discoverable than a nested
  `global.clusterIngress.caNodeInjector.enabled: true`.
- *Standalone install.* Someone running v3 elsewhere who wants
  containerd-level CA trust on a new cluster can `helm install
  node-ca-injector` alone with just a CA Secret reference — they don't
  need the rest of the runtime.

**What it renders (mirroring v3's `07-node-ca-injector.yaml`):**

- `ServiceAccount` `node-ca-injector` in the release namespace.
- `ClusterRole`/`ClusterRoleBinding` `educates-node-ca-injector`
  granting `get/list/watch` on Ingresses (controller watches them to
  derive the registry-host list).
- `Role`/`RoleBinding` `node-ca-injector` granting full ConfigMap
  management in the release namespace (controller writes the
  `educates-registry-hosts` ConfigMap; DaemonSet pods mount it).
- `Deployment` `node-ca-injector-controller` (1 replica, runs
  `controller` subcommand).
- `DaemonSet` `node-ca-injector` (privileged, runs `sync` subcommand,
  mounts the CA Secret + the hosts ConfigMap + hostPath
  `/etc/containerd/certs.d`).
- `SecretCopier` auto-derived when the CA ref's namespace is foreign.

**Relationship to the per-pod ca-trust-store init container:** these
are two complementary mechanisms.

- *Per-pod init container* (in session-manager and lookup-service
  Deployments) writes the CA into `/etc/pki/ca-trust` *inside the
  pod*. Targets specific pods that need to verify TLS against the
  private CA from inside their own process tree.
- *node-ca-injector* writes containerd registry-CA configuration to
  `/etc/containerd/certs.d` *on the host node*. Targets the
  container runtime itself — image pulls from registries fronted by
  the private CA, including pulls performed by pods we don't render
  ourselves (third-party operators, kubelet, docker-in-docker workers
  inside workshop sessions).

Both can be enabled independently; both consume the same
`global.clusterIngress.caCertificateRef`.

**Why disabled by default:** the DaemonSet runs privileged on every
node and writes host filesystem state (`/etc/containerd/certs.d`).
Defaulting it to off matches v3's behaviour and avoids surprising
chart users who don't have a private CA.

### Schema validation split: umbrella validates the umbrella + globals; subcharts validate themselves

**Date:** 2026-05-03.
**Decision:** The umbrella chart has its own `values.schema.json`
focused on validating the cross-cutting `global:` block and the
top-level shape (subchart toggles, no unknown keys). It does NOT
re-validate each subchart's full values surface — every subchart
ships its own `values.schema.json` and Helm validates each
`.Values.<subchart>` block against the matching subchart schema
independently. Subchart blocks at the umbrella level are typed as
opaque `{ "type": "object" }`.

**Why split this way:**

- *No duplication.* Subchart shapes already live in subchart schemas;
  copying them into the umbrella would mean two places to update on
  every change.
- *Catches the typos that fall through everywhere else.* Subchart
  schemas treat `global` as opaque (correctly — they shouldn't
  dictate the umbrella's contract). That meant a typo like
  `global.clusterSecuirty.policyEngine: Kyverno` produced no error;
  every subchart fell back to its local `clusterSecurity` defaults
  and the user's intended override was silently dropped. The
  umbrella schema closes this gap: `additionalProperties: false` on
  every level of `global:` rejects the misspelling at template time.
- *Catches unknown top-level keys.* Same `additionalProperties:
  false` discipline at the umbrella root catches a user who
  accidentally writes e.g. `sesion-manager:` (typo in subchart name)
  — Helm would otherwise treat it as inert top-level data.

**Verified:** all three classes of typo trigger a schema error at
`helm template` time:
- misspelled global key (e.g., `global.clusterSecuirty`),
- misspelled global nested field (e.g.,
  `global.imageRegistry.namespece`),
- unknown top-level key (e.g., `sesion-manager`).

**Reconsider trigger:** if global shapes start churning faster than
the chart release cadence, or if cross-subchart validation invariants
emerge that no single schema can express (e.g., "if X global is set
then Y subchart toggle must be true"), revisit. Helm doesn't natively
support cross-chart schema invariants — those would need a CI lint
rather than a schema.

### `imageRegistry` is a development override; publish-time defaults live in Chart.yaml annotations

**Date:** 2026-05-05.
**Decision:** The user-facing image-registry knob is renamed to
`development.imageRegistry` (subchart-local) and
`global.development.imageRegistry` (umbrella). It is **empty by default**
and intended to be left empty in normal use. The publish-time default
registry — what consumers of an upstream/fork chart see for chart-pod
+ runtime-children image refs — is sourced from Chart.yaml annotations:

```yaml
annotations:
  educates.dev/image-registry-host: "ghcr.io"
  educates.dev/image-registry-namespace: "educates"
```

The release workflow updates these annotations per fork (one `yq -i`
call per Chart.yaml). Each subchart's helper resolves the effective
prefix as: `development.imageRegistry` (user override) →
`global.development.imageRegistry` (umbrella global) → Chart.yaml
annotation → `fail`.

The `development.imageRegistry` knob has TWO simultaneous effects when
set:

1. **Chart-rendered + runtime-spawned image refs** resolve against
   `{host}/{namespace}` instead of the annotation defaults (chart pod,
   pause container, every Educates-published entry in the
   `imageVersions` helper).
2. **The runtime config blob's `imageRegistry` field** is emitted with
   the same `{host}/{namespace}`. The runtime exports
   `IMAGE_REPOSITORY={host}/{namespace}` into workshop sessions, so
   workshop YAMLs containing `$(image_repository)/<name>:<version>`
   placeholders resolve there.

When `development.imageRegistry` is empty (normal use), effect (2)
emits an empty `imageRegistry` block into the runtime config — the
runtime falls back to `registry.default.svc.cluster.local`
(`session-manager/handlers/operator_config.py:35`), the in-cluster
Service routing to the local development registry. Released workshops
have fully-qualified content image refs in their YAMLs without
`$(image_repository)` placeholders (the workshop's own publish workflow
substitutes them at workshop-publish time), so the in-cluster fallback
only matters for the local-dev workflow.

**Why two helpers, not one:**

- `resolvedImageRegistry` (with annotation fallback) → used to compose
  chart-rendered refs. Always returns a populated value in normal use
  (annotation provides it).
- `resolvedDevelopmentImageRegistry` (NO annotation fallback, user/global
  only) → used to compose the runtime config blob's `imageRegistry`
  field. Returns empty in normal use, populated when the user overrides.

This split is the whole point: chart pods need refs that work without
user input (annotations supply them); the runtime config's
`imageRegistry` field deliberately stays empty in normal use to avoid
silently breaking the local-dev workflow when a user later runs
`educates publish-workshop` against a "normal" install.

**Why this supersedes the earlier `imageRegistry` decision:**

The previous shape (`imageRegistry: { host: "ghcr.io", namespace:
"educates" }` populated in subchart `values.yaml`) tangled three
concerns:

1. Where chart pods + runtime children pull their images from (a
   publish-time concern — depends on which fork shipped the chart).
2. The `IMAGE_REPOSITORY` env var workshops see (a per-install concern
   — should be empty for the in-cluster fallback in normal use).
3. The user-facing override knob (a relocation/dev concern).

Conflating (1) and (2) silently broke the dev workflow on installs
that left the populated default in place — a workshop with a
`$(image_repository)` placeholder would resolve to `ghcr.io/educates/...`,
not `localhost:5001`, and the pull would fail. v3 avoided this with
its `imageRegistry: ""` default by-design schema (build-time refs were
baked into the OCI bundle separately). The new split restores that
separation: annotations carry (1), `development.imageRegistry` carries
(3), the runtime config carries (2) only when (3) is set.

**Why the `development:` namespace:**

Renaming from `imageRegistry` to `development.imageRegistry` makes the
intent explicit. The block is signal-named — anyone reading the values
file sees "this is for development" before they read the comment.
Mirrors the explicit framing in v3's schema comment
(`carvel-packages/installer/.../00-schema.yaml:28-35`).

**Consequences to be aware of:**

- The release workflow MUST update Chart.yaml annotations per fork
  (alongside `appVersion`). Without that step, a fork's published chart
  points at upstream `ghcr.io/educates/...` rather than the fork's
  registry. Document this in the release runbook.
- Local-dev users set `global.development.imageRegistry` (umbrella) or
  `<subchart>.development.imageRegistry` (standalone install) to point
  at their local registry. `educates publish-workshop` and the workshop
  runtime's `$(image_repository)` resolution then both honour the same
  setting.
- Helpers across the four image-rendering subcharts duplicate the
  annotation-fallback logic (~10 lines each). Same scale of duplication
  as the existing `resolvedImageRegistry` helpers; revisit only if a
  library chart becomes warranted.

**Reconsider trigger:** if Chart.yaml annotation editing turns out to
be brittle in the release workflow (e.g., `yq` formatting drift), move
the publish-time defaults to a chart-bundled `published-defaults.yaml`
file loaded by the helper instead.

### Operator project strips kubebuilder's `config/` kustomize tree; controller-gen targets the Helm chart directly

**Date:** 2026-05-06.
**Decision:** The v4 operator project at `installer/operator/` is
bootstrapped with `kubebuilder init` + `kubebuilder create api`, but
the generated `config/` kustomize tree is removed. `controller-gen`
is configured (via the operator's `Makefile`) to write CRDs and RBAC
manifests directly into the operator Helm chart at
`installer/charts/educates-installer/crds/` and
`installer/charts/educates-installer/templates/rbac/` respectively.
**Why:** The Helm chart is the artefact users consume — both for
imperative `helm install` and declarative GitOps. Maintaining a
parallel kustomize representation under `config/` would mean two
sources of truth for the same manifests, with a copy/transform step
between them. Stripping `config/` makes the chart the canonical
output and `make manifests` the single regeneration step.
**Consequences to be aware of:**
- `kubebuilder edit` and other kubebuilder subcommands that assume
  the standard project layout will not work as documented. Project
  edits go through `Makefile` targets and direct file edits instead.
- The operator's `Makefile` carries explicit `controller-gen` invocations
  with chart-path output flags, replacing kubebuilder's default ones.
- `kubebuilder` generators (`kubebuilder create webhook` etc.) may
  still drop files into `config/` if invoked; treat this as a chore to
  clean up, not a recurring concern (we don't re-scaffold often).
**Reconsider trigger:** if we end up needing the `config/` tree for
something kubebuilder-native (e.g., webhook bootstrap), revisit and
either (a) re-introduce it as a build intermediate gitignored from
the chart output, or (b) generate webhook manifests by hand into the
chart.

### Operator CRD types: spec is full r3 from Phase 0; status grows alongside reconcilers

**Date:** 2026-05-06.
**Decision:** Phase 0 lands the *full* r3 spec surface for all four
CRDs (EducatesClusterConfig, SecretsManager, LookupService,
SessionManager) as Go types with kubebuilder validation and default
markers. The status surface, however, is minimal at Phase 0:
`observedGeneration`, `phase`, and `conditions` only. Richer status
fields described in the r3 doc (`ingress`, `policyEnforcement`,
`imageRegistry`, `bundledChartVersions`, `deploymentRef`, `url`,
`installedVersion`, etc.) are added in the phase whose reconciler
first produces them.
**Why:** Spec is the user-facing API contract — locking it early
catches drift in CI and lets users (and the migration tool) write
against it before reconcilers exist. Status, by contrast, is
implementation-coupled: a status field with no producer is dead API
surface that drifts from r3 silently. Growing status alongside
reconcilers keeps the Go types and the controller behaviour
consistent.
**Consequences to be aware of:**
- `docs/architecture/educates-crd-draft-v1alpha1-r3.md` remains the
  *target* status spec; the Go types catch up phase by phase. When a
  status field lands in Go, the corresponding doc section is the
  source of truth for shape and semantics.
- Conditions follow Kubernetes convention (`Ready` plus PascalCase
  domain-specific types). The list of condition types grows by phase
  — Phase 0 ships only `Ready`; phase-specific conditions
  (`ValidationSucceeded`, `CertificatesReady`, `ClusterConfigAvailable`,
  etc.) are added with their producers.
**Reconsider trigger:** if a downstream consumer (CLI, GitOps tooling)
needs to read a status field before its reconciler exists, promote
that specific field early with a clear "not yet populated" semantics
and document it.

### Operator image: local `make docker-build` only at Phase 0; publish wiring deferred to Phase 6

**Date:** 2026-05-06.
**Decision:** At Phase 0, the operator chart's `image.repository` /
`image.tag` defaults are a local-dev placeholder
(e.g. `ghcr.io/educates/educates-operator:dev`), the operator
`Makefile` ships a `docker-build` target that builds and tags that
image locally, and `make smoke-test` uses it directly against a kind
cluster (loading the image into kind nodes via `kind load`). No
publish-time annotation pattern, no release workflow integration, no
CI image build — those land in Phase 6 alongside the broader release
process work.
**Why:** Phase 0 is the skeleton phase. The image only needs to exist
for the local smoke test; replicating the runtime chart's
annotation-driven publish-time defaults would be premature scaffolding
for a release process that hasn't been built yet. Phase 6 designs
the operator's release flow as a whole and adds annotations + a
release workflow at that point.
**Consequences to be aware of:**
- A user trying to install the chart from a clone without first running
  `make docker-build` + `kind load` will see `ImagePullBackOff`. The
  chart's README and CLAUDE.md should call this out for as long as
  Phase 0's image story is in effect.
- When Phase 6 introduces publish-time defaults, mirror the runtime
  chart's `educates.dev/image-registry-host` /
  `-namespace` Chart.yaml annotation pattern (see "`imageRegistry` is
  a development override" decision) so the two charts share a single
  release-workflow contract.
**Reconsider trigger:** if Phase 1 or 2 work needs the operator image
in a published location (e.g., for an external test cluster), promote
the publish wiring earlier rather than working around it.

### ClusterIssuer access via `unstructured.Unstructured`; ClusterIssuer watch deferred to Phase 2

**Date:** 2026-05-06.
**Decision:** Phase 1 Inline validation reads ClusterIssuers via
`unstructured.Unstructured` against
`cert-manager.io/v1`/`ClusterIssuer`, NOT via vendored cert-manager
Go types. The reconciler's `Watches()` setup intentionally omits a
ClusterIssuer watch — it watches Secrets (cache-restricted to the
operator namespace) and IngressClasses (cluster-scoped) only.
**Why unstructured for reads:** Phase 1 only needs existence + the
`Ready` status condition. Vendoring `cert-manager.io` Go types pulls
in the cert-manager module (and a few hundred transitive packages)
for two field reads. Unstructured is cheap, requires no go.mod
addition, and cleanly degrades when the cert-manager CRD is absent
(`meta.IsNoMatchError` is surfaced as a validation error, not a
reconcile retry).
**Why no ClusterIssuer watch yet:** an unstructured `Watches()`
needs the GVK resolvable at cache-startup; if the cert-manager CRD
is absent, manager startup fails hard. Inline-mode users may run
without cert-manager (using `StaticCertificate`-style external TLS
or no TLS issuer at all), so making the watch unconditional in
Phase 1 would block startup on installs that don't need it. CRD
discovery at startup is a workable alternative but adds startup
complexity for a behaviour that's only relevant when a
ClusterIssuer is actually referenced.
**What this means in practice:** In Phase 1, an Inline user whose
referenced ClusterIssuer transitions from Ready=False to Ready=True
will not see status flip without a spec touch. Acceptable Phase 1
limitation given the scope cut.
**Phase 2 promotion:** Phase 2 Managed mode unconditionally installs
cert-manager via the Helm SDK, so the CRD is guaranteed present.
Phase 2 vendors `cert-manager.io/v1` Go types (the operator interacts
with them more deeply: ClusterIssuer creation, Certificate creation +
Ready waiting) and adds the ClusterIssuer watch unconditionally at
that point. The Phase 1 unstructured read in
`internal/controller/config/validator.go::checkClusterIssuer` gets
refactored to typed access in the same change.
**Reconsider trigger:** if Phase 1 users complain about manual
re-trigger for ClusterIssuer drift before Phase 2 lands, add CRD
discovery at startup and conditionally install the watch — costs
~30 lines.

### Phase 1 reconciler defensive guard for mode==Inline && spec.inline==nil

**Date:** 2026-05-06.
**Decision:** The Phase 1 EducatesClusterConfig reconciler carries a
defensive guard: when `spec.mode == Inline` but `spec.inline == nil`,
mark the resource Degraded with the message
`spec.inline: Inline mode requires spec.inline to be set`. The CEL
exclusivity rule that lands alongside this code (Phase 1 step 1)
already makes the case structurally unreachable through the apiserver,
so the guard is belt-and-suspenders.
**Why guard anyway:** CEL rules can be bypassed in pathological
scenarios (a controller writing directly to etcd; a future webhook
configuration disabling CEL evaluation; CRD reapply during upgrade
when a new rule is added but old objects predate it). The guard is
~5 lines and produces a much better failure mode than a nil-pointer
panic.
**Reconsider trigger:** when admission webhooks land (post-v1alpha1)
and CEL bypass becomes infeasible, the guard becomes pure dead code
and can be removed. Flagged in the development plan's Phase 1 carry-
over list for v1beta1 review.

### No `educates-cluster-services` umbrella chart; the operator is the sole installer

**Date:** 2026-05-06.
**Decision:** Cluster services (cert-manager, Contour, Kyverno,
external-dns) are installed exclusively by the v4 operator via Helm SDK
calls against vendored upstream charts. There is no, and there is no
plan for, an `educates-cluster-services` umbrella chart that would offer
a standalone Helm-only install path for these services. The only
umbrella chart in the v4 design remains `educates-training-platform`,
which packages the Educates runtime components.
**Why:** A standalone cluster-services chart would duplicate what
`mode: Managed` already does (single action installs all four services)
without serving a real audience: BYO users go `mode: Inline` and bring
their own cluster services, so they don't want a chart from us either.
The operator is the single integration point — it owns ordering,
readiness checks, finalizer-driven uninstall, and ClusterIssuer/
Certificate creation, none of which a plain Helm install can provide.
This consolidates earlier statements in `decisions.md:73-83` ("Subcharts
do not vendor upstream charts" — runtime subcharts package only Educates
components; cluster services are operator-installed) into a positive
decision so the question doesn't recur.
**Reconsider trigger:** if a concrete user demand emerges for installing
cluster services via plain Helm without the operator. Until then, the
operator is the only consumer and the only installer.

### Vendored upstream charts live as tarballs at `installer/vendored-charts/`

**Date:** 2026-05-06.
**Decision:** Upstream Helm charts the operator drives (cert-manager
first, then Contour, Kyverno, external-dns) are vendored into the
repository as tarballs under `installer/vendored-charts/<name>-<version>.tgz`.
A `make vendor-charts` target pulls them from upstream and verifies
checksums; the tarballs themselves are checked in. The operator's Helm
SDK loader reads chart bytes from this path at runtime (the tarballs are
embedded into the operator image at build time, or — during
development — read from disk).
**Why tarballs over unpacked directories:** Helm SDK's `loader.Load`
accepts both, but tarballs are atomic, content-addressable, and don't
invite ad-hoc local edits that drift from the upstream chart. Diff
review of a chart bump is `git show` of the tarball replacement plus
the README update, not a directory-wide diff that mingles real changes
with reformatting noise. The intent is "we ship the upstream chart
unmodified"; the tarball makes that intent enforceable.
**Why vendor at all:** Per development-plan open item #2, every chart
update must be a deliberate, testable change — not whatever the
upstream registry happens to serve at install time. Vendoring also
enables air-gapped installs without runtime registry access, and gives
us a single point of control for image-relocation rewrites in Phase 6.
**Layout consequence:** With no `educates-cluster-services` umbrella
(see preceding entry), `installer/vendored-charts/` has a single
consumer — the operator — so there is no need to expose it as a Helm
`file://` repository. If a future consumer appears, the same on-disk
location can be referenced via `repository: file://...` from a
`Chart.yaml` `dependencies` block without moving the bytes.

**Amendment — 2026-05-11.** Directory relocated from
`installer/vendored-charts/` to `installer/operator/vendored-charts/`.
Phase 2 Session 2 established that the operator is the sole consumer in
practice, and `//go:embed` (the build-time mechanism this decision
already endorsed) requires the embedded path to live inside the
embedding Go module. The substantive content of the decision — tarballs
over directories, checksum-verified, no runtime registry pulls — is
unchanged. A hypothetical future consumer can still reach the bytes via
`file://...` references; the path inside `installer/operator/` is no
more or less accessible than a sibling directory would have been.

### cert-manager CRDs are an operator install prerequisite (all modes)

**Date:** 2026-05-06.
**Decision:** Starting with Phase 2, the cert-manager.io CRDs must be
installed in the cluster *before* the v4 operator starts — including
for Inline-mode-only installs that never reference a ClusterIssuer.
Managed-mode installs satisfy the prerequisite inherently (the operator
installs cert-manager); Inline-mode-only installs must apply the
cert-manager CRDs (or full cert-manager) up front. The operator chart
documents this; chart-level enforcement (e.g., a pre-install hook that
checks for the CRD) is deferred — failure mode at startup is loud
enough.
**Why:** The reconciler now uses a typed `Watches(&cmv1.ClusterIssuer{},
...)` on the manager, and controller-runtime requires the GVK to be
resolvable at cache startup. A missing CRD causes hard manager startup
failure. Conditional CRD discovery + dynamic watch addition is workable
(~30 lines, flagged as the alternative in the Phase 1 deferral entry)
but not worth the complexity once cert-manager is a first-class
dependency for the project's primary install path. Even Inline users
typically have cert-manager already, since BYO ingress + TLS is the
common reason to use Inline.
**Reconsider trigger:** if a meaningful population of users emerges
with no cert-manager at all (e.g., StaticCertificate-only installs in
restricted environments), make the watch conditional on CRD discovery
at startup. Costs ~30 lines and an extra startup probe.
