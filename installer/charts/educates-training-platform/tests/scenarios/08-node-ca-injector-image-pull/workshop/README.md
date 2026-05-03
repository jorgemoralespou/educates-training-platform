# Lab — node-ca-injector image pull

Verify that the cluster's containerd trusts the cluster's wildcard CA by
building, pushing, and deploying a container image through the workshop
session's private OCI registry. If the deployment rolls out, the CA is
present in the node's containerd trust configuration.
