---
title: Summary
---

You just exercised the full image-pull path through a private CA:

1. The per-session OCI registry is published at a subdomain of the
   cluster ingress, served over HTTPS with a wildcard certificate
   signed by a private CA.
2. The workshop session's docker daemon trusted that CA, so `docker
   push` from the session worked without `--insecure-registry`.
3. The `default` ServiceAccount in the session namespace had registry
   credentials attached, so the Pod's image pull authenticated.
4. **The node's containerd trusted the same CA** — without that trust,
   the Pod would still be in `ImagePullBackOff` regardless of points 1
   through 3.

Point 4 is the bit this workshop tests. The trust is wired by the
`node-ca-injector` subchart: a privileged DaemonSet on every node
writes per-host CA configuration into `/etc/containerd/certs.d/`,
keyed off the same `clusterIngress.caCertificateRef` Secret the chart
distributes via SecretCopier.

If you saw the Deployment roll out successfully, this scenario passed.

You can clean up the deployment if you like, then close the session.

```terminal:execute
command: |-
  kubectl -n {{< param session_namespace >}} delete -f ~/exercises/deployment.yaml
```
