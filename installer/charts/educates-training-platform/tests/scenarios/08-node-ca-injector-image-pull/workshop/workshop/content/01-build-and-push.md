---
title: Build and push the image
---

Start by creating a minimal Dockerfile. The image just needs something
that keeps a process alive long enough for the rollout to settle.

```editor:create-file
file: ~/exercises/Dockerfile
text: |
  FROM busybox:1.36
  CMD ["sh", "-c", "echo node-ca-pull-test ready; sleep 3600"]
```

Build the image inside the workshop session's docker daemon. The
session uses its own daemon, isolated from the host.

```terminal:execute
command: |-
  docker build -t node-ca-pull-test:1 ~/exercises
```

The next step uses two environment variables that Educates pre-sets in
the terminal: `REGISTRY_HOST` (the per-session registry hostname,
under the cluster ingress wildcard) and `REGISTRY_USERNAME` /
`REGISTRY_PASSWORD` (per-session credentials). Take a quick look so the
following commands make sense.

```terminal:execute
command: |-
  echo "REGISTRY_HOST=${REGISTRY_HOST}"
```

Tag the freshly built image for the per-session registry, then push.
Educates has already populated `~/.docker/config.json` with the
registry credentials, so `docker push` authenticates without a manual
`docker login`.

```terminal:execute
command: |-
  docker tag node-ca-pull-test:1 ${REGISTRY_HOST}/node-ca-pull-test:1
  docker push ${REGISTRY_HOST}/node-ca-pull-test:1
```

If the push completed without TLS errors, the workshop session's docker
daemon trusts the wildcard CA — that's the runtime-side overlay
(`session-manager/handlers/workshopsession.py`) doing its job. The next
page is the part this scenario actually targets: containerd on the
node.
