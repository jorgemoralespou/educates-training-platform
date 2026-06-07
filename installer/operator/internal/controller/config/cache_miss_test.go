/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package config

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestWarnIfCacheMiss_CachedNamespace_NoLog(t *testing.T) {
	r := &EducatesClusterConfigReconciler{
		CachedSecretNamespaces: map[string]bool{
			"educates-installer": true,
			"educates-secrets":   true,
		},
	}
	buf, ctx := bufLogContext()
	r.warnIfCacheMiss(ctx, "educates-secrets", "spec.test")
	if buf.Len() != 0 {
		t.Errorf("cached namespace should not log, got: %s", buf.String())
	}
}

func TestWarnIfCacheMiss_UncachedNamespace_Logs(t *testing.T) {
	r := &EducatesClusterConfigReconciler{
		CachedSecretNamespaces: map[string]bool{
			"educates-installer": true,
			"educates-secrets":   true,
		},
	}
	buf, ctx := bufLogContext()
	r.warnIfCacheMiss(ctx, "team-namespace", "spec.ingress.certificates.bundledCertManager.customCA.caCertificateRef")

	s := buf.String()
	for _, want := range []string{
		"cache miss",
		"team-namespace",
		"restart the operator pod",
		"spec.ingress.certificates.bundledCertManager.customCA.caCertificateRef",
		"educates-installer",
		"educates-secrets",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("log missing %q in:\n%s", want, s)
		}
	}
}

func TestWarnIfCacheMiss_EmptyCacheSet_NoLog(t *testing.T) {
	// Empty CachedSecretNamespaces (test-mode default) disables the
	// warning so tests that don't supply the cache scope aren't spammed.
	r := &EducatesClusterConfigReconciler{}
	buf, ctx := bufLogContext()
	r.warnIfCacheMiss(ctx, "anywhere", "spec.test")
	if buf.Len() != 0 {
		t.Errorf("empty cache set should not log, got: %s", buf.String())
	}
}

// bufLogContext returns a buffer and a context carrying a logr.Logger
// that writes every line into the buffer. Sufficient for substring
// assertions on log output.
func bufLogContext() (*bytes.Buffer, context.Context) {
	var buf bytes.Buffer
	log := funcr.New(func(prefix, args string) {
		if prefix != "" {
			buf.WriteString(prefix + ": ")
		}
		buf.WriteString(args + "\n")
	}, funcr.Options{})
	return &buf, logf.IntoContext(context.Background(), logr.Logger(log))
}
