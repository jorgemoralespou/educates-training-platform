{{/*
Common labels applied to all resources rendered by this chart.
*/}}
{{- define "educates-installer.labels" -}}
app.kubernetes.io/name: educates-installer
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
app.kubernetes.io/part-of: educates
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Selector labels — stable across upgrades; must not include the chart
version.
*/}}
{{- define "educates-installer.selectorLabels" -}}
app.kubernetes.io/name: educates-installer
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Resolve the effective image registry: development.imageRegistry (user
override) → global.development.imageRegistry (uniformity with the
runtime subcharts; this chart is standalone so it's normally unset) →
Chart.yaml `educates.dev/image-registry-*` annotations (publish-time
defaults). Returned as a YAML string — consume via `fromYaml`.
*/}}
{{- define "educates-installer.resolvedImageRegistry" -}}
{{- $local := default dict (default dict .Values.development).imageRegistry -}}
{{- $global := default dict (default dict (default dict .Values.global).development).imageRegistry -}}
{{- $merged := mergeOverwrite (deepCopy $local) $global -}}
{{- if not $merged.host -}}
  {{- $_ := set $merged "host" (index .Chart.Annotations "educates.dev/image-registry-host" | default "") -}}
{{- end -}}
{{- if not $merged.namespace -}}
  {{- $_ := set $merged "namespace" (index .Chart.Annotations "educates.dev/image-registry-namespace" | default "") -}}
{{- end -}}
{{- toYaml $merged -}}
{{- end -}}

{{- define "educates-installer.imageRegistryPrefix" -}}
{{- $ir := include "educates-installer.resolvedImageRegistry" . | fromYaml -}}
{{- $host := default "" $ir.host -}}
{{- $ns := default "" $ir.namespace -}}
{{- if and $host $ns -}}
{{ $host }}/{{ $ns }}
{{- else if $host -}}
{{ $host }}
{{- else -}}
{{- fail "imageRegistry.host could not be resolved. Either set Chart.yaml annotation `educates.dev/image-registry-host` (publish-time default) or override locally via .development.imageRegistry / .global.development.imageRegistry." -}}
{{- end -}}
{{- end -}}

{{- define "educates-installer.image.repository" -}}
{{- if .Values.image.repository -}}
{{ .Values.image.repository }}
{{- else -}}
{{ include "educates-installer.imageRegistryPrefix" . }}/educates-operator
{{- end -}}
{{- end -}}

{{/*
Resolve the container image tag, defaulting to .Chart.AppVersion when unset.
*/}}
{{- define "educates-installer.image.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{/*
Auto-derive imagePullPolicy: Always for floating tags, IfNotPresent
otherwise. An explicit pullPolicy in values wins.
*/}}
{{- define "educates-installer.image.pullPolicy" -}}
{{- if .Values.image.pullPolicy -}}
{{ .Values.image.pullPolicy }}
{{- else -}}
{{- $tag := include "educates-installer.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}
