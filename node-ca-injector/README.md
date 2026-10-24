Node CA Injector
================

This directory holds the source code for the node CA injector, which makes
the container runtime on every cluster node trust the CA certificate the
Educates ingress uses. A workshop session can run its own image registry
behind the cluster ingress. When the ingress certificate is issued by a
private or self-signed CA, containerd on the nodes does not trust it, so the
kubelet cannot pull images from those registries. The node CA injector
configures containerd to trust the CA for each registry host.

How it works
------------

A single binary provides two commands, which run as separate workloads.

`node-ca-injector controller` runs as a Deployment. It watches Ingress
resources across the cluster, and on every change lists the Ingresses
labelled `training.educates.dev/application=registry`, the label the session
manager puts on the Ingress of each workshop session registry. It collects
the host names from their rules and writes them, sorted and without
duplicates, as a JSON array under the `hosts.json` key of the
`educates-registry-hosts` ConfigMap. The ConfigMap lives in the namespace
named by `OPERATOR_NAMESPACE`, or `educates` when that is unset. It is
created when missing and updated only when the set of hosts changes.

`node-ca-injector sync` runs as a privileged DaemonSet that tolerates every
taint, so it reaches every node. It does not call the Kubernetes API: it
reads the ConfigMap and the CA certificate from volume mounts. Once at
startup and then every 10 seconds, it:

1. copies the CA certificate to `_educates-ca/ca.crt` under the node's
   `/etc/containerd/certs.d` directory;
2. writes `<host>/hosts.toml` for every host in `hosts.json`, pointing
   containerd at that CA certificate, together with an `.educates-managed`
   marker file;
3. removes the directory of any host no longer listed, but only when it
   carries the marker, so registry configuration created by anything else
   is left alone.

A file is only rewritten when its content changes. A missing `hosts.json`,
which is the case until the controller has created the ConfigMap, counts as
an empty host list. containerd reads these files only when its registry
`config_path` includes `/etc/containerd/certs.d`.

The sync command reads from fixed paths, which these environment variables
override:

| Variable     | Default                        | Holds                         |
| ------------ | ------------------------------ | ----------------------------- |
| `HOSTS_FILE` | `/config/hosts/hosts.json`     | The host list                 |
| `CA_FILE`    | `/config/ca/ca.crt`            | The CA certificate            |
| `CERTS_D`    | `/host/etc/containerd/certs.d` | The node's containerd configuration, mounted from the host |

The `hosts.toml` files name the CA certificate by its path on the node,
`/etc/containerd/certs.d/_educates-ca/ca.crt`, not by the path inside the
container.

Deployment
----------

The node CA injector is not installed on its own. The Educates operator
installs it as the `node-ca-injector` Helm release, from the subchart in
`installer/charts/educates-training-platform/charts/node-ca-injector`, as an
optional extra of the `SessionManager` custom resource. The
`spec.nodeCATrust.mode` field of the `SessionManager` decides whether it is
installed:

- `Auto`, the default, installs it only when a CA is configured, either
  published by the `EducatesClusterConfig` or set as
  `spec.ingressOverrides.caCertificateSecretRef` on the `SessionManager`.
- `Enabled` always installs it, and refuses when no CA is configured.
- `Disabled` keeps it uninstalled, removing a release left by an earlier
  reconcile.

The nodes trust the CA the session manager uses: the key-free copy of
`spec.ingressOverrides.caCertificateSecretRef` when that is set, and
otherwise the Secret named by
`EducatesClusterConfig.status.ingress.caCertificateSecretRef`. Only the
`ca.crt` key of that Secret is mounted into the DaemonSet. The image can be
replaced with a `node-ca-injector` entry in `spec.images.overrides` of the
`SessionManager`.

The controller runs under the `node-ca-injector` service account, with a
ClusterRole that can read Ingresses and a Role that can manage ConfigMaps in
its own namespace.

Building and testing
--------------------

The module is part of the Go workspace at the root of the repository.

```bash
cd node-ca-injector
go build ./...
go test ./...
```

The tests in `internal/sync` run the sync loop against a temporary
directory. The controller has no tests.

Run `make image-node-ca-injector` from the root of the repository to build
the container image for the host architecture and push it to the local
registry at `localhost:5001`, or add `PUSH_IMAGES=false` to load it into the
local Docker daemon instead. The image holds only the statically linked
binary, built `FROM scratch`. The build and publish workflow publishes it to
the GitHub Container Registry as `educates-node-ca-injector`.
