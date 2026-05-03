---
title: Overview
---

This workshop verifies that the cluster's container runtime (containerd)
trusts the cluster's wildcard CA. It does so by walking through a
realistic image-build pipeline: write a small Dockerfile, build it,
push it to the workshop session's private registry, and create a
Kubernetes Deployment that pulls the image from that registry.

The session registry is an HTTPS endpoint at a subdomain of the cluster
ingress, fronted by the same wildcard certificate the workshop's portal
uses. The certificate is signed by a private CA that is **not** in any
public trust store. For the Deployment's image pull to succeed, the
node's containerd needs that CA in its per-registry trust
configuration.

## What you will do

1. Write a trivial `Dockerfile` and build a small image inside the
   workshop session's docker daemon.
2. Tag the image for the per-session registry and push it.
3. Create a Kubernetes Deployment that pulls the image you just pushed.
4. Watch the rollout complete — or hang in `ImagePullBackOff` if the CA
   trust is missing.

## What success looks like

`kubectl rollout status` returns `successfully rolled out` within a
handful of seconds. That confirms containerd on every node carrying
this Deployment's pod can speak HTTPS to the per-session registry and
verify the certificate against the cluster CA.
