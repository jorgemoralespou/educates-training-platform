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
)

// defaultExternalSecretsNS is the v3-convention namespace the CLI's
// laptop flow puts CA Secrets in. Always included in the cache scope so
// a fresh operator pod (first install, ECC not yet created) still picks
// up CA changes once the user creates the CR.
const defaultExternalSecretsNS = "educates-secrets"

// discoverCachedSecretNamespaces returns the deduped, sorted set of
// namespaces the operator should populate informers for, given:
//
//   - operatorNamespace (always included — user-supplied Secrets like
//     TLS, image-pull, CustomCA-in-operator-NS live here)
//   - defaultExternalSecretsNS (always included — v3 / CLI convention
//     for cross-namespace CA refs; covers first-install before any CR
//     exists)
//   - any additional namespaces referenced by the current
//     EducatesClusterConfig singleton's CASecretReference fields
//     (CustomCA in Managed mode, CACertificateSecretRef in Inline mode)
//
// Boot-time only: if the user later edits the ECC to point at a new
// namespace, the operator needs to restart to pick up watches there.
// The reconciler emits a Warning event in that case so it's user-visible.
func discoverCachedSecretNamespaces(ctx context.Context, restCfg *rest.Config, scheme *runtime.Scheme, operatorNamespace string) ([]string, error) {
	// One-shot uncached client just for this read. Manager isn't built
	// yet, so the regular cached client isn't available; the request is
	// cheap (one Get against a cluster-scoped singleton) and only happens
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

	ecc := &configv1alpha1.EducatesClusterConfig{}
	if err := c.Get(ctx, types.NamespacedName{Name: "cluster"}, ecc); err != nil {
		if apierrors.IsNotFound(err) {
			// No CR yet — fall back to defaults. First reconcile after
			// CR creation will use APIReader for the actual Secret
			// fetch; only the watch-driven enqueue depends on the
			// cache scope, and laptop installs land in the default
			// 'educates-secrets' which is already in the set.
			return setToSortedSlice(set), nil
		}
		return nil, err
	}

	// Managed mode: CustomCA.caCertificateRef
	if ecc.Spec.Ingress != nil &&
		ecc.Spec.Ingress.Certificates.BundledCertManager != nil &&
		ecc.Spec.Ingress.Certificates.BundledCertManager.CustomCA != nil {
		if ns := ecc.Spec.Ingress.Certificates.BundledCertManager.CustomCA.CACertificateRef.Namespace; ns != "" {
			set[ns] = struct{}{}
		}
	}

	// Inline mode: caCertificateSecretRef
	if ecc.Spec.Inline != nil && ecc.Spec.Inline.Ingress.CACertificateSecretRef != nil {
		if ns := ecc.Spec.Inline.Ingress.CACertificateSecretRef.Namespace; ns != "" {
			set[ns] = struct{}{}
		}
	}

	return setToSortedSlice(set), nil
}

func setToSortedSlice(s map[string]struct{}) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
