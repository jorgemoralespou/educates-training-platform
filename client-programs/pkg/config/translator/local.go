package translator

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TranslateLocal converts EducatesLocalConfig into the deployable output.
//
// Translator invariants applied here (per the locked Phase 5 design):
//   - mode: Managed
//   - ingress.ingressClassName: contour
//   - ingress.controller.provider: BundledContour
//   - ingress.certificates.provider: BundledCertManager
//   - ingress.certificates.bundledCertManager.issuerType: CustomCA
//   - ingress.certificates.bundledCertManager.customCA.caCertificateRef.name:
//     educates-custom-ca
//
// Static field defaults (clusterAdmin true, lookupService true,
// imagePrePuller false, operator.logLevel info, cluster.listenAddress
// 127.0.0.1) have already been applied by EducatesLocalConfig.WithDefaults()
// at load time.
//
// Environment-dependent defaults are NOT applied here:
//   - ingress.domain stays empty unless the caller set it (host-IP nip.io
//     defaulting belongs upstream of the translator).
//   - operator.image.tag stays as-is (CLI-binary-version defaulting
//     belongs in command code that has access to the build info).
func TranslateLocal(cfg *v1alpha1.EducatesLocalConfig) *Output {
	out := &Output{
		OperatorChartValues:   localOperatorChartValues(cfg),
		EducatesClusterConfig: wrapCR(apiVersionConfig, "EducatesClusterConfig", localECCSpec(cfg)),
		SecretsManager:        wrapCR(apiVersionPlatform, "SecretsManager", localSecretsManagerSpec(cfg)),
		SessionManager:        wrapCR(apiVersionPlatform, "SessionManager", localSessionManagerSpec(cfg)),
	}
	if cfg.LookupService != nil && *cfg.LookupService {
		out.LookupService = wrapCR(apiVersionPlatform, "LookupService", localLookupServiceSpec(cfg))
	}
	return out
}

func localOperatorChartValues(cfg *v1alpha1.EducatesLocalConfig) map[string]interface{} {
	values := map[string]interface{}{}
	if cfg.Operator.Image.Repository != "" || cfg.Operator.Image.Tag != "" {
		image := map[string]interface{}{}
		if cfg.Operator.Image.Repository != "" {
			image["repository"] = cfg.Operator.Image.Repository
		}
		if cfg.Operator.Image.Tag != "" {
			image["tag"] = cfg.Operator.Image.Tag
		}
		values["image"] = image
	}
	if len(cfg.Operator.ImagePullSecrets) > 0 {
		// Helm template emits this verbatim into the pod spec; k8s
		// expects [{name: ...}] not [string].
		secrets := make([]interface{}, len(cfg.Operator.ImagePullSecrets))
		for i, name := range cfg.Operator.ImagePullSecrets {
			secrets[i] = map[string]interface{}{"name": name}
		}
		values["imagePullSecrets"] = secrets
	}
	if cfg.Operator.LogLevel != "" {
		// Chart does not yet template a logLevel value; setting it here
		// is forward-compatible and ignored by current renders.
		values["logLevel"] = cfg.Operator.LogLevel
	}
	return values
}

// localECCSpec builds the EducatesClusterConfig.spec for Local mode.
// Always Managed; always BundledContour + CustomCA cert-manager.
func localECCSpec(cfg *v1alpha1.EducatesLocalConfig) map[string]interface{} {
	ingress := map[string]interface{}{
		"ingressClassName": "contour",
		"controller": map[string]interface{}{
			"provider": "BundledContour",
		},
		"certificates": map[string]interface{}{
			"provider": "BundledCertManager",
			"bundledCertManager": map[string]interface{}{
				"issuerType": "CustomCA",
				"customCA": map[string]interface{}{
					"caCertificateRef": map[string]interface{}{
						"name": "educates-custom-ca",
					},
				},
			},
		},
	}
	if cfg.Ingress.Domain != "" {
		ingress["domain"] = cfg.Ingress.Domain
	}

	spec := map[string]interface{}{
		"mode":    "Managed",
		"ingress": ingress,
	}
	return spec
}

// localSecretsManagerSpec — empty spec; the operator derives image/resources
// from chart defaults + ECC status.
func localSecretsManagerSpec(cfg *v1alpha1.EducatesLocalConfig) map[string]interface{} {
	spec := map[string]interface{}{}
	if cfg.Operator.LogLevel != "" {
		spec["logLevel"] = cfg.Operator.LogLevel
	}
	return spec
}

// localLookupServiceSpec — minimal; ingress.prefix=lookup is the conventional
// hostname segment.
func localLookupServiceSpec(cfg *v1alpha1.EducatesLocalConfig) map[string]interface{} {
	spec := map[string]interface{}{
		"ingress": map[string]interface{}{
			"prefix": "lookup",
		},
	}
	if cfg.Operator.LogLevel != "" {
		spec["logLevel"] = cfg.Operator.LogLevel
	}
	return spec
}

// localSessionManagerSpec carries the session-manager runtime knobs the
// CLI surfaces in the narrow EducatesLocalConfig shape.
//
// TODO(phase4-followup): clusterAdmin and secretPropagation have no
// landing field in the current SessionManager CRD. They are dropped here
// pending the CRD additions tracked in the v4 development plan. The
// operator will need spec.clusterAdmin (bool) and spec.secretPropagation
// (imagePullSecretNames list) before this translator can wire them up.
func localSessionManagerSpec(cfg *v1alpha1.EducatesLocalConfig) map[string]interface{} {
	spec := map[string]interface{}{}
	if cfg.Operator.LogLevel != "" {
		spec["logLevel"] = cfg.Operator.LogLevel
	}
	if cfg.WebsiteStyling.DefaultTheme != "" {
		spec["defaultTheme"] = cfg.WebsiteStyling.DefaultTheme
	}
	if len(cfg.WebsiteStyling.ThemeDataRefs) > 0 {
		refs := make([]interface{}, len(cfg.WebsiteStyling.ThemeDataRefs))
		for i, r := range cfg.WebsiteStyling.ThemeDataRefs {
			refs[i] = map[string]interface{}{
				"namespace": r.Namespace,
				"name":      r.Name,
			}
		}
		spec["themes"] = map[string]interface{}{"dataRefs": refs}
	}
	if cfg.ImagePrePuller != nil {
		spec["imagePrePuller"] = map[string]interface{}{
			"enabled": *cfg.ImagePrePuller,
		}
	}
	if len(cfg.ImageVersions) > 0 {
		overrides := make([]interface{}, len(cfg.ImageVersions))
		for i, iv := range cfg.ImageVersions {
			overrides[i] = map[string]interface{}{
				"name":  iv.Name,
				"image": iv.Image,
			}
		}
		spec["images"] = map[string]interface{}{"overrides": overrides}
	}
	return spec
}
