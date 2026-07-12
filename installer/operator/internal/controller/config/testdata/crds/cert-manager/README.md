# cert-manager CRDs for envtest

These are the cert-manager CRD YAMLs used by the controller's envtest
suite to register the `cert-manager.io/v1` types in the test API server.
Without them, the ClusterIssuer watch fails to establish at cache
startup and the validator's Inline-mode ClusterIssuer code path is
unreachable.

**Source:** `github.com/cert-manager/cert-manager v1.20.2`,
files `deploy/crds/cert-manager.io_clusterissuers.yaml` and
`deploy/crds/cert-manager.io_certificates.yaml` from the module cache.

**Refresh:** when the operator's cert-manager Go module is bumped,
copy the matching CRDs from the module cache into this directory:

```
cp $(go env GOMODCACHE)/github.com/cert-manager/cert-manager@<version>/deploy/crds/cert-manager.io_*.yaml \
   installer/operator/internal/controller/config/testdata/crds/cert-manager/
chmod +w installer/operator/internal/controller/config/testdata/crds/cert-manager/*.yaml
```

**What's here:** ClusterIssuer (used by the Inline-mode validator)
and Certificate (used by the Managed-mode wildcard certificate
pipeline). Issuer (namespaced) is not used by the operator and is
deliberately omitted.
