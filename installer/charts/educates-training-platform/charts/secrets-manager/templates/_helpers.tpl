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
