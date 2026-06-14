# educates-installer operator

Go operator that reconciles the four v4 Educates CRDs
(`EducatesClusterConfig`, `SecretsManager`, `LookupService`,
`SessionManager`) and is shipped as the `educates-installer` Helm chart at
[`installer/charts/educates-installer/`](../charts/educates-installer/).

## Architecture

- [`docs/architecture/educates-v4-development-plan.md`](../../docs/architecture/educates-v4-development-plan.md)
- [`docs/architecture/educates-crd-draft-v1alpha1-r3.md`](../../docs/architecture/educates-crd-draft-v1alpha1-r3.md)
- [`docs/architecture/decisions.md`](../../docs/architecture/decisions.md)

## Layout

- `api/config/v1alpha1/` — `EducatesClusterConfig` types.
- `api/platform/v1alpha1/` — `SecretsManager`, `LookupService`,
  `SessionManager` types + shared common types.
- `internal/controller/{config,platform}/` — reconciler skeletons (Phase 0
  stubs that log and return).
- `cmd/main.go` — manager wiring all four controllers.

The kubebuilder-default `config/` kustomize tree is intentionally absent;
`controller-gen` writes CRDs and RBAC directly into the chart. See
[decisions.md](../../docs/architecture/decisions.md).

## Make targets

| Target | What |
|---|---|
| `make manifests` | Regenerate CRDs + ClusterRole into the `educates-installer` chart |
| `make generate` | Regenerate DeepCopy methods |
| `make test` | Run envtest (downloads `setup-envtest` binaries on first run) |
| `make envtest` | Download envtest binaries only |
| `make docker-build` | Build the operator image (`IMG=…` to override) |
| `make smoke-test` | Local kind-based smoke test |
| `make lint` | golangci-lint |
| `make build` | Build the manager binary |
| `make run` | Run the manager against `~/.kube/config` |

To reproduce the full installer-operator CI job locally (chart-version
lint, `go vet`/`build`, CRD/RBAC + DeepCopy drift checks, envtest and
`golangci-lint`) in one command, run `make ci-operator` from the
repository root. See [Running CI checks locally](../../developer-docs/build-instructions.md#running-ci-checks-locally).
