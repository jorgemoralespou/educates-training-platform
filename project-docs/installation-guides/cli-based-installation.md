CLI Based Installation
======================

The instructions below pertain to installing Educates into an existing Kubernetes cluster using the Educates CLI. The CLI installs the operator Helm chart (a copy is embedded in the CLI binary), applies the four platform custom resources derived from your configuration file, and waits for each to report ready.

Creating a configuration file
-----------------------------

Write a configuration file for your scenario using one of the `cli.educates.dev/v1alpha1` kinds. For example, for GKE:

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesGKEConfig
gcp:
  project: my-gcp-project
domain: workshops.example.com
acme:
  email: admin@example.com
```

See [configuration settings](configuration-settings) for all configuration kinds and their fields, and [infrastructure providers](infrastructure-providers) for provider-specific prerequisites (cloud IAM, DNS zones).

Previewing the installation
---------------------------

To see exactly what would be installed without touching the cluster:

```shell
educates admin platform render --config config.yaml
```

This prints a single YAML stream with the operator chart values followed by the four custom resources, in deploy order. The output is deterministic for a given config file and CLI version, which makes it suitable for diffing, review, and committing to a Git repository for [GitOps-driven installation](helm-based-installation).

Deploying the platform
----------------------

```shell
educates admin platform deploy --config config.yaml
```

This performs, in order:

1. `helm upgrade --install` of the `educates-installer` operator chart (CRDs included).
2. Apply `EducatesClusterConfig`, wait for `Ready=True` — in Managed mode this is where the operator installs cert-manager, Contour, and the other cluster services.
3. Apply `SecretsManager`, wait for `Ready=True`.
4. Apply `LookupService` (if enabled) and `SessionManager`, wait for both.

Progress for each step is reported as it happens. If a step does not become ready within the per-resource timeout (configurable with `--timeout`), the command fails with the failing resource's status conditions — inspect them with `kubectl describe educatesclusterconfig cluster` or the equivalent for the other kinds.

Use `--verbose` to also stream Helm SDK debug output when troubleshooting.

Kubeconfig and context
----------------------

By default the CLI uses the standard kubeconfig (`$KUBECONFIG` or `$HOME/.kube/config`) and its current context. Use `--kubeconfig` to point at an alternate file and `--context` to select a context:

```shell
educates admin platform deploy --config config.yaml --kubeconfig kubeconfig.yaml --context educates-cluster
```

Updating configuration
----------------------

To amend the configuration of an existing installation, edit your configuration file and re-run the same deploy command:

```shell
educates admin platform deploy --config config.yaml
```

The Helm release is upgraded and the custom resources re-applied; the operator reconciles the differences in place. Note that configuration changes will not necessarily affect training portals or workshop environments which have already been created, and may only affect those created after that point.

Deleting the installation
-------------------------

```shell
educates admin platform delete
```

This is the reverse of deploy: it deletes `SessionManager`, `LookupService`, `SecretsManager`, and `EducatesClusterConfig` in order, waiting for each to finish draining (the `EducatesClusterConfig` finalizer removes the operator-installed cluster services in reverse install order), and finally uninstalls the operator chart. No configuration file is needed — the resources are always the four singletons named `cluster`.

A confirmation prompt lists everything about to be deleted; pass `--yes` to skip it (required in CI and other non-interactive shells).

By default the four CRDs and the operator and `educates-secrets` namespaces are left in place so a subsequent deploy can reuse them. To remove those too:

```shell
educates admin platform delete --yes --purge
```
