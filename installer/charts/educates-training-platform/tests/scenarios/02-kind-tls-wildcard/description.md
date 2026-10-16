# Scenario 02 — Kind + Contour + Kyverno + wildcard TLS (offline-generated)

A TLS-on scenario that exercises the chart's HTTPS values shape and
secret-propagation path without relying on cert-manager to issue the
wildcard cert. It is the chart-level counterpart of the v4 operator's
`StaticCertificate` provider in Managed mode, which serves a wildcard
certificate issued outside the cluster and installs no cert-manager.

## Layout of the test

1. `educates local cluster create` provisions kind + Contour + Kyverno
   only (no cert-manager, no certs package, no Educates package).
2. `pre-install.sh` runs between cluster-create and the chart install:
   - Uses the supplied wildcard certificate, or generates one for
     `*.${DOMAIN}` with `openssl`, into a tmp dir, signed by the
     supplied CA or by a self-signed root CA.
   - Checks the certificate as the operator checks a
     `StaticCertificate`: its SANs cover `*.${DOMAIN}`, it has not
     expired, and it verifies against the CA.
   - Creates the namespace `educates-secrets`.
   - `kubectl create secret tls wildcard-tls -n educates-secrets ...`,
     holding only `tls.crt` and `tls.key`, the shape
     `staticCertificate.tlsSecretRef` names.
   - `kubectl create secret generic wildcard-ca -n educates-secrets ...`,
     holding only `ca.crt` and never a private key, the shape
     `staticCertificate.caCertificateSecretRef` names.
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
- A wildcard certificate and a key-free CA trust Secret of the shapes
  `StaticCertificate` takes are enough for the runtime: no CA private
  key reaches the cluster.

## Out of scope

- Using cert-manager to issue the wildcard cert. Scenario 03 covers
  that.
- The operator itself. This scenario installs the runtime chart
  standalone; the operator's `StaticCertificate` handling is covered by
  its envtest suite and by a local `educates local cluster create` run.
- Trusted CA chain in the host browser. The CA generated here is
  self-signed; you'll get the usual "untrusted certificate" prompt when
  opening the portal in a browser. Click through to verify the
  workshop loads.

## Notes for the runner

- The CA + wildcard cert are generated fresh on each test run, into a
  tmp directory printed by `pre-install.sh`. They are not committed to
  the repo.
- With `--tls-cert/--tls-key`, pass the CA that issued the certificate
  with `--ca-cert`; the CA key is not needed. A certificate that does
  not verify against the CA fails `pre-install.sh`, as it would fail
  the operator's check.
- The `pre-install.sh` hook is generic — `run-scenario.sh` invokes it
  if present in the scenario folder. Future scenarios can drop one in
  the same way.
