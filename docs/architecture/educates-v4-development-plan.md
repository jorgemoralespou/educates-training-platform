# Educates v4 Installer — Development Plan

> **Status:** Living document. Update as decisions are made and phases complete.
> **Owner:** Solo work, primary developer + Claude as collaborator.
> **Target:** Educates v4 — breaking change from v3, replacing the Carvel-based
> installer with a Helm chart + Go operator while leaving the runtime
> (session-manager, secrets-manager, lookup-service, training-portal,
> workshop images) functionally unchanged.

---

## 1. What we're building, and why

### Goals

- Replace the Carvel-based installer (ytt + kbld + kapp + kapp-controller) with a Helm chart + Go operator.
- Preserve a single user-facing workflow that works identically for imperative install (CLI) and declarative install (GitOps via ArgoCD/Flux).
- Provide opinionated zero-to-hero installation while supporting Bring-Your-Own (BYO) for cluster services like cert-manager, ingress controllers, policy engines.
- Cleanly separate three configuration concerns: local laptop setup, cluster infrastructure + services, Educates-specific runtime.
- Establish a path off Carvel, which is poorly maintained and burdensome to keep current.

### Non-goals

- Re-implementing or modifying the Educates runtime (session-manager, secrets-manager, lookup-service, training-portal, workshop images, tunnel-manager).
- Backwards compatibility with the v3 configuration format at runtime. A one-shot migration tool will translate v3 configs to v4 CRs; nothing else.
- Multi-tenant Educates installations on a single cluster.
- Cross-cluster references between component CRs.

### Scope of the v4 change

- **Replaces:** `carvel-packages/installer/`, the kapp-based deploy/delete CLI commands, the YTT/kbld toolchain.
- **Adds:** A new Go operator with four CRDs, a Helm chart for the operator, a Helm chart for the Educates runtime (with three subcharts: secrets-manager, lookup-service, session-manager), thinner CLI commands wrapping these.
- **Touches but doesn't change:** The CLI's local-cluster commands (`educates local cluster create`, etc.) — they'll still work, but their internals call the new install path.
- **Leaves alone:** Everything in `session-manager/`, `secrets-manager/`, `training-portal/`, `lookup-service/`, `tunnel-manager/`, `workshop-images/`, `node-ca-injector/`, `assets-server/`, `image-cache/`. These continue to function exactly as in v3.

### Reference documents

These should live in `docs/architecture/` once the repo is set up:

- **`crd-draft-v1alpha1.md`** — the four CRD designs (EducatesClusterConfig, SecretsManager, LookupService, SessionManager). Currently at revision 3.
- **`installer-research.md`** — the survey of how kube-prometheus-stack, ArgoCD, Flux, Backstage, Crossplane, Tanzu handle multi-chart packaging. Background context for design choices.
- **This document** — the development plan.

---

## 2. Architecture summary

### High-level shape

```
User runs `helm install` (or points GitOps at it)
       ↓
educates-installer Helm chart
   - operator Deployment
   - 4 CRDs (cluster-scoped)
   - RBAC
       ↓
User (or CLI) applies CRs:
   - EducatesClusterConfig (singleton, named 'cluster')
   - SecretsManager        (singleton, named 'cluster')
   - SessionManager        (singleton, named 'cluster')
   - LookupService         (singleton, named 'cluster', optional)
       ↓
Operator reconciles each CR:
   - EducatesClusterConfig in Managed mode: installs cluster services
     (cert-manager, contour, kyverno, external-dns) via Helm SDK,
     creates ClusterIssuer + Certificate, publishes status.
   - EducatesClusterConfig in Inline mode: validates user-provided refs,
     publishes status without installing anything.
   - Component CRs read EducatesClusterConfig.status, install via the
     educates-training-platform Helm chart's relevant subchart, watch
     dependencies (e.g., SessionManager waits on SecretsManager.Ready).
       ↓
Educates runtime (unchanged from v3):
   - secrets-manager
   - session-manager + training-portal
   - lookup-service
```

### Key design decisions (already made)

| Decision | Choice |
|---|---|
| Install mechanism | Helm chart for the operator + 4 cluster-scoped singleton CRDs |
| Operator language | Go with controller-runtime / Kubebuilder |
| Cluster-service installation | Helm Go SDK calls to upstream charts (no vendoring) |
| Runtime packaging | One umbrella chart `educates-training-platform` with three subcharts |
| Configuration approach | Capability-oriented CRDs (e.g., `certificates.provider`) not config dumps |
| BYO support | Each cluster service has a `Bundled | External` discriminator |
| Mode model | EducatesClusterConfig has `mode: Managed | Inline` (immutable) |
| CRD validation | CEL for structural; reconciler for semantic; no admission webhooks in v1alpha1 |
| Default policy engine | Kyverno (cluster + workshop), explicitly opt-out |
| Image relocation | Build-time wrap with `helm dt` (or equivalent) for air-gapped; `imageRegistry.prefix` on CR for online mirroring |
| Air-gapped digest pinning | Out of scope for v1alpha1; published values files per release |
| Multi-component dependency | SessionManager requires SecretsManager.Ready; runtime check, not spec field |
| Status as interface | EducatesClusterConfig.status is the contract components consume |
| Singleton enforcement | CEL rule: `metadata.name == 'cluster'` |
| CRD versioning | Start at v1alpha1; expect breaking changes before v1beta1 |

