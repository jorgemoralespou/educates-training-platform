(configuration-settings)=
Configuration Settings
======================

Educates installations driven by the CLI are described by a single YAML configuration file using one of the `cli.educates.dev/v1alpha1` kinds. Each kind targets a scenario: the narrow kinds (`EducatesLocalConfig`, `EducatesGKEConfig`, `EducatesEKSConfig`, `EducatesInlineConfig`) expose only the choices that scenario actually has, while `EducatesConfig` is the escape hatch giving full control over the underlying custom resources.

Every kind has a published JSON schema at `https://schemas.educates.dev/cli/v1alpha1/<Kind>.json`. Add a modeline at the top of your file to get completion and validation in any editor with a YAML language server:

```yaml
# yaml-language-server: $schema=https://schemas.educates.dev/cli/v1alpha1/EducatesGKEConfig.json
```

Files created by `educates local config init` include the modeline automatically.

EducatesLocalConfig
-------------------

The laptop scenario: a local kind cluster with operator-managed cluster services and a self-signed CA. This is the only kind that lives at a fixed location (`<data-home>/config.yaml`, where `<data-home>` is `$XDG_DATA_HOME/educates`, overridable via `EDUCATES_CLI_DATA_HOME`) and is managed with the `educates local config` commands rather than edited as a project file. See [local environment](local-environment).

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: workshops.educates.test    # empty = <host-IP>.nip.io fallback
cluster:
  listenAddress: 127.0.0.1
lookupService: true                  # default true
clusterAdmin: true                   # default true
operator:
  logLevel: info
```

Key fields (all optional — an `apiVersion` + `kind` stub is a valid config):

* `ingress.domain` — wildcard ingress domain. When empty and deploying with `--local-config`, the CLI falls back to `<host-IP>.nip.io`.
* `cluster.*` — kind cluster shape: `listenAddress`, API server overrides, pod/service subnets, host volume mounts, registry pull-through mirrors.
* `resolver.*` — local DNS resolver settings (macOS `*.educates.test` style resolution).
* `clusterAdmin`, `lookupService` — component toggles (both default `true`).
* `secretPropagation.imagePullSecretNames` — locally cached pull secrets to propagate into the cluster.
* `imageVersions` — per-image reference overrides.
* `operator.image.*`, `operator.imagePullSecrets`, `operator.logLevel` — operator deployment settings. Image repository and tag default from the CLI binary's own version, so they normally stay unset.

Settings outside the laptop scenario — DNS providers, ACME, image registry prefixes, alternative ingress — are deliberately rejected by this kind's schema. Use `EducatesConfig` for those.

EducatesGKEConfig
-----------------

The GKE production scenario: Contour with a LoadBalancer service, cert-manager issuing a wildcard certificate via ACME with the CloudDNS DNS01 solver, external-dns managing the DNS records, Kyverno policy enforcement. Authentication to Google Cloud uses Workload Identity.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesGKEConfig
gcp:
  project: my-gcp-project
  # certManagerServiceAccount / externalDNSServiceAccount default to
  # cert-manager@{project}.iam.gserviceaccount.com / external-dns@...
domain: workshops.example.com
acme:
  email: admin@example.com
  # server defaults to the Let's Encrypt production endpoint
```

`gcp.project`, `domain` and `acme.email` are required. The component toggles (`lookupService`, `clusterAdmin`, ...) and `operator` block from `EducatesLocalConfig` are available here too. When TLS for the ingress domain is terminated outside the cluster (a cloud load balancer or proxy forwarding plain HTTP inward), set `externalTLSTermination: true` so generated portal and workshop URLs use `https` — see [secure HTTP connections](secure-http-connections). The scenario's other architecture choices are locked — to deviate, use `EducatesConfig`.

See [infrastructure providers](infrastructure-providers) for the Google Cloud IAM and DNS zone prerequisites.

EducatesEKSConfig
-----------------

The EKS equivalent: the same managed stack, with ACME using the Route53 DNS01 solver and IAM Roles for Service Accounts (IRSA) for authentication.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesEKSConfig
aws:
  accountId: "123456789012"
  region: us-east-1
  route53HostedZoneId: Z0123456789ABCDEF
  # certManagerRoleARN / externalDNSRoleARN default to
  # arn:aws:iam::{accountId}:role/educates-cert-manager / educates-external-dns
domain: workshops.example.com
acme:
  email: admin@example.com
