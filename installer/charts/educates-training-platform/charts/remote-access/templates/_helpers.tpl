{{- define "remote-access.labels" -}}
app.kubernetes.io/name: remote-access
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: remote-access
app.kubernetes.io/part-of: educates
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}