---

## 3. Phased plan

Work proceeds in sequenced phases. Each phase has a definition of done that must be met before the next phase starts.

### Pre-phase: educates-training-platform Helm chart (1–2 weeks)

**Build this in parallel with Phase 0.** It's separable from the operator and de-risks Phase 4 substantially.

**What to build:**
- Umbrella chart `educates-training-platform`.
- Three subcharts:
  - `secrets-manager` — installs the existing secrets-manager Deployment with values for image, log level, image pull secrets, resources.
  - `lookup-service` — installs the existing lookup-service with values for ingress (hostname, TLS), image, log level, resources.
  - `session-manager` — installs session-manager, training-portal, and supporting services (assets-server, image-cache, docker-registry) with values for domain, TLS, themes, registry mirrors, image overrides.
- Dependencies declared with `condition` flags so subcharts can be enabled/disabled.
- Image refs use values placeholders so `helm dt wrap`/`unwrap` can relocate them.

**Done when:**
- `helm install educates-training-platform ./chart -f values-test.yaml` works against a kind cluster with manually-set-up cert-manager + contour + kyverno + a wildcard cert.
- All three subcharts can be enabled/disabled independently.
- The runtime works end-to-end: user can browse to the training portal, request a workshop, get a session.

**Why first:** This proves the runtime *can* be installed via Helm. It's a reality-check on the whole approach. If something about the runtime resists Helm packaging, you want to know now, not in Phase 4.

**Note:** This chart is what the Educates project will publish ongoing as the canonical Helm install for the runtime. Even users who don't want the operator can `helm install educates-training-platform`.

### Pre-phase follow-up: bundle v3 Kyverno policies into the chart  *(done — 2026-04)*

Vendored into the `session-manager` subchart in commit `8e659b5c` under
`installer/charts/educates-training-platform/charts/session-manager/files/kyverno-policies/`:

- `cluster-policies/` — applied directly as cluster-wide ClusterPolicy
  resources (mirrors v3's `01-clusterpolicies.yaml`: PSS baseline +
  restricted profiles plus operational best-practices).
- `workshop-policies/` — concatenated into the `kyverno-policies.yaml`
  key of the `educates-config` Secret; session-manager clones each rule
  per workshop environment with a namespace selector added (mirrors v3's
  `06-secrets.yaml` feed).

Source provenance is recorded in `files/kyverno-policies/README.md`.
Kyverno *engine* installation remains out of scope for the runtime
chart — that's the operator's job at cluster-services scope (Phase 2/3).

The current toggle shape (`bundledKyvernoPolicies.{cluster,workshop}Policies`,
`additionalKyvernoPolicies.{cluster,workshop}Policies`, `openshift.enabled`)
is superseded by `clusterSecurity` / `workshopSecurity` in the typed-values
follow-up below; the on-disk policy files do not move.

### Pre-phase follow-up: typed runtime-config values  *(planned, ready to start)*

**Trigger met (2026-04):** scenarios 01–06 now cover local-HTTP, TLS
wildcard, cert-manager issuer, website theme, image-pull secrets, and
additional Kyverno policies. That's enough validation surface to
refactor the values shape against without regressing tested behaviour.

**Canonical target shape:**

The full target values surface and JSON schema live as docs-of-record at:

- `docs/architecture/session-manager-chart-values.yaml`
- `docs/architecture/session-manager-chart-values-schema.json`

These are the source of truth for this follow-up. If something below
diverges from those documents, the documents win — update the plan, not
the docs.

**Problem this solves:**

The current pre-phase chart has the well-known runtime config as an
opaque map under `session-manager.config`, plus a separately-typed
`session-manager.secretPropagation` block, plus ad-hoc toggles for
Kyverno bundling and OpenShift. The same input often appears twice in
slightly different forms (e.g., the wildcard TLS Secret name +
namespace shows up both as runtime config and as secret-propagation
upstream). Standalone chart users (the audience for the pre-phase
chart per its own "Note" above) end up needing to know the v3 schema
by heart and write opaque YAML for most of the runtime config.

**What to build:**

Refactor the `session-manager` subchart `values.yaml` and JSON schema to
match the docs-of-record. Concretely, promote out of `config` and
restructure into these top-level blocks (full field list in the doc):

- `clusterIngress` — `domain` (required after merging with `.global.clusterIngress`),
  `class`, `protocol` (auto from TLS ref), `tlsCertificateRef`,
  `caCertificateRef`, `caNodeInjector.enabled`. Promoted to umbrella
  `global.clusterIngress` so lookup-service (Ingress) and session-manager
  (chart-rendered ca-trust-store init container) read a single source of
  truth.
