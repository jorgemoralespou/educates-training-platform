Version 4.0.0
=============

New Features
------------

* Educates is now installed by a Kubernetes operator. The new
  ``educates-installer`` Helm chart installs the operator together with four
  cluster-scoped custom resource definitions — ``EducatesClusterConfig``
  (``config.educates.dev/v1alpha1``) for cluster-wide infrastructure and
  services, and ``SecretsManager``, ``LookupService`` and ``SessionManager``
  (``platform.educates.dev/v1alpha1``) for the individual Educates platform
  components. The same chart can be installed imperatively using
  ``helm install``, or declaratively by pointing a GitOps tool such as ArgoCD
  or Flux at it. See the installation guides in the documentation for the
  supported installation paths.

* ``EducatesClusterConfig`` supports two modes. In ``Managed`` mode the
  operator installs and manages the cluster services Educates depends on —
  cert-manager for TLS certificates, Contour as the ingress controller,
  Kyverno for security policy enforcement, and optionally external-dns for DNS
  record management — from versions of their Helm charts bundled with the
  operator. In ``Inline`` mode you instead declare the equivalent resources
  which already exist in the cluster (ingress class, wildcard TLS certificate
  secret, policy engine), allowing Educates to be installed onto clusters with
  pre-existing infrastructure, such as OpenShift.

* In ``Managed`` mode, wildcard TLS certificates can be issued from a custom
  certificate authority, or obtained automatically from an ACME provider such
  as LetsEncrypt using DNS01 challenges, with support for Amazon Route53
  (using IAM Roles for Service Accounts on EKS) and Google Cloud DNS (using
  Workload Identity on GKE).

* A standalone ``educates-training-platform`` umbrella Helm chart, with
  subcharts for the secrets-manager, lookup-service and session-manager
  components, can be used to install the Educates runtime without the
  operator for users who want to manage cluster services and configuration
  entirely themselves.

* The CLI configuration file format is now kind based, under
  ``cli.educates.dev/v1alpha1``. ``EducatesLocalConfig`` configures the local
  (kind) cluster environment, the ``EducatesGKEConfig``, ``EducatesEKSConfig``
  and ``EducatesInlineConfig`` kinds provide opinionated configuration for
  GKE, EKS and bring-your-own clusters respectively, and ``EducatesConfig``
  is an escape hatch which carries the operator custom resource specs
  verbatim with no CLI defaulting. JSON schemas for all the configuration
  kinds are embedded in the CLI for validation and published at
  ``https://schemas.educates.dev/cli/v1alpha1/`` for editor integration.

* New ``educates admin platform`` commands drive the installation
  end-to-end. ``deploy`` installs the operator chart, applies the platform
  custom resources and waits for each to report ``Ready``. ``render`` emits
  the Helm chart values and custom resources for inspection, or for committing
  to a GitOps repository. ``delete`` uninstalls everything in reverse order
  and supports ``--yes`` and ``--purge`` options. ``educates local cluster
  create`` lays down the full local environment — kind cluster, local image
  registry and platform — in a single command, with preflight checks for
  common problems such as ports already being in use.

* The ``educates local config`` command group now provides ``init``, ``get``,
  ``set``, ``view`` and ``edit`` commands for managing the local
  configuration, with validation against the configuration schemas.

* Workshop sessions on local clusters are now served using TLS certificates
  issued in-cluster by cert-manager from a local certificate authority managed
  using ``educates local secrets add ca``. The generated CA is cached locally
  and reused for every future cluster using the same ingress domain. The CA
  certificate can be exported as a PEM file using
  ``educates local secrets export NAME --pem`` for importing into the
  operating system trust store, so browsers trust the workshop URLs; the
  quick start guide includes per-platform import instructions.

* TLS termination by an external load balancer or proxy in front of the
  cluster is now supported by setting ``externalTLSTermination`` in the CLI
  configuration kinds, or ``ingressOverrides.protocol`` in the
  ``SessionManager`` custom resource.

* The Helm charts are published as OCI artifacts to
  ``ghcr.io/educates/charts`` with each release, and a digest-pinned list of
  all images used by a release is attached to the GitHub release to support
  mirroring images into air-gapped registries.

* ``imageVersions`` entries in the CLI configuration now reach every
  platform image: entries named ``secrets-manager`` and ``lookup-service``
  are routed to those components' own custom resources, and the
  ``session-manager``, ``pause-container`` and ``node-ca-injector`` names —
  previously ignored — now override the images they refer to. For
  contributors, a developer-built CLI (one whose embedded version is not a
  release version) automatically targets its compiled-in image registry for
  all platform images, so building Educates from source and deploying the
  locally built system requires no configuration of image references;
  explicit configuration entries always take precedence. See the developer
  documentation for the local build workflow.

