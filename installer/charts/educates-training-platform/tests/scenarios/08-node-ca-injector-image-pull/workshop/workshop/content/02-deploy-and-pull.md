---
title: Deploy and watch the rollout
---

The image is now sitting in the per-session registry at
`${REGISTRY_HOST}/node-ca-pull-test:1`. Create a Kubernetes Deployment
that pulls from there and runs in the workshop's session namespace.

The session namespace already has a `kubernetes.io/dockerconfigjson`
secret named via `${REGISTRY_SECRET}` attached to the `default`
ServiceAccount, so authentication on pull is handled. What's *not*
handled by Educates' own machinery is the TLS trust on the node — that
is what node-ca-injector is responsible for.

```editor:create-file
file: ~/exercises/deployment.yaml
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: node-ca-pull-test
  spec:
    replicas: 1
    selector:
      matchLabels:
        app: node-ca-pull-test
    template:
      metadata:
        labels:
          app: node-ca-pull-test
      spec:
        containers:
          - name: app
            image: REGISTRY_HOST_PLACEHOLDER/node-ca-pull-test:1
            command: ["sh", "-c", "echo node-ca-pull-test ready; sleep 3600"]
```

Substitute the per-session registry hostname into the manifest, then
apply it.

```terminal:execute
command: |-
  sed -i "s|REGISTRY_HOST_PLACEHOLDER|${REGISTRY_HOST}|" ~/exercises/deployment.yaml
  kubectl -n {{< param session_namespace >}} apply -f ~/exercises/deployment.yaml
```

Watch the rollout. The status command blocks until the Deployment
reports `successfully rolled out`, or until it times out — which is
what would happen if containerd couldn't pull the image because of an
untrusted certificate.

```terminal:execute
command: |-
  kubectl -n {{< param session_namespace >}} rollout status deployment/node-ca-pull-test --timeout=120s
```

If the previous command printed `deployment "node-ca-pull-test"
successfully rolled out`, **you've just observed node-ca-injector
working end-to-end**: the image came back over HTTPS from the
per-session registry, the wildcard cert verified against the CA that
node-ca-injector installed under `/etc/containerd/certs.d/`, and the
pod started.

{{< note >}}
If the rollout instead times out, the Pod will be in
`ImagePullBackOff`. Run `kubectl -n {{< param session_namespace >}}
describe deployment node-ca-pull-test` and check the events on the
underlying ReplicaSet's Pods — a TLS verification error in the events
indicates the node-level CA trust is missing.
{{< /note >}}

For a quick sanity check that the pod is alive, look at its log:

```terminal:execute
command: |-
  kubectl -n {{< param session_namespace >}} logs deployment/node-ca-pull-test
```

The expected output is `node-ca-pull-test ready`.