- `clusterSecurity` — `policyEngine: Kyverno | PodSecurityStandards |
  OpenShiftSCC | None`, `additionalKyvernoPolicies[]`. Promoted to
  umbrella `global.clusterSecurity` so SCC ClusterRoleBindings in BOTH
  session-manager and secrets-manager are gated on the same value.
  Replaces the previous `bundledKyvernoPolicies.clusterPolicies` toggle,
  the per-subchart `openshift.enabled` toggles, and
  `additionalKyvernoPolicies.clusterPolicies`.
- `workshopSecurity` — `rulesEngine: Kyverno | None`,
  `additionalKyvernoPolicies[]`. Replaces
  `bundledKyvernoPolicies.workshopPolicies` and
  `additionalKyvernoPolicies.workshopPolicies`.
- `development.imageRegistry` — local-development override (subchart-
  local + umbrella `global.development.imageRegistry`). Empty by default;
  publish-time defaults come from Chart.yaml annotations
  (`educates.dev/image-registry-host` / `-namespace`, updated per-fork by
  the release workflow). When set, ONE knob redirects both (a) chart-
  rendered + runtime-spawned Educates image refs and (b) the
  `IMAGE_REPOSITORY` env var workshop sessions see for `$(image_repository)`
  content placeholder resolution. Mirrors v3's `imageRegistry` schema knob.
  Upstream pins (docker-in-docker, loftsh-*, debian-base) are NOT
  relocated; override their `imageVersions` entries directly when
  mirroring.
- `imageVersions[]` — empty by default; chart-shipped defaults are
  produced by the `session-manager.imageVersions` template helper,
  mirroring v3's `images.yaml`. Educates-published entries derive their
  tag from `Chart.AppVersion`; upstream pins (`docker-in-docker`,
  `loftsh-*`, `debian-base`) are hard-coded to specific tags. User
  values are merged BY NAME — overrides replace just the matching
  default, other defaults pass through, names not in the default list
  are appended (forward-compat). Used for airgap relocation and feature-
  image variants.
- Top-level `image`, `imagePullSecrets[]` (chart-pod pull only — distinct
  from `secretPropagation.imagePullSecretNames`), `resources`,
  `clusterAdmin` (default `false`, changed from v3 default of `true`).
- `trainingPortal.credentials.{admin,robot}` and
  `trainingPortal.clients.robot` — empty-by-default; runtime generates.
- `sessionCookies.domain`.
- `clusterStorage` — `class`, `user`, `group`.
- `clusterRuntime.class`.
- `clusterNetwork.blockCIDRs[]` — defaults to AWS metadata IPv4 + IPv6.
- `dockerDaemon` — `networkMTU`, `proxyCache.{remoteURL,username,password}`.
- `workshopAnalytics` — `google`, `clarity`, `amplitude`, `webhook`.
- `websiteStyling` — replaces flat `websiteTheme: {}` map. Structured
  inline blocks (`workshopDashboard`, `workshopInstructions`,
  `workshopStarted`, `workshopFinished`, `trainingPortal`) plus
  `defaultTheme`, `themeDataRefs[]`, `frameAncestors[]`.
- `imagePuller` — unchanged in shape (`enabled`, `pauseImage`, `prePullImages[]`).
- `secretPropagation` — `imagePullSecretNames[]` and
  `upstream.{imagePullSecrets,websiteThemes}[]`. The `upstream.ingressTLS`
  and `upstream.ingressCA` fields are **removed** — auto-derived from
  `clusterIngress.tlsCertificateRef.namespace` and
  `clusterIngress.caCertificateRef.namespace` when those differ from the
  release namespace.
- `config` — opaque escape hatch, deep-merged on top of the
  typed-derived `educates-operator-config.yaml` Secret content.

The chart composes the `educates-config` Secret from these typed values.
Auto-creates SecretCopiers for ingress TLS/CA, themes, and pull secrets
whose source namespace differs from the release namespace.

**Defaulting policy — important note:**

Be deliberate about what the chart defaults vs. what it leaves empty for
the session-manager runtime to handle:

- **Empty is correct for `trainingPortal.credentials.*` and
  `trainingPortal.clients.robot.*`** — most installs leave these unset,
  and the session-manager's `operator_config.py` already has
  `generate_password(...)` fallbacks that produce the right thing at
  runtime. The chart **must not** materialise `randAlphaNum`-generated
  values for these — they would rotate on every `helm upgrade` and break
  workshops mid-session. The session-manager owns credential generation;
  the chart only renders user-supplied values when present.
- **Sensible defaults are fine for** `clusterIngress.protocol` (auto
  `http` when no `tlsCertificateRef`, `https` otherwise — matches the
  runtime's own derivation), `clusterSecurity.policyEngine` (default
  `Kyverno` per the v4 mode decision), `workshopSecurity.rulesEngine`
  (default `Kyverno`), `clusterStorage.group` (default `1`),
  `clusterNetwork.blockCIDRs` (default AWS metadata IPv4 + IPv6),
  `clusterAdmin` (default `false`), `dockerDaemon.networkMTU` (default
  `1400`).
