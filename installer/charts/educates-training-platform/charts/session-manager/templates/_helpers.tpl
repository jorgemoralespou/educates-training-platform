{{- define "session-manager.labels" -}}
app.kubernetes.io/name: session-manager
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: session-manager
app.kubernetes.io/part-of: educates
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "session-manager.selectorLabels" -}}
deployment: session-manager
{{- end -}}

{{- define "session-manager.image.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{- define "session-manager.image.pullPolicy" -}}
{{- if .Values.image.pullPolicy -}}
{{ .Values.image.pullPolicy }}
{{- else -}}
{{- $tag := include "session-manager.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "session-manager.pause.image.tag" -}}
{{- default .Chart.AppVersion .Values.imagePuller.pauseImage.tag -}}
{{- end -}}

{{- define "session-manager.pause.image.pullPolicy" -}}
{{- if .Values.imagePuller.pauseImage.pullPolicy -}}
{{ .Values.imagePuller.pauseImage.pullPolicy }}
{{- else -}}
{{- $tag := include "session-manager.pause.image.tag" . -}}
{{- if or (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Build the multi-doc YAML stream for the `kyverno-policies.yaml` key of
the `educates-config` Secret. session-manager reads the result via
`yaml.load_all` and clones each rule per workshop environment.
Concatenates:
  1. Bundled workshop policies under files/kyverno-policies/workshop-policies/
     (when bundledKyvernoPolicies.workshopPolicies).
  2. User-supplied ClusterPolicy objects from .Values.kyvernoPolicies.
Each document is separated by `---\n`.
*/}}
{{- define "session-manager.kyvernoPoliciesContent" -}}
{{- $bundle := default dict .Values.bundledKyvernoPolicies -}}
{{- $workshopEnabled := dig "workshopPolicies" true $bundle -}}
{{- $additional := default dict .Values.additionalKyvernoPolicies -}}
{{- $extras := default list (index $additional "workshopPolicies") -}}
{{- $output := "" -}}
{{- if $workshopEnabled -}}
  {{- range $path, $_ := .Files.Glob "files/kyverno-policies/workshop-policies/*.yaml" -}}
    {{- $content := $.Files.Get $path | trim -}}
    {{- $output = printf "%s---\n%s\n" $output $content -}}
  {{- end -}}
{{- end -}}
{{- range $extras -}}
  {{- $content := toYaml . | trim -}}
  {{- $output = printf "%s---\n%s\n" $output $content -}}
{{- end -}}
{{- $output -}}
{{- end -}}

{{/*
Auto-derive imagePullPolicy for an arbitrary fully-qualified image ref
passed in as a string.
*/}}
{{- define "session-manager.derivedPullPolicy" -}}
{{- $parts := splitList ":" . -}}
{{- $tag := "" -}}
{{- if eq (len $parts) 1 -}}
  {{- $tag = "" -}}
{{- else -}}
  {{- $tag = last $parts -}}
{{- end -}}
{{- if or (eq $tag "") (eq $tag "latest") (eq $tag "main") (eq $tag "master") (eq $tag "develop") -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
