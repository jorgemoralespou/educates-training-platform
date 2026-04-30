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

{{/*
Derive ingress protocol. `clusterIngress.protocol` wins when set; otherwise
"https" if a tlsCertificateRef.name is provided, "http" otherwise. Mirrors
the runtime's own derivation in operator_config.py.
*/}}
{{- define "session-manager.derivedProtocol" -}}
{{- $ci := .Values.clusterIngress -}}
{{- $tlsRef := default dict $ci.tlsCertificateRef -}}
{{- if $ci.protocol -}}
{{- $ci.protocol -}}
{{- else if $tlsRef.name -}}
https
{{- else -}}
http
{{- end -}}
{{- end -}}

{{/*
Build the YAML stream for the `kyverno-policies.yaml` key of the
`educates-config` Secret. session-manager reads the result via
`yaml.load_all` and clones each rule per workshop environment.
Concatenates:
  1. Bundled workshop policies under files/kyverno-policies/workshop-policies/
     (when workshopSecurity.rulesEngine == "Kyverno").
  2. User-supplied ClusterPolicy objects from
     workshopSecurity.additionalKyvernoPolicies (also gated on Kyverno).
Each document is separated by `---\n`.
*/}}
{{- define "session-manager.kyvernoPoliciesContent" -}}
{{- $ws := .Values.workshopSecurity -}}
{{- $output := "" -}}
{{- if eq $ws.rulesEngine "Kyverno" -}}
  {{- range $path, $_ := .Files.Glob "files/kyverno-policies/workshop-policies/*.yaml" -}}
    {{- $content := $.Files.Get $path | trim -}}
    {{- $output = printf "%s---\n%s\n" $output $content -}}
  {{- end -}}
  {{- range default list $ws.additionalKyvernoPolicies -}}
    {{- $content := toYaml . | trim -}}
    {{- $output = printf "%s---\n%s\n" $output $content -}}
  {{- end -}}
{{- end -}}
{{- $output -}}
{{- end -}}

{{/*
Default `imageVersions` set the chart ships, mirroring v3's
carvel-installer `images.yaml`. Two kinds of entries:

  - Educates-published images. Tag derived from `.Chart.AppVersion` so a
    chart release that bumps the runtime moves these in lock-step.
  - Upstream pins (docker-in-docker, loftsh-*, debian-base) that aren't
    Educates-published. Hard-coded to specific upstream tags.

User overrides in `.Values.imageVersions` are merged in BY NAME — each
user entry replaces the default with the same `name`, but other defaults
pass through untouched. Names not in this default list are appended,
which preserves forward-compat with new runtime images.

Returns the merged list as a YAML array string (consume via fromYamlArray).
*/}}
{{- define "session-manager.imageVersions" -}}
{{- $repo := "ghcr.io/educates" -}}
{{- $v := .Chart.AppVersion -}}
{{- $defaults := list
    (dict "name" "training-portal"         "image" (printf "%s/educates-training-portal:%s" $repo $v))
    (dict "name" "docker-registry"         "image" (printf "%s/educates-docker-registry:%s" $repo $v))
    (dict "name" "tunnel-manager"          "image" (printf "%s/educates-tunnel-manager:%s" $repo $v))
    (dict "name" "image-cache"             "image" (printf "%s/educates-image-cache:%s" $repo $v))
    (dict "name" "assets-server"           "image" (printf "%s/educates-assets-server:%s" $repo $v))
    (dict "name" "contour-bundle"          "image" (printf "%s/educates-contour-bundle:%s" $repo $v))
    (dict "name" "base-environment"        "image" (printf "%s/educates-base-environment:%s" $repo $v))
    (dict "name" "jdk8-environment"        "image" (printf "%s/educates-jdk8-environment:%s" $repo $v))
    (dict "name" "jdk11-environment"       "image" (printf "%s/educates-jdk11-environment:%s" $repo $v))
    (dict "name" "jdk17-environment"       "image" (printf "%s/educates-jdk17-environment:%s" $repo $v))
    (dict "name" "jdk21-environment"       "image" (printf "%s/educates-jdk21-environment:%s" $repo $v))
    (dict "name" "conda-environment"       "image" (printf "%s/educates-conda-environment:%s" $repo $v))
    (dict "name" "debian-base-image"       "image" "debian:sid-20230502-slim")
    (dict "name" "docker-in-docker"        "image" "docker:27.5.1-dind")
    (dict "name" "loftsh-kubernetes-v1.31" "image" "ghcr.io/loft-sh/kubernetes:v1.31.1")
    (dict "name" "loftsh-kubernetes-v1.32" "image" "ghcr.io/loft-sh/kubernetes:v1.32.1")
    (dict "name" "loftsh-kubernetes-v1.33" "image" "ghcr.io/loft-sh/kubernetes:v1.33.4")
    (dict "name" "loftsh-kubernetes-v1.34" "image" "ghcr.io/loft-sh/kubernetes:v1.34.0")
    (dict "name" "loftsh-vcluster"         "image" "ghcr.io/loft-sh/vcluster-oss:0.30.2")
-}}
{{- $overrides := dict -}}
{{- range default list .Values.imageVersions -}}
  {{- $_ := set $overrides .name .image -}}
{{- end -}}
{{- $merged := list -}}
{{- $defaultNames := dict -}}
{{- range $defaults -}}
  {{- $name := .name -}}
  {{- $image := .image -}}
  {{- if hasKey $overrides $name -}}
    {{- $image = index $overrides $name -}}
  {{- end -}}
  {{- $merged = append $merged (dict "name" $name "image" $image) -}}
  {{- $_ := set $defaultNames $name true -}}
{{- end -}}
{{- range default list .Values.imageVersions -}}
  {{- if not (hasKey $defaultNames .name) -}}
    {{- $merged = append $merged (dict "name" .name "image" .image) -}}
  {{- end -}}
{{- end -}}
{{- toYaml $merged -}}
{{- end -}}

