(quick-start-guide)=
Quick Start Guide
=================

The quickest way to install and start experimenting with Educates is to install it on your local machine using a Kubernetes cluster created using Kind. To make this process easier, Educates provides a command line tool called `educates` you can use to create a cluster and deploy Educates, as well as deploy and manage workshops under Educates.

This local Educates environment is also the recommended setup for working on your own workshop content, as it provides you with a local image registry which can be used to hold both custom workshop base images and your published workshops. Together this provides a quick local workflow for iterating on changes to your workshop content, without needing to publish anything to third party sites.

A detailed description on how to install Educates into any Kubernetes cluster is included later in the documentation.

Host system requirements
------------------------

To deploy Educates on your local machine using the Educates command line tool the following are required:

* You need to be running macOS or Linux. If using Windows you will need WSL (Windows subsystem for Linux). The Educates command line tool has primarily been tested on macOS.

* You need to have a working `docker` environment. The Educates command line tool has primarily been tested with [Docker Desktop](https://www.docker.com/products/docker-desktop/) but you can use [Colima](https://github.com/abiosoft/colima) if you are running on macOS.

* You need to have sufficient memory and disk resources allocated to the `docker` environment to run Kubernetes, Educates etc.

* You cannot be running an existing Kubernetes cluster created using Kind.

* You cannot be using port 80 (HTTP) and 443 (HTTPS) on the local machine as these will be required by the Kubernetes ingress controller.

* You need to have port 53 (DNS) available on the local machine when using macOS if you want to enable a local DNS resolver.

* You need to have port 5001 available on the local machine as this will be used for a local image registry.

If you are using [Docker Desktop](https://www.docker.com/products/docker-desktop/), you will need to enable the following:

* Allow the default Docker socket to be used (Settings->Advanced).

* Allow privileged port mapping (Settings->Advanced).

Depending on the Docker Desktop version you are running, you may also need to enable/disable:

* Use kernel networking for UDP (Settings->Resources->Network).

In case you are using [Colima](https://github.com/abiosoft/colima), you need to add the following lines to the educates configuration file:

```
$ educates local config edit
cluster:
  listenAddress: 0.0.0.0
```

Downloading the CLI
-------------------

To download the Educates CLI visit the releases page at:

* [https://github.com/educates/educates-training-platform/releases](https://github.com/educates/educates-training-platform/releases)

Find the most recent released version and download the `educates` CLI program for your platform.

* `educates-linux-amd64` - Linux (amd64)
* `educates-linux-arm64` - Linux (arm64)
* `educates-darwin-amd64` - macOS (amd64)
* `educates-darwin-arm64` - macOS (arm64)

Rename the downloaded program to `educates`, make it executable (`chmod +x educates`), and place it somewhere in your application search path.

To download the latest version using `curl` and mark it executable, you can use the appropriate command for your operating system and architecture below.

::::{tab-set}

:::{tab-item} Linux (amd64)
```
curl -o educates -sL https://github.com/educates/educates-training-platform/releases/latest/download/educates-linux-amd64 && chmod +x educates
```
:::

:::{tab-item} Linux (arm64)
```
curl -o educates -sL https://github.com/educates/educates-training-platform/releases/latest/download/educates-linux-arm64 && chmod +x educates
```
:::

:::{tab-item} macOS (amd64)
```
curl -o educates -sL https://github.com/educates/educates-training-platform/releases/latest/download/educates-darwin-amd64 && chmod +x educates
```
:::

:::{tab-item} macOS (arm64)
```
curl -o educates -sL https://github.com/educates/educates-training-platform/releases/latest/download/educates-darwin-arm64 && chmod +x educates
```
:::

::::

If you are running macOS with Apple silicon (arm64), the Intel 64 (amd64) binary will still work and be run under Rosetta emulation, however, by using it you will be able to use both `amd64` and `arm64` images in the Kubernetes cluster. If you use the Apple silicon (arm64) binary you will only be able to use `amd64` images in the Kubernetes cluster. Neither of the macOS binaries are signed so you will need to tell macOS to trust it before you can run it.

The `educates` CLI is also published as the `educates-cli` container image, a multi-architecture (amd64/arm64) Linux image with the binary at `/educates`. It can be run directly, or used in a `Dockerfile` if needing to embed the `educates` CLI in a container image:

```
FROM fedora:42

COPY --from=ghcr.io/educates/educates-cli:X.Y.Z /educates /usr/local/bin/educates
```

Replace `X.Y.Z` with the version of Educates you want to use. The image is public; if you get an authentication failure make sure you haven't previously logged into GitHub container registry with a GitHub personal access token which has since expired, as that will cause a failure even though the image is public.

Default ingress domain
----------------------

Educates requires a valid fully qualified domain name (FQDN) to use with Kubernetes ingresses which it creates.

By default, the `educates` CLI when creating a cluster will automatically use a `nip.io` address which consists of the IP address of your local machine as the ingress domain. For example `192-168-1-1.nip.io`.

If a `nip.io` address is relied upon, some features of Educates may not be able to be used. This is because those features require that you also have access to a wildcard TLS certificate for the ingress domain. Since you don't control the `nip.io` domain, there is no way for you to generate the required TLS certificate using a service such as LetsEncrypt. You could however using your own self signed certificate authority (CA) create a wildcard TLS certificate for the `nip.io` domain but you will need to configure macOS to use the CA, as well as configure Educates to know about the CA.

Also be aware that some home internet routers may block `nip.io` addresses from working. This is because of what is called [DNS rebinding protection](https://en.wikipedia.org/wiki/DNS_rebinding#Protection). You may have to re-configure your router to disable DNS rebinding protection. Alternatively, you can set up your host DNS resolver to use a public DNS provider such as Google (8.8.8.8) or Cloudflare (1.1.1.1).

For the initial deployment we will rely on a `nip.io` address. How to use an alternate ingress domain and a TLS certificate will be covered later.

Local Kubernetes cluster
------------------------

To create a local Kubernetes cluster using Kind and deploy Educates, run the commands:

```
educates local config init
educates local cluster create
```

With the default configuration the cluster is served over plain HTTP using a `nip.io` ingress domain, so no TLS certificate or certificate authority is needed and the cluster comes up ready to use. This is the quickest way to start experimenting. Some browser features that require a secure context, such as clipboard access, may be limited over plain HTTP, and a few Educates features such as the per session image registry require a trusted secure ingress. When you want trusted HTTPS, see [Serving workshops over HTTPS](serving-workshops-over-https) below.

This command will perform the following steps:

* Create the Kubernetes cluster using Kind.

* Deploy an image registry accessible via port 5001 on the local machine, and configure the cluster to trust it.

* Install the Educates operator, which in turn installs the required cluster services: Contour as the ingress controller exposed via ports 80/443 on the local machine, and a security policy engine. When you configure trusted HTTPS, cert-manager is also installed to issue TLS certificates from your local CA.

* Deploy the Educates training platform components.

Creation of the Kubernetes cluster, including the deployment of any required services and Educates, can take up to 5 minutes depending on your network speed.

Once the Kubernetes cluster has been created, you should be able to access it immediately using `kubectl` as the configuration will be added to your local Kube configuration. The name of the Kube config context for the cluster is `kind-educates`.

(serving-workshops-over-https)=
Serving workshops over HTTPS (optional)
---------------------------------------

The default cluster is served over plain HTTP. To serve workshops over trusted HTTPS you need an ingress domain you can issue a wildcard TLS certificate for, which rules out `nip.io` since you do not control that domain. The recommended approach for a local machine is to use your own domain, for example `educates-local-dev.test`, together with the Educates local DNS resolver, which resolves the domain to your machine without needing a public DNS registry.

Set this up before creating the cluster, in the following order. The examples use `educates-local-dev.test` as the domain.

First, if you want to use a domain name, deploy the local DNS resolver for it. The `--domain` flag tells the resolver which domain to serve before it is set in the configuration:

```
educates local resolver deploy --local-config --domain educates-local-dev.test
```

On macOS you also need to register the domain with the system resolver, and the resolver setup differs by operating system, so follow the full steps under [Local DNS resolver](local-dns-resolver) in the local environment guide.

Next, set the ingress domain in the configuration. Setting a domain turns off the insecure default:

```
educates local config set ingress.domain educates-local-dev.test
```

Then create a certificate authority (CA) for the domain. With no `--cert`/`--key` arguments a self-signed CA is generated for you and cached locally, and it is reused for every future cluster with the same domain. To supply an existing CA certificate and key with `--cert`/`--key` instead, for example one created with `mkcert`, see [the local environment guide](custom-ingress-domain). Create the CA with:

```
educates local secrets add ca educates-local-dev-test-ca --domain educates-local-dev.test
```

Finally create the cluster, running `educates local cluster delete` first if you already created the plain HTTP cluster:

```
educates local cluster create
```

Educates installs cert-manager, issues the wildcard certificate from your CA, and serves workshop URLs over HTTPS.

(trusting-the-workshop-certificates)=
Trusting the workshop certificates
----------------------------------

Workshop URLs are then served with TLS certificates signed by your local CA. Until that CA certificate is imported into your operating system trust store, your browser will warn that the connection is not private every time you open the training portal or a workshop session.

First export the CA certificate from the local secrets cache as a PEM file:

```
educates local secrets export educates-local-dev-test-ca --pem > educates-local-dev-test-ca.pem
```

Then import it into the trust store for your operating system:

::::{tab-set}

:::{tab-item} macOS
```
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain educates-local-dev-test-ca.pem
```

This covers Safari and Chrome.
:::

:::{tab-item} Linux (Debian/Ubuntu)
```
sudo cp educates-local-dev-test-ca.pem /usr/local/share/ca-certificates/educates-local-dev-test-ca.crt
sudo update-ca-certificates
```

Note that Chrome on Linux does not use the system trust store; it reads the NSS database in your home directory instead:

```
certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n educates-local-dev-test-ca -i educates-local-dev-test-ca.pem
```
:::

:::{tab-item} Linux (Fedora/RHEL)
```
sudo cp educates-local-dev-test-ca.pem /etc/pki/ca-trust/source/anchors/educates-local-dev-test-ca.pem
sudo update-ca-trust
```

Note that Chrome on Linux does not use the system trust store; it reads the NSS database in your home directory instead:

```
certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n educates-local-dev-test-ca -i educates-local-dev-test-ca.pem
```
:::

::::

Firefox maintains its own certificate store on every platform. Import the PEM file via Settings → Privacy & Security → Certificates → View Certificates → Authorities → Import, and check "Trust this CA to identify websites".

Restart your browser after importing for the change to take effect. The import only needs to be done once: the CA is reused for every future cluster created with the same ingress domain.

Deploying a workshop
--------------------

The Educates CLI is intended primarily for people who need to create workshop content. Before we get to how you can create your own workshop, let's start by deploying an existing workshop. In this case we will use an existing workshop which teaches about the fundamentals of using a Kubernetes cluster to deploy an application.

To deploy this workshop run:

```
educates deploy-workshop -f https://github.com/educates/lab-k8s-fundamentals/releases/latest/download/workshop.yaml
```

This will load the workshop resource definition into the Kubernetes cluster. If a training portal instance is not already running one will be deployed. A workshop environment for this specific workshop will then be created and registered with the training portal.

Accessing the workshop
----------------------

To access the workshop you just deployed, run:

```
educates browse-workshops
```

This should open your web browser on the URL for the training portal dashboard.

Note that the training portal will have a password and you will need to be logged in, however the `educates browse-workshops` command will automatically log you in.

If you want to share the URL for accessing the training portal, or enter it manually in the web browser, you can run:

```bash
educates list-portals
```

to get the details.

If the training portal was being accessed by a different user, or you were doing it from a different browser, you will be prompted to enter the training portal password.

To view the password you can run:

```
educates view-credentials
```

Enter the password if prompted. You should then be shown the list of workshops registered with the training portal and can start a workshop.

Note that the first time you run a workshop it may be slow to startup as the container image for the workshop environment will need to be pulled down to the local Kubernetes cluster. So be a bit patient if you have a slow internet connection.

When you have completed the workshop and you exit it, the workshop session will be shutdown and you will be returned to the training portal dashboard.

Deleting the workshop
---------------------

When you no longer require this workshop and wish to delete the workshop environment, run:

```
educates delete-workshop -f https://github.com/educates/lab-k8s-fundamentals/releases/latest/download/workshop.yaml
```

This requires you to provide the same URL for the location of the workshop definition you used when you deployed the workshop. If you do not remember the URL, you can view it by running:

```
educates list-workshops
```

Instead of using the URL, you can also use the name of the workshop as displayed when listing the workshops. For example:

```
educates delete-workshop -n educates-cli--lab-k8s-fundamentals-0129afe
```
