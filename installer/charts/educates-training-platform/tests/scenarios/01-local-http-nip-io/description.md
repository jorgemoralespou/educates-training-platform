# Scenario 01 — Local HTTP, nip.io domain

A minimal, certificate-free install for local-laptop iteration.

- **Cluster:** kind, created by `educates local cluster create`.
- **Prerequisites installed by the v3 CLI:** Contour (ingress) and Kyverno
  (policy engine). The v3 Educates package itself is **disabled**
  (`clusterPackages.educates.enabled: false`) so the v4 chart can install
  the runtime in its place.
- **Domain:** `127-0-0-1.nip.io` — the nip.io trick that resolves any
  `*.127-0-0-1.nip.io` to `127.0.0.1`, where kind exposes its ingress.
- **TLS / CA:** none. Ingress runs on plain HTTP.
- **Subcharts enabled:** `secrets-manager`, `session-manager`. The
  `lookup-service` and `remote-access` subcharts are disabled — they are
  not exercised by a single-cluster local install.

## What this proves

- The chart renders and applies cleanly with all TLS/CA-related values
  empty.
- The session-manager runtime config blob can drive the v3 runtime via
  the `educates-config` Secret produced by the chart.
- A workshop session can be requested and reached through Contour over
  plain HTTP, without any cert-manager involvement.

## Known limitations

- Image references are pinned to the v3 runtime tag (`3.7.1`) via
  per-subchart `image.tag` overrides, because no `4.0.0-alpha.1` runtime
  images exist yet. This is expected when testing the chart standalone.
- The session-manager `config` blob is set inline in `chart-values.yaml`.
  In a v4 install the operator will derive these fields from the
  SessionManager CR + EducatesClusterConfig.status.
