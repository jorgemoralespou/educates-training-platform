# Follow-up issues

GitHub issues to open once the v4 chart-based install ships in
`develop`. Each entry is a near-ready issue draft — title, motivating
context, scope, acceptance criteria — that can be transcribed to the
GitHub issue tracker with minimal further editing.

The format mirrors `decisions.md`: one heading per issue, prose body,
date the entry was added. New issues append at the end.

Mark items here as `*(opened: <issue link>)*` once the issue is
filed in the tracker, and `*(landed: <commit/PR>)*` once resolved.
Items that turn out to be irrelevant after further work get
`*(dropped)*` with a one-line reason. Don't delete — the history is
itself useful for future planning.

---

### Simplify `operator_config.py` IMAGE_REPOSITORY resolution

**Date added:** 2026-05-05.
**Trigger to file:** v4 chart shipped in `develop`; users no longer
install via the v3 carvel installer for new clusters.

**Context:**

`session-manager/handlers/operator_config.py:26-35` currently composes
`IMAGE_REPOSITORY` from the runtime config's `imageRegistry.host` +
`imageRegistry.namespace`, with a fallback to
`registry.default.svc.cluster.local` when the host is empty.
`secrets-manager/handlers/operator_config.py` reads its own
`OPERATOR_NAMESPACE` from the same blob.

In v3, that compose logic mattered because the carvel-installer baked
image refs into the OCI bundle at build time AND populated
`imageRegistry` in the operator config so workshops with
`$(image_repository)` placeholders could resolve runtime-spawned
images consistently. The runtime's `image_reference()` function used
`IMAGE_REPOSITORY` as the default prefix when an image wasn't listed
in `imageVersions`.

In v4 the chart populates `imageVersions` with the full set of
runtime-spawned image refs explicitly — every short-name the runtime
knows about (`training-portal`, `base-environment`, the loftsh
images, etc.) is present with a fully-qualified reference, sourced
from Chart.yaml annotations. The `IMAGE_REPOSITORY` fallback is
hit only by:

1. `$(image_repository)` placeholders in workshop content YAMLs, when
   workshops authored in dev are deployed to dev clusters via
   `educates publish-workshop` + `educates deploy-workshop`. Released
   workshops have these placeholders pre-resolved.
2. Hypothetical short-names not yet in `imageVersions`. None known
   today.

**Scope:**

Tighten `operator_config.py` so the runtime stops mixing the two
conceptual roles `imageRegistry` was playing in v3:

- Drop the `imageRegistry.host` + `imageRegistry.namespace` compose
  logic. Read `IMAGE_REPOSITORY` directly from a single
  `imageRepository` field in the operator config (the chart can emit
  this directly when `development.imageRegistry` is set), with the
  same `registry.default.svc.cluster.local` fallback.
- Drop the runtime-side fallback path in `image_reference()` for
  images not in `imageVersions`. Treat unknown short-names as a config
  error rather than silently composing an unresolvable ref against
  `IMAGE_REPOSITORY`.

**Why now (after develop ships) and not earlier:**

Until v4 is in `develop`, the v3 carvel installer is the production
install path. Changing `operator_config.py` shape would force a
coordinated v3 carvel-installer + runtime change. Easier to wait until
v4 owns the install and then simplify the runtime to match the
narrower contract the chart actually provides.

**Acceptance criteria:**

- `operator_config.py` reads `IMAGE_REPOSITORY` from a single config
  field (`imageRepository: ""` populated by the chart, or empty for
  the in-cluster fallback). No host/namespace compose logic.
- `image_reference()` raises a clear error for short-names absent from
  `imageVersions` instead of composing a default ref.
- Chart's `session-manager.operatorConfigYAML` helper updated to emit
  the simplified field (or kept emitting the existing shape for
  backward compatibility during a deprecation window — decide at
  filing time).
- All 8 chart scenarios still pass.
- Workshop with a `$(image_repository)/<name>` placeholder still
  resolves correctly when `development.imageRegistry` is set.

---

### Drop `clusterIngress.tlsCertificate` / `caCertificate` inline forms from `operator_config.py`

**Date added:** 2026-05-05.
**Trigger to file:** when filing the previous issue, or independently.

**Context:**

`session-manager/handlers/operator_config.py:52-65` accepts an inline
`clusterIngress.tlsCertificate: { tls.crt, tls.key }` block as an
alternative to the `tlsCertificateRef` form, materialising a Secret
named `<domain>-tls` from the inline content. Same for
`clusterIngress.caCertificate`.

The v4 chart only emits the `*Ref` forms — references to existing
Secrets (typically populated by cert-manager or a pre-install hook).
The inline form is dead code in v4-installed clusters.

