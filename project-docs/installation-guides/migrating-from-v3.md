(migrating-from-v3)=
Migrating from Educates v3
==========================

Educates v4 replaces the installation mechanism. v3 installed the platform as Carvel packages (driven by the `educates` CLI or a pre-installed `kapp-controller`); v4 installs a Helm chart containing a Kubernetes operator, driven by four custom resources — see [installation instructions](installation-instructions) for the new model.

Two facts shape the migration:

* **The Educates runtime is unchanged.** Workshop definitions, workshop content, published workshop OCI artifacts, and training portal configurations carry over as they are — the `training.educates.dev` custom resource APIs did not change in v4.
* **There is no in-place migration.** A v3 and a v4 installation cannot coexist on one cluster, and v4 does not upgrade a v3 install. You delete the v3 installation and install v4.

Migrating a cluster installation
--------------------------------

1. **Export your workshop resources.** Deleting the v3 installation removes the `training.educates.dev` CRDs and with them every `Workshop` and `TrainingPortal` resource. Save them first:

   ```shell
   kubectl get workshops -o yaml > workshops-backup.yaml
   kubectl get trainingportals -o yaml > trainingportals-backup.yaml
   ```

   If your definitions already live in files or Git (recommended), skip this.

2. **Delete the v3 installation**, using whichever mechanism installed it: `educates delete-platform` with your v3 CLI, or delete the `kapp-controller` `App`/`PackageInstall` resources if you installed via GitOps. Verify the educates namespaces and CRDs are gone before proceeding.

3. **Write a v4 configuration file** for your scenario (see the translation tables below and [configuration settings](configuration-settings)).

4. **Install v4** following [CLI-based installation](cli-based-installation) or [Helm-based installation](helm-based-installation).

5. **Re-apply your workshop resources.** Strip the exported resources of `status`, `metadata.uid`, `metadata.resourceVersion` and similar server-side fields (unnecessary if applying from your own files), then `kubectl apply` them. Training portals will recreate their workshop environments from scratch; any in-flight workshop sessions from the v3 install are not preserved.

Translating your v3 configuration
---------------------------------

The v3 `clusterInfrastructure.provider` value points at the v4 configuration kind to use:

```text
| v3 provider           | v4 configuration kind                                    |
|-----------------------|----------------------------------------------------------|
| kind                  | EducatesLocalConfig (migrated automatically — see below) |
| gke                   | EducatesGKEConfig                                        |
| eks                   | EducatesEKSConfig                                        |
| openshift             | EducatesInlineConfig                                     |
| generic               | EducatesInlineConfig                                     |
| minikube, vcluster    | EducatesConfig (escape hatch)                            |
| custom                | EducatesConfig (escape hatch)                            |
```

Common v3 values map to v4 fields as follows:

```text
| v3 values.yaml                          | v4 equivalent                                              |
|-----------------------------------------|------------------------------------------------------------|
| clusterIngress.domain                   | domain (GKE/EKS/Inline kinds) / ingress.domain (Local)     |
| clusterIngress.tlsCertificateRef        | wildcardCertificateSecret (Inline) — Secret must live in   |
|                                         | the operator namespace                                     |
| clusterIngress.caCertificateRef         | caCertificateSecret (Inline) / local secrets add ca (Local)|
| clusterIngress.protocol                 | ingress.insecure: true (Local, plain HTTP) /               |
|                                         | externalTLSTermination: true (GKE/EKS/Inline kinds)        |
| aws.region / aws.irsaRoles.*            | aws.region / aws.certManagerRoleARN /                      |
|                                         | aws.externalDNSRoleARN (EKS kind)                          |
| gcp.project / gcp.workloadIdentity.*    | gcp.project / gcp.certManagerServiceAccount /              |
|                                         | gcp.externalDNSServiceAccount (GKE kind)                   |
| clusterSecurity / policy engines        | policyEnforcement.clusterEngine / workshopEngine (Inline)  |
| imageRegistry (mirror)                  | imageRegistry.prefix (Inline / EducatesClusterConfig)      |
| imageVersions                           | imageVersions (same shape)                                 |
| lookupService.enabled                   | lookupService (boolean toggle)                             |
```

Anything not expressible in a narrow kind is reachable through `EducatesConfig`, which carries the four custom resource specs verbatim. Keep your v3 `values.yaml` open beside the [configuration settings](configuration-settings) reference while re-declaring; the JSON schemas give completion and validation in your editor as you go.

Laptop installs migrate automatically
-------------------------------------

For local kind-cluster users, the v4 CLI migrates your configuration on first use. When a v4 command needs the local config (`educates local cluster create`, `educates admin platform deploy --local-config`, ...) and finds a v3 `values.yaml` in the CLI data home with `clusterInfrastructure.provider` empty or `kind`, it:

* translates it to a v4 `config.yaml` (`EducatesLocalConfig`) — carrying over the ingress domain, kind cluster settings (listen address, API server, subnets, volume mounts, registry mirrors), local DNS resolver settings, image version overrides, website styling references, and image pull secret propagation;
* renames the original to `values.yaml.v3-backup`; and
* prints a one-line notice of what it did. No prompt, no flag — you run the same command you would have run on v3.

If the v3 file's provider is anything other than kind or empty, the CLI refuses with instructions instead: cloud and BYO configurations carry intent the CLI cannot infer (DNS, ACME, identity wiring), so they must be re-declared against a v4 kind as described above. The `values.yaml` is left untouched for reference.

One caveat: v3 cached its local CA differently (an `Opaque` Secret holding only the certificate). v4's install needs the CA key as well, so a v3-cached CA cannot be reused — the migration warns about this, and you regenerate with:

```shell
educates local secrets add ca <domain>-ca --domain <domain>
```

Command changes
---------------

The local-environment commands moved under grouped names:

```text
| v3 command                  | v4 command                       |
|-----------------------------|----------------------------------|
| educates create-cluster     | educates local cluster create    |
| educates delete-cluster     | educates local cluster delete    |
| educates deploy-platform    | educates admin platform deploy   |
| educates delete-platform    | educates admin platform delete   |
```

The workshop tooling (`educates publish-workshop`, `deploy-workshop`, `browse-workshops`, ...) is unchanged.

Removed in v4
-------------

* The Carvel/`kapp-controller` installation path. GitOps installs now point at the published Helm chart — see [Helm-based installation](helm-based-installation).
* Pre-canned provider configurations for `minikube` and `vcluster`. Equivalent installs are possible via `EducatesConfig` or `EducatesInlineConfig`.

Certificate-less, plain-HTTP installs are still supported. A v3 `clusterIngress.protocol: http` install maps to `ingress.insecure: true` on `EducatesLocalConfig`, which the laptop migration applies automatically. On other kinds, `externalTLSTermination` now installs no in-cluster certificate, and at the cluster-configuration level the `None` certificates provider serves the same role. See [secure HTTP connections](secure-http-connections).
