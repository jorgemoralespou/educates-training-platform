# CLAUDE.md

This file is a briefing for Claude Code working in this repository. Read it
fully before doing anything substantial. Sections are ordered by importance —
the top is what changes day-to-day, the bottom is mostly stable.

---

## What's happening right now

The v4 install path is the only install path in this repo. v3
(Carvel-based installer, `carvel-packages/installer/`, kapp-controller)
has been deleted from the CLI and tree.

- **Install pipeline:** Helm-chart + Go operator. Users run
  `educates admin platform deploy` (helm install operator chart +
  kubectl apply 4 platform CRs), or `educates local cluster create`
  for the laptop flow (kind + registry + deploy in one).
- **v3 is gone.** There's no in-place migration. Users on v3 delete
  their old install and follow the v4 path. The CLI silently migrates
  a v3 `values.yaml` → v4 `config.yaml` on first run when the v3
  provider was kind (or empty); other providers get a clear
  re-declare error (Phase 5 step 10, landed 2026-06-06).
- **CLI config is a kind ladder** (`cli.educates.dev/v1alpha1`):
  `EducatesLocalConfig` (laptop; lives at `<data-home>/config.yaml`),
  scenario kinds `EducatesGKEConfig` / `EducatesEKSConfig` /
  `EducatesInlineConfig`, and the `EducatesConfig` escape hatch (CR
  specs verbatim, no CLI defaults). JSON schemas are embedded at
  `client-programs/pkg/config/v1alpha1/schemas/`. Data home is
  `$XDG_DATA_HOME/educates/`, overridable via `EDUCATES_CLI_DATA_HOME`.
  See decisions log.

**Carvel libraries still live in the CLI** (`carvel.dev/imgpkg`,
`kapp`, `ytt`, `vendir`) — they power the **workshop tooling**
(`educates {cluster,docker} workshop ...` commands for publish /
deploy / serve). The install path no longer touches them.

**The Educates runtime is not changing in v4.** Components in
`session-manager/`, `secrets-manager/`, `lookup-service/`,
`training-portal/`, `tunnel-manager/`, `workshop-images/`, and supporting
services keep their current Python/kopf/Django implementations. Only the
installation mechanism and packaging changes.

Phases 0–5 are complete. The active work is post-Phase-5 hardening
(operator follow-ups from `docs/architecture/follow-up-issues.md`, CI
drift checks) and Phase 6 release prep. Day-to-day, that's what code
changes should advance.

---

## Repository scope: what's safe to change vs not

When working on v4 installer tasks:

**Safe to create/modify:**
- New code for the v4 installer (operator, CRDs, Helm charts). Charts should
  live in `installer/charts`, operator code in `installer/operator`,
  vendored upstream Helm charts in `installer/operator/vendored-charts`.
- The CLI in `client-programs/` — needs significant changes for v4. The
  existing Carvel-related code will be removed; new commands wrap the
  Helm-chart + CR-apply workflow.
- Documentation in `project-docs/` for v4 installation flows.
- Architecture documents in `docs/architecture/`.

**Don't touch unless explicitly asked:**
- `session-manager/`, `secrets-manager/`, `training-portal/`,
  `lookup-service/`, `tunnel-manager/`, `node-ca-injector/`,
  `assets-server/`, `image-cache/` — runtime components, not changing in v4.
- `workshop-images/` — workshop runtime, orthogonal to installer work.
- (Deleted) `carvel-packages/` and `vendir.yml` — gone with v3,
  including the release workflow's old "Publish educates-installer
  bundle" job (removed 2026-06-10; a v4 chart-publish step replaces it
  in Phase 6).

**Special case:** if a v4 task needs a runtime component change (very rare —
e.g., a config flag the runtime needs to consume differently), flag it
explicitly before changing the runtime. These changes have wider
implications.

---

## Working norms

How I expect to collaborate:

- **Ask before destructive operations.** `rm`, `git push`, `git reset --hard`,
  deleting files outside `/tmp` — confirm first.
- **Don't push to remote.** Commits are fine when asked; pushes are mine.
- **Small, focused commits with clear messages.** Conventional Commits style
  preferred (`feat(operator): ...`, `fix(crd): ...`).
- **No `Co-Authored-By: Claude` trailers in commit messages.** Commits are
  authored by me; collaboration with you is a working detail, not a
  history artefact. Plain commit message body, no attribution footer.
- **Don't run tests unless asked.** I'll often want to inspect intermediate
  state without test runs interfering.
- **Verify Helm chart values before suggesting them.** `helm show values
  <chart>` is the source of truth, not your training data.