**Scope:**

Drop the inline-form parsing in `operator_config.py`. The `*Ref` forms
remain as the single way to reference TLS / CA material.

**Why:** simplifies the runtime, narrows the attack surface (no inline
TLS material flowing through the runtime config), and matches what the
v4 chart actually emits.

**Acceptance criteria:**

- Inline-form code paths removed from `operator_config.py`.
- Existing scenarios still pass (none use the inline form).
- v3 release notes / migration guide flag the removal as a
  `migrate-config`-handled translation.

---

### CI lint: Chart.yaml annotations stay in sync across subcharts

**Date added:** 2026-05-05.
**Trigger to file:** any time after the `development.imageRegistry` +
annotations refactor lands.

**Context:**

The four image-rendering subcharts each carry their own copy of
`educates.dev/image-registry-host` and `-namespace` annotations in
their `Chart.yaml`. The release workflow updates them per fork. If a
release accidentally updates only some, image refs across subcharts
would diverge silently.

**Scope:**

Add a CI step (or pre-commit hook) that asserts all four subchart
Chart.yamls have identical values for the two annotations. Failing the
lint blocks the merge.

**Acceptance criteria:**

- CI step checks all four `Chart.yaml`s have matching
  `educates.dev/image-registry-host` and `-namespace` annotations.
- Lint fails with a clear error pointing at the offending subchart.

**Optional extension:** also check that all five `Chart.yaml`s
(umbrella + subcharts) share the same `version` and `appVersion`.
The umbrella's `dependencies[].version` should match the subchart
versions too.

---

### Document the chart release workflow's annotation update step

**Date added:** 2026-05-05.
**Trigger to file:** before the first chart release that consumers
will install from a fork.

**Context:**

The `development.imageRegistry` + annotations design relies on the
release workflow updating each subchart's
`educates.dev/image-registry-host` and `-namespace` annotations per
fork. The release runbook currently only mentions `appVersion`
bumping.

**Scope:**

Update `docs/release-runbook.md` (or wherever the release runbook
lives — create if missing) with the annotation-update step, including
the `yq -i` command pattern and the rationale (decisions.md entry).

**Acceptance criteria:**

- Release runbook documents the annotation update across all four
  Chart.yamls (or refers to the CI lint that enforces consistency).
- Worked example for the upstream release case AND the fork release
  case.

---

### Harden cert-manager readiness with a synthetic admission probe

**Date added:** 2026-05-11.
**Trigger to file:** if we observe `CertificatesReady=True` flips
while the cert-manager admission webhook is in fact unhealthy — i.e.,
Certificates created in the same reconcile pass fail with
`InternalError`/`failed calling webhook` despite the operator
considering cert-manager ready.

**Context:**

Phase 2 Session 2 commit 2 implemented cert-manager readiness as
"all three cert-manager Deployments (controller, webhook, cainjector)
report `Available=True`." This is what v3's installer used and what
most cert-manager-driven operators ship. The Phase 2 carry-forward
originally proposed adding `GET /apis/cert-manager.io/v1` against
the API server as the readiness gate, but that endpoint resolves
from CRD presence alone — it does not exercise the admission webhook
pod — so it was rejected as a stronger probe (see Phase 2 Session 2
plan amendment).

The Deployment-availability check has a real (if rare) failure mode:
the webhook pod can be Available per its readiness probe while its
mutating/validating admission path is broken (TLS rotation race,
webhook handler panic-and-recover loop, mis-wired Service Endpoints).
In that window the operator races ahead, creates a ClusterIssuer and
Certificate, the apiserver calls the webhook, the webhook errors,
and the user sees confusing failures on resources the operator just
created.

**Scope:**

Add an *Option B* readiness gate alongside the existing Deployment
check:

1. Construct a sentinel `Certificate` object in-memory — `metadata.name`
   derived from `EducatesClusterConfig` UID so it's stable per
   instance — pointing at the wildcard issuer with `dnsNames` for the
   wildcard domain. Do **not** persist it.
2. Issue a server-side dry-run create via the typed client:
   `r.Create(ctx, sentinel, client.DryRunAll)`. This forces the
   apiserver to invoke cert-manager's admission webhook end-to-end
   without writing anything to etcd.
3. Treat a successful dry-run as "webhook is serving"; treat
   `webhook unavailable`, `i/o timeout`, or `connection refused`
   errors as "not ready, retry."
4. Gate ClusterIssuer/Certificate SSA on this probe in addition to
   Deployment availability.