Features Changed
----------------

* The bundled Kyverno security policies are now ``ValidatingPolicy`` resources
  (``policies.kyverno.io``), the policy type recommended from Kyverno 1.18,
  replacing the previous CEL ``ClusterPolicy`` form. This covers the cluster-wide
  Pod Security Standards profiles and the per-workshop rules. The per-workshop
  rules are now applied as ``ValidatingPolicy`` objects scoped to each workshop
  environment's session namespaces. There is no action required for workshops
  that rely on the built-in rules.

Deprecations
------------

* Following Kyverno's own deprecation of the ``ClusterPolicy`` resource
  (``kyverno.io``) — deprecated in Kyverno 1.18 and scheduled for removal in
  1.20 — workshops that supply their own Kyverno policies as ``ClusterPolicy``
  objects via ``workshopSecurity.additionalKyvernoPolicies`` are correspondingly
  deprecated. Such policies are still honoured for now (Educates continues to
  scope them per workshop session, and logs a warning when it does), but support
  will be removed on the same timeline as Kyverno's removal of ``ClusterPolicy``.
  Migrate workshop-provided policies to ``ValidatingPolicy``.

* The Carvel-based installer from version 3 has been removed. Educates is no
  longer packaged or installed as a ``kapp-controller`` package: the
  ``educates-installer`` package bundle and package repository are no longer
  published, and the CLI no longer requires or interacts with
  ``kapp-controller`` for installation. There is no in-place upgrade from
  version 3 — delete the existing version 3 installation and install version
  4 following the new installation guides. A migration guide mapping version
  3 configuration settings onto their version 4 equivalents is included in
  the documentation.

* The local CLI configuration file is now ``config.yaml`` in the CLI data
  home, using the new ``EducatesLocalConfig`` format, replacing the version 3
  ``values.yaml``. On first use the CLI migrates an existing version 3
  ``values.yaml`` automatically when the configured provider was ``kind`` (or
  unset); configurations for other providers must be re-declared using the
  new configuration kinds. As part of the new format some top-level keys have
  been renamed, including ``localKindCluster`` to ``cluster``,
  ``localDNSResolver`` to ``resolver``, and the kind cluster networking
  fields ``podCIDR`` and ``serviceCIDR`` to ``podSubnet`` and
  ``serviceSubnet``.

* Fully certificate-less installations are no longer possible. Workshop
  sessions are always served over HTTPS, so the cluster configuration must
  provide certificate settings — a wildcard certificate, a certificate
  authority, or ACME issuer details. On local clusters,
  ``educates local cluster create`` stops and prints the exact
  ``educates local secrets add ca`` command to run when no CA exists yet for
  the ingress domain.

* The ``imageCache`` configuration setting has been renamed to
  ``imagePrePuller``.

* The CLI binaries are no longer published as the ``educates-client-programs``
  imgpkg bundle. Download the binaries from the GitHub release for the
  version, or use the ``educates-cli`` container image when embedding the CLI
  in a container image.

* The browser JavaScript bundles for the workshop renderer and gateway
  applications in the workshop base environment image are now generated using
  esbuild instead of browserify. This eliminates security alerts arising from
  the deprecated elliptic package which was an indirect dependency of
  browserify, but for which no fixed version is available.

* The default Hugo theme used when rendering workshop instructions has been
  replaced with a new self contained implementation which no longer depends on
  JavaScript code generated from the classic renderer. The original theme is
  still included under the name ``educates-classic`` and can be selected by
  setting the ``WORKSHOP_RENDERER_THEME`` environment variable for a workshop
  to ``educates-classic`` if any issues are encountered with the new theme. The
  recommended way of setting this environment variable is via the ``env``
  override for a workshop in the ``TrainingPortal`` resource definition. The
  ``educates-classic`` theme will be removed in a future version at the same
  time as the classic renderer is removed.

* The ``baseurl`` short code for the Hugo renderer no longer includes a
  trailing slash when expanded, so the documented usage of
  ``{{<baseurl>}}/path`` now produces a URL with a single slash. Previously the
  expansion always ended in a slash, resulting in a repeated slash. Although
  web servers will generally collapse the repeated slash when workshop
  instructions are served from a workshop session, in the case of static HTML
  files generated using ``educates render-workshop``, where URLs are relative
  to the root, the result was a URL starting with a double slash, which a
  browser would incorrectly interpret as a scheme relative URL. Note that if
  workshop instructions relied on the undocumented form ``{{<baseurl>}}path``,
  without a slash after the short code, they will need to be updated to use the
  documented form.

Bugs Fixed
----------

* ...
