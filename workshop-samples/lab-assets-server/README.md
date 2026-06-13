Assets Server Test
==================

Test workshop for exercising the Educates workshop environment assets server. It
verifies that the assets server can serve static files and can generate `tar`,
`tar.gz`/`tgz`, and `zip` archives of directories on the fly.

The assets server is a shared HTTP server deployed once per workshop
environment. It is prepopulated on startup, using `vendir`, with files that
workshop sessions can then download. In this workshop the assets server is
loaded with the Educates platform source code, downloaded and unpacked from the
`develop` branch source archive on GitHub, and the workshop instructions then
download files and directory archives from it.

**Enabling the assets server:**

The assets server is deployed for a workshop environment when one or more
sources are listed under `spec.environment.assets.files`. The entries use the
same syntax as `vendir`, and `vendir` unpacks `tar`/`zip` archives and OCI
images by default. Setting `spec.environment.assets.ingress.enabled` to `true`
exposes the assets server through a public ingress with anonymous access, with
the hostname available as the `$(assets_repository)` data variable:

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-assets-server
spec:
  environment:
    assets:
      ingress:
        enabled: true
      files:
      - http:
          url: https://github.com/educates/educates-training-platform/archive/refs/heads/develop.tar.gz
        path: educates-source
```

**Generating archives of directories:**

Because `vendir` unpacks archives into individual files, the assets server
provides a way to download a whole directory as a single archive again. Request
a directory path with an archive extension suffix (`/.tar`, `/.tar.gz`, `/.tgz`,
or `/.zip`) and the server generates the archive on the fly:

```
$(ingress_protocol)://$(assets_repository)/educates-source/.tar.gz
```
