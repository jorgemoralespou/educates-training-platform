package translator

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TranslateGKE converts EducatesGKEConfig into the deployable output.
// ECC.spec is mode: Managed with the full GKE-prod stack: BundledContour
// (LoadBalancer envoy), BundledCertManager with ACME-DNS01-CloudDNS,
// BundledExternalDNS with CloudDNS, BundledKyverno.
//
// opts is accepted for signature uniformity with TranslateLocal; the
// CASecret fields are not consumed (ACME does its own cert lifecycle).
func TranslateGKE(cfg *v1alpha1.EducatesGKEConfig, _ Options) (*Output, error) {
	out := &Output{
		OperatorChartValues:   operatorChartValuesFor(cfg.Operator),
		EducatesClusterConfig: wrapCR(apiVersionConfig, "EducatesClusterConfig", gkeECCSpec(cfg)),
		SecretsManager:        wrapCR(apiVersionPlatform, "SecretsManager", logLevelOnlySpec(cfg.Operator.LogLevel)),
		SessionManager:        wrapCR(apiVersionPlatform, "SessionManager", scenarioSessionManagerSpec(cfg.Operator.LogLevel, cfg.WebsiteStyling, cfg.ImagePrePuller, cfg.ImageVersions)),
	}
	if cfg.LookupService != nil && *cfg.LookupService {
		out.LookupService = wrapCR(apiVersionPlatform, "LookupService", scenarioLookupServiceSpec(cfg.Operator.LogLevel))
	}
	return out, nil
}

// gkeECCSpec builds the Managed-mode ECC.spec for GKE.
func gkeECCSpec(cfg *v1alpha1.EducatesGKEConfig) map[string]interface{} {
	cloudDNS := map[string]interface{}{
		"project":                        cfg.GCP.Project,
		"workloadIdentityServiceAccount": cfg.GCP.CertManagerServiceAccount,
	}
	acme := map[string]interface{}{
		"email": cfg.ACME.Email,
		"solvers": map[string]interface{}{
			"dns01": map[string]interface{}{
				"provider": "CloudDNS",
				"cloudDNS": cloudDNS,
			},
		},
	}
	if cfg.ACME.Server != "" {
		acme["server"] = cfg.ACME.Server
	}

	return map[string]interface{}{
		"mode": "Managed",
		"ingress": map[string]interface{}{
			"domain":           cfg.Domain,
			"ingressClassName": "contour",
			"controller": map[string]interface{}{
				"provider": "BundledContour",
				"bundledContour": map[string]interface{}{
					"envoyServiceType": "LoadBalancer",
				},
			},
			"certificates": map[string]interface{}{
				"provider": "BundledCertManager",
				"bundledCertManager": map[string]interface{}{
					"issuerType": "ACME",
					"acme":       acme,
				},
			},
		},
		"dns": map[string]interface{}{
			"provider": "BundledExternalDNS",
			"bundledExternalDNS": map[string]interface{}{
				"provider": "CloudDNS",
				"sources":  []interface{}{"service"},
				"cloudDNS": map[string]interface{}{
					"project":                        cfg.GCP.Project,
					"workloadIdentityServiceAccount": cfg.GCP.ExternalDNSServiceAccount,
				},
			},
		},
		"policyEnforcement": map[string]interface{}{
			"clusterPolicy":  map[string]interface{}{"engine": "Kyverno"},
			"workshopPolicy": map[string]interface{}{"engine": "Kyverno"},
			"kyverno":        map[string]interface{}{"provider": "Bundled"},
		},
	}
}

// logLevelOnlySpec is the shared body used by SecretsManager — only
// logLevel surfaces, the operator derives image/resources from chart
// defaults + ECC.status.
func logLevelOnlySpec(logLevel string) map[string]interface{} {
	spec := map[string]interface{}{}
	if logLevel != "" {
		spec["logLevel"] = logLevel
	}
	return spec
}

// scenarioLookupServiceSpec is shared by every scenario kind that
// emits LookupService.
func scenarioLookupServiceSpec(logLevel string) map[string]interface{} {
	spec := map[string]interface{}{
		"ingress": map[string]interface{}{"prefix": "lookup"},
	}
	if logLevel != "" {
		spec["logLevel"] = logLevel
	}
	return spec
}

// scenarioSessionManagerSpec is the cloud-scenario-shaped SessionManager
// builder. Mirrors localSessionManagerSpec minus the laptop-specific
// storage.storageGroup / network.blockedCidrs invariants.
func scenarioSessionManagerSpec(logLevel string, ws v1alpha1.LocalWebsiteStylingConfig, ipp *bool, imageVersions []v1alpha1.ImageVersion) map[string]interface{} {
	spec := map[string]interface{}{}
	if logLevel != "" {
		spec["logLevel"] = logLevel
	}
	if ws.DefaultTheme != "" {
		spec["defaultTheme"] = ws.DefaultTheme
	}
	if len(ws.ThemeDataRefs) > 0 {
		refs := make([]interface{}, len(ws.ThemeDataRefs))
		for i, r := range ws.ThemeDataRefs {
			refs[i] = map[string]interface{}{"namespace": r.Namespace, "name": r.Name}
		}
		spec["themes"] = map[string]interface{}{"dataRefs": refs}
	}
	if ipp != nil {
		spec["imagePrePuller"] = map[string]interface{}{"enabled": *ipp}
	}
	if len(imageVersions) > 0 {
		overrides := make([]interface{}, len(imageVersions))
		for i, iv := range imageVersions {
			overrides[i] = map[string]interface{}{"name": iv.Name, "image": iv.Image}
		}
		spec["images"] = map[string]interface{}{"overrides": overrides}
	}
	return spec
}
