# Educates CRDs — v1alpha1 Draft (revision 3)

> **Status:** Draft for team review. Revision 3: incorporates feedback on
> infrastructure shape, DNS placement, ACME solvers, policy structure, Helm
> values customization, image pull secrets, defaults/status strategy, and CA
> naming.

## Overview

Four CRDs, all cluster-scoped, all enforced as singletons named `cluster`:

1. **`EducatesClusterConfig`** (`config.educates.dev/v1alpha1`) — cluster-wide infrastructure and services. Two modes: **Managed** (operator installs cluster services) and **Inline** (user declares pre-existing resources).
2. **`SecretsManager`** (`platform.educates.dev/v1alpha1`) — secrets-manager component.
3. **`LookupService`** (`platform.educates.dev/v1alpha1`) — lookup-service component.
4. **`SessionManager`** (`platform.educates.dev/v1alpha1`) — the session-manager component. Requires `SecretsManager` to be Ready.

**Component contract with `EducatesClusterConfig`:** components read exclusively from `EducatesClusterConfig.status`. They don't look at `.spec`, don't care about mode. Their precondition is "an `EducatesClusterConfig` exists with `Ready: True`."

---

## Shared concepts

### Singleton enforcement

Each CRD includes a CEL validation rule:

```yaml
x-kubernetes-validations:
  - rule: "self.metadata.name == 'cluster'"
    message: "This resource must be named 'cluster' (singleton per cluster)."
```

### Mode immutability (`EducatesClusterConfig` only)

```yaml
x-kubernetes-validations:
  - rule: "self.spec.mode == oldSelf.spec.mode"
    message: "spec.mode is immutable. To switch modes, delete and recreate this resource."
```

### Common status conventions

```yaml
status:
  observedGeneration: <int>
  phase: Pending | Installing | Validating | Ready | Degraded | Uninstalling
  conditions:
    - type: Ready
      status: "True" | "False" | "Unknown"
      reason: <PascalCase>
      message: <human-readable>
      lastTransitionTime: <timestamp>
      observedGeneration: <int>
```

### Defaulting strategy

- **Static defaults go in the CRD schema** (`default:` in OpenAPI). They populate `spec` at admission time, making `kubectl get -o yaml` self-documenting.
- **Computed defaults** (those depending on other fields, e.g., `bundledContour.replicas` varying by infrastructure provider) are resolved by the reconciler.
- **Status publishes effective values** so components and humans have a single source of truth, regardless of whether values came from schema defaults, user input, or operator computation.
- **Status is kept minimal** — only the inter-CR contract plus conditions. Full config introspection is not a status concern.

### Inline-mode validation

When `EducatesClusterConfig.spec.mode: Inline`, the operator validates referenced resources before publishing status:

- Secrets exist in the operator namespace with the expected keys.
- ClusterIssuer exists and is `Ready: True` (if `clusterIssuerRef` set).
- IngressClass exists.

On failure, `phase: Degraded`, `Ready: False`, condition with a message pointing at the offending field. Components see the config as not Ready and refuse to proceed.

**Inline validation — required vs optional fields:**

| Field | Required? | Validated as |
|---|---|---|
| `inline.ingress.domain` | Required | Non-empty string, DNS-compliant |
| `inline.ingress.ingressClassName` | Required | IngressClass with this name exists |
| `inline.ingress.wildcardCertificateSecretRef.name` | Required | Secret exists, has `tls.crt` and `tls.key` keys, cert is valid for `*.<domain>` |
| `inline.ingress.caCertificateSecretRef.name` | Optional | If present: Secret exists, has `ca.crt` key |
| `inline.ingress.clusterIssuerRef.name` | Optional | If present: ClusterIssuer exists and is `Ready: True` |
| `inline.policyEnforcement.clusterPolicyEngine` | Required | Enum value |
| `inline.policyEnforcement.workshopPolicyEngine` | Required | Enum value |

### Secret key conventions