- **No defaults — required input** for `clusterIngress.domain`. The
  chart fails fast at template time if missing, not silently fall through
  to the runtime's `educates-local-dev.test` fallback.

**Validation:**

Add a JSON schema (`values.schema.json`) to the subchart, derived from
`docs/architecture/session-manager-chart-values-schema.json`. Helm
enforces it on `helm install`/`upgrade`/`template`, catching shape errors
before they reach the runtime.

**Done when:**

- The `session-manager` subchart `values.yaml` matches the shape in the
  docs-of-record.
- A `values.schema.json` exists in the subchart and matches the schema
  doc.
- All six existing scenarios (`01-local-http-nip-io`, `02-kind-tls-wildcard`,
  `03-kind-cert-manager-issuer`, `04-website-theme`,
  `05-image-pull-secrets`, `06-additional-kyverno-policies`) pass after
  their `chart-values.yaml` files are updated to the typed shape (no
  `config:` block needed for the cases they cover; no
  `secretPropagation.upstream.ingressTLS/ingressCA`; no
  `bundledKyvernoPolicies` / `additionalKyvernoPolicies.{cluster,workshop}Policies`
  / `openshift.enabled`).
- A new scenario exercises one or more still-opaque fields via the
  `config:` escape hatch, proving deep-merge semantics.
- `decisions.md` has an entry that supersedes the earlier "Runtime chart
  values shape is operator-driven, not v3-driven" decision, with the
  reasoning above.

### Phase 0: Foundations (1–2 weeks)  *(done — 2026-05)*

**Layout (decided 2026-05-06):**
- Operator code: `installer/operator/` — kubebuilder project, with the
  generated `config/` kustomize tree stripped. `controller-gen` writes
  CRDs and RBAC directly into the operator chart. See decisions log.
- Operator chart: `installer/charts/educates-installer/` — sibling to
  `educates-training-platform/`. Installs operator Deployment, RBAC,
  and the four CRDs (in `crds/`).
- Module path: `github.com/educates/educates-training-platform/installer/operator`,
  added to `go.work`.
- API packages: `api/config/v1alpha1/` (EducatesClusterConfig) and
  `api/platform/v1alpha1/` (SecretsManager, LookupService,
  SessionManager).

**What to build:**
- Bootstrap with `kubebuilder init --domain educates.dev` + four
  `kubebuilder create api` invocations (groups: `config`, `platform`).
  Strip the generated `config/` kustomize tree; `controller-gen` is
  pointed at the chart paths instead.
- Translate the r3 CRD draft into Go types with `kubebuilder` markers
  (`+kubebuilder:validation:*`, `+kubebuilder:default=*`).
  - **Spec: full r3 shape**, including static defaults.
  - **Status: minimal** — `observedGeneration`, `phase`, `conditions`
    only. Richer status fields (`ingress`, `policyEnforcement`,
    `imageRegistry`, `bundledChartVersions`, etc.) are added in the
    phase that introduces the reconciler producing them. Avoids dead
    API surface. See decisions log.
- Generate CRD manifests (`make manifests`) directly into
  `installer/charts/educates-installer/crds/`.
- Add CEL validation rules — Phase 0 scope is narrowed:
  - Singleton name (`self.metadata.name == 'cluster'`) on all four CRDs.
  - Mode immutability (`self.spec.mode == oldSelf.spec.mode`) on
    `EducatesClusterConfig`.
  - **Mode-field exclusivity** (Managed fields forbidden when
    `mode: Inline`, vice versa) is deferred to Phase 1 — it's
    structural validation tied to fields whose semantics Phase 1 is the
    first to exercise.
- Four trivial reconcilers wired into the manager: each logs that it
  observed the CR and returns. No status writes, no watches on
  referenced resources, no finalizers.
- Minimal RBAC in the chart: `get/list/watch/update/patch` on the four
  CRDs and their `/status` + `/finalizers` only. Watches on referenced
  Secrets/ClusterIssuers/IngressClasses are added in Phase 1 with the
  validator.
- Operator image story: `make docker-build` for local development
  only. Chart `image.repository`/`tag` defaults to a local-dev
  placeholder. Publish-time annotations (mirroring the runtime chart's
  `educates.dev/image-*` pattern) are deferred to Phase 6. See
  decisions log.
- One envtest spec exercising "valid CR is accepted; CR with
  `metadata.name != 'cluster'` is rejected" against each kind. Ginkgo,
  in-process apiserver via `setup-envtest`. Runs locally via
  `make test`; CI runs the same target.
- Local-only `make smoke-test`: `kind create` + `helm install` +
  `kubectl apply` of sample CRs + `kubectl logs` assertion that the
  reconcile log line appeared. Not in CI yet — kind-in-CI lands in
  Phase 2 alongside chart-install testing.
