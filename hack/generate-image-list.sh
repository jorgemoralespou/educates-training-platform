#!/usr/bin/env bash
# Generate the digest-pinned platform image list for a release.
#
#   hack/generate-image-list.sh <version> <registry-host> <registry-namespace> [--no-digests]
#
# Writes one fully qualified image reference per line to stdout
# (`<repo>:<tag>@sha256:<digest>`), covering everything an air-gapped
# install of the platform needs (see decisions.md "Image relocation is
# a published digest-pinned list"):
#
#   - the Educates platform images at the released version (operator,
#     runtime components, pause-container, docker-registry, ...) plus
#     the workshop base-environment image, composed from the given
#     registry host/namespace;
#   - the upstream cluster-service images (cert-manager, Contour,
#     external-dns, Kyverno), extracted by rendering the vendored
#     chart tarballs with default values. Defaults are a superset of
#     what the operator enables, so this over-collects slightly rather
#     than ever missing an image.
#
# Workshop environment images beyond base-environment (jdk*, conda)
# are deliberately excluded — they add many GB per release. Air-gap
# users append them to the list as needed.
#
# Digest resolution uses skopeo (preinstalled on GitHub runners) and
# requires the images to already be published — the release workflow
# runs this after the image-publish jobs. Use --no-digests for a local
# dry run without registry access.
set -euo pipefail

usage() {
    echo "usage: $0 <version> <registry-host> <registry-namespace> [--no-digests]" >&2
    exit 1
}

VERSION="${1:-}"
REGISTRY_HOST="${2:-}"
REGISTRY_NAMESPACE="${3:-}"
[ -n "$VERSION" ] && [ -n "$REGISTRY_HOST" ] && [ -n "$REGISTRY_NAMESPACE" ] || usage
RESOLVE_DIGESTS=true
if [ "${4:-}" = "--no-digests" ]; then
    RESOLVE_DIGESTS=false
elif [ -n "${4:-}" ]; then
    usage
fi

cd "$(dirname "$0")/.."

VENDORED_CHARTS_DIR=installer/operator/vendored-charts

# Educates-built platform images: the publish-generic-images matrix
# (with the operator's published name) plus the workshop
# base-environment. educates-cli and the docker extension are client
# tools, not platform images, and stay out.
PLATFORM_IMAGES="
docker-registry
pause-container
session-manager
training-portal
secrets-manager
tunnel-manager
image-cache
assets-server
lookup-service
node-ca-injector
operator
base-environment
"

# Upstream cluster-service charts the operator installs in Managed
# mode. Globs resolve the single vendored tarball per chart so version
# bumps don't touch this script.
UPSTREAM_CHART_GLOBS="
cert-manager-*.tgz
contour-*.tgz
external-dns-*.tgz
kyverno-*.tgz
"

emit() {
    local ref="$1"
    if $RESOLVE_DIGESTS; then
        local digest
        # --override-os keeps darwin hosts working; .Digest is the
        # multi-arch index digest (verified equal to the raw manifest
        # sha256), which is what a relocation copy should pin.
        digest=$(skopeo inspect --override-os linux --format '{{.Digest}}' "docker://$ref") || {
            echo "failed to resolve digest for $ref" >&2
            exit 1
        }
        echo "$ref@$digest"
    else
        echo "$ref"
    fi
}

for name in $PLATFORM_IMAGES; do
    emit "$REGISTRY_HOST/$REGISTRY_NAMESPACE/educates-$name:$VERSION"
done

# Image references appear in rendered manifests as `image:` fields and,
# for cert-manager's acmesolver, as a `--*-image=` controller argument.
extract_chart_images() {
    local tarball="$1"
    helm template image-list-probe "$tarball" 2>/dev/null |
        grep -ohE '(image: *"?[^"[:space:]]+"?|--[a-z0-9-]*image=[^"[:space:]]+)' |
        sed -E 's/^image: *"?//; s/"$//; s/^--[a-z0-9-]*image=//'
}

upstream_refs=""
for glob in $UPSTREAM_CHART_GLOBS; do
    # shellcheck disable=SC2086 -- glob expansion is the point
    set -- $VENDORED_CHARTS_DIR/$glob
    [ -f "$1" ] || {
        echo "no vendored chart matching $glob in $VENDORED_CHARTS_DIR" >&2
        exit 1
    }
    upstream_refs+="$(extract_chart_images "$1")"$'\n'
done

while read -r ref; do
    [ -n "$ref" ] || continue
    emit "$ref"
done < <(echo "$upstream_refs" | sort -u)