- TLS secrets: exactly `tls.crt` and `tls.key`. Standard Kubernetes `kubernetes.io/tls` type.
- CA secrets: exactly `ca.crt`. No alternative names accepted. Users with certs under other key names must rewrap.
- ImagePullSecrets: standard `dockerconfigjson` type, key `.dockerconfigjson`. Standard Kubernetes convention.

### Operator watches

The operator reconciles on:

1. Changes to the CR itself (`generation` bump).
2. Changes to **referenced resources** (Secrets, ClusterIssuers, IngressClasses) via name-targeted watches.

This means external changes — like a user deleting the TLS Secret — are detected within seconds and reflected in status. Without this, a CR could show `Ready: True` while reality has drifted.

### CEL validation strategy

All structural validation (mode/inline exclusivity, singleton name, immutability, field-presence rules) uses CEL in the CRD schema. Semantic validation (referenced resources exist and are usable) happens in the reconciler. Admission webhooks are not used in v1alpha1.

### Operational block pattern

Every Bundled cluster-service block exposes the same `operational` knobs:

```yaml
operational:
  replicas: <int>
  resources:
    requests: { cpu, memory }
    limits: { cpu, memory }
  tolerations: [...]
  nodeSelector: { ... }
  priorityClassName: <string>
  podAnnotations: { ... }
  podLabels: { ... }
```

This is intentionally duplicated in each bundled block rather than factored out, because multi-deployment charts (e.g., cert-manager with controller + webhook + cainjector) may add deployment-specific variants later. Duplication is cheaper than a clever schema reference.

---

## 1. `EducatesClusterConfig`

### Spec