- **Verify Kubernetes API semantics before suggesting them.** Especially
  finalizers, namespace deletion, controller-runtime cache behavior. When in
  doubt, ask me to verify, or read the source.
- **Prefer asking over assuming.** A clarifying question costs me 10 seconds.
  Wrong code costs me 30 minutes of debugging plus the time to undo it.
- **When uncertain about a design decision, stop and ask.** Don't pick a
  direction and run with it.
- **Always draft a plan or check-list when working with complex tasks**. If you envision
  the task is going to be long, either plan properly if needed, or at least create
  a task-list to track progress.

---

## Reference documents (read these when relevant)

These documents live in `docs/architecture/` and contain decisions that
shouldn't be relitigated:

- **`educates-crd-draft-v1alpha1-r3.md`** — full design of the four v4 CRDs
  (EducatesClusterConfig, SecretsManager, LookupService, SessionManager).
  Currently at revision 3. Read before any CRD-related work.
- **`educates-v4-development-plan.md`** — phased plan for v4 implementation, estimates,
  collaboration playbook, risks, open items. Read before starting a new
  phase.
- **`decisions.md`** — log of architectural decisions and their rationale.
  One paragraph per decision. Update when significant decisions are made.

If something I ask contradicts these docs, point at the doc. If the doc is
wrong, we update it explicitly — don't silently diverge from it.

---

## Build and run commands

### Install path (v4 only)

CLI-driven (laptop or single command from CI):

```bash
educates local secrets add ca <domain>-ca --domain <domain>   # one-time: generate self-signed CA
educates local config init                                    # one-time: write a minimal config
educates local cluster create                                 # kind + registry + deploy in one
# Or after the cluster's already up:
educates admin platform deploy --local-config
educates admin platform render --local-config                 # dry-run / GitOps preview
educates admin platform delete                                # uninstall
```

Raw helm + kubectl path (no CLI):

```bash
helm install educates-installer ./installer/charts/educates-installer \
  --namespace educates-installer --create-namespace
kubectl apply -f educates-cluster-config.yaml
kubectl apply -f educates-secrets-manager.yaml
kubectl apply -f educates-lookup-service.yaml       # optional
kubectl apply -f educates-session-manager.yaml
```

#### Operator project (Phase 0+)

The Go operator lives at `installer/operator/` (kubebuilder project,
with the standard `config/` kustomize tree stripped — `controller-gen`
writes CRDs and RBAC directly into the `educates-installer` chart).
Module path: `github.com/educates/educates-training-platform/installer/operator`,
in `go.work`. API packages: `api/config/v1alpha1` (EducatesClusterConfig)
and `api/platform/v1alpha1` (SecretsManager, LookupService,
SessionManager).

Make targets, all run from `installer/operator/`:

```bash
make manifests                 # Regenerate CRDs + RBAC into the chart
make generate                  # Regenerate deepcopy
make test                      # Run envtest (downloads binaries on first run)
make envtest                   # Just download envtest binaries
make docker-build              # Build local operator image (Phase 0 dev only)
make smoke-test                # kind + helm install + apply CR + assert log line
make lint                      # golangci-lint
make vendor-charts             # Download upstream charts into vendored-charts/, verify SHA256
make verify-vendored-charts    # Re-verify SHA256 of tarballs already on disk
```

Phase status (as of 2026-06-10):

- **Phase 0 (foundations) — done.** Scaffold, CRDs, chart, envtest, smoke
  test, CI all in place. Reconcilers were stubs.
- **Phase 1 (Inline mode) — done.** EducatesClusterConfig Inline-mode
  validator + watches + finalizer + status contract live; the three
  platform reconcilers (SecretsManager, LookupService, SessionManager)
  are still stubs until Phase 4.
- **Phase 2 (Bundled cert-manager end-to-end) — done.** Session 1
  groundwork (vendoring + Helm SDK wrapper + typed cert-manager access)
  plus Session 2's three commits land the full Managed-mode pipeline:
  embedded-chart install via `//go:embed`; cert-manager Deployment
  readiness gate; CustomCA Secret copy operator-ns → cert-manager-ns;
  ClusterIssuer + wildcard Certificate via SSA (field manager
  `educates-installer`); `status.ingress` published with
  wildcardCertificateSecretRef + clusterIssuerRef; `CertificatesReady`
  condition tied to `Certificate.Ready`; finalizer drains in reverse
  install order.
