/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"strings"

	"github.com/go-logr/logr"
)

// filteringLogSink wraps a logr.LogSink to demote specific
// controller-runtime ERROR messages to V(1)/debug so they don't
// dominate the operator pod's log surface in normal operation.
//
// Today there is exactly one such message: controller-runtime's
// internal/source/kind.go logs "if kind is a CRD, it should be
// installed before calling Start" with a full stack trace every
// time its internal poll-retry loop fires. We hit it in two
// situations:
//
//  1. Post-uninstall: the CRDWatcher activated a watch for a
//     cert-manager.io kind while cert-manager was installed; once
//     `cleanupManaged` does `helm uninstall cert-manager` the CRD
//     is gone, but the registered Source can't be removed
//     (controller-runtime offers no public API for it). The Source
//     enters its retry loop and spams ERRORs every 10s until the
//     operator pod restarts.
//
//  2. Edge race: a CRD is removed between the CRDWatcher's
//     discovery probe (success) and the underlying Kind source's
//     own discovery probe (fail). Unlikely but possible.
//
// In both cases the operator's own classifier
// (`isCertManagerCRDMissingErr` →
// `CertificatesReady=False reason=CertManagerCRDsMissing`)
// already surfaces the correct user-facing state. The
// controller-runtime ERROR + stack trace adds no diagnostic
// value beyond what's in our condition message, but visually
// dominates the log and looks alarming. Demoting to V(1) keeps
// it discoverable with `--zap-log-level=debug` (or equivalent
// verbosity bump) without scaring users in default operation.
//
// Tracked as a follow-up (quiet the controller-runtime Kind source
// after cert-manager CRDs are removed). This sink covers the visual
// symptom; the proper fix
// (Source teardown when no longer needed) remains an upstream
// contribution opportunity.
type filteringLogSink struct {
	inner logr.LogSink
}

// The operator filters two distinct ERROR messages that fire when a
// previously-registered watch's CRD disappears (typical end-of-life
// teardown). Both are emitted in tight loops every few seconds until
// the operator pod restarts.
//
//   - kindSourceCRDMissingMessage: from
//     controller-runtime/pkg/internal/source/kind.go's discovery
//     retry loop. Fires when the Kind source can't resolve its GVK
//     via discovery.
//
//   - reflectorFailedToWatchMessage: from
//     k8s.io/client-go/tools/cache.DefaultWatchErrorHandler. Fires
//     when the reflector backing an informer can't LIST the kind
//     (apiserver returns "the server could not find the requested
//     resource"). Distinct from the source.Kind message because the
//     Kind source successfully started the informer before the CRD
//     was deleted; the failure shifts from the source layer to the
//     reflector layer.
//
// We additionally gate the reflector filter on the well-known error
// substring "the server could not find the requested resource" so
// generic transient failures (connection refused, timeouts) still
// surface at ERROR.
const (
	kindSourceCRDMissingMessage   = "if kind is a CRD, it should be installed before calling Start"
	reflectorFailedToWatchMessage = "Failed to watch"
	reflectorKindMissingSubstring = "the server could not find the requested resource"
)

func (s *filteringLogSink) Init(info logr.RuntimeInfo) {
	s.inner.Init(info)
}

func (s *filteringLogSink) Enabled(level int) bool {
	return s.inner.Enabled(level)
}

func (s *filteringLogSink) Info(level int, msg string, kv ...any) {
	s.inner.Info(level, msg, kv...)
}

func (s *filteringLogSink) Error(err error, msg string, kv ...any) {
	switch {
	case strings.Contains(msg, kindSourceCRDMissingMessage):
		// controller-runtime source.Kind discovery retry loop.
		s.inner.Info(1, "watch source retry: kind not currently resolvable (post-uninstall or pre-install race; expected)",
			append([]any{"originalError", err}, kv...)...)
		return
	case msg == reflectorFailedToWatchMessage &&
		err != nil &&
		strings.Contains(err.Error(), reflectorKindMissingSubstring):
		// client-go reflector LIST retry loop. Only filter when the
		// underlying cause is "resource not found" — other reflector
		// failures (connection refused, transient apiserver errors)
		// still surface at ERROR.
		s.inner.Info(1, "watch reflector retry: kind not currently resolvable (post-uninstall or pre-install race; expected)",
			append([]any{"originalError", err}, kv...)...)
		return
	}
	s.inner.Error(err, msg, kv...)
}

func (s *filteringLogSink) WithName(name string) logr.LogSink {
	return &filteringLogSink{inner: s.inner.WithName(name)}
}

func (s *filteringLogSink) WithValues(kv ...any) logr.LogSink {
	return &filteringLogSink{inner: s.inner.WithValues(kv...)}
}
