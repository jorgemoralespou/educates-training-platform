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
*(landed: 2026-05-13, at the start of Phase 3 — predicates were
implemented as per-kind mapping functions rather than separate
predicate.Predicate objects; same outcome, slightly less ceremony.)*

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

---

### Remove the cert-manager CRD operator-startup prerequisite

**Date added:** 2026-05-12.
*(landed: 2026-05-13 via the deferred-watch pattern —
`internal/controller/config/crd_watcher.go` — after an initial
attempt with unstructured Watches proved insufficient. The
unstructured form still requires GVK resolution at cache-sync
time, which fails on a vanilla cluster. The deferred pattern
registers the cert-manager watches at runtime via
Controller.Watch() once discovery confirms the CRDs exist. See
decisions.md amendment on the 2026-05-06 entry for the full
design + verified mechanics.)*

**Trigger to file:** start of Phase 3, before any additional typed
watches on bundled-service CRDs (Contour HTTPProxy, Kyverno
ClusterPolicy, etc.) are added — applying the same pattern to all
cluster-service kinds at once is cheaper than retrofitting it later.

**Context:**

Today the operator chart documents that cert-manager.io CRDs must
be installed in the cluster *before* the operator pod starts (see
decisions.md → "cert-manager CRDs are an operator install
prerequisite"). The reason is purely mechanical: the reconciler
uses typed `Watches(&cmv1.ClusterIssuer{}, ...)` and
`Watches(&cmv1.Certificate{}, ...)`, and controller-runtime
requires the GVK to be resolvable at cache startup.

End-to-end testing during Phase 2 Session 2 surfaced that this
prerequisite is a real friction point — the user has to apply
cert-manager CRDs out-of-band before `helm install
educates-installer`, which contradicts the project goal of "one
install command, no preceding steps". The original decision
accepted the friction in exchange for typed ergonomics in the
validator; with Phase 3 about to add three more bundled cluster
services (each with their own CRDs), the friction multiplies and
the decision should be reversed.

**Decision (reversal of decisions.md 2026-05-06 entry):**

The operator must start successfully on any vanilla cluster, with
no CRD prerequisites. Bundled-mode installs are then responsible
for applying their own CRDs via the chart they install.

**Design — split typed vs unstructured by use site:**

The GVK-at-startup constraint only applies to `Watches()`. API
calls (Get/Create/Update/Patch) resolve GVKs at request time, so
typed clients work fine *after* the CRDs exist in the cluster.
That lets us keep most of the ergonomic typed code we have today,
and only pay the unstructured tax on the watch layer:

1. **Watches: always unstructured.**
   Replace
   `Watches(&cmv1.ClusterIssuer{}, ...)` and
   `Watches(&cmv1.Certificate{}, ...)` in
   `educatesclusterconfig_controller.go::SetupWithManager` with
   unstructured-kind watches:
   ```
   ci := &unstructured.Unstructured{}
   ci.SetGroupVersionKind(schema.GroupVersionKind{
       Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuer"})
   Watches(ci, handler.EnqueueRequestsFromMapFunc(mapToSingleton))
   ```
   Unstructured watches start successfully whether or not the CRD
   exists; events flow once the CRD lands.

2. **Validator read paths: unstructured.**
   `validator.go::checkClusterIssuer` reverts to the Phase 1
   pre-typed implementation (Get an `unstructured.Unstructured`,
   read `status.conditions[Ready]` by field path). The field access
   is a handful of lines — not worth the typed-package import for
   a single read.

3. **Owned-resource create/update: keep typed.**
   `certmanager.go::ensureClusterIssuer` and
   `ensureWildcardCertificate` keep constructing typed
   `cmv1.ClusterIssuer{...}` / `cmv1.Certificate{...}` and
   SSA-patching them. These calls only execute *after*
   `ensureCertManagerReady` has confirmed cert-manager is up,
   which means the CRDs are in the apiserver. Typed is more
   readable and SSA serialization works the same.

4. **certificateReady read: typed.**
   Same reasoning — it only runs after cert-manager is up.

**Phase 3 corollary:**

Apply the same pattern uniformly to Contour, Kyverno, external-dns:
- Watches against any of their CRD-defined kinds → unstructured.
- Validation reads of user-supplied references → unstructured.
- Operator-owned creates/updates → typed (resources of the
  bundled service can only be created after the bundled service
  is installed, by definition).

**Acceptance criteria:**

- `helm install educates-installer` on an empty kind cluster
  (no cert-manager CRDs, no anything) succeeds and the operator
  pod reaches Ready without manual intervention.
- A Managed-mode `EducatesClusterConfig` then installs cert-manager
  (including its CRDs via the chart) and reaches Ready end-to-end.
- An Inline-mode `EducatesClusterConfig` referencing an existing
  ClusterIssuer still works — the unstructured watch picks up
  drift on the referenced object once the CRD exists.
- envtest specs still pass with the typed CRD vendored under
  `testdata/crds/cert-manager/` (envtest can register typed CRDs
  even when the runtime path is unstructured).
- decisions.md's "cert-manager CRDs prerequisite" entry is amended
  with a reversal block dated when this lands; the original
  decision text is preserved so the reversal is honest.
- CLAUDE.md "Watches:" and "ClusterIssuer access" bullet points
  are updated to reflect the unstructured-at-the-edge / typed-in-
  the-middle split.

**Risks:**

- Unstructured field access is stringly-typed and easier to
  fat-finger than typed reads. Mitigation: keep the unstructured
  surface narrow (validator reads only), and unit-test the
  field-path expressions against a real CRD-shaped object.
- The pre-existing envtest drift spec (`ClusterIssuer deletion
  → status flips to Degraded`) currently runs against the typed
  watch path. It should still pass with unstructured watches,
  but the test setup needs to register the kind for the
  unstructured client — verify on the first pass.

**Alternative considered (and rejected):** ship cert-manager (and
later Contour / Kyverno / external-dns) CRDs in the
`educates-installer` chart's `crds/` directory. Mechanically
cleanest user experience but couples the operator chart's
lifecycle to four upstream CRD shapes; chart bumps require
re-vendoring CRDs in lockstep; Inline-mode users who never
install cert-manager would still get its CRDs in their cluster.
The user prefers a leaner operator install that imposes nothing
beyond its own kinds.

---

### Quiet the controller-runtime Kind source after cert-manager CRDs are removed

**Date added:** 2026-05-13.
*(partially landed: 2026-05-13 via `cmd/logsink.go` — a
`logr.LogSink` wrapper that demotes the specific controller-runtime
ERROR message "if kind is a CRD, it should be installed before
calling Start" to V(1)/debug. The visual noise is suppressed in
default operation; the underlying controller-runtime gap — no API
to tear down a registered Source — remains and would need an
upstream contribution to fully fix.)*

**Trigger to file:** post-Phase-3 release prep, or sooner if a
user reports the log noise as a real concern. Cosmetic — does not
affect correctness.

**Context:**

The deferred-watch pattern (decisions.md → "cert-manager CRDs
prerequisite" 2026-05-13 amendment) registers cert-manager watches
at runtime once their CRDs are present. The operator's own code
paths classify CRD-removal cleanly via
`isCertManagerCRDMissingErr` and surface a
`CertificatesReady=False reason=CertManagerCRDsMissing` /
`phase=Degraded` status, so the user-visible state is correct.

But controller-runtime offers no public API to remove or stop a
registered `Source` short of cancelling the entire controller's
context (verified: `pkg/internal/controller/controller.go` only
adds to `startWatches`, never removes; `Source` interface has no
`Stop()`). Once `CRDWatcher` activates the cert-manager.io
watches and cert-manager is later uninstalled (the normal end of
a Managed-mode teardown, or a user `kubectl delete crd`), the
Kind source's polling-retry loop spams "if kind is a CRD, it
should be installed before calling Start" every 10s in the
operator pod log until the pod restarts.

**Scope sketches (two complementary mitigations):**

1. **Self-restart after successful cleanupManaged drain.** When
   the finalizer is removed and the EducatesClusterConfig is
   about to be GC'd, signal the operator process to exit cleanly
   (e.g., os.Exit(0) after a brief delay, or close the manager's
   context). Kubernetes restarts the pod; the new pod starts
   fresh, no stale sources. Crude but practical. Doesn't help
   the abnormal-deletion case (user deletes CRDs out-of-band
   while the CR still exists) — that one only resolves on
   manual pod restart.

2. **Upstream controller-runtime contribution** to expose a way
   to remove a Source, or to stop one's underlying informer.
   Significant design discussion. Possibly there's an alternative
   shape — a `Source` that wraps the Kind source and can be
   externally stopped — that we could prototype downstream first.

**Acceptance criteria:**

- After a normal `kubectl delete educatesclusterconfig cluster`
  on a Managed-mode install, the operator log goes quiet within
  a pod restart window (≤ ~30s); no recurring retry-loop
  errors.
- The abnormal-deletion case (CRDs deleted out-of-band) at
  least surfaces a clear instruction to the user via the
  `CertManagerCRDsMissing` condition message; log noise is
  best-effort.

**Out of scope here:** the operator's own error-path
classification — that lands with the deferred-watch pattern and
is the user-facing fix.

---

### Expose an external-dns `domainFilters` override on the CRD

**Date added:** 2026-05-14.
**Trigger to file:** when a user reports they want external-dns
to manage records under a domain different from
`spec.ingress.domain`, or to manage multiple domains, or to
narrow further by record name. v1alpha1 hard-codes
`domainFilters: [spec.ingress.domain]` to match what the v3
Carvel installer did; this is fine for the single-domain Educates
flow but doesn't cover legitimate multi-tenant / multi-domain
setups.

**Context:**

`renderExternalDNSValues` (installer/operator/internal/controller/
config/externaldns.go) currently sets
`domainFilters: [spec.ingress.domain]` unconditionally. The v3
installer's EKS/GKE overlays optionally let the user point at a
different zone via `clusterInfrastructure.aws.route53.hostedZone`
or `clusterInfrastructure.gcp.cloudDNS.zone`. Our zoneIdFilters
(for AWS) is already driven from
`spec.dns.bundledExternalDNS.route53.hostedZoneID`, so the AWS
case is partially covered — but the `domainFilters` value is
still pinned to the ingress domain.

**Scope:**

Add an optional `domainFilters []string` field on
`BundledExternalDNSConfig`. Use `spec.ingress.domain` as the
default when unset (current behaviour). Pass through verbatim
when the user sets it. Same `[]string`→`[]any` translation as
the other slice values (helm values.schema.json gotcha).

**Concrete scenario (observed 2026-05-14, GKE + CloudDNS):**

User has a single top-level CloudDNS zone `google.educates.dev`.
First install with `domain: academy-01.google.educates.dev`
failed to publish DNS records *until the user created a dedicated
sub-zone* `academy-01.google.educates.dev` with NS delegation
from the parent. Reason: external-dns `--domain-filter` is a
*record-name* filter, not a zone selector. With the filter
pinned to `academy-01.google.educates.dev`, external-dns
rejected the parent zone `google.educates.dev` as "wrong
suffix" — even though a record like `*.academy-01...` could
legitimately live inside the parent zone in DNS terms.

In v3 carvel, the user worked around this by setting
`domain_filter` to the parent zone name (see
`carvel-packages/installer/bundle/config/ytt/_ytt_lib/packages/
external-dns/overlays/defaults.star`). v1alpha1 doesn't expose
the same knob — this follow-up fixes that.

**Scope (v1alpha1 — option 1):**

Add an optional `domainFilters []string` field on
`BundledExternalDNSConfig`. Use `spec.ingress.domain` as the
default when unset (current behaviour). Pass through verbatim
when the user sets it. Same `[]string`→`[]any` translation as
the other slice values (helm values.schema.json gotcha).

This is backwards-compatible — users with a dedicated sub-zone
keep working unchanged; users with only a parent zone set
`domainFilters: [google.educates.dev]` and external-dns writes
records into the parent.

**Acceptance criteria (v1alpha1):**

- `spec.dns.bundledExternalDNS.domainFilters: ["a.example.com",
  "b.example.com"]` results in external-dns watching both
  domains.
- Empty / unset preserves the current single-domain default.
- envtest spec covers both cases.

**Potential follow-on (deferred — option 3):**

If users start sharing a single CloudDNS project across
multiple clusters with overlapping `domainFilters`, add a
companion `cloudDNS.zoneIDs []string` (and Route53 mirror)
that drives external-dns's `--google-zone-id-filter` /
`--zone-id-filter`. That layers a zone-level guardrail on top
of the record-level filter so a cluster can't accidentally
mutate another cluster's records. Defer until a concrete
multi-cluster-shared-zone ask comes in — record-level
filtering plus per-cluster `txtOwnerId` already covers the
common case.

---

### ACME static-credentials Secret support (Route53 + CloudDNS)

**Date added:** 2026-05-14.
**Trigger to file:** users without IRSA/Workload Identity (on-prem
or smaller clouds) ask for ACME-DNS01 support backed by long-lived
credentials.

**Context:**

v1alpha1 lands ACME-DNS01 with identity-based auth only —
`Route53Config.IAMRoleARN` (IRSA on EKS) and
`CloudDNSConfig.WorkloadIdentityServiceAccount` (Workload Identity
on GKE). The CRD already reserves `CredentialsSecretRef` on both
provider configs, but the operator validator rejects it as "not
yet supported in v1alpha1".

cert-manager's underlying API does support static credentials —
`Route53.accessKeyIDSecretRef` + `Route53.secretAccessKeySecretRef`
on AWS, `CloudDNS.serviceAccountSecretRef` on GCP. The work is in
the operator: copy the user-supplied Secret from the operator
namespace into the cert-manager namespace (mirroring the
CustomCA-copy pattern), then reference the copied Secret from the
ClusterIssuer's ACME solver spec.

**Scope:**

1. Validator: accept `CredentialsSecretRef` on Route53 and
   CloudDNS provider configs; enforce key presence
   (`aws_access_key_id` + `aws_secret_access_key` for Route53,
   `credentials.json` for CloudDNS) via the same
   `checkCustomCASecret`-style helper.
2. Copy step: ensure helper analogous to
   `ensureCustomCASecretCopy` that mirrors the credentials Secret
   into the cert-manager namespace under a deterministic name.
3. `buildACMEIssuer`: when CredentialsSecretRef is set, populate
   `Route53.SecretAccessKeyID/SecretAccessKey` or
   `CloudDNS.ServiceAccount` instead of relying on identity-based
   inference.
4. Mutual exclusivity: existing CEL/validator already enforces
   one mechanism only — verify the rule still applies after
   reactivating CredentialsSecretRef.

**Acceptance criteria:**

- `educates-installer` reaches Ready on a kind cluster (or any
  cluster without IRSA/WI) using static AWS credentials.
- Validator returns clear messages on missing keys / both
  mechanisms set.
- envtest covers: valid static creds (Route53 + CloudDNS),
  missing key, and the "both set" rejection.

---

### Expose ACME server URL choice (staging vs production)

**Date added:** 2026-05-14.
**Trigger to file:** post-Phase-3 polish — testing real ACME on
fresh clusters quickly hits Let's Encrypt production rate limits.

**Context:**

`ACMEConfig.Server` is exposed in v1alpha1 and defaults to
`https://acme-v02.api.letsencrypt.org/directory` when unset. The
field works today; what's missing is good documentation and
example values for Let's Encrypt staging
(`https://acme-staging-v02.api.letsencrypt.org/directory`) and
guidance on when each is appropriate. CLI / docs should call out
the staging endpoint for first-time testing because the operator
exits the install pipeline on rate-limit errors and the user
sees a hard failure rather than a transient one.

**Scope:**

- Reference docs for the `acme.server` field with a worked
  example for staging.
- Example CR snippets in `project-docs/` covering both Route53
  and CloudDNS for production + staging.
- Consider a `--acme-staging` shortcut on `educates install`
  when the CLI rewrite (Phase 5) reaches the
  `EducatesClusterConfig` builder.

**Acceptance criteria:**

- Reference docs clearly differentiate production and staging
  use cases.
- Sample CRs in repository demonstrate both setups.

---

### SessionManager: wire remaining spec fields into chart values

**Date added:** 2026-05-14.
**Trigger to file:** any user reporting that themes, image cache,
registry mirrors, or default access credentials configured on the
SessionManager CR don't take effect.

**Context:**

`renderSessionManagerValues` (installer/operator/internal/controller/
platform/sessionmanager_controller.go) maps the SessionManagerSpec
fields the v1alpha1 CRD exposes today onto the session-manager
subchart's values shape. Four spec blocks are reserved in the CRD
but not yet wired through; the reconciler explicitly discards them
via `_ = obj.Spec.<Field>` so the gap is visible in the source:

1. **`spec.themes` + `spec.defaultTheme`** — the subchart accepts
   `websiteStyling.themeDataRefs` (Secret refs only) plus an inline
   blob. The CRD's `ThemeSource` supports `ConfigMap`, `Secret`, and
   `URL`. Translating ConfigMap-sourced themes requires either
   extending the subchart to accept ConfigMap refs or having the
   operator copy/transform the ConfigMap into a Secret in the
   release namespace at apply time.
2. **`spec.defaultAccessCredentials`** — the runtime carries an
   admin/robot credentials pipeline (now stable via Helm `lookup`
   in `resolvedTrainingPortal`), but the CRD's
   `DefaultAccessCredentials` is a separate concept that pre-seeds
   *workshop* (not portal-admin) credentials. The subchart has no
   typed value for it yet; landing it requires a chart-side
   addition plus an operator mapping.
3. **`spec.imagePrePuller`** — `imagePrePuller.enabled: true` should
   toggle the chart's `imagePrePuller.enabled` plus pre-populate
   `imagePrePuller.images` from the resolved image inventory. Neither
   half is in place yet.
4. **`spec.registryMirrors`** — the v3 carvel runtime had a
   workshop-side registry mirror story (rewriting workshop
   container pulls to internal mirrors). No chart wiring in v4 yet;
   needs a runtime-config field and a chart values shape.

**Scope:**

One follow-up PR per item, in order of demand. (1) likely first
because themes are user-facing; (3) and (4) tend to land together
because they share the air-gap/mirror story.

**Acceptance criteria:**

- Each item's spec block, when set on a SessionManager CR, takes
  effect at runtime on a real cluster (verified manually).
- envtest specs assert the values map renders the expected
  subchart-values shape for each.
- Reconciler removes the `_ = obj.Spec.<Field>` placeholder and
  notes the mapping in `renderSessionManagerValues`'s comment.

---

### Block EducatesClusterConfig finalize while platform CRs exist

**Date added:** 2026-05-14.
*(landed: 2026-06-10 — Managed-mode deletion path checks the three
platform singletons before `cleanupManaged`; while any exist it
publishes `Ready=False reason=PlatformCRsPresent` /
`phase=Uninstalling` naming the offenders and requeues. Watches on the
three platform kinds re-enqueue the ECC on their deletion; a 15s
requeue backstops missed events. envtest covers both ordering paths.)*

**Trigger to file:** observed on a real GKE cluster — deleting
`EducatesClusterConfig` first then the three platform CRs led to
SessionManager's finalizer drain failing in a tight loop with the
opaque helm error `failed to delete release: session-manager`.

**Context:**

The Phase 4 platform reconcilers (SecretsManager, LookupService,
SessionManager) install helm releases that track resources
created from cluster-services CRDs (kyverno ClusterPolicy in
particular). When the user deletes `EducatesClusterConfig`
first, ECC's finalizer drains the cluster services in reverse
install order — which removes Kyverno (and its CRDs). The
SessionManager helm release Secret still references kyverno
ClusterPolicy resources by name; when the SessionManager
finalizer subsequently runs `helm uninstall`, helm can't
enumerate those kinds anymore (CRD gone, NoMatchError under the
hood) and collapses the per-resource error into a generic
"failed to delete release" with no detail.

User experience: SessionManager stuck `Uninstalling` forever;
`kubectl delete sessionmanager cluster` hangs; the only way out
is to manually patch the finalizer off and clean up the orphan
`educates` namespace by hand.

The architectural fix is to make EducatesClusterConfig's
finalizer refuse to proceed while any of the three platform CRs
exist. The user then sees a clear "Stuck terminating: platform
CRs still present" status message and learns the order.

**Scope:**

- Extend `EducatesClusterConfigReconciler.cleanupManaged` (or
  its caller) with a pre-flight check: list SecretsManager,
  LookupService, SessionManager singletons. If any exist (even
  in Terminating state), publish a `Ready=False` /
  `Phase=Uninstalling` condition with reason
  `PlatformCRsPresent` and message naming the offenders;
  requeue without proceeding.
- Watch on the three platform CR kinds from the
  EducatesClusterConfigReconciler so deletion events re-enqueue
  ECC.
- envtest spec: delete ECC while a SecretsManager exists;
  assert ECC stays terminating and surfaces the condition;
  delete SecretsManager; assert ECC unblocks.

**Why not "ignore missing kinds during uninstall"?**

Tempting alternative is to make the helm wrapper tolerant of
NoMatchError during uninstall (treat as "already gone, drain
proceeds"). This works but masks a real ordering bug: the user
intended the platform CRs to drain first, and silently completing
the SessionManager uninstall with kyverno-related resources
half-orphaned in the `educates` namespace leaves the cluster in
an inconsistent state. Refusal + clear error is the better UX.

**Related visibility fix already landed:**

helm SDK's slog handler is now wired to the operator pod's
stderr at Debug level (commit TBD), so future runs surface the
per-resource error detail behind the collapsed
"failed to delete release" message. That helps diagnosis even
when the architectural fix isn't yet in place.

**Acceptance criteria:**

- Deleting ECC while any platform CR exists publishes the
  `PlatformCRsPresent` condition and does not proceed with
  cluster-service cleanup.
- Deleting platform CRs first lets ECC's finalizer run cleanly.
- envtest covers both ordering paths.


---

### Revisit `imageCache` naming (was `imagePuller` in v3)

*(resolved: 2026-05-27 — standardised on `imagePrePuller` across CRD,
chart, and CLI; list field renamed to `images`. See decisions.md "Image
pre-pull feature named `imagePrePuller`". Original analysis kept below.)*

**Date added:** 2026-05-20.
**Trigger to file:** raised during Phase 5 CLI config design when
deciding what name to expose in `EducatesLocalConfig`. The mismatch
between v3 muscle memory (`imagePuller`) and the CRD r3 name
(`imageCache`) is the kind of papercut that lingers if not chosen
deliberately.

**Context:**

- **v3 name:** `imagePuller` (under
  `client-programs/pkg/config/installationconfig.go::ImagePullerConfig`,
  with fields `enabled` and `prePullImages[]`).
- **v4 CRD r3 name:** `SessionManager.spec.imageCache` (single
  `enabled` boolean; pre-pull list is intended to be derived
  from the chart's resolved image inventory rather than user-
  listed — see the existing follow-up "SessionManager: wire
  remaining spec fields into chart values" item 3).
- **Chart-side name:** session-manager subchart still uses
  `imagePuller.enabled` / `prePullImages` (matches v3 terminology).
- **EducatesLocalConfig (Phase 5):** currently planned to expose
  `imageCache: false` to align with the CRD; will surface to users
  as the new name on first contact.

**What the feature actually does:**

A DaemonSet runs on each node that pre-pulls (caches) workshop-
related images so workshop session startup is fast. "Cache" is
accurate as a noun (the cached images on each node); "puller" is
accurate as a verb (what populates the cache). Either name is
defensible.

**Action item:**

Decide which name to standardise on across all surfaces, then
align the three places:

1. **Option A — keep `imageCache` everywhere.** Rename the chart
   field `imagePuller` → `imageCache` (breaking for any standalone
   chart users). v3 terminology is dropped. EducatesLocalConfig
   stays as drafted.
2. **Option B — revert to `imagePuller` everywhere.** Rename the
   CRD field `imageCache` → `imagePuller` (only impacts in-flight
   v1alpha1 — no users yet). Chart stays as-is. EducatesLocalConfig
   becomes `imagePuller: false`. Preserves v3 muscle memory.
3. **Option C — pick a third name.** Candidates: `prePuller`,
   `imageWarmup`, `nodeImageCache`. Apply consistently.

**Recommendation:** decide before Phase 4 lands the SessionManager
reconciler — renaming the CRD field is cheap pre-Phase-4 and
expensive after.

**Acceptance criteria:**

- One name chosen and applied consistently across CRD spec,
  session-manager subchart values, and `EducatesLocalConfig` /
  `EducatesConfig` translator.
- CRD draft r3 (or its successor) updated.
- decisions.md entry recording the choice and reasoning.

---

### CLI: CI drift checks for embedded operator chart and generated `EducatesConfig` schema

**Date added:** 2026-06-10 (accumulated during Phase 5 steps 2 + 5).
**Trigger to file:** immediately — both artifacts can silently drift
today.

**Context:**

Two CLI-embedded artifacts are copies of generated/canonical sources:
`client-programs/pkg/deployer/chart/files/` is a copy of
`installer/charts/educates-installer/` (refreshed by `make
embed-installer-chart`; the Makefile comment explicitly notes the CI
check is a follow-up), and
`client-programs/pkg/config/v1alpha1/schemas/EducatesConfig.schema.json`
is generated from the CRD OpenAPI schemas by `make
generate-cli-schemas`. Neither is verified in CI; a CRD or chart
change without the corresponding regen ships a CLI that installs
stale manifests or validates against a stale schema.

**Scope:**

CI steps (CLI workflow or installer-operator-ci.yaml) that run both
make targets and fail on `git diff --exit-code` over the embedded
copies.

**Acceptance criteria:**

- CI fails when `installer/charts/educates-installer/` and the
  embedded chart copy differ.
- CI fails when the committed `EducatesConfig.schema.json` differs
  from freshly generated output.
- Failure message names the make target to run.

---

### CLI: deploy/delete hardening follow-ups

**Date added:** 2026-06-10 (accumulated during Phase 5 steps 5–6).
**Trigger to file:** post-Phase-5; none block daily use.

Already landed from the same accumulation: deploy-side CRD lifecycle
(`14216722`, `a9109bf1`), structured progress reporting (`8909ff53`),
`--yes` + `--purge` on delete (`4b4a1741`), create-preflight existence
guard + port probe (`c4b59f15`).

**Still open, in rough priority order:**

1. **Mocked tests for cluster-touching deploy code.**
   `client-programs/pkg/deployer/{apply,wait,prereq,helm}` have no
   unit tests — confidence currently comes from kind smoke runs only.
   fake.NewClientBuilder + the helm in-memory client wrapper would
   unblock CI-time coverage.
2. **`--force-finalizers` on delete.** A wedged finalizer currently
   requires a manual `kubectl patch`; flag automates it for users who
   accept the loss.
3. **Apply-all-then-wait-all CR orchestration.** Sequential
   apply-then-wait is fragile with bidirectional CR readiness deps
   (the LookupService/SessionManager cycle, worked around in
   `0d79afc6` by pairing them). Cleaner: apply every CR upfront, then
   poll the set for Ready with a shared deadline.
4. **Operator image rebuild guidance.** Detect a dev-tag pod whose
   image digest doesn't match the latest local build; warn or accept
   `--rebuild-operator` (docker-build + kind load + rollout).
5. **Helm release labels/annotations.** Stamp releases with CLI
   version + build SHA so `helm list` shows provenance.

**Acceptance criteria:** each item lands as its own small PR with
tests; this entry gets per-item `*(landed: ...)*` marks.

---

### CLI: `local config` UX papercuts

**Date added:** 2026-06-10 (accumulated during Phase 5 step 7).
**Trigger to file:** on user demand; cheap wins for daily ergonomics.

1. **Path suggestions on missing-field errors** — `get ingress.domian`
   → "did you mean `ingress.domain`?" via a schema-walker producing
   valid paths + fuzzy match.
2. **Tab completion on dotted paths** — Cobra `ValidArgsFunction`
   walking the embedded JSON schema.
3. **`set` for list paths** — e.g. `set resolver.extraDomains[0]
   foo.test`; today only map paths work.
4. **`unset PATH`** — remove a key without re-initing the file.

**Acceptance criteria:** each papercut fixed with schema-driven tests;
no hand-maintained path lists.

---

### CLI: scenario-kind regression tests (escape-hatch round-trip, sample-CR parity)

**Date added:** 2026-06-10 (accumulated during Phase 5 step 11).
**Trigger to file:** before the next scenario kind or CRD field change.

**Context:**

The scenario kinds (Local/GKE/EKS/Inline) and the `EducatesConfig`
escape hatch both translate to the same four CRs, and each scenario
kind was admitted against a tested sample CR in `installer/samples/`.
Neither relationship is pinned by a test today.

**Scope:**

1. **Round-trip test:** `render --config inline.yaml` and `render
   --config escape.yaml` declaring the same EducatesClusterConfig must
   produce identical output.
2. **Sample-CR parity test:** render the minimal scenario config per
   kind and diff against its `installer/samples/` CR — catches drift
   in either direction.
3. Clearer error when `local cluster create` is given a non-Local
   kind (today's message doesn't say which kinds are accepted).
4. **Cloud auth alternatives** (static credentials for GKE/EKS) stay
   rejected until the operator-side follow-up "ACME static-credentials
   Secret support" lands; CLI schema + translator follow it.

**Acceptance criteria:** tests live with the translator package and
run in CI; the error message names accepted kinds.
