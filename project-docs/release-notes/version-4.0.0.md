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

* When a bundled Helm chart fails to install, the operator now reports the
  failure on the owning custom resource — the relevant condition goes to
  ``False`` with reason ``ReleaseFailed`` and the underlying Helm error, and
  the resource phase becomes ``Degraded`` — rather than reporting ``Ready``.
  The operator recovers on its own once the cause is resolved: applying an
  updated operator image or chart triggers a reinstall of a failed first
  install, an in-place upgrade when a previously working release exists, or a
  rollback when a release is stuck mid-operation. A failed release whose inputs
  are unchanged is left untouched rather than reinstalled repeatedly.

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
  ``set``, ``view``, ``edit`` and ``explain`` commands for managing the local
  configuration, with validation against the configuration schemas. The
  ``view`` command prints the effective configuration with all CLI defaults
  filled in, so the values the CLI supplies on your behalf (including the
  ``<host-IP>.nip.io`` domain and the insecure default it implies) are
  explicit rather than hidden behind a sparse file; pass ``--raw`` to print
  the configuration file exactly as written instead. The ``init`` command
  writes a minimal file by default, or with ``--defaults`` writes the
  fully-defaulted configuration with those same values materialised into
  the file.

* A new ``educates local config explain`` command describes configuration
  fields from the embedded JSON schema, in the style of ``kubectl explain``,
  without needing a cluster or a configuration file. It takes a dotted field
  path (for example ``educates local config explain ingress.insecure``) and
  prints the field's type, default and description along with its
  sub-fields. The kind defaults to ``EducatesLocalConfig`` and is chosen
  with ``--kind``, which accepts a kind name or a short alias (``local``,
  ``gke``, ``eks``, ``inline``, ``escape``), so the GKE, EKS, inline and
  escape-hatch kinds can be explored the same way. The descriptions come
  from the schemas, which now carry field documentation for every kind.

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
  mirroring images into air-gapped registries. The list is intentionally
  complete: in addition to the platform and bundled cluster-service images it
  includes every image in the session-manager image inventory — the
  workshop base, the JDK and conda workshop environments, and the images the
  vcluster workshop application uses (``vcluster`` itself, its loft-sh
  Kubernetes distribution images, the Contour and Envoy images for vcluster
  ingress, ``docker-in-docker`` and the debian base). When mirroring, delete
  the entries you do not use rather than guessing which ones might be missing.

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

* Educates can be installed without in-cluster TLS. The
  ``EducatesClusterConfig`` certificates provider accepts a new ``None``
  value, which installs no cert-manager, issues no certificate, and serves
  workshops over plain HTTP. A companion ``ingress.protocol`` setting
  (``http`` or ``https``) asserts the scheme of the generated workshop and
  portal URLs independently of whether the cluster terminates TLS, so a
  cluster sitting behind an external load balancer that terminates TLS can
  still generate ``https`` URLs while presenting no in-cluster certificate.
  On the local configuration, setting ``ingress.insecure`` selects a plain
  HTTP install and skips the one-time ``educates local secrets add ca``
  step. A local configuration that sets no ingress domain now defaults to
  this insecure mode, since the ``<host-IP>.nip.io`` address it falls back
  to cannot obtain a trusted certificate; set a custom ``ingress.domain``
  to get the certificate-backed HTTPS install. On the
  ``EducatesGKEConfig``, ``EducatesEKSConfig`` and
  ``EducatesInlineConfig`` kinds, ``externalTLSTermination`` now maps to
  this provider, so no unused in-cluster certificate is issued when TLS is
  handled by an external load balancer or proxy.

Features Changed
----------------

* The bundled Kyverno security policies are now ``ValidatingPolicy`` resources
  (``policies.kyverno.io``), the policy type recommended from Kyverno 1.18,
  replacing the previous CEL ``ClusterPolicy`` form. This covers the cluster-wide
  Pod Security Standards profiles and the per-workshop rules. The per-workshop
  rules are now applied as ``ValidatingPolicy`` objects scoped to each workshop
  environment's session namespaces. There is no action required for workshops
  that rely on the built-in rules.

* The training portal has been made more resilient when allocating and
  managing workshop sessions under load. Previously, when many sessions were
  being requested at the same time, or when the Kubernetes REST API was slow
  to respond or applying throttling, session allocation could fail or requests
  could back up, sometimes surfacing as a ``database is locked`` error or as
  failed or timed out session requests. Updates the training portal makes to
  Kubernetes are now performed in a way that no longer blocks other session
  activity while waiting on the Kubernetes API, reducing failed sessions and
  improving reliability under high traffic.

