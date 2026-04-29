# Educates Training Platform — Current State Summary

> Source: public documentation at docs.educates.dev and educates.dev blog posts (accessed April 2026),
> plus the repository at github.com/educates/educates-training-platform (structure deduced from
> vendir.yml, release artifacts, and issues — not from direct code reading).
>
> **Confidence levels:**
> - **High confidence:** installer CLI behavior, config structure, supported providers, cluster packages.
> - **Medium confidence:** exact repo directory layout (deduced, not browsed file-by-file).
> - **Inferred:** runtime operator internals (session-manager, training-portal) — known to be Python+kopf
>   from error logs, but not studied in detail for this summary.

---

## 1. What Educates Is

A Kubernetes-based platform for hosting interactive workshop environments. Self-hosted, CNCF-adjacent,
Apache 2.0. Spun out of VMware/Broadcom, now independent.

**Core runtime components** (deployed into the cluster, not the installer):

- `session-manager` — Python/kopf operator managing `Workshop`, `WorkshopEnvironment`, `WorkshopSession`, `WorkshopRequest`.
- `training-portal` — user-facing web UI for browsing/starting workshops, plus REST API.
- `secrets-manager` — custom secret copier/injector (replaces Carvel secretgen for security reasons).
- `tunnel-manager` — for exposing services out of workshop sessions.
- `lookup-service`, `assets-server`, `image-cache`, `docker-registry` — supporting services.
- Workshop runtime images: `base-environment`, `jdk{8,11,17,21}-environment`, `conda-environment`, `docker-in-docker`, `loftsh-vcluster`, multiple `rancher-k3s` versions.

**Core CRDs** (`training.educates.dev/v1beta1`):

- `TrainingPortal` — top-level, what admins create to expose workshops.
- `Workshop` — definition of a single workshop.
- `WorkshopEnvironment`, `WorkshopSession`, `WorkshopRequest` — internal, created by the training portal.

---

## 2. Current Installer Architecture (Educates 3.x)

### The CLI

- Binary name: `educates`
- Language: **Go**
- Distribution: single-binary releases for `{darwin,linux}-{amd64,arm64}`.
- Key design choice: **embeds Carvel tools as Go libraries**, not as shell-outs. Uses ytt for templating, kbld for image resolution, kapp for apply/reconcile. This is in-process, no external binaries needed at runtime.

### The two installation paths (must produce equivalent cluster state)

**Path 1 — Imperative (CLI):**
```
educates deploy-platform --config config.yaml [--verbose]
```
- User runs CLI from their laptop / CI.
- CLI reads config, expands it via YTT, resolves images, applies via kapp.
- Blocks until reconciled.
- Counterpart: `educates delete-platform` (or similar; exact name TBD).

**Path 2 — Declarative (kapp-controller):**
```bash
# 1. Install kapp-controller (if not already present)
kubectl apply -f https://github.com/vmware-tanzu/carvel-kapp-controller/releases/latest/download/release.yml

# 2. Install RBAC (namespace educates-installer is created here)
kubectl apply -f https://github.com/educates/educates-training-platform/releases/latest/download/educates-installer-app-rbac.yaml

# 3. Create the config secret
kubectl create secret generic educates-installer -n educates-installer \
  --from-file config.yaml --save-config

# 4. Create the App CR that references the installer bundle
kubectl apply -f https://github.com/educates/educates-training-platform/releases/latest/download/educates-installer-app.yaml
```
- kapp-controller reconciles the App CR.
- Updates: replace the secret; kapp-controller re-reconciles (or `kctrl app kick -a installer.educates.dev -n educates-installer`).
- Uninstall: `kubectl delete -n educates-installer app/installer.educates.dev`.

### Additional CLI commands worth knowing

- `educates create-cluster` — local kind cluster + platform (all in one).
- `educates delete-cluster` — tears down local kind cluster.
- `educates admin platform config --local-config` — generate a minimal config template.
- `educates admin platform values --local-config` — show the expanded internal values (after YTT transformation). This is the "internal" view.
- `educates local config` — local environment settings (resolvers, mirrors etc.).
- `educates deploy-workshop -f <url>` — deploy an individual workshop.
- `educates cluster workshop-request` — test portal REST API.

### Key observation: two configurations today

1. **User-facing config** (what they write) — minimal, e.g.:
   ```yaml
   clusterInfrastructure:
     provider: kind
   clusterPackages:
     contour: {enabled: true}
     kyverno: {enabled: true}
     educates: {enabled: true}
   clusterSecurity: {policyEngine: kyverno}
   clusterIngress: {domain: 172.20.10.12.nip.io}
   workshopSecurity: {rulesEngine: kyverno}
   ```

