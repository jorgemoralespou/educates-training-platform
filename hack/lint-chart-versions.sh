#!/usr/bin/env bash
# Lint: the v4 charts stay version- and annotation-synchronized.
#
# The release pipeline (hack/stamp-release-version.sh) rewrites versions
# and registry annotations across all charts with blanket substitutions,
# which is only safe while the committed tree is uniform. This lint
# fails CI when any chart drifts:
#
#   - educates-installer, the educates-training-platform umbrella and
#     its five subcharts must share one `version`, and the umbrella's
#     dependencies[].version pins must match it.
#   - The image-rendering charts (educates-installer + the four
#     annotated subcharts) must carry identical
#     educates.dev/image-registry-{host,namespace} annotations.
#   - The runtime charts (umbrella + five subcharts) must share one
#     appVersion; educates-installer's appVersion must equal its
#     version.
#   - vendored-charts/embed.go must reference each local subchart
#     tarball at the committed version, with a matching ChartVersion
#     constant and the tarball present on disk.
#
# Requires yq (https://github.com/mikefarah/yq).
set -euo pipefail
cd "$(dirname "$0")/.."

INSTALLER_CHART=installer/charts/educates-installer
UMBRELLA_CHART=installer/charts/educates-training-platform
LOCAL_SUBCHARTS="secrets-manager lookup-service session-manager node-ca-injector remote-access"
ANNOTATED_SUBCHARTS="secrets-manager lookup-service session-manager node-ca-injector"
VENDORED_CHARTS_DIR=installer/operator/vendored-charts
EMBED_GO=$VENDORED_CHARTS_DIR/embed.go

fail=0
err() {
    echo "ERROR: $*" >&2
    fail=1
}

chart_version_const() {
    case "$1" in
        secrets-manager)  echo SecretsManagerChartVersion ;;
        lookup-service)   echo LookupServiceChartVersion ;;
        session-manager)  echo SessionManagerChartVersion ;;
        node-ca-injector) echo NodeCAInjectorChartVersion ;;
        remote-access)    echo RemoteAccessChartVersion ;;
    esac
}

ref_version=$(yq '.version' "$INSTALLER_CHART/Chart.yaml")
ref_host=$(yq '.annotations["educates.dev/image-registry-host"]' "$INSTALLER_CHART/Chart.yaml")
ref_namespace=$(yq '.annotations["educates.dev/image-registry-namespace"]' "$INSTALLER_CHART/Chart.yaml")

[ "$ref_host" != "null" ] || err "$INSTALLER_CHART/Chart.yaml is missing the educates.dev/image-registry-host annotation"
[ "$ref_namespace" != "null" ] || err "$INSTALLER_CHART/Chart.yaml is missing the educates.dev/image-registry-namespace annotation"

installer_app_version=$(yq '.appVersion' "$INSTALLER_CHART/Chart.yaml")
[ "$installer_app_version" = "$ref_version" ] ||
    err "$INSTALLER_CHART/Chart.yaml appVersion ($installer_app_version) != version ($ref_version)"

umbrella_version=$(yq '.version' "$UMBRELLA_CHART/Chart.yaml")
[ "$umbrella_version" = "$ref_version" ] ||
    err "$UMBRELLA_CHART/Chart.yaml version ($umbrella_version) != $INSTALLER_CHART version ($ref_version)"

while IFS=$'\t' read -r dep_name dep_version; do
    [ "$dep_version" = "$ref_version" ] ||
        err "$UMBRELLA_CHART/Chart.yaml dependency $dep_name pins version $dep_version, expected $ref_version"
done < <(yq '.dependencies[] | [.name, .version] | @tsv' "$UMBRELLA_CHART/Chart.yaml")

ref_app_version=$(yq '.appVersion' "$UMBRELLA_CHART/Chart.yaml")

for name in $LOCAL_SUBCHARTS; do
    chart_yaml=$UMBRELLA_CHART/charts/$name/Chart.yaml

    version=$(yq '.version' "$chart_yaml")
    [ "$version" = "$ref_version" ] ||
        err "$chart_yaml version ($version) != $ref_version"

    app_version=$(yq '.appVersion' "$chart_yaml")
    [ "$app_version" = "$ref_app_version" ] ||
        err "$chart_yaml appVersion ($app_version) != umbrella appVersion ($ref_app_version)"

    # embed.go must track the committed subchart version exactly.
    grep -q "^//go:embed $name-$version.tgz\$" "$EMBED_GO" ||
        err "$EMBED_GO does not //go:embed $name-$version.tgz (run 'make package-local-charts' in installer/operator and update embed.go)"
    const=$(chart_version_const "$name")
    grep -q "^const $const = \"$version\"\$" "$EMBED_GO" ||
        err "$EMBED_GO constant $const != \"$version\""
    [ -f "$VENDORED_CHARTS_DIR/$name-$version.tgz" ] ||
        err "$VENDORED_CHARTS_DIR/$name-$version.tgz is missing (run 'make package-local-charts' in installer/operator)"
done

for name in $ANNOTATED_SUBCHARTS; do
    chart_yaml=$UMBRELLA_CHART/charts/$name/Chart.yaml
    host=$(yq '.annotations["educates.dev/image-registry-host"]' "$chart_yaml")
    namespace=$(yq '.annotations["educates.dev/image-registry-namespace"]' "$chart_yaml")
    [ "$host" = "$ref_host" ] ||
        err "$chart_yaml educates.dev/image-registry-host ($host) != $INSTALLER_CHART's ($ref_host)"
    [ "$namespace" = "$ref_namespace" ] ||
        err "$chart_yaml educates.dev/image-registry-namespace ($namespace) != $INSTALLER_CHART's ($ref_namespace)"
done

if [ "$fail" -ne 0 ]; then
    echo "chart version/annotation sync lint failed" >&2
    exit 1
fi
echo "charts in sync: version $ref_version, runtime appVersion $ref_app_version, registry $ref_host/$ref_namespace"