- CI workflow `installer-operator-ci.yaml`: `make manifests`/`generate`
  drift check, `go vet`, `go test` (envtest), `golangci-lint`.
- CLAUDE.md updates pointing at the new directory and conventions.

**Done when:**
- All four CRDs install into envtest and a kind cluster.
- Applying a valid CR triggers a log line from the operator.
- Applying a CR named anything other than `cluster` is rejected by the
  apiserver.
- Mutating `EducatesClusterConfig.spec.mode` is rejected on update.
- `make test` passes locally and in CI.
- `make smoke-test` passes locally.
- No reconcile logic beyond the log line.

### Phase 1: EducatesClusterConfig in Inline mode (2–3 weeks)  *(done — 2026-05)*

**Why Inline first:** Inline mode is pure validation and status writing — no chart installs, no orchestration. It exercises the full controller pattern (watches, status conditions, finalizers) without the complexity of cluster-service installation. Lessons here apply everywhere.

**What was built:**
- Mode-field exclusivity CEL (deferred from Phase 0): two structural
  rules — Managed-mode top-level fields forbidden when `mode: Inline`;
  `spec.inline` forbidden when `mode: Managed`.
- Extended status surface — the inter-CR contract Phase 4 components
  will read: `status.mode`, `status.ingress` (with `NamespacedSecretRef`
  for the wildcard + optional CA), `status.policyEnforcement`,
  `status.imageRegistry` (always populated, empty struct when unset).
- Operator namespace plumbing: chart Deployment downward-API env
  (`OPERATOR_NAMESPACE`) → main.go reads → reconciler struct field.
  Manager `cache.Options.ByObject` restricts the Secret cache to that
  namespace.
- RBAC for referenced resources: `get/list/watch` on Secrets,
  ClusterIssuers, IngressClasses — read-only, kubebuilder markers
  generate into the chart's ClusterRole.
- Inline-mode validator (`validator.go`): IngressClass existence;
  wildcard Secret existence + `tls.crt` + `tls.key` keys; optional CA
  Secret with `ca.crt`; optional ClusterIssuer existence + Ready.
  ClusterIssuer access is via `unstructured.Unstructured` —
  cert-manager Go types vendored in Phase 2.
- Reconcile flow: finalizer
  (`educatesclusterconfig.config.educates.dev/finalizer`) added on
  first sight, drained on delete (Phase 1 cleanup is a no-op; Phase 2
  Managed mode reuses the plumbing for chart uninstall). On Inline
  validation success: populate the status contract + flip
  `Ready=True`/`ValidationSucceeded=True`. On failure: `Phase=Degraded`,
  both conditions `False` with field-specific message
  (`<spec.path>: <reason>`).
- Watches: Secret (cache-restricted to operator namespace) +
  IngressClass (cluster-scoped), each with an `EnqueueRequestsFromMapFunc`
  returning the singleton request. ClusterIssuer watch deferred to
  Phase 2 (see decisions.md — unstructured-watch-vs-absent-CRD
  trade-off).
- 12 envtest specs (6 EducatesClusterConfig CRD validation, 3 platform
  CRD validation, 5 Inline-mode reconciler, 1 manager-driven drift
  test verifying Secret deletion flips status to Degraded).

**Done when (verified):**
- ✅ An Inline-mode `EducatesClusterConfig` reaches `Ready: True` only
  when all referenced resources exist and are valid.
- ✅ Deleting a referenced Secret causes status to flip to `Degraded`
  within seconds (envtest: `watches_test.go`).
- ✅ All integration tests pass (12 specs, config-package coverage 64.5%).

**Carried into Phase 2:**
- ClusterIssuer watch (vendor cert-manager types + add unconditional
  watch since Managed mode always installs cert-manager when bundled).
- Mode-field exclusivity CEL is structurally enforced; the reconciler
  also has a defensive guard for `mode==Inline && spec.inline==nil`
  (CEL bypass case) that becomes redundant once webhooks are added —
  flagged for review in v1beta1.

### Phase 2: One Bundled service end-to-end (3–4 weeks)  *(in progress — Session 1 groundwork done 2026-05)*

**Pick cert-manager as the first Bundled service.** It's the hardest to get right (CRDs, webhook readiness, ClusterIssuer ordering), and getting it right teaches the patterns that apply to the others. Easier services first leave you to discover hard problems later.

**Session 1 — groundwork (done 2026-05):** vendoring + Helm SDK +
typed cert-manager access landed without changing user-visible
behaviour. Concretely:

- **Decisions recorded** in `decisions.md`:
  no `educates-cluster-services` umbrella (operator is the sole
  installer); vendored upstream charts live as tarballs at
  `installer/operator/vendored-charts/<name>-<version>.tgz`; cert-manager CRDs
  are an operator install prerequisite for **all** modes (Inline-only
  too — typed watches require GVK at cache startup).
