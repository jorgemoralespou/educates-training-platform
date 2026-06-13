---
title: Workshop Overview
---

This workshop exercises the Educates workshop environment **assets server**. The
assets server is a shared HTTP server deployed once per workshop environment and
prepopulated with files that workshop sessions can download. It provides data
locality, so common resources are pulled into the cluster once rather than by
every session.

For this workshop the assets server has been loaded with the Educates platform
source code, downloaded and unpacked from the `develop` branch source archive on
GitHub. You will download files and directory archives from it to confirm the
server is working correctly.

The following features are covered:

**Serving static files:**

* Browsing the files held by the assets server
* Downloading an individual static file

**Generating archives of directories:**

* Downloading a directory as a `tar` archive (`/.tar`)
* Downloading a directory as a gzip-compressed `tar` archive (`/.tar.gz` or `/.tgz`)
* Downloading a directory as a `zip` archive (`/.zip`)

The assets server is reached through the `$(assets_repository)` data variable,
which resolves to the public hostname of the server because a shared ingress has
been enabled for it. Anonymous access is allowed, so no credentials are needed
to download from it.
