{{- define "node-ca-injector.labels" -}}
app.kubernetes.io/name: node-ca-injector
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: node-ca-injector
app.kubernetes.io/part-of: educates
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Resolve cross-cutting blocks (imageRegistry, clusterIngress) by deep-merging
the umbrella's `global.<key>` over this subchart's local block. Globals win
where set; subchart-local defaults pass through otherwise. Returned as a
YAML string — consume via `fromYaml`.
*/}}
{{- define "node-ca-injector.resolvedImageRegistry" -}}
{{- $local := default dict .Values.imageRegistry -}}
{{- $global := default dict (default dict .Values.global).imageRegistry -}}
{{- toYaml (mergeOverwrite (deepCopy $local) $global) -}}
{{- end -}}

{{- define "node-ca-injector.resolvedClusterIngress" -}}
{{- $local := default dict .Values.clusterIngress -}}
{{- $global := default dict (default dict .Values.global).clusterIngress -}}
{{- toYaml (mergeOverwrite (deepCopy $local) $global) -}}
{{- end -}}

{{- define "node-ca-injector.imageRegistryPrefix" -}}
{{- $ir := include "node-ca-injector.resolvedImageRegistry" . | fromYaml -}}
{{- $host := default "" $ir.host -}}
{{- $ns := default "" $ir.namespace -}}
{{- if and $host $ns -}}
{{ $host }}/{{ $ns }}
{{- else if $host -}}
{{ $host }}
{{- else -}}
{{- fail "imageRegistry.host is required (set globally under .global.imageRegistry or locally under node-ca-injector.imageRegistry)" -}}
{{- end -}}
{{- end -}}

{{- define "node-ca-injector.image.repository" -}}
{{- if .Values.image.repository -}}
{{ .Values.image.repository }}
{{- else -}}
{{ include "node-ca-injector.imageRegistryPrefix" . }}/educates-node-ca-injector
{{- end -}}
{{- end -}}

{{- define "node-ca-injector.image.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{- define "node-ca-injector.image.pullPolicy" -}}
{{- if .Values.image.pullPolicy -}}
{{ .Values.image.pullPolicy }}
{{- else -}}
{{- $tag := include "node-ca-injector.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Resolve the CA Secret name for the DaemonSet's volume mount. Required —
without a CA ref the DaemonSet has nothing to mount and the chart fails
fast.
*/}}
{{- define "node-ca-injector.caSecretName" -}}
{{- $ci := include "node-ca-injector.resolvedClusterIngress" . | fromYaml -}}
{{- $caRef := default dict $ci.caCertificateRef -}}
{{- if not $caRef.name -}}
{{- fail "node-ca-injector requires clusterIngress.caCertificateRef.name to be set (typically via .global.clusterIngress.caCertificateRef)" -}}
{{- end -}}
{{ $caRef.name }}
{{- end -}}
