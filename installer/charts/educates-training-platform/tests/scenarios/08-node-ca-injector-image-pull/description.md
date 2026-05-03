# Scenario 08 — node-ca-injector image-pull validation

End-to-end test that the cluster's containerd trusts the wildcard CA, by
having the workshop user build → push → deploy a container image through
the per-session registry (which is fronted by the wildcard cert).

## What's tested

- The full chain from chart → operator config → session-manager spawning
  a workshop session → docker daemon trusting the registry → containerd
  on the kind node trusting the registry → Deployment rollout succeeding.
- The decisive step is the `kubectl rollout status` at the end: it
  passes only if containerd on the node successfully pulled the image
  from the per-session registry. Without node-ca-injector writing the
  CA into `/etc/containerd/certs.d/`, the pull would fail with a TLS
  verification error and the rollout would hang in `ImagePullBackOff`.

## Layout

Same TLS+CA setup as scenario 02 (wildcard cert + CA in
`educates-secrets`, auto-derived SecretCopier, ca-trust-store init
container in session-manager and lookup-service Deployments), plus
`node-ca-injector.enabled: true` at the umbrella so the subchart's
controller Deployment + privileged DaemonSet render and run.

The workshop is scenario-local — `./workshop/` is published to the
local registry by the runner via `educates publish-workshop` and
deployed with `educates deploy-workshop -f ./workshop/resources/workshop.yaml`.

## Verification

Interactive — the runner pauses at step 5/6 with the portal URL printed.
Open the workshop in the browser and step through the instructions; the
workshop's final page asserts the Deployment rollout completes. The
runner's exit code is independent of that assertion (the workshop UI is
the test surface).

## Out of scope

- Build-time CA-trust at the docker daemon level. That's exercised
  implicitly when `docker push` succeeds inside the workshop session,
  but the runtime overlay that wires it lives in
  `session-manager/handlers/workshopsession.py` and isn't part of this
  chart-level test.
- Multi-node CA propagation. kind clusters typically run a single node;
  the DaemonSet's per-host fan-out is observable but not fundamentally
  multi-node-tested here.
