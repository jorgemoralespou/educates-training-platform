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
	"testing"
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
