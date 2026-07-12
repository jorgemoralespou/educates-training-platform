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

package config

import (
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsWebhookNotReadyErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("connection refused"), false},
		{
			// Real failure seen during cert-manager bootstrap: cainjector
			// hasn't injected the caBundle yet, so the apiserver can't
			// verify the webhook's TLS cert.
			"x509 webhook handshake",
			errors.New(`Internal error occurred: failed calling webhook "webhook.cert-manager.io": failed to call webhook: Post "https://cert-manager-webhook.cert-manager.svc:443/validate?timeout=30s": tls: failed to verify certificate: x509: certificate signed by unknown authority`),
			true,
		},
		{
			// Earlier-in-startup variant: Service endpoints not populated.
			"webhook connection refused",
			errors.New(`Internal error occurred: failed calling webhook "webhook.cert-manager.io": connection refused`),
			true,
		},
		{
			// Some other webhook timing out — must NOT match (the
			// reconciler doesn't know how to recover, so we want the
			// usual error path with stack trace).
			"other webhook failure",
			errors.New(`Internal error occurred: failed calling webhook "kyverno.policy.k8s.io": connection refused`),
			false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWebhookNotReadyErr(tc.err); got != tc.want {
				t.Errorf("isWebhookNotReadyErr(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsCertManagerCRDMissingErr(t *testing.T) {
	// noMatchErr models what the RESTMapper returns when it has no
	// record of the GVK (operator started without CRDs ever present,
	// or mapper was invalidated).
	noMatchErr := &meta.NoKindMatchError{
		GroupKind:        schema.GroupKind{Group: "cert-manager.io", Kind: "ClusterIssuer"},
		SearchedVersions: []string{"v1"},
	}

	// notFound404 models the apiserver's response when the mapper
	// has cached the GVK from before deletion but the URL handler is
	// gone — discovered during envtest verification of this code
	// path: `kubectl delete crd` followed by a request to the same
	// kind returns this shape, not a NoMatchError.
	notFound404 := &apierrors.StatusError{
		ErrStatus: metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    404,
			Reason:  metav1.StatusReasonNotFound,
			Message: "the server could not find the requested resource (post clusterissuers.cert-manager.io)",
			Details: &metav1.StatusDetails{
				Group: "cert-manager.io",
				Kind:  "clusterissuers",
				Causes: []metav1.StatusCause{
					{
						Type:    metav1.CauseTypeUnexpectedServerResponse,
						Message: "404 page not found",
					},
				},
			},
		},
	}

	// genericObjectNotFound models a routine "Secret foo not found"
	// — same code (404) and Reason (NotFound), but Details.Group is
	// not cert-manager and there's no UnexpectedServerResponse cause.
	// Must NOT match — that's not a CRD-missing error.
	genericObjectNotFound := &apierrors.StatusError{
		ErrStatus: metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    404,
			Reason:  metav1.StatusReasonNotFound,
			Message: `secrets "foo" not found`,
			Details: &metav1.StatusDetails{
				Group: "",
				Kind:  "secrets",
				Name:  "foo",
			},
		},
	}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("connection refused"), false},
		{"object-not-found is not CRD-missing", genericObjectNotFound, false},
		{"NoKindMatchError (mapper-side)", noMatchErr, true},
		{"NoKindMatchError wrapped in fmt.Errorf", fmt.Errorf("get ClusterIssuer: %w", noMatchErr), true},
		{"404 with UnexpectedServerResponse cause", notFound404, true},
		{"404 ... wrapped in fmt.Errorf", fmt.Errorf("Controller.Watch(ClusterIssuer): %w", notFound404), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCertManagerCRDMissingErr(tc.err); got != tc.want {
				t.Errorf("isCertManagerCRDMissingErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