```

`aws.accountId`, `aws.region`, `aws.route53HostedZoneId`, `domain` and `acme.email` are required. As with the GKE kind, `externalTLSTermination: true` asserts `https` URLs when TLS is terminated at an external load balancer.

(defining-configuration-for-ingress)=
EducatesInlineConfig
--------------------

The bring-your-own scenario: the cluster already has an ingress controller, a wildcard TLS certificate, and (optionally) a policy engine, and Educates integrates with them instead of installing anything at cluster scope. This is the path for OpenShift and for shared or centrally-managed clusters.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesInlineConfig
domain: workshops.example.com
ingressClassName: contour                      # or e.g. openshift-default
wildcardCertificateSecret: wildcard-tls        # kubernetes.io/tls Secret for *.{domain}
caCertificateSecret: corporate-ca              # optional, for non-public CAs
policyEnforcement:
  clusterEngine: Kyverno                       # Kyverno | PodSecurityStandards | OpenShiftSCC | None
  workshopEngine: Kyverno                      # Kyverno | None
imageRegistry:
  prefix: registry.internal/educates           # optional mirror prefix
externalTLSTermination: false                  # true when a proxy/LB terminates TLS in front of the cluster
```

`domain`, `ingressClassName` and `wildcardCertificateSecret` are required. The referenced Secrets must exist in the operator's namespace before deployment. See [secure HTTP connections](secure-http-connections) for the certificate options across all scenarios.

Note that with `workshopEngine: None` there is no workshop-level security policy enforcement — see the [cluster requirements](cluster-requirements) discussion before serving untrusted users.

EducatesConfig (escape hatch)
-----------------------------

`EducatesConfig` carries the four custom resource specs verbatim, with no CLI defaulting and no locked invariants — every field of the CRDs is reachable. Use it when a narrow kind almost fits but you need to deviate, or for scenarios with no dedicated kind.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesConfig
target:
  provider: Kind            # optional; controls CLI side effects (local cluster bootstrap)
educatesClusterConfig:
  # verbatim EducatesClusterConfig.spec
  mode: Managed
  ingress:
    domain: workshops.example.com
secretsManager: {}
lookupService: {}           # omit the block entirely to not deploy the component
sessionManager: {}
```

The spec blocks are validated by the cluster's CRD schemas at apply time, not by the CLI. The `EducatesConfig` JSON schema is generated from the CRDs, so editors still get full completion. For the custom resource spec reference, see the sample scenarios in [installer/samples](https://github.com/educates/educates-training-platform/tree/develop/installer/samples) and `kubectl explain educatesclusterconfig.spec` against an installed cluster.

(overriding-container-runtime-class)=
(restricting-session-manager-permissions)=
(restricting-network-access)=
(image-registry-pull-through-cache)=
(tracking-using-google-analytics)=
(tracking-using-microsoft-clarity)=
(tracking-using-amplitude)=
(overriding-styling-of-the-workshop)=
(allowing-sites-to-embed-workshops)=
(overriding-session-cookie-domain)=
Session manager settings
------------------------

Runtime behavior settings live on the `SessionManager` custom resource spec, reachable via the `sessionManager` block of `EducatesConfig` (or directly when applying resources yourself):

* `ingressOverrides` — per-component TLS/CA Secret overrides, plus `protocol` to assert `https` URLs when TLS is terminated outside the cluster (this is what the kinds' `externalTLSTermination` translates to).
* `tracking` — analytics integrations (Google Analytics, Amplitude, Microsoft Clarity, webhooks).
* `sessionCookieDomain` — share the authentication cookie across subdomains.
* `allowedEmbeddingHosts` — sites permitted to embed workshop sessions (CSP frame ancestors).
* `storage` — storage class plus user/group fixups for NFS-style storage providers.
* `network` — packet size (MTU) and blocked CIDR ranges for workshop sessions.
* `images` — per-image overrides for runtime-spawned images.
* `themes`/`defaultTheme` — Secret-sourced workshop themes.
* `imagePrePuller` — pre-pull key images on cluster nodes.
* `nodeCATrust`, `remoteAccess` — node-level CA trust injection and cross-cluster CLI access.

Use `kubectl explain sessionmanager.spec` for the full schema. A few spec blocks (`defaultAccessCredentials`, `registryMirrors`, and ConfigMap/URL-sourced themes) are reserved in the CRD but rejected as not yet supported in this release.

Updating settings
-----------------

Configuration is applied by re-running `educates admin platform deploy` with the changed file (or re-applying the custom resources in a GitOps flow). The operator reconciles differences in place. The `EducatesClusterConfig` mode (`Managed` vs `Inline`) is immutable once set — switching requires deleting and re-creating the installation. Configuration changes will not necessarily affect training portals or workshop environments which already exist.
