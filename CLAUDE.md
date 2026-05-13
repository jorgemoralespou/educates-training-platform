# CLAUDE.md

This file is a briefing for Claude Code working in this repository. Read it
fully before doing anything substantial. Sections are ordered by importance —
the top is what changes day-to-day, the bottom is mostly stable.

---

## What's happening right now

This repository is mid-transition between two major versions of Educates.

- **Educates v3 (current state of the repo):** Carvel-based installer
  (`carvel-packages/installer/`), Go CLI embedding ytt/kbld/kapp/imgpkg as
  libraries, kapp-controller for declarative installs.
- **Educates v4 (in development):** Helm-chart + Go operator installer.
  Replaces `carvel-packages/installer/` and the kapp-based deploy/delete CLI
  flows. Adds four new CRDs and a Go operator that reconciles them.

**Critically: v4 is a breaking change from v3.** Users upgrading must
delete v3 and reinstall under v4. There is no in-place migration; only a
one-shot config translation tool (`educates migrate-config`).

**The Educates runtime is not changing in v4.** Components in
`session-manager/`, `secrets-manager/`, `lookup-service/`,
`training-portal/`, `tunnel-manager/`, `workshop-images/`, and supporting
services keep their current Python/kopf/Django implementations. Only the
installation mechanism and packaging changes.

The active work is the v4 installer. Day-to-day, that's what code changes
should advance.

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
- `carvel-packages/` — being replaced wholesale by v4. Don't refactor;
  it'll be deleted. Only touch for security fixes during v3 maintenance.
- `vendir.yml` — only relevant to the Carvel-based v3 installer.

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

### v3 (existing, still works)

The v3 installer requires a local Docker registry at `localhost:5001`:

```bash
educates create-cluster --cluster-only        # Create kind cluster
educates local config view > developer-testing/educates-installer-values.yaml
make build-core-images                        # Build core platform images
make deploy-platform                          # Deploy v3 platform
make delete-platform                          # Remove v3 deployment
```

### v4 (under development)

Commands will be added as Phase 5 (CLI rewrite) progresses. Pre-Phase 5,
the v4 install path is:

```bash
helm install educates-installer ./installer/charts/educates-installer
kubectl apply -f educates-cluster-config.yaml
kubectl apply -f educates-components.yaml
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

Phase status (as of 2026-05-11):

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
  install order. Currently scoped to
  `provider: BundledCertManager, issuerType: CustomCA` —
  ACME/Static/External providers return explicit "not yet supported"
  validation errors. Phase 3 picks up Contour/Kyverno/external-dns
  next.

Living conventions (carry across phases unless superseded):

- **Spec types carry the full r3 shape from day one.** Status grows
  alongside the reconciler that produces each field. See decisions log.
- **CEL rules:** EducatesClusterConfig has three structural CEL rules
  on spec — singleton name, mode immutability, mode-field exclusivity.
  The three platform CRDs have singleton-name only.
- **RBAC:** EducatesClusterConfig reconciler has read-only `get/list/watch`
  on its referenced kinds (Secrets, ClusterIssuers, IngressClasses) plus
  full access on its own kind. Platform reconcilers have only their own
  kinds — they grow when their reconcilers come online in Phase 4.
- **Watches:** Secret + IngressClass + ClusterIssuer + Certificate +
  Deployment (operator-namespace-scoped Secret cache; cluster-scoped
  IngressClass; cert-manager.io kinds registered as
  `unstructured.Unstructured`; Deployment cluster-wide). cert-manager
  CRDs are **not** a prerequisite for operator startup as of
  2026-05-13 — the unstructured-watch form starts on a vanilla
  cluster and events flow once the CRDs land (during Managed-mode
  install). The 2026-05-06 prerequisite decision was reversed; see
  decisions log.
- **Watch event filtering:** Each watched kind has its own mapping
  function (`mapSecretToSingleton`, `mapIngressClassToSingleton`,
  etc., in `watches.go`) that drops events the reconciler can't
  act on — referenced from spec or operator-owned only. Eliminates
  the cluster-wide reconcile flood that cert-manager bootstrap
  used to trigger. Each new watched kind added in Phase 3 gets
  its own narrowing mapper.
- **cert-manager.io access split:** watches are unstructured (no
  GVK-at-startup requirement); Get / Create / Update / SSA-patch
  calls use typed `cmv1.*` (those only run after
  `ensureCertManagerReady` confirms the CRDs are present, so typed
  GVK resolution always succeeds).
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

## Architecture (current state — v3)

### Runtime components (unchanged in v4)

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

### Installer (v3 — being replaced or partially replaced)

- **carvel-packages/installer/** — ytt/kapp/imgpkg packaging of the
  installer.
- **client-programs/** — Go CLI (`educates`). Embeds Carvel toolchain as
  Go libraries.
- **vendir.yml** — vendors upstream charts (cert-manager, contour, kyverno,
  external-dns, kapp-controller).

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