{{/*
Compose the `educates-operator-config.yaml` Secret content from typed values.
Auto-injects `operator.namespace` (release ns) and `version` (.Chart.AppVersion
— the bundled Educates runtime version, distinct from the chart-package
`Chart.Version`). Lowercases policy/rules engine names to match the runtime's
expected casing. Materialises empty-string TLS/CA refs explicitly — the runtime
reads these via xget() with no default, so absent keys become Python None and
crash later when encoded as strings (see project_runtime_config_quirks memory).
Deep-merges .Values.config on top so the escape hatch wins on conflict.
*/}}
{{- define "session-manager.operatorConfigYAML" -}}
{{- $ci := .Values.clusterIngress -}}
{{- if not $ci.domain -}}
{{- fail "session-manager.clusterIngress.domain is required" -}}
{{- end -}}
{{- $tlsRef := default dict $ci.tlsCertificateRef -}}
{{- $caRef := default dict $ci.caCertificateRef -}}
{{- $cs := .Values.clusterSecurity -}}
{{- $ws := .Values.workshopSecurity -}}
{{- $ir := default dict .Values.imageRegistry -}}
{{- $tp := default dict .Values.trainingPortal -}}
{{- $sc := default dict .Values.sessionCookies -}}
{{- $cstg := default dict .Values.clusterStorage -}}
{{- $crt := default dict .Values.clusterRuntime -}}
{{- $cnet := default dict .Values.clusterNetwork -}}
{{- $dd := default dict .Values.dockerDaemon -}}
{{- $proxy := default dict $dd.proxyCache -}}
{{- $wa := default dict .Values.workshopAnalytics -}}
{{- $wstyle := default dict .Values.websiteStyling -}}
{{- $typed := dict
  "operator" (dict "namespace" .Release.Namespace)
  "version" .Chart.AppVersion
  "clusterIngress" (dict
    "domain" $ci.domain
    "class" (default "" $ci.class)
    "protocol" (include "session-manager.derivedProtocol" .)
    "tlsCertificateRef" (dict
      "name" (default "" $tlsRef.name)
      "namespace" (default "" $tlsRef.namespace)
    )
    "caCertificateRef" (dict
      "name" (default "" $caRef.name)
      "namespace" (default "" $caRef.namespace)
    )
  )
  "clusterSecurity" (dict "policyEngine" (lower $cs.policyEngine))
  "workshopSecurity" (dict "rulesEngine" (lower $ws.rulesEngine))
  "imageRegistry" (dict
    "host" (default "" $ir.host)
    "namespace" (default "" $ir.namespace)
  )
  "imageVersions" (include "session-manager.imageVersions" . | fromYamlArray)
  "trainingPortal" (dict
    "credentials" (dict
      "admin" (dict
        "username" (default "" (dig "credentials" "admin" "username" "" $tp))
        "password" (default "" (dig "credentials" "admin" "password" "" $tp))
      )
      "robot" (dict
        "username" (default "" (dig "credentials" "robot" "username" "" $tp))
        "password" (default "" (dig "credentials" "robot" "password" "" $tp))
      )
    )
    "clients" (dict
      "robot" (dict
        "id" (default "" (dig "clients" "robot" "id" "" $tp))
        "secret" (default "" (dig "clients" "robot" "secret" "" $tp))
      )
    )
  )
  "sessionCookies" (dict "domain" (default "" $sc.domain))
  "clusterStorage" (dict
    "class" (default "" $cstg.class)
    "user" $cstg.user
    "group" (default 1 $cstg.group)
  )
  "clusterRuntime" (dict "class" (default "" $crt.class))
  "clusterNetwork" (dict "blockCIDRs" (default list $cnet.blockCIDRs))
  "dockerDaemon" (dict
    "networkMTU" (default 1400 $dd.networkMTU)
    "proxyCache" (dict
      "remoteURL" (default "" $proxy.remoteURL)
      "username" (default "" $proxy.username)
      "password" (default "" $proxy.password)
    )
  )
  "workshopAnalytics" (dict
    "google" (dict "trackingId" (default "" (dig "google" "trackingId" "" $wa)))
    "clarity" (dict "trackingId" (default "" (dig "clarity" "trackingId" "" $wa)))
    "amplitude" (dict "trackingId" (default "" (dig "amplitude" "trackingId" "" $wa)))
    "webhook" (dict "url" (default "" (dig "webhook" "url" "" $wa)))
  )
  "websiteStyling" (dict
    "defaultTheme" (default "" $wstyle.defaultTheme)
    "frameAncestors" (default list $wstyle.frameAncestors)
  )
-}}
{{- $merged := mergeOverwrite $typed (deepCopy (default dict .Values.config)) -}}
{{- toYaml $merged -}}
{{- end -}}