```yaml
apiVersion: config.educates.dev/v1alpha1
kind: EducatesClusterConfig
metadata:
  name: cluster
spec:

  # -- MODE (immutable) -------------------------------------------------------
  mode: Managed | Inline

  # =========================================================================
  # MANAGED MODE FIELDS
  # CEL rule: mode == 'Managed' → these are valid; inline is forbidden.
  # =========================================================================

  infrastructure:
    provider: Kind | Minikube | EKS | GKE | OpenShift | VCluster | Generic

    # Common cloud config — used when provider is a cloud.
    # Omit for Kind/Minikube/OpenShift/VCluster/Generic.
    cloud:
      project: <string>                  # GCP project / AWS account alias / etc.
      region: <string>
      serviceAccounts:
        # Opaque identity strings, interpreted by provider:
        #   GKE:   GCP service account email (e.g., foo@project.iam.gserviceaccount.com)
        #   EKS:   IAM role ARN (e.g., arn:aws:iam::123:role/my-role)
        #   Other providers: documented per-provider.
        certManager: <string>
        externalDNS: <string>

  ingress:
    domain: <string>                     # wildcard subdomain, e.g., educates.example.com
    ingressClassName: <string>           # required; name of IngressClass used by Educates.
                                         # In Bundled mode: operator creates IngressClass with this name.
                                         # In External mode: existing IngressClass name.

    controller:
      provider: BundledContour | ExternalIngressController
      # Explicit — no default. User must choose.

      bundledContour:                    # when provider: BundledContour
        # replicas default: 1 for Kind/Minikube, 2 otherwise (reconciler-computed)
        operational:
          replicas: <int>
          resources: { ... }
          tolerations: [...]
          nodeSelector: { ... }
          priorityClassName: <string>
          podAnnotations: { ... }
          podLabels: { ... }

      # externalIngressController: no further config needed — ingressClassName above is the contract

    certificates:
      provider: BundledCertManager | ExternalCertManager | StaticCertificate

      bundledCertManager:
        issuerType: ACME | CustomCA

        acme:                            # when issuerType: ACME
          email: <string>
          solvers:
            dns01:                       # required — needed for wildcard certs
              provider: Route53 | CloudDNS | Cloudflare | AzureDNS

              route53:                   # when provider: Route53
                hostedZoneID: <string>
                region: <string>         # optional, defaults to infrastructure.cloud.region

              cloudDNS:                  # when provider: CloudDNS
                zone: <string>           # GCP-style zone name
                project: <string>        # optional, defaults to infrastructure.cloud.project

              cloudflare:                # when provider: Cloudflare
                apiTokenSecretRef:
                  name: <string>
                  key: api-token         # default "api-token"

              azureDNS:                  # when provider: AzureDNS
                resourceGroup: <string>
                subscriptionID: <string>

            http01:                      # optional, rarely needed given DNS01 is required for wildcards
              ingressClassName: <string> # defaults to spec.ingress.ingressClassName

        customCA:                        # when issuerType: CustomCA
          caCertificateRef:
            name: <string>               # Secret in operator namespace, keys: tls.crt, tls.key (the CA's own cert+key)

        operational: { ... }             # applies to cert-manager controller

      externalCertManager:               # cert-manager assumed installed; operator creates only the Certificate
        clusterIssuerRef:
          name: <string>

      staticCertificate:                 # user provides the wildcard TLS cert directly; no cert-manager
        tlsSecretRef:
          name: <string>                 # keys: tls.crt, tls.key
        caCertificateRef:                # optional
          name: <string>                 # key: ca.crt

  dns:
    provider: BundledExternalDNS | Manual | None
    # Static default: None (works for Kind/Minikube; cloud users must set explicitly)

    bundledExternalDNS:                  # when provider: BundledExternalDNS
      operational: { ... }
      # Note: zone auto-discovery from Ingress hostnames is default behavior.
      # Explicit zone configuration deferred to later revision.

    # Manual: user pre-created DNS records. No operator action.
    # None:   no DNS concern (Kind/Minikube with nip.io, etc.).

  policyEnforcement:
    clusterPolicy:
      engine: Kyverno | PodSecurityStandards | OpenShiftSCC | None
      # Static default: Kyverno

    workshopPolicy:
      engine: Kyverno | None
      # Static default: Kyverno
      # Setting to None disables workshop isolation — user takes responsibility.

    kyverno:                             # required if any engine above is Kyverno
      provider: Bundled | External
      # Static default: Bundled

      bundled:                           # when provider: Bundled
        operational: { ... }

      # external: no fields — user ensures Kyverno CRDs are installed

  imageRegistry:                         # optional
    prefix: <string>                     # e.g., internal-registry.corp.local/educates
    # When set, all bundled charts have image refs rewritten to use this prefix.
    # For pre-relocated bundles (via Helm dt wrap/unwrap), this is not needed.

    pullSecrets:                         # optional
      - name: <string>                   # Secret in operator namespace, type dockerconfigjson

  # =========================================================================
  # INLINE MODE FIELDS
  # CEL rule: mode == 'Inline' → this block is required; managed fields forbidden.
  # =========================================================================

  inline:
    ingress:
      domain: <string>                   # required
      ingressClassName: <string>         # required
      wildcardCertificateSecretRef:
        name: <string>                   # required; Secret with tls.crt + tls.key
      caCertificateSecretRef:            # optional
        name: <string>                   # Secret with ca.crt
      clusterIssuerRef:                  # optional
        name: <string>                   # must exist and be Ready

    policyEnforcement:
      clusterPolicyEngine: Kyverno | PodSecurityStandards | OpenShiftSCC | None
      workshopPolicyEngine: Kyverno | None

    imageRegistry:                       # optional (same shape as Managed)
      prefix: <string>
      pullSecrets:
        - name: <string>
```

### Status

