---
title: Workshop Summary
---

In this workshop you exercised the Educates workshop environment assets server.

You confirmed that the assets server:

* Serves static files, by browsing its directory listing and downloading an
  individual file from the unpacked Educates source.
* Generates a gzip-compressed `tar` archive of a directory on request, using the
  `/.tar.gz` (or `/.tgz`) suffix.
* Generates an uncompressed `tar` archive of a directory, using the `/.tar`
  suffix.
* Generates a `zip` archive of a directory, using the `/.zip` suffix.

The assets server was populated once for the whole workshop environment, from a
source archive downloaded with `vendir`, and served the same content to the
session over a shared ingress without requiring credentials.