- **cert-manager Go types vendored**: `github.com/cert-manager/cert-manager v1.20.2`
  added; `cmv1` registered on the manager scheme; Phase 1's
  `unstructured.Unstructured` ClusterIssuer access in
  `validator.go::checkClusterIssuer` refactored to typed.
- **ClusterIssuer watch unconditional** on the EducatesClusterConfig
  reconciler. envtest gets the vendored ClusterIssuer CRD via
  `internal/controller/config/testdata/crds/cert-manager/`; new spec
  asserts ClusterIssuer deletion flips status to Degraded (mirrors the
  Phase 1 Secret-drift test).
- **`internal/helm` package** built around Helm SDK v4 (`helm.sh/helm/v4
  v4.1.4`): `Client.Install/Upgrade/Uninstall/Status` keyed by release
  name, `LoadArchive` for vendored tarballs, a `*rest.Config`-backed
  `restClientGetter` adapter, and a `NewMemoryClient` test factory using
  the in-memory release driver + `kubefake.PrintingKubeClient`.
- **First vendored chart**: `installer/operator/vendored-charts/cert-manager-v1.20.2.tgz`
  with `SHA256SUMS` integrity record; `make vendor-charts` (download +
  verify) and `make verify-vendored-charts` (verify on disk) targets in
  `installer/operator/Makefile`. A unit test in `internal/helm` loads
  the real vendored tarball end-to-end.

**Carry-forward — what Phase 2 still needs:**

- Embed Helm Go SDK ✅ done in Session 1.
- Chart installation pipeline:
  - Pull from upstream OCI registry (or vendored chart copy — decide which). ✅ vendored, decision recorded.
  - Render values from CR fields (with reconciler-computed defaults, e.g., replicas by provider).
  - Apply via the `internal/helm.Client.Install`.
- Real readiness check for cert-manager:
  - Deployment Available is necessary but not sufficient.
  - Verify the cert-manager webhook actually serves: `GET /apis/cert-manager.io/v1` against the API server, expect 200.
  - Optionally verify webhook ValidatingWebhookConfiguration is present and routing to the live service.
