# EducatesClusterConfig samples

Reference `EducatesClusterConfig` resources for the three Managed-mode
scenarios verified during Phase 3.

| File | Scenario | Certificates | DNS | Policy |
|---|---|---|---|---|
| `01-local-kind-customca.yaml` | Local kind / developer machine | BundledCertManager + CustomCA | — | — |
| `02-gke-clouddns-acme.yaml` | GKE production with Workload Identity | BundledCertManager + ACME-DNS01 (CloudDNS) | BundledExternalDNS (CloudDNS) | Bundled Kyverno |
| `03-eks-route53-acme.yaml` | EKS production with IRSA | BundledCertManager + ACME-DNS01 (Route53) | BundledExternalDNS (Route53) | Bundled Kyverno |

Platform-component CRs (apply *after* `EducatesClusterConfig` is Ready):

| File | Component |
|---|---|
| `secretsmanager.yaml` | SecretsManager — installs the secrets-manager runtime |

Apply order:

```bash
helm install educates-installer ./installer/charts/educates-installer \
  --namespace educates-installer --create-namespace
kubectl apply -f installer/samples/<scenario>.yaml
```

Each file's comment header lists the prerequisites (Secrets to create,
IAM/Workload Identity bindings to set up before applying the CR).
