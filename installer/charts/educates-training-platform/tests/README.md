# Pre-phase chart validation scenarios

End-to-end tests for the `educates-training-platform` Helm chart against
real kind clusters. Each scenario provisions the cluster (using the v3
`educates` CLI to install Kubernetes prerequisites without the v3
Educates package), then installs the v4 chart, deploys a sample
workshop, and verifies the runtime is functional.

These tests prove the chart can replace the v3 carvel-based installer
for the runtime, which is the "Done when" criterion for the pre-phase
in `docs/architecture/educates-v4-development-plan.md`.

## Layout

```
tests/
├── README.md              # This file (scenario index)
├── run-scenario.sh        # Test runner (scenario name as $1)
└── scenarios/
    └── <scenario>/
        ├── description.md
        ├── educates-config.yaml   # consumed by `educates local cluster create --config`
        ├── chart-values.yaml      # consumed by `helm install -f`
        ├── pre-install.sh         # optional hook: after cluster create, before helm install
        ├── post-install.sh        # optional hook: after helm install + readiness, before portal create
        ├── post-deploy.sh         # optional hook: after workshop deploy, with PORTAL_URL exported
        └── teardown.sh            # optional hook: always runs in cleanup trap, before cluster delete
```

Hook scripts are executed when present and executable. A non-zero
exit from `pre-install.sh`, `post-install.sh`, or `post-deploy.sh`
fails the scenario. `teardown.sh` runs unconditionally and any error
it returns is logged but ignored.

- **`pre-install.sh`** — stage external dependencies (Secrets in
  foreign namespaces, generated certs, pre-staged registries).
- **`post-install.sh`** — assertions on what the chart rendered
  (grep a Secret for a marker, wait for a SecretCopier to take
  effect, etc.). Runs with `kubectl` already pointing at the
  cluster, before any portal/workshop is created.
