/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"context"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
)

// defaultExternalSecretsNS is the v3-convention namespace the CLI's
// laptop flow puts CA Secrets in. Always included in the cache scope so
// a fresh operator pod (first install, ECC not yet created) still picks
// up CA changes once the user creates the CR.
const defaultExternalSecretsNS = "educates-secrets"

// singletonName is the CEL-enforced name of the EducatesClusterConfig
// and SessionManager singletons.
const singletonName = "cluster"

// discoverCachedSecretNamespaces returns the deduped, sorted set of
// namespaces the operator should populate informers for, given:
//
//   - operatorNamespace (always included — user-supplied Secrets like
//     TLS, image-pull, CustomCA-in-operator-NS live here)
//   - defaultExternalSecretsNS (always included — v3 / CLI convention
//     for cross-namespace CA refs; covers first-install before any CR
//     exists)
//   - any additional namespaces referenced by the SecretReference
//     fields of the current EducatesClusterConfig singleton (CustomCA's
//     CA key pair and CA trust Secret, or StaticCertificate's wildcard
//     certificate and CA trust Secret, in Managed mode; the wildcard
//     certificate and CA trust Secret in Inline mode) and of the
//     SessionManager singleton (its CA override)
//
// Boot-time only: if the user later edits either singleton to point at a
// new namespace, the operator needs to restart to pick up watches there.
// The EducatesClusterConfig reconciler logs a warning in that case.
func discoverCachedSecretNamespaces(
	ctx context.Context,
	restCfg *rest.Config,
	scheme *runtime.Scheme,
	operatorNamespace string,
) ([]string, error) {
	// One-shot uncached client just for this read. Manager isn't built
	// yet, so the regular cached client isn't available; the requests
	// are cheap (one Get per cluster-scoped singleton) and only happen
	// at startup.
	c, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return collectFromClient(ctx, c, operatorNamespace)
}

// collectFromClient is the pure-logic core of discoverCachedSecretNamespaces,
// factored so tests can drive it with a fake client without needing
// envtest or a real REST config.
func collectFromClient(ctx context.Context, c client.Reader, operatorNamespace string) ([]string, error) {
	set := map[string]struct{}{
		operatorNamespace:        {},
		defaultExternalSecretsNS: {},
	}

	// A missing singleton contributes nothing: the first reconcile after
	// it is created uses APIReader for the actual Secret fetch; only the
	// watch-driven enqueue depends on the cache scope, and laptop
	// installs land in the default 'educates-secrets', which is already
	// in the set.
	ecc := &configv1alpha1.EducatesClusterConfig{}
	if err := c.Get(ctx, types.NamespacedName{Name: singletonName}, ecc); err == nil {
		addECCSecretNamespaces(set, ecc)
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	sm := &platformv1alpha1.SessionManager{}
	if err := c.Get(ctx, types.NamespacedName{Name: singletonName}, sm); err == nil {
		if sm.Spec.IngressOverrides != nil {
			if ref := sm.Spec.IngressOverrides.CACertificateSecretRef; ref != nil && ref.Namespace != "" {
				set[ref.Namespace] = struct{}{}
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	return setToSortedSlice(set), nil
}

// addECCSecretNamespaces adds the namespaces the EducatesClusterConfig's
// SecretReference fields name. An empty namespace means the operator
// namespace, which is always in the set.
func addECCSecretNamespaces(set map[string]struct{}, ecc *configv1alpha1.EducatesClusterConfig) {
	// Managed mode: CustomCA's CA key pair and optional CA trust Secret.
	if ecc.Spec.Ingress != nil &&
		ecc.Spec.Ingress.Certificates.BundledCertManager != nil &&
		ecc.Spec.Ingress.Certificates.BundledCertManager.CustomCA != nil {
		ca := ecc.Spec.Ingress.Certificates.BundledCertManager.CustomCA
		if ns := ca.CAKeyPairSecretRef.Namespace; ns != "" {
			set[ns] = struct{}{}
		}
		if ca.CACertificateSecretRef != nil && ca.CACertificateSecretRef.Namespace != "" {
			set[ca.CACertificateSecretRef.Namespace] = struct{}{}
		}
	}

	// Managed mode: StaticCertificate's wildcard Secret and optional CA
	// trust Secret.
	if ecc.Spec.Ingress != nil && ecc.Spec.Ingress.Certificates.StaticCertificate != nil {
		static := ecc.Spec.Ingress.Certificates.StaticCertificate
		for _, ref := range []*configv1alpha1.SecretReference{&static.TLSSecretRef, static.CACertificateSecretRef} {
			if ref != nil && ref.Namespace != "" {
				set[ref.Namespace] = struct{}{}
			}
		}
	}

	// Inline mode: wildcardCertificateSecretRef and caCertificateSecretRef.
	if ecc.Spec.Inline != nil {
		for _, ref := range []*configv1alpha1.SecretReference{
			ecc.Spec.Inline.Ingress.WildcardCertificateSecretRef,
			ecc.Spec.Inline.Ingress.CACertificateSecretRef,
		} {
			if ref != nil && ref.Namespace != "" {
				set[ref.Namespace] = struct{}{}
			}
		}
	}
}

func setToSortedSlice(s map[string]struct{}) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