```yaml
status:
  observedGeneration: <int>
  phase: Pending | Installing | Validating | Ready | Degraded | Uninstalling
  mode: Managed | Inline

  conditions:
    - type: Ready                        # aggregate
    - type: ValidationSucceeded          # Inline only; false if refs missing/invalid
    - type: InfrastructureConfigured     # Managed only
    - type: IngressReady
    - type: CertificatesReady
    - type: DNSReady
    - type: PolicyEnforcementReady

  # Minimal published interface — only what components need
  ingress:
    domain: <string>
    ingressClassName: <string>
    wildcardCertificateSecretRef:
      namespace: <string>                # always operator namespace
      name: <string>
    caCertificateSecretRef:              # optional, present if a CA exists
      namespace: <string>
      name: <string>
    clusterIssuerRef:                    # optional
      name: <string>

  policyEnforcement:
    clusterPolicyEngine: <string>        # resolved effective value
    workshopPolicyEngine: <string>

  imageRegistry:                         # always populated in status, even if empty
    prefix: <string>                     # may be empty string
    pullSecrets:
      - name: <string>

  bundledChartVersions:                  # informational; Managed mode only
    # Useful for users going External later — they know which chart/version we used.
    # May change between Educates releases. Not a stable API.
    contour: <string>
    cert-manager: <string>
    external-dns: <string>
    kyverno: <string>
```

### Design notes

- **`mode` is immutable.** To switch between Managed and Inline, delete and recreate the CR.
- **Mode-specific CEL rules** enforce exclusivity: Managed mode fields forbidden in Inline, and vice versa.
- **Schema defaults + status** mean users can leave most fields blank and still see meaningful config in `kubectl get -o yaml` (defaults populate spec) and in status (effective values).
- **`bundledChartVersions` is documented as informational.** Users going External for a service can see which chart we use internally as a reference for their own install.

---

## 2. `SecretsManager`

### Spec

```yaml
apiVersion: platform.educates.dev/v1alpha1
kind: SecretsManager
metadata:
  name: cluster
spec:
  image:                                 # optional override
    repository: <string>
    tag: <string>

  logLevel: debug | info | warn | error  # static default: info

  resources:
    requests: { cpu, memory }
    limits: { cpu, memory }
```

### Status

```yaml
status:
  observedGeneration: <int>
  phase: Pending | Installing | Ready | Degraded | Uninstalling
  conditions:
    - type: Ready
    - type: ClusterConfigAvailable
    - type: Deployed

  installedVersion: <string>
  deploymentRef:
    namespace: <string>
    name: <string>
```

### Design notes

