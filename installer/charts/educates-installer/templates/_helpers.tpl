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