- **Phase 3 (Contour + external-dns + Kyverno + ACME) — done.** The
  remaining three cluster services land with the same shape as Phase 2:
  vendored Helm chart, Deployment-readiness gate, finalizer drain in
  reverse-install order, per-service Ready condition
  (`IngressReady`, `DNSReady`, `PolicyEnforcementReady`) and
  Bundled-chart version published to `status.bundledChartVersions`.
  cert-manager grows ACME-DNS01 support for Route53 (IRSA on EKS) and
  CloudDNS (Workload Identity on GKE); static-credentials Secrets and
  Cloudflare/AzureDNS providers are reserved in the CRD but rejected as
  "not yet supported" until follow-ups land them. Real-cluster
  verification: kind (CustomCA + Contour, samples/01) and GKE
  (CloudDNS-ACME + external-dns + Kyverno, samples/02).
  Sample CRs live under `installer/samples/`.
- **Phase 4 (platform component reconcilers) — done (2026-05-14).**
  Three sessions: SecretsManagerReconciler, LookupServiceReconciler,
  SessionManagerReconciler. Each gates on
  `EducatesClusterConfig.status` being Ready (SessionManager
  additionally on SecretsManager.Ready), installs its component as a
  Helm release from the vendored runtime subchart tarballs
  (secrets-manager, lookup-service, session-manager + the
  node-ca-injector / remote-access extras) into the shared `educates`
  namespace, and drains it via finalizer. Four SessionManager spec
  blocks are reserved but unwired (`themes`/`defaultTheme`,
  `defaultAccessCredentials`, `imagePrePuller`, `registryMirrors`) —
  see follow-up-issues.md. Known gap: deleting EducatesClusterConfig
  before the platform CRs wedges SessionManager's helm uninstall
  (Kyverno CRDs already gone) — fix tracked as the
  `PlatformCRsPresent` finalizer guard.
- **Phase 5 (CLI rewrite) — done (2026-06-06).** All 11 steps landed:
  kind-discriminated config loader + embedded JSON schemas,
  CRD-derived `EducatesConfig` schema (`make generate-cli-schemas`),
  translator (kind → operator chart values + 4 CRs), `admin platform
  render/deploy/delete` (deploy owns the CRD lifecycle and waits
  Ready; delete drains in reverse order with `--yes`/`--purge`),
  schema-aware `local config init/get/set/view/edit`, `local cluster
  create` tail-calling platform deploy with preflight checks, Carvel
  install path deleted, first-run v3→v4 migration shim, and the
  GKE/EKS/Inline scenario kinds. Open follow-ups tracked in
  follow-up-issues.md.

Living conventions (carry across phases unless superseded):

- **Spec types carry the full r3 shape from day one.** Status grows
  alongside the reconciler that produces each field. See decisions log.
- **CEL rules:** EducatesClusterConfig has three structural CEL rules
  on spec — singleton name, mode immutability, mode-field exclusivity.
  The three platform CRDs have singleton-name only.
- **RBAC:** EducatesClusterConfig reconciler has read-only `get/list/watch`
  on its referenced kinds (Secrets, ClusterIssuers, IngressClasses) plus
  full access on its own kind. Platform reconcilers carry full access
  on their own kinds plus what their chart installs need — grown in
  Phase 4 when the reconcilers came online.
- **Watches:** Secret + IngressClass + Deployment are registered
  in `SetupWithManager` (operator-namespace-scoped Secret cache;
  cluster-scoped IngressClass; Deployment cluster-wide).
  cert-manager.io ClusterIssuer + Certificate are *deferred* —
  registered at runtime by `CRDWatcher`
  (`internal/controller/config/crd_watcher.go`) once discovery
  confirms the CRDs exist. Activation lag is up to the
  CRDWatcher's `PollInterval` (15s in production). cert-manager
  CRDs are **not** a prerequisite for operator startup as of
  2026-05-13; the 2026-05-06 prerequisite decision was reversed
  via the deferred-watch pattern, after a first attempt with
  unstructured watches proved insufficient. See decisions log.
- **Watch event filtering:** Each watched kind has its own mapping
  function (`mapSecretToSingleton`, `mapIngressClassToSingleton`,
  etc., in `watches.go`) that drops events the reconciler can't
  act on — referenced from spec or operator-owned only. Eliminates
  the cluster-wide reconcile flood that cert-manager bootstrap
  used to trigger. Each new watched kind added in Phase 3 gets
  its own narrowing mapper. Deferred-watch kinds use the same
  mapper machinery — `CRDWatcher.Targets` carries `(GVK, MapFunc)`
  pairs.