The dry-run approach is preferable to a real-persistent canary
because it requires no cleanup, generates no garbage in the cluster,
and exercises exactly the path the real Certificate will take.

**Acceptance criteria:**

- New helper `probeCertManagerAdmission(ctx, owner)` returns
  nil/error.
- `reconcileCertManager` invokes it after the Deployment-availability
  gate and before SSA of the ClusterIssuer.
- Errors surface as `CertificatesReady=False reason=WebhookUnavailable`
  with the underlying admission error in the message; reconcile is
  retried with backoff.
- envtest spec: stand up a webhook config pointing at a non-existent
  Service, assert the probe returns an error and CertificatesReady
  stays False until the webhook is wired correctly.

**Cost note:** the dry-run is a real apiserver round-trip, so it
adds latency per reconcile. Reconcile triggers are watch-driven, so
this stays cheap in steady-state; it only fires when something
upstream changed. Acceptable trade for correctness.

---

### Narrow EducatesClusterConfig watches with object-scoped predicates

**Date added:** 2026-05-12.
**Trigger to file:** end of Phase 3 cleanup, once Contour / Kyverno /
external-dns watches have been added and the noise has compounded.

**Context:**

Today `mapToSingleton` enqueues the singleton EducatesClusterConfig on
*any* change to *any* of its watched kinds anywhere in the cluster:

- every Secret in the operator namespace (regardless of name);
- every IngressClass cluster-wide;
- every ClusterIssuer cluster-wide;
- every Certificate cluster-wide;
- every Deployment cluster-wide.

This is correct (the reconciler is idempotent, so over-enqueuing
just wastes CPU) but noisy. During cert-manager bootstrap the
reconciler fires ~20 times in 2 minutes because cert-manager's
chart-installed Deployments roll out, cainjector annotates webhook
configs, the Certificate transitions Issuing → Ready, the wildcard
tls Secret gets created, etc. — none of which is "the operator
needs to re-evaluate anything but cert-manager's readiness".

The noise also widens the cache-staleness race surface: more
reconciles means more chances to fire one against a cached obj
whose status was just updated by the previous reconcile but whose
watch event hasn't reached the informer yet. We already mitigate
the conflict with RetryOnConflict + live-read in
updateStatusWithTransitionLog, but the underlying cost (extra
apiserver round-trips, log churn) scales with the noise.

Phase 3 will add Contour, Kyverno, and external-dns watches —
roughly doubling the watched-kind surface. Adding predicates *after*
that change consolidates the cleanup into one focused commit
instead of spreading it across each phase.

**Scope:**

Replace the unconditional `EnqueueRequestsFromMapFunc(mapToSingleton)`
calls in `SetupWithManager` with predicate-filtered watches. Each
predicate filters at the source — events that don't match never
reach the work queue. Concrete filters:

1. **Secrets**: enqueue only if `name` matches one referenced from
   spec.inline or spec.ingress.certificates.bundledCertManager.customCA.
   The reconciler already knows these names; the predicate looks them
   up from the singleton CR (refetched on each predicate call, or
   cached behind a sync.Map updated from Reconcile).
2. **IngressClass**: enqueue only if `name` matches
   spec.ingress.ingressClassName.
3. **ClusterIssuer**: enqueue only if `name == wildcardClusterIssuer`
   (operator-owned) or matches a user-supplied reference from Inline
   mode.
4. **Certificate**: enqueue only if
   `name == wildcardCertificate && namespace == operatorNamespace`.
5. **Deployment**: enqueue only if `namespace == certManagerNamespace`
   (Phase 3 will add Contour/Kyverno/external-dns namespaces here).

**Acceptance criteria:**

- Reconcile rate during cert-manager bootstrap drops by 5–10×
  (measure with `controller_runtime_reconcile_total{controller="config-..."}`
  before/after).
- envtest specs that exercise drift (Secret deletion, ClusterIssuer
  deletion, etc.) still pass — predicates must not filter out the
  events the reconciler legitimately reacts to.
- No regression in time-to-Ready for fresh installs.

**Risks:**

- A predicate filter that's too aggressive silently swallows
  legitimate drift signals — the reconciler stops noticing
  user-driven changes. Mitigation: each predicate is unit-tested
  with both matching and non-matching events, and the envtest
  drift specs are the integration safety net.
- Predicates that look up spec from the singleton CR add a hot-path
  read; cache it explicitly rather than calling r.Get per event.

**Alternative considered (and rejected for this issue):** logging
each watch event at V(1) inside the mapper. Tells the operator
*what* fired but doesn't reduce work-queue churn — adds observability
without addressing the underlying cost. Worth considering only if
predicate filtering turns out to be insufficient.