* The ``educates`` CLI commands now give clearer feedback when invoked
  incorrectly and document themselves more consistently. Commands that take a
  positional argument — such as ``educates local mirror deploy``,
  ``educates local secrets add``, ``educates cluster session status`` and
  ``educates workshop new`` — now report a specific message naming the
  missing, extra or invalid argument along with a pointer to ``--help``,
  instead of a generic ``accepts 1 arg(s), received 0`` error. Every
  ``educates`` command now also includes usage examples in its ``--help``
  output.

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

* Workshop sessions are served over HTTPS when the cluster configuration
  provides certificate settings, which is a wildcard certificate, a
  certificate authority, or ACME issuer details. On local clusters with a
  custom ingress domain, ``educates local cluster create`` stops and prints
  the exact ``educates local secrets add ca`` command to run when no CA
  exists yet for that domain. A local configuration with no ingress domain
  set instead defaults to a plain HTTP install on a ``<host-IP>.nip.io``
  address and needs no CA, since that address cannot obtain a trusted
  certificate. Set ``ingress.insecure`` to request plain HTTP explicitly
  for a custom domain. See the New Features entry on installing without
  in-cluster TLS for the full picture.

* The ``imageCache`` configuration setting has been renamed to
  ``imagePrePuller``. It is now enabled by default in the CLI configuration
  kinds (``EducatesLocalConfig``, ``EducatesGKEConfig``,
  ``EducatesEKSConfig`` and ``EducatesInlineConfig``), so installs created
  with the CLI pre-pull workshop images onto every node ahead of time
  unless ``imagePrePuller`` is set to ``false``. Installs applied directly
  from custom resources or the Helm chart keep the chart default of
  disabled.

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

* If a user ends a workshop session, or attempts to return to one, after their
  training portal login session has already expired, for example after leaving
  the browser window open for a long period after the workshop session had
  finished, they are now shown the workshop session finished page rather than
  being redirected to the portal login page. Previously an expired login in
  this situation could result in the portal login page being displayed, which
  was undesirable when workshop sessions are coordinated by a custom front end
  through the training portal REST API.

* The ``helm`` CLI included in the workshop base environment image has been
  updated from the 3.x series to 4.x. Helm 4 is a new major release which
  introduces breaking changes relative to Helm 3, so any workshops which use
  the ``helm`` CLI should be verified against the newer Helm version to ensure
  they still behave as expected.

* The set of ``kubectl`` versions bundled in the workshop base environment
  image has changed. The 1.31 and 1.32 versions have been dropped and 1.35 and
  1.36 have been added, so the supported range is now 1.33 to 1.36. The
  ``kubectl`` version is still selected automatically to match the Kubernetes
  cluster the workshop session is connected to. For clusters older than the
  supported range the oldest bundled version, 1.33, is used, and for clusters
  newer than the supported range the most recent bundled version, 1.36, is
  used. Workshops which target clusters running Kubernetes 1.32 or older may
  therefore see a larger client/server version skew and should be verified
  against a supported cluster version.

* The version of ``kind`` embedded in the ``educates`` CLI has been updated
  from 0.29 to 0.32. As a result the default Kubernetes version used when
  creating a local cluster with ``educates local cluster create`` has changed
  from 1.33 to 1.36. A specific node image can still be selected using the
  ``--kind-cluster-image`` option if a different Kubernetes version is required.

* ``educates version`` now also reports the git commit the binary was built
  from, with a ``-dirty`` suffix when it was built from a modified working
  tree. This makes otherwise-identical development builds — which all report
  the floating ``latest`` or ``develop`` version — distinguishable. Release
  binaries continue to report their release version and additionally show the
  commit they were built from.

Bugs Fixed
----------

* The Contour ingress controller deployed inside a vcluster workshop session
  (when the ``vcluster`` application enables ingress) used hardcoded upstream
  Contour and Envoy image references that were not subject to image relocation
  and were absent from the published image list, so vcluster ingress could not
  work in air-gapped or registry-mirrored environments. These images are now
  first-class entries in the ``imageVersions`` inventory, so they are
  relocatable through the same per-name override mechanism as the other
  platform images and are included in the air-gap image list.

* In Inline mode the ``EducatesClusterConfig`` status could remain ``Ready``
  after a referenced ``ClusterIssuer`` was deleted, if the deletion event from
  the cert-manager watch was missed or observed against a momentarily stale
  cache. The reconciler now periodically re-validates referenced resources, so
  the status reliably transitions to ``Degraded`` when a referenced
  ``ClusterIssuer`` disappears.
