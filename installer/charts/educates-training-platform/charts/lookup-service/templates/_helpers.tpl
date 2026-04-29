{{- define "lookup-service.labels" -}}
app.kubernetes.io/name: lookup-service
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: lookup-service
app.kubernetes.io/part-of: educates
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "lookup-service.selectorLabels" -}}
app: lookup-service
{{- end -}}

{{- define "lookup-service.image.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{- define "lookup-service.image.pullPolicy" -}}
{{- if .Values.image.pullPolicy -}}
{{ .Values.image.pullPolicy }}
{{- else -}}
{{- $tag := include "lookup-service.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "lookup-service.caTrust.image.tag" -}}
{{- default .Chart.AppVersion .Values.caTrust.initImage.tag -}}
{{- end -}}

{{- define "lookup-service.caTrust.image.pullPolicy" -}}
{{- if .Values.caTrust.initImage.pullPolicy -}}
{{ .Values.caTrust.initImage.pullPolicy }}
{{- else -}}
{{- $tag := include "lookup-service.caTrust.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}
