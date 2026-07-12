# Scenario 05 — Image pull secret end-to-end with an auth'd local registry

Validates the chart's `imagePullSecrets` plumbing by *actually pulling*
session-manager's image through an authenticated registry. If anything
in the chain (Secret name mismatch, wrong creds, Secret not in the
operator namespace, containerd not configured) is broken,
`session-manager` rollout fails and so does the scenario. No way to
silently pass.

## What `pre-install.sh` does

1. Creates a tmp dir with an `htpasswd` file (Apache MD5,
   `educates-test:educates-test-pass`).
2. Runs `registry:2` as a container named `educates-test-pull-registry`
   on the `educates` docker network with `REGISTRY_AUTH=htpasswd`
   pointing at the file. Publishes container port 5000 to host
   `localhost:5003` for the `docker login + push` step.
3. `docker login localhost:5003` and pushes
   `educates-session-manager:3.7.1` into the registry under the path
   `educates/educates-session-manager:3.7.1`.
4. Writes a `hosts.toml` into the kind control-plane's
   `/etc/containerd/certs.d/educates-test-pull-registry:5000/` so
   containerd will accept HTTP pulls from the registry hostname.
   Modern containerd reloads `certs.d` dynamically — no restart needed.
5. Creates the `test-pull-secret` (`kubernetes.io/dockerconfigjson`) in
   `educates-secrets` only — the source for the chart's upstream
   SecretCopier rule. The in-operator-namespace copy is staged later
   by `post-install.sh` (see below).

## What `chart-values.yaml` does

- Points `session-manager.image.repository` at
  `educates-test-pull-registry:5000/educates/educates-session-manager`.
- Sets `session-manager.imagePullSecrets: [{name: test-pull-secret}]`
  on the Deployment.
- Lists `test-pull-secret` in `secretPropagation.imagePullSecretNames`
  for downstream propagation, and in
  `secretPropagation.upstream.imagePullSecrets` so secrets-manager
  keeps the operator-namespace copy in sync after the initial bootstrap
  by `pre-install.sh`.

## What `post-install.sh` does

Runs immediately after helm has applied chart manifests, *before*
rollout-status. Creates the `test-pull-secret` in the `educates`
operator namespace so the session-manager Pod's pull retry can
succeed. We don't pre-create the operator namespace ourselves — helm
owns that — and we can't rely on secrets-manager to copy the Secret
upstream-from-source either, because secrets-manager itself isn't up
yet (its Pod also needs the pull secret).

## What `post-deploy.sh` does

Greps the session-manager Pod's events for `Successfully pulled image
educates-test-pull-registry:5000/...`. Failure means containerd didn't
actually pull from our registry — runs only after the runner's
`rollout status` and workshop deploy have already succeeded.

## What `teardown.sh` does

`docker rm -f educates-test-pull-registry` and removes the tmp dir.
Runs always (success or fail), before the cluster delete, via the
runner's cleanup trap.

## Out of scope

- secrets-manager's image is left at the public default, so its pull
  doesn't go through the auth'd registry. Adding it would require the
  same pre-stage step for *that* image too, with no extra signal.