{{/*
Map the structured `websiteStyling.inline` values to the flat secret-key shape
the runtime expects in the `default-website-theme` Secret. Returns a YAML map
of stringData entries (or empty if no inline assets are populated).

  workshopDashboard.{html,script,style}    -> workshop-dashboard.{html,js,css}
  workshopInstructions.{html,script,style} -> workshop-instructions.{html,js,css}
  workshopStarted.html                     -> workshop-started.html
  workshopFinished.html                    -> workshop-finished.html
  trainingPortal.{html,script,style}       -> training-portal.{html,js,css}
*/}}
{{- define "session-manager.inlineThemeStringData" -}}
{{- $inline := default dict (default dict .Values.websiteStyling).inline -}}
{{- $entries := dict -}}
{{- $tripleSources := list
    (list "workshopDashboard" "workshop-dashboard")
    (list "workshopInstructions" "workshop-instructions")
    (list "trainingPortal" "training-portal")
-}}
{{- range $tripleSources -}}
  {{- $key := index . 0 -}}
  {{- $prefix := index . 1 -}}
  {{- $block := default dict (index $inline $key) -}}
  {{- if $block.html }}{{- $_ := set $entries (printf "%s.html" $prefix) $block.html -}}{{- end -}}
  {{- if $block.script }}{{- $_ := set $entries (printf "%s.js" $prefix) $block.script -}}{{- end -}}
  {{- if $block.style }}{{- $_ := set $entries (printf "%s.css" $prefix) $block.style -}}{{- end -}}
{{- end -}}
{{- $started := default dict $inline.workshopStarted -}}
{{- if $started.html }}{{- $_ := set $entries "workshop-started.html" $started.html -}}{{- end -}}
{{- $finished := default dict $inline.workshopFinished -}}
{{- if $finished.html }}{{- $_ := set $entries "workshop-finished.html" $finished.html -}}{{- end -}}
{{- toYaml $entries -}}
{{- end -}}
