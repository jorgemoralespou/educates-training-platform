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

{{/*
Resolve the imageRegistry block by deep-merging the umbrella's
`global.imageRegistry` over the subchart's local `imageRegistry`. Globals
win where set; subchart-local defaults pass through otherwise.
*/}}
{{- define "lookup-service.resolvedImageRegistry" -}}
{{- $local := default dict .Values.imageRegistry -}}
{{- $global := default dict (default dict .Values.global).imageRegistry -}}
{{- toYaml (mergeOverwrite (deepCopy $local) $global) -}}
{{- end -}}

{{- define "lookup-service.imageRegistryPrefix" -}}
{{- $ir := include "lookup-service.resolvedImageRegistry" . | fromYaml -}}
{{- $host := default "" $ir.host -}}
{{- $ns := default "" $ir.namespace -}}
{{- if and $host $ns -}}
{{ $host }}/{{ $ns }}
{{- else if $host -}}
{{ $host }}
{{- else -}}
{{- fail "imageRegistry.host is required (set globally under .global.imageRegistry or locally under lookup-service.imageRegistry)" -}}
{{- end -}}
{{- end -}}

{{- define "lookup-service.image.repository" -}}
{{- if .Values.image.repository -}}
{{ .Values.image.repository }}
{{- else -}}
{{ include "lookup-service.imageRegistryPrefix" . }}/educates-lookup-service
{{- end -}}
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
