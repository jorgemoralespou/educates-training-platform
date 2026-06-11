Helm Based Installation
=======================

The instructions below pertain to installing Educates into an existing Kubernetes cluster using Helm and `kubectl` directly, without the Educates CLI. This is the appropriate path for GitOps tooling (ArgoCD, Flux) or wherever you want to manage the installation as plain Kubernetes resources.

The moving parts are described in [installation instructions](installation-instructions): one Helm chart installing the operator and CRDs, then four custom resources driving it.

Installing the operator
-----------------------

The `educates-installer` chart is published to an OCI registry with every release:

```shell
helm install educates-installer oci://ghcr.io/educates/charts/educates-installer \
  --version X.Y.Z \
  --namespace educates-installer --create-namespace
```

This installs the operator and the four CRDs. At this point nothing else has happened — the operator waits for custom resources.

Applying the platform resources
-------------------------------

Author the four custom resources for your scenario. Worked examples for each supported scenario live in [installer/samples](https://github.com/educates/educates-training-platform/tree/develop/installer/samples) in the project repository, and the [configuration settings](configuration-settings) documentation describes the specs. If you use the Educates CLI anywhere, `educates admin platform render --config config.yaml` emits ready-to-apply resources for a high-level config file.

Apply them in dependency order:

```shell
kubectl apply -f educates-cluster-config.yaml
kubectl wait --for=condition=Ready educatesclusterconfig/cluster --timeout=600s

kubectl apply -f educates-secrets-manager.yaml
kubectl apply -f educates-lookup-service.yaml       # optional
kubectl apply -f educates-session-manager.yaml

kubectl wait --for=condition=Ready secretsmanager/cluster sessionmanager/cluster --timeout=600s
```

Strict ordering is a convenience, not a requirement: each component gates itself on its dependencies' status (`SessionManager` refuses to proceed until `EducatesClusterConfig` and `SecretsManager` report ready), so applying everything at once also converges. Waiting per resource just gives clearer feedback on where a problem lies. The status conditions on each resource are the troubleshooting surface:

```shell
kubectl describe educatesclusterconfig cluster
```

Note that any Secrets referenced by name from `EducatesClusterConfig` (for example a wildcard TLS certificate Secret in Inline mode, or a custom CA) must live in the operator's namespace (`educates-installer` above) before the resource can become ready.

GitOps
------

The chart plus the four custom resources are the entire GitOps surface. With Flux:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: educates
  namespace: educates-installer
spec:
  type: oci
  url: oci://ghcr.io/educates/charts
  interval: 1h
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: educates-installer
  namespace: educates-installer
spec:
  chart:
    spec:
      chart: educates-installer
      version: "X.Y.Z"
      sourceRef:
        kind: HelmRepository
        name: educates
  interval: 1h
```

With ArgoCD, register `ghcr.io/educates/charts` as an OCI Helm repository and create an Application with `chart: educates-installer`. Put the four custom resources in a second Application (or a later sync wave) so they apply after the CRDs exist; from there the operator's own dependency gating handles ordering.

Uninstalling
------------

Delete the custom resources before uninstalling the chart, so the operator's finalizers can drain what they installed:

```shell
kubectl delete sessionmanager/cluster lookupservice/cluster secretsmanager/cluster
kubectl delete educatesclusterconfig/cluster
helm uninstall educates-installer --namespace educates-installer
```

Delete `EducatesClusterConfig` last: its finalizer removes the operator-managed cluster services (Kyverno among them), and the platform components need those services present while draining. Uninstalling the chart first would remove the operator that processes the finalizers, wedging deletion.

Air-gapped and mirrored registries
----------------------------------

See [air-gapped installation](airgapped-installation) for mirroring the release images into an internal registry and pointing the installation at it.
