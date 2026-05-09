# Vendored upstream Helm charts

This directory holds upstream Helm charts the v4 operator drives via the
Helm SDK. The bytes are checked into the repository so that:

- chart-version bumps are deliberate, reviewable git events, not whatever
  the upstream registry happens to serve at install time;
- the operator can install in air-gapped environments without runtime
  registry access;
- image-relocation rewrites in Phase 6 have a single point of control.

See `docs/architecture/decisions.md` →
*"Vendored upstream charts live as tarballs at `installer/vendored-charts/`"*
for the full rationale.

## Layout

```
installer/vendored-charts/
├── README.md                    (this file)
├── SHA256SUMS                   (one line per tarball: <hash>  <filename>)
└── <name>-<version>.tgz         (one tarball per chart)
```

`SHA256SUMS` is the integrity record. `make vendor-charts` (in
`installer/operator/Makefile`) downloads each chart from upstream and
verifies its hash against this file before writing into place.

## Current contents

| Chart | Version | Upstream | Used by |
|---|---|---|---|
| cert-manager | v1.20.2 | https://charts.jetstack.io | EducatesClusterConfig (BundledCertManager) |

Future entries (added per phase 3): contour, kyverno, external-dns.

## Refreshing or adding a chart

1. Update or extend the version table above.
2. Update `SHA256SUMS` with the expected hash for the new tarball
   (compute via `shasum -a 256 <file>` after downloading from a trusted
   source).
3. Update the chart list in `installer/operator/Makefile`'s
   `vendor-charts` target.
4. Run `make -C installer/operator vendor-charts`. The target downloads
   each chart, verifies the SHA256 against `SHA256SUMS`, and writes the
   tarball into this directory only on a hash match.
5. Test that the operator's Helm wrapper can load the new tarball
   (covered by Phase 2+ reconciler integration tests).
6. Commit the updated tarball, `SHA256SUMS`, and any code that depends
   on the new version.

The intent is "we ship the upstream chart unmodified" — never edit a
vendored tarball or unpack-and-repack it. If a change to the chart is
needed, it goes upstream first or is applied via Helm values at
install time, not by patching the bytes.
