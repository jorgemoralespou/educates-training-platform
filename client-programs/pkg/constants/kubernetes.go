package constants

import "sort"

// KubernetesVersionToKindImage maps a supported Kubernetes minor version to
// the kind node image (with digest) published for the kind release this CLI
// vendors (sigs.k8s.io/kind v0.32.0). The supported set is kept in sync with
// the kubectl versions shipped in the workshop base environment and with
// go.mod's k8s.io/* — see the educates-upgrade-kubernetes protocol.
var KubernetesVersionToKindImage = map[string]string{
	"1.36": "kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5",
	"1.35": "kindest/node:v1.35.5@sha256:ce977ae6d65918d0b58a5f8b5e940429c2ce42fa3a5619ec2bbc60b949c0ac95",
	"1.34": "kindest/node:v1.34.8@sha256:02722c2dedddcfc00febf5d27fbeb9b7b2c14294c82109ff4a85d89ac9ba3256",
	"1.33": "kindest/node:v1.33.12@sha256:3f5c8443c620245e4d355cfe09e96a91ead32ceaa569d3f1ca9edf0cb2fe2ff4",
}

// DefaultKubernetesVersion is the version 'local cluster create' uses when
// --kubernetes-version is not given.
const DefaultKubernetesVersion = "1.36"

// SupportedKubernetesVersions returns the supported Kubernetes minor
// versions, sorted, for use in flag help and validation errors.
func SupportedKubernetesVersions() []string {
	versions := make([]string, 0, len(KubernetesVersionToKindImage))
	for v := range KubernetesVersionToKindImage {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions
}
