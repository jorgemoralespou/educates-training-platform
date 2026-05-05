# Follow-up issues

GitHub issues to open once the v4 chart-based install ships in
`develop`. Each entry is a near-ready issue draft — title, motivating
context, scope, acceptance criteria — that can be transcribed to the
GitHub issue tracker with minimal further editing.

The format mirrors `decisions.md`: one heading per issue, prose body,
date the entry was added. New issues append at the end.

Mark items here as `*(opened: <issue link>)*` once the issue is
filed in the tracker, and `*(landed: <commit/PR>)*` once resolved.
Items that turn out to be irrelevant after further work get
`*(dropped)*` with a one-line reason. Don't delete — the history is
itself useful for future planning.

---

### Simplify `operator_config.py` IMAGE_REPOSITORY resolution

**Date added:** 2026-05-05.
**Trigger to file:** v4 chart shipped in `develop`; users no longer
install via the v3 carvel installer for new clusters.

**Context:**

`session-manager/handlers/operator_config.py:26-35` currently composes
`IMAGE_REPOSITORY` from the runtime config's `imageRegistry.host` +
`imageRegistry.namespace`, with a fallback to
`registry.default.svc.cluster.local` when the host is empty.
`secrets-manager/handlers/operator_config.py` reads its own
`OPERATOR_NAMESPACE` from the same blob.

In v3, that compose logic mattered because the carvel-installer baked
image refs into the OCI bundle at build time AND populated
`imageRegistry` in the operator config so workshops with
`$(image_repository)` placeholders could resolve runtime-spawned
images consistently. The runtime's `image_reference()` function used
`IMAGE_REPOSITORY` as the default prefix when an image wasn't listed
in `imageVersions`.

In v4 the chart populates `imageVersions` with the full set of
runtime-spawned image refs explicitly — every short-name the runtime
knows about (`training-portal`, `base-environment`, the loftsh
images, etc.) is present with a fully-qualified reference, sourced
from Chart.yaml annotations. The `IMAGE_REPOSITORY` fallback is
hit only by:

1. `$(image_repository)` placeholders in workshop content YAMLs, when
   workshops authored in dev are deployed to dev clusters via
   `educates publish-workshop` + `educates deploy-workshop`. Released
   workshops have these placeholders pre-resolved.
2. Hypothetical short-names not yet in `imageVersions`. None known
   today.

**Scope:**

Tighten `operator_config.py` so the runtime stops mixing the two
conceptual roles `imageRegistry` was playing in v3:

- Drop the `imageRegistry.host` + `imageRegistry.namespace` compose
  logic. Read `IMAGE_REPOSITORY` directly from a single
  `imageRepository` field in the operator config (the chart can emit
  this directly when `development.imageRegistry` is set), with the
  same `registry.default.svc.cluster.local` fallback.
- Drop the runtime-side fallback path in `image_reference()` for
  images not in `imageVersions`. Treat unknown short-names as a config
  error rather than silently composing an unresolvable ref against
  `IMAGE_REPOSITORY`.

**Why now (after develop ships) and not earlier:**

Until v4 is in `develop`, the v3 carvel installer is the production
install path. Changing `operator_config.py` shape would force a
coordinated v3 carvel-installer + runtime change. Easier to wait until
v4 owns the install and then simplify the runtime to match the
narrower contract the chart actually provides.

**Acceptance criteria:**

- `operator_config.py` reads `IMAGE_REPOSITORY` from a single config
  field (`imageRepository: ""` populated by the chart, or empty for
  the in-cluster fallback). No host/namespace compose logic.
- `image_reference()` raises a clear error for short-names absent from
  `imageVersions` instead of composing a default ref.
- Chart's `session-manager.operatorConfigYAML` helper updated to emit
  the simplified field (or kept emitting the existing shape for
  backward compatibility during a deprecation window — decide at
  filing time).
- All 8 chart scenarios still pass.
- Workshop with a `$(image_repository)/<name>` placeholder still
  resolves correctly when `development.imageRegistry` is set.

---

### Drop `clusterIngress.tlsCertificate` / `caCertificate` inline forms from `operator_config.py`

**Date added:** 2026-05-05.
**Trigger to file:** when filing the previous issue, or independently.

**Context:**

`session-manager/handlers/operator_config.py:52-65` accepts an inline
`clusterIngress.tlsCertificate: { tls.crt, tls.key }` block as an
alternative to the `tlsCertificateRef` form, materialising a Secret
named `<domain>-tls` from the inline content. Same for
`clusterIngress.caCertificate`.

The v4 chart only emits the `*Ref` forms — references to existing
Secrets (typically populated by cert-manager or a pre-install hook).
The inline form is dead code in v4-installed clusters.

**Scope:**

Drop the inline-form parsing in `operator_config.py`. The `*Ref` forms
remain as the single way to reference TLS / CA material.

**Why:** simplifies the runtime, narrows the attack surface (no inline
TLS material flowing through the runtime config), and matches what the
v4 chart actually emits.

**Acceptance criteria:**

- Inline-form code paths removed from `operator_config.py`.
- Existing scenarios still pass (none use the inline form).
- v3 release notes / migration guide flag the removal as a
  `migrate-config`-handled translation.

---

### CI lint: Chart.yaml annotations stay in sync across subcharts

**Date added:** 2026-05-05.
**Trigger to file:** any time after the `development.imageRegistry` +
annotations refactor lands.

**Context:**

The four image-rendering subcharts each carry their own copy of
`educates.dev/image-registry-host` and `-namespace` annotations in
their `Chart.yaml`. The release workflow updates them per fork. If a
release accidentally updates only some, image refs across subcharts
would diverge silently.

**Scope:**

Add a CI step (or pre-commit hook) that asserts all four subchart
Chart.yamls have identical values for the two annotations. Failing the
lint blocks the merge.

**Acceptance criteria:**

- CI step checks all four `Chart.yaml`s have matching
  `educates.dev/image-registry-host` and `-namespace` annotations.
- Lint fails with a clear error pointing at the offending subchart.

**Optional extension:** also check that all five `Chart.yaml`s
(umbrella + subcharts) share the same `version` and `appVersion`.
The umbrella's `dependencies[].version` should match the subchart
versions too.

---

### Document the chart release workflow's annotation update step

**Date added:** 2026-05-05.
**Trigger to file:** before the first chart release that consumers
will install from a fork.

**Context:**

The `development.imageRegistry` + annotations design relies on the
release workflow updating each subchart's
`educates.dev/image-registry-host` and `-namespace` annotations per
fork. The release runbook currently only mentions `appVersion`
bumping.

**Scope:**

Update `docs/release-runbook.md` (or wherever the release runbook
lives — create if missing) with the annotation-update step, including
the `yq -i` command pattern and the rationale (decisions.md entry).

**Acceptance criteria:**

- Release runbook documents the annotation update across all four
  Chart.yamls (or refers to the CI lint that enforces consistency).
- Worked example for the upstream release case AND the fork release
  case.