2. **Expanded internal values** (what actually drives the installer) — detailed, per-package, with image pins, per-provider defaults. Produced by YTT transformation of (1).

Same schema top-level, different level of detail. This duality is one of the pain points the user flagged.

---

## 3. Configuration Schema (Current)

Top-level sections:

| Section | Purpose | Example keys |
|---------|---------|-------------|
| `clusterInfrastructure` | Which K8s flavour + cloud-specific params | `provider`, `gcp.*`, `aws.*` |
| `clusterPackages` | Which cluster services to install and how | `contour`, `cert-manager`, `external-dns`, `certs`, `kyverno`, `kapp-controller`, `educates` |
| `clusterSecurity` | Cluster-wide policy engine | `policyEngine`: kyverno / pod-security-policies / security-context-constraints |
| `clusterIngress` | Ingress domain config | `domain`, TLS refs |
| `workshopSecurity` | Per-workshop policy engine | `rulesEngine`: kyverno |

**Supported infrastructure providers:**

- `kind` — local Docker-based
- `minikube` — local
- `eks` — Amazon EKS (needs `aws.region`, `aws.route53.hostedZone`, `aws.irsaRoles.{external-dns,cert-manager}`)
- `gke` — Google GKE (needs `gcp.project`, `gcp.cloudDNS.zone`, `gcp.workloadIdentity.{external-dns,cert-manager}`)
- `openshift` — uses SCC instead of kyverno for cluster security, native OpenShift ingress
- `vcluster` — Loft vCluster (minimal install, no ingress controller from installer)
- `generic` — user brings ingress + DNS, installer does minimum
- `custom` — user provides everything themselves

**Cluster packages (things the installer installs alongside Educates):**

| Package | Default state | Purpose |
|---------|---------------|---------|
| `contour` | Usually enabled (not on vcluster/openshift) | Ingress controller |
| `cert-manager` | Enabled on eks/gke | TLS cert management |
| `external-dns` | Enabled on eks/gke | DNS record management |
| `certs` | Enabled on eks/gke | Creates ACME wildcard ClusterIssuer |
| `kyverno` | Enabled by default | Policy engine (cluster + workshop) |
| `kapp-controller` | Enabled only on specific cases | Needed if workshops themselves use it |
| `educates` | Always enabled | The actual training platform |

---

## 4. Repo Structure (Deduced)

```
educates-training-platform/
├── README.md
├── vendir.yml                    # Tracks upstream vendored deps
├── project-docs/                 # Sphinx docs (what's at docs.educates.dev)
├── carvel-packages/
│   └── installer/
│       └── bundle/
│           └── config/
│               └── ytt/
│                   └── _ytt_lib/
│                       ├── infrastructure/        # Per-provider overlays
│                       │   ├── kind/
│                       │   ├── gke/
│                       │   ├── eks/
│                       │   ├── openshift/
│                       │   ├── vcluster/
│                       │   └── ...
│                       └── packages/              # Each cluster service
│                           ├── cert-manager/
│                           │   └── upstream/      # Vendored via vendir
│                           ├── contour/
│                           ├── external-dns/
│                           ├── kyverno-restricted/
│                           ├── kyverno-baseline/
│                           ├── kyverno-policies/
│                           └── educates/          # The training platform chart/ytt
├── client-programs/              # Likely the Go CLI source
├── session-manager/              # Python/kopf operator
├── training-portal/              # Python/Django web app
├── secrets-manager/              # Python/kopf operator
├── tunnel-manager/               # Likely Go
├── workshop-images/
│   ├── base-environment/
│   ├── jdk8-environment/
│   ├── jdk11-environment/
│   └── ...
└── [other component directories]
```

Related repos in the `educates` org:
- `educates-training-platform` — main monorepo (above)
- `lab-*` — sample workshops
- `labs-installation-guides` — workshops about installation
- `educatesenv` — version manager for the CLI

---

## 5. Local vs Cloud Differences

### Local (`kind`, `minikube`)

- Ingress: Contour bound to `hostPorts` so it's reachable via `localhost`.
- DNS: `nip.io` (embeds IP in hostname) or Educates Local Resolver (macOS DNS resolver config).
- Registry: local image registry deployed alongside the cluster.
- TLS: self-managed certs (CLI manages a local CA).
- Policy: kyverno (default).
- No external-dns, no cert-manager.
- One-command: `educates create-cluster` does kind + registry + platform.

### Cloud (`gke`, `eks`)