- **`post-deploy.sh`** — end-to-end runtime assertions.
  `PORTAL_URL` is exported (resolved from
  `trainingportal/educates-cli`'s `status.educates.url`) so the
  hook can `curl` the live portal and validate that chart-driven
  config actually reached the runtime.
- **`teardown.sh`** — clean up out-of-cluster resources created by
  `pre-install.sh` (docker containers, tmp dirs). Runs always —
  on success, on failure, on `--keep`. Should be idempotent.

## Running a scenario

```sh
./run-scenario.sh 01-local-http-nip-io
```

The runner prints status at each step. After deploying the workshop it
pauses with the portal URL and credentials, so you can browse and
exercise it. Press Enter (or send `--no-wait`) to proceed to the
teardown step.

Pass `--keep` to skip the cluster-delete step (handy when iterating on
a fix).

### Reusing settings via `.env`

The runner sources `tests/.env` (gitignored) before reading flags.
Copy `tests/.env.example` to `tests/.env` and put your usual
`DOMAIN`, `CA_CERT_PATH`, etc. in it once — subsequent runs no longer
need the flags. Explicit flags and pre-set shell env vars still
override the file.

### Domain and TLS material

Scenarios are templated with `envsubst`; `${DOMAIN}` is the only
placeholder used today. The runner resolves it as follows:

- `--domain <domain>` flag (or `DOMAIN` env var) wins.
- Otherwise, the first non-loopback IPv4 of `en0`–`en4` is used to
  build `<host-ip>.nip.io`, matching the educates CLI's
  `GetHostIpAsDns()` default. Fails fast if no usable interface is
  found — pass `--domain` explicitly when on a CI runner or VM.

For TLS scenarios, you can pass trusted material so browsers don't
warn:

- `--tls-cert <path>` and `--tls-key <path>` — a wildcard leaf cert and
  its private key, valid for `*.<DOMAIN>`.
- `--ca-cert <path>` — a CA cert. When supplied alongside `--ca-key`,
  the scenario signs a fresh wildcard leaf with it; when supplied
  alongside `--tls-cert/--tls-key`, it's just published as the
  `wildcard-ca` Secret so the runtime trusts the chain.
- `--ca-key <path>` — CA private key paired with `--ca-cert`. The
  runner auto-detects the mkcert sibling (`rootCA.pem` ↔
  `rootCA-key.pem`) when `--ca-key` is omitted.

If you already have a trusted CA set up via mkcert (per the
[educates docs](https://docs.educates.dev/en/stable/getting-started/local-environment.html#custom-ingress-domain))
the simplest invocation is:

```sh
./run-scenario.sh 02-kind-tls-wildcard \
  --ca-cert "$(mkcert -CAROOT)/rootCA.pem"
```

The runner picks up the matching `rootCA-key.pem` automatically and
the scenario signs a fresh wildcard leaf for the resolved `${DOMAIN}`.

When all of `--tls-cert/--tls-key/--ca-cert/--ca-key` are omitted, the
scenario's `pre-install.sh` falls back to generating a self-signed CA
+ wildcard cert at runtime — browsers will warn.

## Scenarios

| ID | Description | TLS | Notes |
|---|---|---|---|
| `01-local-http-nip-io` | Local kind, HTTP, nip.io domain, no TLS, no CA, no lookup-service. | No | Smallest viable install. Exercises the chart's "everything optional is off" path. |
| `02-kind-tls-wildcard` | Local kind with Contour + Kyverno, offline-generated wildcard TLS Secret + CA copied via SecretCopier, HTTPS ingress. | Yes | Exercises the chart's TLS values shape and `secretPropagation.upstream.*`. cert-manager is not used for issuance. |
| `03-kind-cert-manager-issuer` | Local kind with cert-manager + certs + Contour + Kyverno; wildcard cert is *issued* by cert-manager from a user-provided CA. | Yes | Closest to the v4 operator's `Managed`-mode `BundledCertManager + CustomCA` shape. Pre-install hook stages the CA Secret in the layout cert-manager's `ca:` issuer expects. |
| `04-website-theme` | HTTP base + custom `session-manager.websiteTheme` value. | No | Asserts that the chart serialises the theme map into the `default-website-theme` Secret. |
| `05-image-pull-secrets` | HTTP base + session-manager pulls its image through an htpasswd-protected local registry. | No | Stands up an auth'd `registry:2` container, mirrors `educates-session-manager:3.7.1` into it, configures kind containerd, and stages the pull secret. `kubectl rollout status deployment/session-manager` is the real test — fails if any link in the chart's pull-secret chain breaks. `teardown.sh` removes the registry container. |
| `06-additional-kyverno-policies` | HTTP base + default-bundled v3 Kyverno policies + a user-supplied marker ClusterPolicy. | No | After workshop deploy, asserts `clusterpolicy/educates-environment-*` contains rules from the bundled baseline + operational set and from the user-supplied bucket. |
| `07-config-escape-hatch` | HTTP base + `session-manager.config` opaque overrides. | No | Asserts the escape hatch deep-merges on top of typed-derived values (`dockerDaemon.networkMTU` override wins) and passes unknown fields through (`experimental.markerKey`). |
| `08-node-ca-injector-image-pull` | TLS base + node-ca-injector; workshop user builds → pushes → deploys through the per-session registry. | Yes | The closing `kubectl rollout status` passes only if containerd on the kind node trusts the wildcard CA that fronts the registry — proving node-ca-injector wrote `/etc/containerd/certs.d/`. |
| `09-kind-all-options` | Kitchen sink: every subchart on (incl. lookup-service + remote-access) and every session-manager value block populated. | Yes | Asserts the full typed-values surface lands in the `educates-config` blob, all SecretCopier paths (TLS/CA, themes, pull secret), the optional subchart rollouts, and the external `defaultTheme` served by the portal. |

## Adding a scenario

Pick the next free ID (e.g. `03-...`), create a folder under `scenarios/`
with the three files (`description.md`, `educates-config.yaml`,
`chart-values.yaml`), and append a row to the table above.
