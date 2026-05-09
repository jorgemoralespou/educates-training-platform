# cert-manager CRDs for envtest

These are the cert-manager CRD YAMLs used by the controller's envtest
suite to register the `cert-manager.io/v1` types in the test API server.
Without them, the ClusterIssuer watch fails to establish at cache
startup and the validator's Inline-mode ClusterIssuer code path is
unreachable.

**Source:** `github.com/cert-manager/cert-manager v1.20.2`,
file `deploy/crds/cert-manager.io_clusterissuers.yaml` from the module
cache.

**Refresh:** when the operator's cert-manager Go module is bumped, run
`make vendor-test-crds` (lands with the chart-vendoring Make target in
the Phase 2 chart-tarball task) to copy the matching CRDs from the
module cache into this directory.

**Why only ClusterIssuer for now:** Phase 1's Inline-mode validator only
references `ClusterIssuer`. Phase 2 will add `Certificate` (and possibly
`Issuer`) when the operator drives a wildcard certificate end-to-end.
