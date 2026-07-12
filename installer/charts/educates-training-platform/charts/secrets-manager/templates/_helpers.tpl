{{/*
Common labels applied to all resources rendered by this subchart.
*/}}
{{- define "secrets-manager.labels" -}}
app.kubernetes.io/name: secrets-manager
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: secrets-manager
app.kubernetes.io/part-of: educates
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Selector labels — stable across upgrades, must not include the chart version.
*/}}
{{- define "secrets-manager.selectorLabels" -}}
deployment: secrets-manager
{{- end -}}

{{/*
Resolve cross-cutting blocks (imageRegistry, clusterSecurity) by deep-merging
the umbrella's `global.<key>` over this subchart's local block. Globals win
where set; subchart-local defaults pass through otherwise. Returned as a
YAML string — consume via `fromYaml`.
*/}}
{{- define "secrets-manager.resolvedImageRegistry" -}}
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

{{- define "secrets-manager.resolvedClusterSecurity" -}}
{{- $local := default dict .Values.clusterSecurity -}}
{{- $global := default dict (default dict .Values.global).clusterSecurity -}}
{{- toYaml (mergeOverwrite (deepCopy $local) $global) -}}
{{- end -}}

{{- define "secrets-manager.imageRegistryPrefix" -}}
{{- $ir := include "secrets-manager.resolvedImageRegistry" . | fromYaml -}}
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

{{- define "secrets-manager.image.repository" -}}
{{- if .Values.image.repository -}}
{{ .Values.image.repository }}
{{- else -}}
{{ include "secrets-manager.imageRegistryPrefix" . }}/educates-secrets-manager
{{- end -}}
{{- end -}}

{{/*
Resolve the container image tag, defaulting to .Chart.AppVersion when unset.
*/}}
{{- define "secrets-manager.image.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{/*
Auto-derive imagePullPolicy: Always for floating tags, IfNotPresent otherwise.
An explicit pullPolicy in values wins.
*/}}
{{- define "secrets-manager.image.pullPolicy" -}}
{{- if .Values.image.pullPolicy -}}
{{ .Values.image.pullPolicy }}
{{- else -}}
{{- $tag := include "secrets-manager.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}