- **cert-manager.io access split:** watches go through
  CRDWatcher's deferred-registration path (no GVK-at-startup
  requirement); Get / Create / Update / SSA-patch calls use
  typed `cmv1.*` (those only run after `ensureCertManagerReady`
  confirms the CRDs are present, so typed GVK resolution always
  succeeds). On CRD *removal* post-activation (rare; mostly the
  end of Managed-mode teardown), the reconciler classifies the
  resulting `NoMatchError` / 404-with-UnexpectedServerResponse
  via `isCertManagerCRDMissingErr` and surfaces
  `CertificatesReady=False reason=CertManagerCRDsMissing`,
  `phase=Degraded`. Controller-runtime's underlying Kind source
  keeps spamming retry errors in the log until pod restart —
  captured as a follow-up.
- **Helm SDK:** v4 (`helm.sh/helm/v4`), wrapped by
  `installer/operator/internal/helm` so reconcilers don't repeat
  `action.Configuration` boilerplate. Use `helm.NewClient(restCfg, ns)`
  in production and `helm.NewMemoryClient(ns)` in tests.
- **Vendored upstream charts** live at `installer/operator/vendored-charts/<name>-<version>.tgz`,
  integrity-recorded in `SHA256SUMS`. The operator loads them via
  `helm.LoadArchive`. Refresh with `make vendor-charts` after updating
  the version + hash entries. No `educates-cluster-services` umbrella
  chart exists or is planned — the operator is the sole installer of
  cluster services.
- **Operator image:** local-dev placeholder + `make docker-build` only.
  Publish-time annotations + release workflow land in Phase 6. Running
  `helm install` against the chart from a clone requires `make
  docker-build` + `kind load` first.
- **Operator namespace** is supplied via the `OPERATOR_NAMESPACE` env
  var (downward API in the chart Deployment). User-supplied Secrets
  referenced from `EducatesClusterConfig.spec.inline` must live there.

### Common

```bash
make build-client-programs                    # Build educates CLI binary
cd client-programs && go mod tidy             # Tidy CLI deps
cd node-ca-injector && go test ./...          # Run Go tests
make build-project-docs                       # Build Sphinx docs
make prune-all                                # Clean caches and build artifacts
```

---

## Architecture

### Runtime components

