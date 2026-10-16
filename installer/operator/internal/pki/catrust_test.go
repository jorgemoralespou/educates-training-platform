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

package pki_test

import (
	"bytes"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
	"github.com/educates/educates-training-platform/installer/operator/internal/testpki"
)

func secretWith(data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "educates-secrets", Name: "source"},
		Data:       data,
	}
}

func concat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

func TestCACertificate_DerivesTheCATrust(t *testing.T) {
	root := testpki.NewRootCA()
	inter := root.NewIntermediateCA()
	otherRoot := testpki.NewRootCA()

	cases := []struct {
		name string
		rule pki.CASourceRule
		data map[string][]byte
		want []byte
	}{
		{
			name: "key pair of a self-signed root without ca.crt yields the root",
			rule: pki.FromCAKeyPair,
			data: map[string][]byte{"tls.crt": root.CertPEM, "tls.key": root.KeyPEM},
			want: root.CertPEM,
		},
		{
			name: "key pair of an intermediate with its chain yields the root of the chain",
			rule: pki.FromCAKeyPair,
			data: map[string][]byte{"tls.crt": inter.ChainPEM(), "tls.key": inter.KeyPEM},
			want: root.CertPEM,
		},
		{
			name: "key pair of an intermediate alone yields the intermediate",
			rule: pki.FromCAKeyPair,
			data: map[string][]byte{"tls.crt": inter.CertPEM, "tls.key": inter.KeyPEM},
			want: inter.CertPEM,
		},
		{
			name: "key pair with ca.crt yields its ca.crt",
			rule: pki.FromCAKeyPair,
			data: map[string][]byte{"tls.crt": inter.CertPEM, "tls.key": inter.KeyPEM, "ca.crt": root.CertPEM},
			want: root.CertPEM,
		},
		{
			name: "CA trust Secret yields its ca.crt and ignores a tls.key beside it",
			rule: pki.FromCATrustSecret,
			data: map[string][]byte{"ca.crt": root.CertPEM, "tls.crt": root.CertPEM, "tls.key": root.KeyPEM},
			want: root.CertPEM,
		},
		{
			name: "CA trust Secret keeps every certificate of a bundle",
			rule: pki.FromCATrustSecret,
			data: map[string][]byte{"ca.crt": concat(root.CertPEM, otherRoot.CertPEM)},
			want: concat(root.CertPEM, otherRoot.CertPEM),
		},
		{
			name: "a private key inside ca.crt never reaches the result",
			rule: pki.FromCATrustSecret,
			data: map[string][]byte{"ca.crt": concat(root.CertPEM, root.KeyPEM)},
			want: root.CertPEM,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pki.CACertificate(secretWith(tc.data), tc.rule)
			if err != nil {
				t.Fatalf("CACertificate: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("CACertificate returned\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestCACertificate_RejectsUnusableSources(t *testing.T) {
	root := testpki.NewRootCA()
	leaf := root.NewLeaf(testpki.WithDNSNames("*.example.test"))

	cases := []struct {
		name       string
		rule       pki.CASourceRule
		data       map[string][]byte
		wantReason string
	}{
		{
			name:       "key pair without tls.key",
			rule:       pki.FromCAKeyPair,
			data:       map[string][]byte{"tls.crt": root.CertPEM},
			wantReason: pki.ReasonCAKeyPairIncomplete,
		},
		{
			name:       "key pair without tls.crt",
			rule:       pki.FromCAKeyPair,
			data:       map[string][]byte{"tls.key": root.KeyPEM},
			wantReason: pki.ReasonCAKeyPairIncomplete,
		},
		{
			name:       "key pair whose certificate is not a CA",
			rule:       pki.FromCAKeyPair,
			data:       map[string][]byte{"tls.crt": leaf.ChainPEM(), "tls.key": leaf.KeyPEM},
			wantReason: pki.ReasonCAKeyPairNotCA,
		},
		{
			name:       "key pair whose tls.crt is not a certificate",
			rule:       pki.FromCAKeyPair,
			data:       map[string][]byte{"tls.crt": []byte("not a certificate"), "tls.key": root.KeyPEM},
			wantReason: pki.ReasonCAKeyPairUnparseable,
		},
		{
			name:       "key pair whose ca.crt is not a certificate",
			rule:       pki.FromCAKeyPair,
			data:       map[string][]byte{"tls.crt": root.CertPEM, "tls.key": root.KeyPEM, "ca.crt": []byte("junk")},
			wantReason: pki.ReasonCAKeyPairUnparseable,
		},
		{
			name:       "CA trust Secret without ca.crt",
			rule:       pki.FromCATrustSecret,
			data:       map[string][]byte{"tls.crt": root.CertPEM},
			wantReason: pki.ReasonCATrustSecretIncomplete,
		},
		{
			name:       "CA trust Secret whose ca.crt holds only a key",
			rule:       pki.FromCATrustSecret,
			data:       map[string][]byte{"ca.crt": root.KeyPEM},
			wantReason: pki.ReasonCATrustSecretUnparseable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pki.CACertificate(secretWith(tc.data), tc.rule)
			var serr *pki.CertificateSecretError
			if !errors.As(err, &serr) {
				t.Fatalf("CACertificate error = %v, want a *pki.CertificateSecretError", err)
			}
			if serr.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q (message %q)", serr.Reason, tc.wantReason, serr.Message)
			}
		})
	}
}
