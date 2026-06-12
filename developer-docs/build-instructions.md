Build Instructions
==================

If you are hoping to contribute to Educates, before you attempt to build a local version of Educates and start modifying any code, read our [contribution guidelines](../CONTRIBUTING.md) and reach out to us to discuss any changes you are considering.

Local Kubernetes environment
----------------------------

To do development on the core Educates platform you will need access to a Kubernetes cluster. For this we recommend you use a local Kubernetes cluster created using [Kind](https://kind.sigs.k8s.io/). Rather than you create this Kind cluster yourself, you can create it using the `educates` CLI. This will ensure that the Kind cluster is setup properly for how the Educates code is structured for doing local development.

When creating a local Kubernetes cluster with Educates the `educates create-cluster` command is used. For the case of wanting to do local development on Educates itself, you need to disable installation of the services required by Educates, and the core Educates platform. Provided you are using version 3.0 or later of the Educates CLI this is done using the command:

```
educates create-cluster --cluster-only
```

You can subsequently delete the local Kubernetes environment by running:

```
educates delete-cluster
```

Ensure you read the main Educates documentation quick start guide for any requirements around running the local Kubernetes cluster and Educates, deployed using the `educates` CLI.

Local docker image registry
---------------------------

When the local Kubernetes cluster is created using `educates create-cluster`, a local docker image registry will also be deployed to the local docker environment.

The docker image registry will be available at `localhost:5001` and will be used to hold container images built from the Educates source code. This image registry will also be used as the source of images when Educates is deployed to the local Kubernetes cluster.

If over time the amount of storage consumed by the local docker image cache increases to the point where overall available space within the local docker environment runs low, you can try to clean out unreferenced image layers by running:

```
docker image prune
```

The local docker image registry created using the Educates CLI can also grow in size due to unreferenced images. To prune unreferenced image layers kept by the local docker image registry you can run:

```
educates local registry prune
```

If you need to delete the local docker image registry and redeloy it, you can run:

```
educates local registry delete
```

to delete it, and:

```
educates local registry deploy
```

to recreate it. You will however need to push any previously built images to the local docker image registry again if this is done.

Note that this later command will create/update service resources in the Kubernetes cluster which are used to map and make available the local docker registry in the cluster. The original `educates create-cluster` command will also configure `containerd` within the Kubernetes cluster to trust the local docker image registry. It is thus important to use the `educates` CLI to deploy the local docker image registry rather than attempting to deploy a local docker image registry yourself.

Defining installer configuration
--------------------------------

Before building and deploying Educates from source code, you will need a configuration describing the target cluster and what is to be installed. The local configuration is managed by the `educates` CLI itself and lives in the CLI data home (by default `$XDG_DATA_HOME/educates/config.yaml`). Create a minimal configuration by running:

```
educates local config init
```

You can inspect it with `educates local config view` and change it with `educates local config edit`. The local configuration uses the `EducatesLocalConfig` format and should contain at least:

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: 192-168-1-1.nip.io
```

The `EducatesLocalConfig` kind implies an opinionated configuration suitable for a Kubernetes cluster created using Kind: the operator deploys an ingress router for the cluster using Contour, cert-manager for TLS, and Kyverno for implementing cluster and workshop security policies.

The `domain` should be set to be a `nip.io` address mapping to the IP address of your local host where you are doing development, or some other FQDN which maps to your local host. If left unset, the deploy command falls back to a `nip.io` address derived from your host IP.

If the configuration requires additional secrets these will need added to the local Kubernetes cluster in the namespace required by the configuration. If these secrets had previously been added to the local secrets cache, you can copy them to the local Kubernetes cluster by running:

```
educates local secrets sync
```

Building Educates platform images
---------------------------------

To build the container images for the Educates training platform you can run:

```
make build-core-images
```

NOTE: By default, images will be pushed to a local registry. There's instructions above on how to provide a local registry
via the Educates cli.

This will trigger the building of the container images and push the resulting images to the local docker image registry.

Targets for make are also available for building and pushing to the local docker image registry individual container images.

```
make build-session-manager
make build-secrets-manager
make build-training-portal
...
```

See the [Makefile](../Makefile) for more details of the make targets that are available. There are parameters that can be
set to alter behavior of the Makefile. There's comprehensive documentation at the top of the file.

Once the container images have been built and pushed to the local docker image registry, you can then deploy everything by running:

```
educates admin platform deploy --local-config
```

This installs the operator from the Helm chart embedded in the CLI, applies the four platform custom resources, and waits for each to report Ready. By default the platform runs the published `ghcr.io/educates` images; to have it run your locally built images instead, add `imageVersions` overrides to the local configuration (`educates local config edit`) pinning components at the local image registry, for example:

```yaml
imageVersions:
- name: session-manager
  image: localhost:5001/educates-session-manager:latest
- name: training-portal
  image: localhost:5001/educates-training-portal:latest
```

If you are working on the installer operator itself, build and load its image and point the operator deployment at it:

```
cd installer/operator
make docker-build IMG=ghcr.io/educates/educates-operator:dev
kind load docker-image ghcr.io/educates/educates-operator:dev --name educates
```

then set `operator.image.repository` and `operator.image.tag` in the local configuration (or pass `--set image.repository=... --set image.tag=dev` if installing the chart directly with `helm install` from `installer/charts/educates-installer`). See the [operator README](../installer/operator/README.md) and the make targets there (`make smoke-test`, `make test`) for the operator development loop.

To delete everything deployed to the cluster, use:

```
educates admin platform delete
```

Building additional workshop images
-----------------------------------

When using `make build-core-images`, it will only build the main workshop base image. That is, it will not build workshop base images for Java and Python.

If you want to build all workshop base images you can instead run:

```
make build-all-images
```

Note that this will consume a lot more storage space in the local docker environment. In general you will probably want to configure the local docker environment with 100Gi or more of storage space to be used across local image caching, the local docker image registry and the Kubernetes cluster itself.

As well as the `build-all-images`, make targets are also supplied for building and pushing to the local docker image registry individual workshop base images.

```
make build-base-environment
make build-jdk8-environment
make build-jdk11-environment
make build-jdk17-environment
make build-jdk21-environment
make build-conda-environment
```

See the [Makefile](../Makefile) for more details of the make targets that are available.

Building the Educates CLI program
---------------------------------

If needing to work on the `educates` CLI it can be built using the command:

```
make build-client-programs
```

You can then run the `educates` CLI program from the `client-programs/bin` subdirectory. The name of the compiled CLI will incorporate the target system and machine architecture, e.g.: `educates-linux-amd64`.

Note that when building the `educates` CLI from local source code, the embedded project version will be `develop` and the operator Helm chart embedded in the CLI is the one from your source tree. Platform images default to the published `ghcr.io/educates` images; use `imageVersions` overrides in the local configuration, as described above, when the platform should run locally built images instead.

Cleaning up available storage space
-----------------------------------

Running successive builds of the container images will incrementally result in more and more storage space being consumed as all layers of builds will be cached even if no longer being used.

Commands such as `docker image prune` can prune images from the local docker build cache, but will not reclaim storage for unused image layers in the local docker image registry.

To clean up available storage space across the local docker image build cache, the local docker image registry, and also any local file system space used to to local source code builds you can run:

```
make prune-all
```

Note that this will run `docker system prune` rather than `docker image prune`, which will also result in unused docker networks and volumes being cleaned up.

Also note that this doesn't reclaim space used by the image cache of `containerd` on the Kubernetes cluster nodes. If you are doing a lot of work on Educates, especially changes to the workshop base images and you deploy workshops using many successive versions of the images, eventually you can run out of storage space due to the `containerd` image cache. In this case there isn't really anything simple you do can except for deleting the Kubernetes cluster and starting over.

Building docs.educates.dev locally
----------------------------------

If you're working on updates or additions to the project documentation served at [docs.educates.dev](https://docs.educates.dev), you might want to preview your changes locally before opening a PR. To build and preview the docs locally, you can run:

```
make build-project-docs
make open-project-docs
```

Sample workshops for testing
----------------------------

A set of sample workshops are provided for testing and demonstrating different features of the Educates platform. These are not intended for end user consumption but serve as minimal pre-canned workshops for verifying platform functionality.

For details on the available sample workshops and instructions on how to deploy them, see the [workshop samples README](../workshop-samples/README.md).
