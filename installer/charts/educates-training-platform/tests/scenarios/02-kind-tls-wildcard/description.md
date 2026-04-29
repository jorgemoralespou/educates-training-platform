# Scenario 02 — Kind + Contour + Kyverno + wildcard TLS (offline-generated)

A TLS-on scenario that exercises the chart's HTTPS values shape and
secret-propagation path without relying on cert-manager to issue the
wildcard cert. Cert-manager-as-issuer is intentionally out of scope here
— that's the v4 operator's job in Phase 2 of the development plan.

## Layout of the test

1. `educates local cluster create` provisions kind + Contour + Kyverno
   only (no cert-manager, no certs package, no Educates package).
2. `pre-install.sh` runs between cluster-create and the chart install:
   - Generates a self-signed root CA and a wildcard cert for
     `*.127-0-0-1.nip.io` with `openssl`, into a tmp dir.
   - Creates the namespace `educates-secrets`.
   - `kubectl create secret tls wildcard-tls -n educates-secrets ...`
   - `kubectl create secret generic wildcard-ca -n educates-secrets ...`
3. `helm install` lands the chart with `protocol: https` and
   `secretPropagation.upstream.{ingressTLS,ingressCA}` pointing at those
   Secrets. The session-manager's SecretCopier then copies them into
   the operator namespace at runtime.

## What this proves

- The chart's TLS/CA values shape works end-to-end: ingress over HTTPS,
  a wildcard cert wired into Contour, CA propagated to workshop
  sessions.
- The `secretPropagation.upstream.*` copies actually work — Secrets
  created in a foreign namespace are pulled into `educates` by the
  SecretCopier resources the chart renders.
- The session-manager runtime correctly switches to HTTPS based on the
  presence of `clusterIngress.tlsCertificateRef.name` in the config
  blob.

## Out of scope

- Using cert-manager to issue the wildcard cert. A future scenario 03
  can exercise the v3 CLI's CA-injection point + cert-manager + certs
  packages for that.
- Trusted CA chain in the host browser. The CA generated here is
  self-signed; you'll get the usual "untrusted certificate" prompt when
  opening the portal in a browser. Click through to verify the
  workshop loads.

## Notes for the runner

- The CA + wildcard cert are generated fresh on each test run, into a
  tmp directory printed by `pre-install.sh`. They are not committed to
  the repo.
- The `pre-install.sh` hook is generic — `run-scenario.sh` invokes it
  if present in the scenario folder. Future scenarios can drop one in
  the same way.