- Ingress: Contour with a cloud LB service.
- DNS: external-dns reconciling Route53 / Cloud DNS, needs IAM/Workload Identity.
- TLS: cert-manager with ACME ClusterIssuer.
- Workload identity must be set up *before* installer runs.
- No one-command; user creates cluster first, then `educates deploy-platform`.

### vCluster / OpenShift

- Assume the host cluster has ingress + DNS handled.
- Install only Educates + minimal policy (Kyverno or OpenShift SCC).

---

## 6. Known Pain Points (From User's Own Framing)

1. **Carvel is poorly maintained** (former team gone, mostly dependabot updates).
2. **Owning the "cluster services" story** (cert-manager, external-dns, kyverno, contour, kapp-controller) is a drag — updates lag because installer vendors them.
3. **kapp is great imperatively, but the declarative story requires kapp-controller** — users don't want it.
4. **Two configurations** (user config + expanded internal config) is a duality users don't see but that shapes the implementation.
5. **Configuration mixes three concerns** that should be separate: local-dev-only settings, infrastructure provider config, Educates-proper config.

---

## 7. Proposed Future Architecture (Validated)

### High-level shape

```
┌──────────────────────────────────────────┐
│  User runs `helm install educates-...`   │
│            OR points ArgoCD/Flux at it   │
└──────────────┬───────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────┐
│ Thin Helm chart: educates-installer      │
│ - Installs operator Deployment           │
│ - Installs CRDs (EducatesPlatform + ... )│
│ - Installs RBAC                          │
└──────────────┬───────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────┐
│ User (or CLI) applies EducatesPlatform CR│
│ (cluster-scoped, named `cluster`)        │
│                                          │
│ spec.infrastructure: ...                 │
│ spec.clusterServices: ...                │
│ spec.educates: ...                       │
└──────────────┬───────────────────────────┘
               │ reconciled by
               ▼
┌──────────────────────────────────────────┐
│ Go operator (new)                        │
│ - Uses Helm Go SDK to install upstream   │
│   charts: cert-manager, contour,         │
│   external-dns, kyverno                  │
│ - Creates ClusterIssuer, Certificate     │
│   etc. as CRs after deps are ready       │
│ - Installs educates-training-platform    │
│   Helm chart                             │
│ - Has finalizer → clean uninstall in     │
│   reverse order                          │
└──────────────┬───────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────┐
│ Educates runtime (unchanged):            │
│ - session-manager (Python/kopf)          │
│ - training-portal (Python/Django)        │
│ - secrets-manager                        │
│ - etc.                                   │
└──────────────────────────────────────────┘
```

### CLI role (thin)

- `educates install` → `helm install` + `kubectl apply` of CR (or just renders, user applies).
- `educates uninstall` → delete CR (wait for finalizer) + `helm uninstall`.
- `educates apply -f config.yaml` → render CR and apply.
- `educates render -f config.yaml` → output CR to stdout (for GitOps).
- Local-dev-only commands (`create-cluster`, `delete-cluster`) stay as they are — they're orthogonal to the platform install, they're about kind setup + calling the platform install.

### Where current pieces map

| Current | Future |
|---------|--------|
| Go CLI with embedded Carvel libs | Go CLI (much thinner) + Go operator (new) |
| Carvel bundles vendoring cert-manager etc. | Helm SDK calls to upstream charts |
| YTT overlays for per-provider config | Operator Go code (switch on `spec.infrastructure.provider`) |
| ytt-rendered Educates manifests | `educates-training-platform` Helm chart |
| Config secret + App CR | `EducatesPlatform` CR (singleton) |
| Python/kopf session-manager, training-portal | **Unchanged** — these are runtime, not install |

### What we'd NOT rewrite

- `session-manager` (Python/kopf) — leave it.
- `training-portal` (Python/Django) — leave it.
- `secrets-manager` — leave it.
- Workshop image build pipeline — leave it.

The migration is purely about the installer + CLI. Everything else is fine.

---

## 8. Open Questions to Validate Before Implementation

1. Which user flows must remain unchanged? (e.g., `educates create-cluster` one-shot on a laptop)
2. Does "one `EducatesPlatform` per cluster" hold? (Answered: yes, per earlier conversation.)
3. Will the new CR schema be `v1alpha1` with an explicit migration path from the old YAML config?
4. Does the local `kind` flow need to bypass the operator entirely (since it's known-good)? Or go through the same code path as cloud?
5. What's the strategy for upstream chart versions — vendored as values, or discovered at runtime?
6. For `cert-manager` specifically, what's the ordering contract? (Install chart → wait for webhook ready → create ClusterIssuer → wait for ready → install rest.)
7. Are users OK with a persistent operator in-cluster, or should we consider a run-once Job model as simpler alternative?