- **No `replicas`**: secrets-manager is singleton at the pod level (today it can't scale beyond 1).
- **No `clusterConfig` block**: reads from `EducatesClusterConfig.status` implicitly.
- **ImagePullSecrets** come from `EducatesClusterConfig.status.imageRegistry.pullSecrets`.

---

## 3. `LookupService`

### Spec

```yaml
apiVersion: platform.educates.dev/v1alpha1
kind: LookupService
metadata:
  name: cluster
spec:
  ingress:
    prefix: <string>                     # required, e.g., "educates-api"
    # Full hostname: <prefix>.<EducatesClusterConfig.status.ingress.domain>

    tlsSecretRef:                        # optional override of cluster wildcard
      name: <string>

  image:
    repository: <string>
    tag: <string>

  logLevel: debug | info | warn | error  # default: info

  resources:
    requests: { cpu, memory }
    limits: { cpu, memory }

  # Auth, rate-limiting, storage settings — to be added when lookup-service owner specifies them
```

### Status

```yaml
status:
  observedGeneration: <int>
  phase: ...
  conditions:
    - type: Ready
    - type: ClusterConfigAvailable
    - type: IngressReady
    - type: Deployed

  url: <string>                          # full URL: https://<prefix>.<domain>
  installedVersion: <string>
```

---

## 4. `SessionManager`

### Spec

```yaml
apiVersion: platform.educates.dev/v1alpha1
kind: SessionManager
metadata:
  name: cluster
spec:
  # -- DEPENDENCIES ---------------------------------------------------------
  # Requires SecretsManager.Ready and EducatesClusterConfig.Ready.
  # No explicit refs — singletons.

  # -- INGRESS OVERRIDES (rare) ---------------------------------------------
  # session-manager uses the bare domain from EducatesClusterConfig.status.ingress.domain
  # directly. TrainingPortal CRs prefix it for individual portal hostnames.

  ingressOverrides:                      # optional
    tlsSecretRef:
      name: <string>
    caCertificateSecretRef:
      name: <string>
    protocol: http | https               # optional (added post-r3, 2026-06-11).
    # Asserts the scheme of generated portal/workshop URLs when TLS is
    # terminated outside the cluster (external load balancer / proxy
    # forwarding plain HTTP inward). Empty derives from TLS presence.
    # Restores v3's clusterIngress.protocol for URL generation; full
    # certificate-less installs remain a follow-up (see
    # follow-up-issues.md "External load balancer support").

  # -- WORKSHOP POLICY OVERRIDE ---------------------------------------------
  workshopPolicyOverride:                # optional
    engine: Kyverno | None
    # When set, overrides EducatesClusterConfig's workshopPolicy engine.

  # -- IMAGES ---------------------------------------------------------------
  # Image registry prefix and pullSecrets come from EducatesClusterConfig.status.imageRegistry.
  # Here we only allow per-image overrides.
  images:
    overrides:                           # optional, open list
      - name: <string>                   # e.g., "session-manager", "jdk17-environment"
        image: <string>                  # full image ref including tag or digest

  # -- THEMES ---------------------------------------------------------------
  themes:
    - name: <string>
      source:
        type: ConfigMap | Secret | URL
        configMapRef:
          name: <string>
          namespace: <string>
        # additional source types TBD by owner

  defaultTheme: <string>

  # -- ANALYTICS / TRACKING -------------------------------------------------
  tracking:
    googleAnalytics:
      trackingId: <string>
    amplitude:
      trackingId: <string>
    clarity:
      trackingId: <string>
    webhook:
      url: <string>

  # -- DEFAULTS -------------------------------------------------------------
  defaultAccessCredentials:              # optional
    username: <string>
    passwordSecretRef:
      name: <string>

  sessionCookieDomain: <string>
  allowedEmbeddingHosts:
    - <string>

  # -- STORAGE --------------------------------------------------------------
  storage:
    storageClass: <string>
    storageGroup: <int>
    storageUser: <int>

  # -- NETWORK --------------------------------------------------------------
  network:
    packetSize: <int>
    blockedCidrs:
      - <cidr>

  # -- IMAGE PRE-PULLER -----------------------------------------------------
  imagePrePuller:
    enabled: <bool>

  # -- REGISTRY MIRRORS -----------------------------------------------------
  # For workshop containers pulling from mirrored registries.
  registryMirrors:
    - mirror: <string>
      url: <string>

  # -- LOG LEVEL ------------------------------------------------------------
  logLevel: debug | info | warn | error  # default: info
```

### Status

```yaml
status:
  observedGeneration: <int>
  phase: ...
  conditions:
    - type: Ready
    - type: ClusterConfigAvailable
    - type: SecretsManagerAvailable
    - type: ComponentsDeployed
    - type: CRDsRegistered               # training.educates.dev CRDs present

  installedVersion: <string>
  trainingCRDsGroup: training.educates.dev
  components:
    - name: session-manager
      image: <string>
      ready: <bool>
    - name: training-portal
      image: <string>
      ready: <bool>
    # ... etc
```

### Design notes

- **No `replicas`**: today each component is pod-singleton.
- **No `images.registry` field**: centralized in `EducatesClusterConfig.spec.imageRegistry.prefix`.
- **`images.overrides` is an open list** of name/image pairs, matching today's `imageVersions` shape. Any image the operator knows about can be overridden by name.

---

## Config-to-CR mapping table

| Today's config path | New CR | New path |
|---|---|---|
| `localKindCluster.*` | — | CLI-only |
| `localDNSResolver.*` | — | CLI-only |
| `clusterInfrastructure.provider` | EducatesClusterConfig | `spec.infrastructure.provider` |
| `clusterInfrastructure.gcp.project` | EducatesClusterConfig | `spec.infrastructure.cloud.project` |
| `clusterInfrastructure.gcp.cloudDNS.zone` | EducatesClusterConfig | `spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.cloudDNS.zone` |
| `clusterInfrastructure.gcp.workloadIdentity.certManager` | EducatesClusterConfig | `spec.infrastructure.cloud.serviceAccounts.certManager` |
| `clusterInfrastructure.gcp.workloadIdentity.externalDNS` | EducatesClusterConfig | `spec.infrastructure.cloud.serviceAccounts.externalDNS` |
| `clusterInfrastructure.aws.region` | EducatesClusterConfig | `spec.infrastructure.cloud.region` |
| `clusterInfrastructure.aws.route53.hostedZone` | EducatesClusterConfig | `spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.route53.hostedZoneID` |
| `clusterInfrastructure.aws.irsaRoles.certManager` | EducatesClusterConfig | `spec.infrastructure.cloud.serviceAccounts.certManager` |
| `clusterInfrastructure.aws.irsaRoles.externalDNS` | EducatesClusterConfig | `spec.infrastructure.cloud.serviceAccounts.externalDNS` |
| `clusterInfrastructure.caCertificateRef` | EducatesClusterConfig | `spec.ingress.certificates.bundledCertManager.customCA.caCertificateRef` OR `spec.inline.ingress.caCertificateSecretRef` |
| `clusterPackages.contour.enabled` | EducatesClusterConfig | implicit via `spec.ingress.controller.provider: BundledContour` |
| `clusterPackages.contour.settings.*` | EducatesClusterConfig | `spec.ingress.controller.bundledContour.operational.*` |
| `clusterPackages.cert-manager.enabled` | EducatesClusterConfig | implicit via `spec.ingress.certificates.provider: BundledCertManager` |
| `clusterPackages.external-dns.enabled` | EducatesClusterConfig | implicit via `spec.dns.provider: BundledExternalDNS` |
| `clusterPackages.certs.enabled` | EducatesClusterConfig | implicit — created when `certificates.provider` is Bundled or External |
| `clusterPackages.kyverno.enabled` | EducatesClusterConfig | implicit via `spec.policyEnforcement.kyverno.provider: Bundled` |
| `clusterPackages.kapp-controller.enabled` | — | Dropped |
| `clusterSecurity.policyEngine` | EducatesClusterConfig | `spec.policyEnforcement.clusterPolicy.engine` |
| `workshopSecurity.rulesEngine` | EducatesClusterConfig | `spec.policyEnforcement.workshopPolicy.engine` |
| `clusterIngress.domain` | EducatesClusterConfig | `spec.ingress.domain` |
| `clusterIngress.tlsCertificateRef` | EducatesClusterConfig | Managed: `spec.ingress.certificates.staticCertificate.tlsSecretRef`; Inline: `spec.inline.ingress.wildcardCertificateSecretRef` |
| `clusterPackages.educates.settings.imageVersions[]` | SessionManager | `spec.images.overrides[]` |
| `clusterPackages.educates.settings.clusterIngress` | EducatesClusterConfig | `spec.ingress.*` (no duplication) |
| `clusterPackages.educates.settings.clusterSecurity` | EducatesClusterConfig | `spec.policyEnforcement.clusterPolicy` |
| `clusterPackages.educates.settings.workshopSecurity` | EducatesClusterConfig | `spec.policyEnforcement.workshopPolicy` |
| `lookupService.enabled` | — | Implicit: create a `LookupService` CR to enable |
| `lookupService.ingressPrefix` | LookupService | `spec.ingress.prefix` |

---

## Example scenarios

### Scenario A — Local kind development (Managed, CustomCA)

```yaml
---
apiVersion: config.educates.dev/v1alpha1
kind: EducatesClusterConfig
metadata:
  name: cluster
spec:
  mode: Managed
  infrastructure:
    provider: Kind
  ingress:
    domain: educates.test
    ingressClassName: contour
    controller:
      provider: BundledContour
      bundledContour:
        operational:
          replicas: 1
    certificates:
      provider: BundledCertManager
      bundledCertManager:
        issuerType: CustomCA
        customCA:
          caCertificateRef:
            name: educates.test-ca
  dns:
    provider: None
  policyEnforcement:
    clusterPolicy:
      engine: Kyverno
    workshopPolicy:
      engine: Kyverno
    kyverno:
      provider: Bundled
---
apiVersion: platform.educates.dev/v1alpha1
kind: SecretsManager
metadata:
  name: cluster
spec: {}
---
apiVersion: platform.educates.dev/v1alpha1
kind: SessionManager
metadata:
  name: cluster
spec: {}
---
# Optional
apiVersion: platform.educates.dev/v1alpha1
kind: LookupService
metadata:
  name: cluster
spec:
  ingress:
    prefix: educates-api
```

### Scenario B — GKE production (Managed, ACME DNS01 via CloudDNS)

```yaml
---
apiVersion: config.educates.dev/v1alpha1
kind: EducatesClusterConfig
metadata:
  name: cluster
spec:
  mode: Managed
  infrastructure:
    provider: GKE
    cloud:
      project: educates-testing
      region: us-central1
      serviceAccounts:
        certManager: demo-cert-manager@educates-testing.iam.gserviceaccount.com
        externalDNS: demo-external-dns@educates-testing.iam.gserviceaccount.com
  ingress:
    domain: gcp.educates.academy
    ingressClassName: contour
    controller:
      provider: BundledContour
      bundledContour:
        operational:
          replicas: 2
    certificates:
      provider: BundledCertManager
      bundledCertManager:
        issuerType: ACME
        acme:
          email: ops@educates.academy
          solvers:
            dns01:
              provider: CloudDNS
              cloudDNS:
                zone: educates-academy-zone
                # project inherited from infrastructure.cloud.project
  dns:
    provider: BundledExternalDNS
  policyEnforcement:
    clusterPolicy:
      engine: Kyverno
    workshopPolicy:
      engine: Kyverno
    kyverno:
      provider: Bundled
---
apiVersion: platform.educates.dev/v1alpha1
kind: SecretsManager
metadata:
  name: cluster
spec: {}
---
apiVersion: platform.educates.dev/v1alpha1
kind: SessionManager
metadata:
  name: cluster
spec: {}
```

### Scenario C — EKS production (Managed, ACME DNS01 via Route53, mirrored registry)

```yaml
apiVersion: config.educates.dev/v1alpha1
kind: EducatesClusterConfig
metadata:
  name: cluster
spec:
  mode: Managed
  infrastructure:
    provider: EKS
    cloud:
      project: "123456789012"            # AWS account ID
      region: eu-west-1
      serviceAccounts:
        certManager: arn:aws:iam::123456789012:role/educates-cert-manager
        externalDNS: arn:aws:iam::123456789012:role/educates-external-dns
  ingress:
    domain: aws.educates.example.com
    ingressClassName: contour
    controller:
      provider: BundledContour
      bundledContour:
        operational:
          replicas: 3
    certificates:
      provider: BundledCertManager
      bundledCertManager:
        issuerType: ACME
        acme:
          email: ops@example.com
          solvers:
            dns01:
              provider: Route53
              route53:
                hostedZoneID: Z1234ABCDEFGHI
  dns:
    provider: BundledExternalDNS
  policyEnforcement:
    clusterPolicy:
      engine: Kyverno
    workshopPolicy:
      engine: Kyverno
    kyverno:
      provider: Bundled
  imageRegistry:
    prefix: 123456789012.dkr.ecr.eu-west-1.amazonaws.com/educates
    pullSecrets:
      - name: ecr-pull-secret
```

### Scenario D — Partial BYO (Managed, mixed bundled and external)

Existing Contour and Kyverno; operator manages cert-manager only.

```yaml
apiVersion: config.educates.dev/v1alpha1
kind: EducatesClusterConfig
metadata:
  name: cluster
spec:
  mode: Managed
  infrastructure:
    provider: Generic
  ingress:
    domain: educates.example.com
    ingressClassName: contour            # existing IngressClass
    controller:
      provider: ExternalIngressController
    certificates:
      provider: BundledCertManager
      bundledCertManager:
        issuerType: ACME
        acme:
          email: ops@example.com
          solvers:
            dns01:
              provider: Cloudflare
              cloudflare:
                apiTokenSecretRef:
                  name: cloudflare-token
  dns:
    provider: Manual
  policyEnforcement:
    clusterPolicy:
      engine: Kyverno
    workshopPolicy:
      engine: Kyverno
    kyverno:
      provider: External                 # existing Kyverno
```

### Scenario E — Full BYO on OpenShift (Inline mode)

User has cert-manager, ingress, SCC, Kyverno all in place; wildcard cert pre-provisioned.

```yaml
---
apiVersion: config.educates.dev/v1alpha1
kind: EducatesClusterConfig
metadata:
  name: cluster
spec:
  mode: Inline
  inline:
    ingress:
      domain: apps.openshift.example.com
      ingressClassName: openshift-default
      wildcardCertificateSecretRef:
        name: educates-wildcard-tls
      caCertificateSecretRef:
        name: corporate-ca
      clusterIssuerRef:
        name: letsencrypt-prod
    policyEnforcement:
      clusterPolicyEngine: OpenShiftSCC
      workshopPolicyEngine: Kyverno
---
apiVersion: platform.educates.dev/v1alpha1
kind: SecretsManager
metadata:
  name: cluster
spec: {}
---
apiVersion: platform.educates.dev/v1alpha1
kind: SessionManager
metadata:
  name: cluster
spec: {}
```

### Scenario F — Standalone LookupService (Inline, no cluster services)

```yaml
---
apiVersion: config.educates.dev/v1alpha1
kind: EducatesClusterConfig
metadata:
  name: cluster
spec:
  mode: Inline
  inline:
    ingress:
      domain: lookup.example.com
      ingressClassName: nginx
      wildcardCertificateSecretRef:
        name: lookup-wildcard-tls
    policyEnforcement:
      clusterPolicyEngine: None
      workshopPolicyEngine: None
---
apiVersion: platform.educates.dev/v1alpha1
kind: LookupService
metadata:
  name: cluster
spec:
  ingress:
    prefix: api
  # URL: https://api.lookup.example.com
```

---

## Open items for v1alpha1 → v1beta1

1. **SessionManager.spec.themes structure** — owner review needed.
2. **LookupService component-specific settings** (auth, rate limiting, storage) — owner review needed.
3. **external-dns explicit zones** — deferred, add if needed.
4. **bundledCertManager operational sub-blocks** — cert-manager has controller + webhook + cainjector deployments. If per-deployment overrides become necessary, `operational` gains sub-blocks.
5. **Inline-mode re-validation on external changes** — implementation detail. With watches set up on referenced resources, validation runs on every change. Confirm behavior matches expectation.
6. **Validation error surfacing** — condition messages should be specific enough to guide fixes. Review wording during implementation.

---

## What's intentionally NOT in this draft

- Dev-mode overrides (`--package-repository`, `--version`) — CLI flags only.
- Per-image digest pinning in spec — release-artifact concern.
- Multi-instance support — singleton via name constraint.
- Cross-cluster references — each cluster self-contained.
- Old-config compatibility — `educates migrate-config` CLI handles translation.
- Raw Helm values passthrough — curated operational blocks only.
- Managed ↔ Inline mode transitions — explicitly unsupported; requires delete + recreate.
