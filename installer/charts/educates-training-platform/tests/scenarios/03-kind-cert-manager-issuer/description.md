# Scenario 03 — Kind + cert-manager as wildcard cert issuer

Production-shaped install where cert-manager is the actual issuer of
the wildcard certificate, signed by a CA the user provides. This is
the closest pre-phase scenario to the v4 operator's `Managed` mode for
`EducatesClusterConfig.spec.certificates.provider:
BundledCertManager + issuerType: CustomCA`.

## How it differs from scenario 02

Scenario 02 pre-generates the wildcard leaf in a shell script and
publishes both `wildcard-tls` and `wildcard-ca` Secrets directly,
bypassing cert-manager. Scenario 03 instead lets cert-manager do the
issuing — we only stage the CA — so we exercise the v3 installer's
`certs` package end-to-end (ClusterIssuer with `ca:` provider +
wildcard `Certificate` resource).

## Layout of the test

1. `educates local cluster create --cluster-only` provisions kind only.
2. `pre-install.sh` runs:
   - Creates the `educates-secrets` namespace.
   - Materialises the CA Secret `local-root-ca` in `educates-secrets`
     with three keys: `tls.crt` + `tls.key` (consumed by cert-manager's
     `ca:` issuer) and `ca.crt` (consumed by the v4 runtime for chain
     trust).
   - CA material comes from `--ca-cert/--ca-key` flags (or the mkcert
     auto-detected sibling); falls back to a freshly-generated
     self-signed CA when both are absent.
3. `educates admin platform deploy` installs cert-manager + certs +
   Contour + Kyverno. The v3 kind overlay sees
   `clusterInfrastructure.caCertificateRef` and wires:
   - cert-manager with `clusterResourceNamespace: educates-secrets`.
   - certs package with `local.caCertificateRef: local-root-ca` →
     creates a `ClusterIssuer` named `educateswildcard` and a
     `Certificate` that produces `educateswildcard` Secret in
     `educates-secrets`.
4. `helm install` for the chart, referencing `educateswildcard` (TLS)
   and `local-root-ca` (CA) for SecretCopier propagation into the
   operator namespace.

## What this proves

- The v3 installer can be driven without the educates package, with
  cert-manager + certs handling actual cert issuance from a
  user-provided CA.
- Our chart's TLS values and `secretPropagation.upstream.*` shape
  works against cert-manager-managed Secrets just as well as against
  hand-rolled ones.
- A trusted CA passed via `--ca-cert/--ca-key` flows all the way
  through to the workshop session pods (which need the CA to validate
  TLS to the portal).

## Out of scope

- ACME-based issuance (Let's Encrypt). That's a separate scenario for
  cloud installs and isn't applicable to local kind.
- Cert rotation testing. cert-manager will rotate, but the test
  doesn't span long enough to observe it.