- **session-manager/** — Python/kopf operator managing workshop sessions,
  environments, allocations, training portals, vcluster integration. Main
  control plane.
- **secrets-manager/** — Python/kopf operator handling secret copying,
  exporting, importing, injection across namespaces.
- **training-portal/** — Django web app, user-facing portal. Exposes REST API.
- **lookup-service/** — Python service for service discovery.
- **tunnel-manager/** — Hybrid Python/Go operator for network tunneling.
- **node-ca-injector/** — Go program injecting CA certificates into
  Kubernetes nodes.
- **workshop-images/** — Dockerfiles for workshop session containers.

### Installer

- **installer/charts/educates-installer/** — the operator Helm chart.
  Installed by `educates admin platform deploy` (via embedded copy at
  `client-programs/pkg/deployer/chart/files/`) or by raw `helm install`.
- **installer/operator/** — the Go operator that reconciles the four
  CRDs (EducatesClusterConfig, SecretsManager, LookupService,
  SessionManager). Uses helm.sh/helm/v4 SDK to install cluster
  services (cert-manager, contour, kyverno, external-dns) from
  vendored upstream charts.
- **client-programs/** — Go CLI (`educates`). Holds the v4 install
  pipeline (load → translate → helm install + apply CRs + wait Ready)
  and the workshop tooling (publish, deploy, render — these still
  use carvel libs for OCI bundle and kapp deploy).

### Go workspace

`go.work` covers `client-programs/` and `node-ca-injector/`. Other Go
services (`assets-server/`, `tunnel-manager/`) have their own `go.mod` and
are built only via Docker.

---

## Architecture (target state — v4)

The v4 installer has three layers:

1. **`educates-installer` Helm chart** — installs the operator, four CRDs,
   and RBAC. This is what users `helm install` (imperative) or what
   ArgoCD/Flux points at (declarative). Same artifact, both paths.
2. **Go operator** — reconciles the four CRDs. Uses Helm Go SDK to install
   upstream charts (cert-manager, contour, kyverno, external-dns) and the
   `educates-training-platform` chart for the runtime. Has finalizers for
   clean uninstall.
3. **`educates-training-platform` Helm chart** — umbrella chart with three
   subcharts (secrets-manager, lookup-service, session-manager). Installs
   the Educates runtime. Also usable standalone for users who don't want
   the operator.

The four CRDs (all cluster-scoped, all singletons named `cluster`):

- **`EducatesClusterConfig`** (`config.educates.dev/v1alpha1`) —
  cluster-wide infrastructure and services. Two modes: `Managed` (operator
  installs services) and `Inline` (user declares pre-existing resources).
- **`SecretsManager`** (`platform.educates.dev/v1alpha1`) — secrets-manager
  component.
- **`LookupService`** (`platform.educates.dev/v1alpha1`) — lookup-service
  component.
- **`SessionManager`** (`platform.educates.dev/v1alpha1`) — session-manager
  component. Requires SecretsManager.Ready.

Components consume `EducatesClusterConfig.status` as their input contract.
They never read its `.spec` directly. This is what lets Inline mode work
without components knowing the difference.

For full schema details, see `docs/architecture/educates-crd-draft-v1alpha1-r3.md`.

---

## Key conventions and gotchas

**CRDs and Go types:**
- Field names follow Kubernetes conventions: `camelCase` in YAML/JSON,
  `PascalCase` in Go structs, lowercase tags.
- Use `+kubebuilder:` markers for validation, defaults, printer columns.
- Singleton enforcement uses CEL: `self.metadata.name == 'cluster'`.
- Mode immutability uses CEL with `oldSelf`.

**Status conventions:**
- Status is the public interface for component CRs and humans. Treat it as
  versioned API surface.
- Keep status minimal — only the inter-CR contract plus conditions.
- Use standard Kubernetes condition types (`Ready`, plus PascalCase
  domain-specific ones like `CertificatesReady`).

**Watches:**
- Reconcilers must watch referenced resources (Secrets, ClusterIssuers,
  IngressClasses), not just react to CR generation changes. External
  changes — like a deleted TLS secret — must propagate to status within
  seconds.
- Use controller-runtime `.Watches()` with a mapping function targeting
  resources by name.

**Helm SDK usage:**
- Target `helm v4`
- Use `helm.sh/helm/v4/pkg/action` for chart operations.
- Don't shell out to the `helm` binary; use the SDK in-process.
- Always verify chart values against the upstream chart's current
  `values.yaml`. Don't rely on memory.

**Readiness checks for cluster services:**
- `Deployment.status.availableReplicas == replicas` is necessary but not
  sufficient.
- For cert-manager: also verify the API discovery responds (e.g.,
  `GET /apis/cert-manager.io/v1` returns 200).
- For Kyverno: similar webhook readiness check.
- For Contour: IngressClass exists and the controller is healthy.

**Local dev environment:**
- Local kind cluster expects port 5001 for the local Docker registry.
- macOS users may have an `educates resolver` providing `*.educates.test`
  resolution to a local IP (typically `10.10.10.1`).
- Use `educates create-cluster --cluster-only` if you want kind without v3
  Educates installed, then test v4 against it.

**Image relocation:**
- For air-gapped, prefer `helm dt wrap`/`unwrap` or equivalent at build
  time, not runtime.
- For online but mirrored registries, use the
  `EducatesClusterConfig.spec.imageRegistry.prefix` field.
- Don't replicate `kbld`-style runtime digest resolution in the operator;
  pin in published values files instead.

---

## Glossary

Educates uses domain-specific terms that have precise meanings:

- **Workshop** — a Kubernetes resource (Workshop CR) defining a workshop's
  content, environment requirements, session lifetime, etc. The recipe.
- **Workshop Environment** — a deployed instance of a workshop, ready to
  spawn sessions. Created by the training portal from a Workshop.
- **Workshop Session** — an actual user's session, with its own namespace,
  resources, and (optionally) vcluster.
- **Training Portal** — a deployed instance of the user-facing UI. Multiple
  training portals can exist on one Educates install, each serving
  different workshops.
- **Cluster service** — an operational dependency of Educates that lives at
  cluster scope (cert-manager, contour, kyverno, external-dns). Distinct
  from Educates components.
- **Educates component** — secrets-manager, lookup-service, session-manager.
  These three are individually deployable; session-manager depends on
  secrets-manager.
- **BYO** — Bring Your Own. Used when the user provides a cluster service
  themselves (their own cert-manager, their own ingress controller) and
  Educates uses it via External-mode discriminators or full Inline mode.
- **Managed mode** vs **Inline mode** — `EducatesClusterConfig.spec.mode`
  values. Managed = operator installs cluster services. Inline = user
  asserts what already exists.
- **Operator namespace** — the namespace where the v4 operator runs and
  where it expects to find user-provided Secrets (TLS, CA, image-pull
  secrets) referenced by name in CRs.

---

## When in doubt

- If a question is architectural and there's no docs-of-record for it,
  start a conversation in Claude Desktop and update the docs after.
- If a question is about how to implement something concrete, ask in this
  Claude Code session.
- If unsure which it is, default to asking. Asking is cheap.