- Post-install resource creation:
  - `ClusterIssuer` (configured per CR's `acme` or `customCA` block).
  - `Certificate` (the wildcard).
- Wait for Certificate `Ready: True`.
- Status fields: `wildcardCertificateSecretRef`, `clusterIssuerRef`, `bundledChartVersions.cert-manager`.
- Conditions: `CertificatesReady`.
- Finalizer: on delete, reverse order — Certificate, ClusterIssuer, uninstall cert-manager chart.
- Integration tests against kind: full install, verify Certificate issued, delete, verify cleanup.

**Done when:**
- Applying a Managed-mode `EducatesClusterConfig` with `certificates.provider: BundledCertManager, issuerType: CustomCA` results in:
  - cert-manager installed in its namespace.
  - ClusterIssuer created and Ready.
  - Wildcard Certificate created and Ready.
  - Status reflects all of this within ~2 minutes.
- `kubectl delete educatesclusterconfig cluster` cleans up everything in correct order.
- The reconciler is tolerant of in-progress states (cert-manager installing, Certificate provisioning, etc.) — no spurious errors.

**This phase is where you learn the most.** Budget for it. Expect to discover at least one Helm SDK or controller-runtime quirk that costs you a day.

### Phase 3: Remaining cluster services (2–3 weeks)

Now that the patterns are proven, repeat for:

- **Contour** (BundledContour) — easiest. Chart, Service, IngressClass. Ordering: install before anything that uses ingress.
- **external-dns** (BundledExternalDNS) — easy. Chart, identity wiring (workload identity / IRSA annotation on ServiceAccount).
- **Kyverno** (Bundled) — medium. Has its own webhook readiness gotcha similar to cert-manager. Two engines (clusterPolicy and workshopPolicy) reference one Kyverno install.

For each: install chart, real readiness check, status fields, finalizer order. 2–4 days each in flow.

**Done when:**
- A Managed-mode `EducatesClusterConfig` matching the local kind scenario (Scenario A in the CRD draft) reaches `Ready: True` end-to-end.
- A Managed-mode config matching the GKE production scenario (Scenario B) installs all four cluster services in correct order.
- Deletion cleans up in reverse order without orphans.

### Phase 4: Component CRDs (3–4 weeks)

Order: SecretsManager → LookupService → SessionManager.

**Why this order:**
- SecretsManager is smallest, exercises the cross-CR dependency pattern (it depends on EducatesClusterConfig.Ready, but nothing depends on it, so failures are isolated).
- LookupService introduces the prefix-and-domain pattern.
- SessionManager is biggest, depends on both EducatesClusterConfig and SecretsManager, and exercises the runtime-chart-with-subchart pattern.

**For each component:**
- Reconciler reads `EducatesClusterConfig.status` (refuse to proceed unless Ready).
- For SessionManager: also check `SecretsManager.status` (refuse unless Ready).
- Install the component via the corresponding subchart of `educates-training-platform`.
  - Pass values derived from CR + cluster config status.
- Status: install status, deployment refs, URLs (for LookupService).
- Conditions: `ClusterConfigAvailable`, `Deployed`, plus component-specific.
- Finalizer with chart uninstall.

**Done when:**
- Scenario A from the CRD draft works fully end-to-end: local kind cluster install with `EducatesClusterConfig` + `SecretsManager` + `SessionManager` (+ optionally `LookupService`), all reaching Ready.
- Scenario B (GKE production with all components) works end-to-end.
- Scenario E (full BYO on OpenShift, Inline mode) works.
- Deletion order is correct: SessionManager → LookupService → SecretsManager → EducatesClusterConfig.

### Phase 5: CLI rewrite (1–2 weeks)

The CLI shrinks dramatically because the operator owns the heavy lifting.

**What to build:**
- `educates admin platform deploy` — installs the operator Helm chart + applies the four CRs derived from input config.
- `educates admin platform delete` — deletes CRs (waits for finalizers) + uninstalls Helm chart.
- `educates admin platform render` — outputs CR YAML for GitOps without applying.
- `educates admin platform values` — replaced by `educates admin platform render` + `kubectl get -o yaml` (defaults are visible in spec post-apply).
- `educates local cluster create` — internally calls the new platform deploy at the end. CLI-side concerns (kind, registry, resolver) unchanged externally.
- `educates migrate-config` — translates v3 config YAML to v4 CR YAML files. One-shot tool, not a runtime adapter.

**What to delete from the CLI:**
- All Carvel-related code: ytt invocation, kbld invocation, kapp invocation, the in-process Carvel libraries.
- The kapp-controller declarative path (replaced by Helm + CRs).

**Done when:**
- All today-supported CLI flows work against the new operator.
- Migration tool produces valid CRs for at least three real v3 configs (local kind, GKE, OpenShift).

### Phase 6: Polish and release prep (2–3 weeks)

- Documentation rewrite (current `docs.educates.dev` content for installation).
- Migration guide for v3 → v4 users.
- Helm chart distribution (OCI registry, GitHub releases).
- Image relocation pipeline: evaluate `helm dt`, decide Apache fork or alternative, integrate into release pipeline.
- Release process documentation.
- Test against real environments: GKE, EKS, OpenShift (Inline mode), local kind.

**Done when:**
- A user external to the project can install Educates v4 from published artifacts following the docs alone.
- All scenarios in the CRD draft are demonstrably working.
- A release tag exists.

---

## 4. Estimate and pacing

Solo, full-time-equivalent estimate per phase:

| Phase | Estimate |
|---|---|
| Pre-phase: runtime chart | 1–2 weeks |
| Phase 0: Foundations | 1–2 weeks |
| Phase 1: Inline mode | 2–3 weeks |
| Phase 2: First Bundled service | 3–4 weeks |
| Phase 3: Remaining cluster services | 2–3 weeks |
| Phase 4: Component CRDs | 3–4 weeks |
| Phase 5: CLI rewrite | 1–2 weeks |
| Phase 6: Polish and release | 2–3 weeks |
| **Total** | **15–23 weeks** |

Actual elapsed time will be longer because solo development isn't full-time-equivalent and there are always interruptions. Plan for **5–7 months** of calendar time as a realistic target.

**Don't sub-divide phases until you're inside them.** Sub-tasks emerge from doing the work; planning them all upfront wastes time and produces wrong plans.

---

## 5. Working with Claude — playbook

This is the section that codifies how the human-AI collaboration works on this project. Update it as patterns emerge.

### Tools and where to use them

- **Claude Desktop (web/app):** for design discussions, CRD revisions, planning, document drafting, reviewing scenarios. Conversations happen in a project so they share context.
- **Claude Code (terminal):** for actual coding, building, testing, git operations. Reads `CLAUDE.md` from the repo on every session for context.
- **Project knowledge in Claude Desktop:** the durable memory across Desktop conversations. Key documents (this plan, the CRD draft, decisions log) live here. Update them as decisions are made.
- **CLAUDE.md in the repo:** the durable memory for Claude Code. Mirrors the most important decisions from project knowledge in condensed form. Updated alongside project knowledge.

### What Claude is good at

- Architectural sounding board, especially when a question has multiple valid answers.
- Boilerplate generation: Go types from CRD specs, status condition helpers, watch setups, test scaffolds.
- Translating designs to code: "here's what the field should mean; write the reconciler logic that implements it."
- Reading and synthesizing: comparing chart values schemas, summarizing how other projects solved similar problems.
- First-draft documentation: reference docs, runbooks, migration guides.

### What Claude is unreliable at

- Anything requiring observation of the live cluster — without an MCP, Claude can't see what's actually running.
- Subtle Kubernetes semantics (finalizer/namespace race conditions, cache coherence). 80% accurate is dangerous because the 20% wrong looks plausible.
- Knowing exact current values of upstream Helm charts. Always verify with `helm show values`.
- Long-term consistency across sessions. Claude doesn't remember; the docs do.
- Picking what to do next. The plan is yours; Claude executes against it.

### Workflow patterns

**Design questions:** in Claude Desktop. Hash them out, save the result to project knowledge as a document. Don't let design decisions live only in chat history.

**Implementation:** in Claude Code, scoped tightly per session. "Write the EducatesClusterConfig types" is one session. "Add the watch on referenced Secrets" is another. Avoid trying to implement whole phases in one chat — quality drops as context fills.

**Debugging:** paste actual error messages, not summaries. If using read-only Kubernetes MCP, let Claude check cluster state directly. Otherwise, paste `kubectl describe` and log output verbatim.

**Code review:** paste the code, ask Claude to look for issues. Decent at catching obvious bugs, race conditions, missed cases. Not a substitute for testing.

**When stuck:** explain the situation in chat. Often clarifies your own thinking. If not, Claude might spot something.

### Anti-patterns

- **Don't ask Claude to drive the project.** "What should I do today?" produces mediocre answers. The plan picks tasks; Claude executes them.
- **Don't trust generated code blindly.** Especially for Helm SDK calls, controller-runtime patterns, and Kubernetes API usage. Verify against current docs.
- **Don't let chat-only artifacts accumulate.** If something good came out of a conversation, save it as a file. Otherwise it's lost.
- **Don't re-litigate decisions.** When Claude suggests something contrary to a documented decision, point at the document. If you keep relitigating, the decision wasn't well-documented — fix the document.

### Keeping documents current

Whenever an architectural decision changes, three places need updating:

1. The CRD draft (if it affects the schema).
2. This plan (if it affects scope, phases, or estimates).
3. CLAUDE.md (always, since it's the briefing for Claude Code).

A simple rule: **don't merge a code change that contradicts the docs without updating the docs in the same PR.**

### Decisions log

Maintain a separate `docs/architecture/decisions.md` with one-paragraph entries for each significant architectural decision and its rationale. Reference it from CLAUDE.md. This is what prevents relitigation and orients new contributors (or future you) faster than reading commit history.

Format: short title, date, what was decided, why. No need for full ADR formality unless the team grows.

---

## 6. Risks and mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Helm SDK or controller-runtime quirk costs significant time in Phase 2 | High | Budget extra time for Phase 2; treat it as the learning phase. |
| Solo development pace drops due to context-switching with other responsibilities | High | Document state at end of each work session; resume sessions from docs, not memory. |
| `helm dt` (or chosen image-relocation tool) becomes unmaintained | Medium | Don't make the install path depend on it; keep it as build-time only. Identify backup tool early. |
| Educates runtime turns out to require something Helm can't express cleanly | Low | The pre-phase chart shakes this out before operator work starts. |
| CRD design reveals gaps once implementation starts | High | Plan for one CRD revision (v1alpha2) before v1beta1. Don't fight it. |
| Phase estimates are wrong in aggregate | High | The estimate is a range, not a date. Communicate phase completion, not deadlines. |

---

## 7. Open items at start of work

These are unresolved as of the start of Phase 0 / pre-phase:

1. **Repo location:** Decision has been made to use a new directory `helm-charts` in current repository.
2. **Vendor upstream charts vs pull from registry at runtime.** Decision has been made to use upstream charts, but to vendor them into the repository at build time. Make the update of every chart a conscious decision, so that changes can be curated and properly tested.
3. **OperationalBlock pattern:** deferred per CRD draft r3. Add when it becomes necessary.
4. **Image relocation tool:** `helm dt` evaluated; license check required. Backup options: `relok8s`, custom Go using `go-containerregistry`. Decide in Phase 6.
5. **Read-only Kubernetes MCP setup for Claude Code:** decide once Phase 1 has something running. Not needed earlier.
6. **`SecretsManager.spec.clusterConfig` block existence:** noted in CRD draft as possibly unnecessary. Confirm during Phase 4.

---

## 8. What good looks like at the end

Concrete success criteria for v4 release:

- A user with a fresh GKE cluster can install Educates v4 with a single `helm install` of the installer chart followed by `kubectl apply` of three CRs (or a single `educates admin platform deploy` command). Working portal in ~10 minutes.
- A user with an existing OpenShift cluster, their own cert-manager, and their own ingress can deploy Educates v4 with an Inline-mode `EducatesClusterConfig` and three component CRs without the operator installing anything cluster-level.
- An ArgoCD-managed cluster reconciles a Git repo containing the operator chart and CR YAML files and brings up Educates without human intervention.
- A v3 user runs `educates migrate-config v3-config.yaml > v4-crs.yaml`, then deletes their v3 install and applies the v4 CRs, and gets back to a working state.
- The operator's own uninstall (delete EducatesClusterConfig) cleans up everything it installed, in correct order, with no orphans.

When all five of those work reliably, v4 is ready to release.
