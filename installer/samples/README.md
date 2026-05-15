# EducatesClusterConfig samples

Reference `EducatesClusterConfig` resources for the three Managed-mode
scenarios verified during Phase 3.

| File | Scenario | Certificates | DNS | Policy |
|---|---|---|---|---|
| `01-local-kind-customca.yaml` | Local kind / developer machine (Managed) | BundledCertManager + CustomCA | — | — |
| `02-gke-clouddns-acme.yaml` | GKE production with Workload Identity (Managed) | BundledCertManager + ACME-DNS01 (CloudDNS) | BundledExternalDNS (CloudDNS) | Bundled Kyverno |
| `03-eks-route53-acme.yaml` | EKS production with IRSA (Managed) | BundledCertManager + ACME-DNS01 (Route53) | BundledExternalDNS (Route53) | Bundled Kyverno |
| `04-openshift-inline.yaml` | OpenShift / BYO cluster services (Inline) | pre-existing wildcard TLS Secret | — (cluster-managed) | OpenShiftSCC |

Platform-component CRs (apply *after* `EducatesClusterConfig` is Ready):

| File | Component |
|---|---|
| `secretsmanager.yaml` | SecretsManager — installs the secrets-manager runtime |
| `lookupservice.yaml` | LookupService — installs the lookup-service runtime (prefix + cluster domain → full hostname) |
| `sessionmanager.yaml` | SessionManager — installs the session-manager runtime (requires SecretsManager to be Ready first) |

Apply order:

```bash
helm install educates-installer ./installer/charts/educates-installer \
  --namespace educates-installer --create-namespace
kubectl apply -f installer/samples/<scenario>.yaml
kubectl apply -f installer/samples/secretsmanager.yaml
kubectl apply -f installer/samples/lookupservice.yaml      # optional
kubectl apply -f installer/samples/sessionmanager.yaml
```

Each file's comment header lists the prerequisites (Secrets to create,
IAM/Workload Identity bindings to set up before applying the CR).

## Deletion order

Delete in **reverse** order:

```bash
kubectl delete sessionmanager cluster
kubectl delete lookupservice cluster      # if applied
kubectl delete secretsmanager cluster
kubectl delete educatesclusterconfig cluster
```

Deleting `EducatesClusterConfig` first drains the cluster services
(cert-manager, contour, kyverno, external-dns); platform-component
finalizers then can't clean up resources whose CRDs are already gone,
and you'll see opaque `helm uninstall ... failed to delete release`
errors from the operator. A follow-up
(`Block EducatesClusterConfig finalize while platform CRs exist`)
will turn this into an explicit refusal with a clear message; until
that lands, the order above is required.
