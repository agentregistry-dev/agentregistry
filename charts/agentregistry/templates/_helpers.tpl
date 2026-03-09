{{/*
Expand the name of the chart.
*/}}
{{- define "agentregistry.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this
(by the DNS naming spec). If release name contains chart name it will be used
as a full name.
*/}}
{{- define "agentregistry.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "agentregistry.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Standard labels — merges commonLabels when defined.
*/}}
{{- define "agentregistry.labels" -}}
helm.sh/chart: {{ include "agentregistry.chart" . }}
{{ include "agentregistry.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: {{ include "agentregistry.name" . }}
{{- if .Values.commonLabels }}
{{ toYaml .Values.commonLabels }}
{{- end }}
{{- end }}

{{/*
Selector labels — stable subset used in matchLabels.
*/}}
{{- define "agentregistry.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agentregistry.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Annotations — merges commonAnnotations when defined.
Returns empty string when no annotations to emit.
Usage: include "agentregistry.annotations" (dict "annotations" .Values.someAnnotations "context" $)
*/}}
{{- define "agentregistry.annotations" -}}
{{- $custom := .annotations | default dict }}
{{- $common := .context.Values.commonAnnotations | default dict }}
{{- $merged := merge $custom $common }}
{{- if $merged }}
{{- toYaml $merged }}
{{- end }}
{{- end }}

{{/* ======================================================================
   Image helpers
   ====================================================================== */}}

{{/*
Return the proper Agent Registry image name.
Uses global.imageRegistry as override if set.
Digest takes precedence over tag.
*/}}
{{- define "agentregistry.image" -}}
{{- $registry := .Values.image.registry -}}
{{- if .Values.global }}
  {{- if .Values.global.imageRegistry }}
    {{- $registry = .Values.global.imageRegistry -}}
  {{- end }}
{{- end }}
{{- if .Values.image.digest }}
{{- printf "%s/%s@%s" $registry .Values.image.repository .Values.image.digest }}
{{- else }}
{{- printf "%s/%s:%s" $registry .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}
{{- end }}

{{/*
Return the list of image pull secrets.
Merges global.imagePullSecrets + image.pullSecrets, de-duplicating by name.
*/}}
{{- define "agentregistry.imagePullSecrets" -}}
{{- $secrets := list }}
{{- if .Values.global }}
  {{- range .Values.global.imagePullSecrets }}
    {{- if kindIs "string" . }}
      {{- $secrets = append $secrets (dict "name" .) }}
    {{- else }}
      {{- $secrets = append $secrets . }}
    {{- end }}
  {{- end }}
{{- end }}
{{- range .Values.image.pullSecrets }}
  {{- if kindIs "string" . }}
    {{- $secrets = append $secrets (dict "name" .) }}
  {{- else }}
    {{- $secrets = append $secrets . }}
  {{- end }}
{{- end }}
{{- if $secrets }}
imagePullSecrets:
  {{- toYaml $secrets | nindent 2 }}
{{- end }}
{{- end }}

{{/* ======================================================================
   ServiceAccount
   ====================================================================== */}}

{{/*
Create the name of the service account to use.
*/}}
{{- define "agentregistry.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "agentregistry.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/* ======================================================================
   Secret helpers
   ====================================================================== */}}

{{/*
Return the secret name containing AGENT_REGISTRY_JWT_PRIVATE_KEY.
Priority: global.existingSecret → config.existingSecret → chart-managed secret.
*/}}
{{- define "agentregistry.secretName" -}}
{{- .Values.global.existingSecret | default .Values.config.existingSecret | default (include "agentregistry.fullname" .) }}
{{- end }}

{{/*
Return the secret name that holds POSTGRES_PASSWORD.
Priority: global.existingSecret → database.existingSecret → chart-managed secret.
*/}}
{{- define "agentregistry.passwordSecretName" -}}
{{- .Values.global.existingSecret | default .Values.database.existingSecret | default (include "agentregistry.fullname" .) }}
{{- end }}

{{/* ======================================================================
   Database URL
   ====================================================================== */}}

{{/*
Return the PostgreSQL database URL.
If database.url is set, use it directly.
Otherwise build from individual fields, injecting POSTGRES_PASSWORD at runtime.
*/}}
{{- define "agentregistry.databaseUrl" -}}
{{- if .Values.database.url }}
{{- .Values.database.url }}
{{- else }}
{{- printf "postgres://%s:$(%s)@%s:%s/%s?sslmode=%s"
      .Values.database.username
      "POSTGRES_PASSWORD"
      .Values.database.host
      (toString .Values.database.port)
      .Values.database.database
      .Values.database.sslMode }}
{{- end }}
{{- end }}

{{/* ======================================================================
   Resource management
   ====================================================================== */}}

{{/*
Return resource requests/limits.
If .resources is non-empty, use it directly.
Otherwise, map .resourcesPreset to a set of defaults.

Usage: include "agentregistry.resources" (dict "resources" .Values.resources "preset" .Values.resourcesPreset)
*/}}
{{- define "agentregistry.resources" -}}
{{- if .resources }}
{{- toYaml .resources }}
{{- else }}
{{- $preset := .preset | default "none" }}
{{- if eq $preset "nano" }}
requests:
  cpu: 100m
  memory: 128Mi
limits:
  cpu: 200m
  memory: 256Mi
{{- else if eq $preset "micro" }}
requests:
  cpu: 250m
  memory: 256Mi
limits:
  cpu: 500m
  memory: 512Mi
{{- else if eq $preset "small" }}
requests:
  cpu: 250m
  memory: 256Mi
limits:
  cpu: "1"
  memory: 1Gi
{{- else if eq $preset "medium" }}
requests:
  cpu: 500m
  memory: 512Mi
limits:
  cpu: "2"
  memory: 2Gi
{{- else if eq $preset "large" }}
requests:
  cpu: "1"
  memory: 1Gi
limits:
  cpu: "4"
  memory: 4Gi
{{- else if eq $preset "xlarge" }}
requests:
  cpu: "2"
  memory: 2Gi
limits:
  cpu: "8"
  memory: 8Gi
{{- else if eq $preset "2xlarge" }}
requests:
  cpu: "4"
  memory: 4Gi
limits:
  cpu: "16"
  memory: 16Gi
{{- end }}
{{- end }}
{{- end }}

{{/* ======================================================================
   Security context helpers
   ====================================================================== */}}

{{/*
Return a pod-level securityContext, stripping the synthetic "enabled" key.
Usage: include "agentregistry.podSecurityContext" .Values.podSecurityContext
*/}}
{{- define "agentregistry.podSecurityContext" -}}
{{- if .enabled }}
{{- $ctx := omit . "enabled" }}
{{- if $ctx }}
{{- toYaml $ctx }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Return a container-level securityContext, stripping the synthetic "enabled" key.
Usage: include "agentregistry.containerSecurityContext" .Values.containerSecurityContext
*/}}
{{- define "agentregistry.containerSecurityContext" -}}
{{- if .enabled }}
{{- $ctx := omit . "enabled" }}
{{- if $ctx }}
{{- toYaml $ctx }}
{{- end }}
{{- end }}
{{- end }}

{{/* ======================================================================
   Affinity preset helpers
   ====================================================================== */}}

{{/*
Return a podAffinity term (soft or hard).
Usage: include "agentregistry.affinities.pod" (dict "type" "soft" "context" $)
*/}}
{{- define "agentregistry.affinities.pod" -}}
{{- $labelSelector := dict "matchLabels" (include "agentregistry.selectorLabels" .context | fromYaml) }}
{{- if eq .type "soft" }}
preferredDuringSchedulingIgnoredDuringExecution:
  - weight: 1
    podAffinityTerm:
      labelSelector:
        {{- toYaml $labelSelector | nindent 8 }}
      topologyKey: kubernetes.io/hostname
{{- else if eq .type "hard" }}
requiredDuringSchedulingIgnoredDuringExecution:
  - labelSelector:
      {{- toYaml $labelSelector | nindent 6 }}
    topologyKey: kubernetes.io/hostname
{{- end }}
{{- end }}

{{/*
Return a podAntiAffinity term (soft or hard).
Usage: include "agentregistry.affinities.podAnti" (dict "type" "soft" "context" $)
*/}}
{{- define "agentregistry.affinities.podAnti" -}}
{{- $labelSelector := dict "matchLabels" (include "agentregistry.selectorLabels" .context | fromYaml) }}
{{- if eq .type "soft" }}
preferredDuringSchedulingIgnoredDuringExecution:
  - weight: 1
    podAffinityTerm:
      labelSelector:
        {{- toYaml $labelSelector | nindent 8 }}
      topologyKey: kubernetes.io/hostname
{{- else if eq .type "hard" }}
requiredDuringSchedulingIgnoredDuringExecution:
  - labelSelector:
      {{- toYaml $labelSelector | nindent 6 }}
    topologyKey: kubernetes.io/hostname
{{- end }}
{{- end }}

{{/*
Return a nodeAffinity term (soft or hard).
Usage: include "agentregistry.affinities.node" (dict "type" "soft" "key" "foo" "values" (list "a" "b"))
*/}}
{{- define "agentregistry.affinities.node" -}}
{{- if eq .type "soft" }}
preferredDuringSchedulingIgnoredDuringExecution:
  - weight: 1
    preference:
      matchExpressions:
        - key: {{ .key }}
          operator: In
          values:
            {{- toYaml .values | nindent 12 }}
{{- else if eq .type "hard" }}
requiredDuringSchedulingIgnoredDuringExecution:
  nodeSelectorTerms:
    - matchExpressions:
        - key: {{ .key }}
          operator: In
          values:
            {{- toYaml .values | nindent 12 }}
{{- end }}
{{- end }}

{{/*
Compose the full affinity block.
If .Values.affinity is set it wins entirely. Otherwise build from presets.
*/}}
{{- define "agentregistry.affinity" -}}
{{- if .Values.affinity }}
{{- toYaml .Values.affinity }}
{{- else }}
{{- $affinity := dict }}
{{- if .Values.podAffinityPreset }}
{{- $_ := set $affinity "podAffinity" (include "agentregistry.affinities.pod" (dict "type" .Values.podAffinityPreset "context" .) | fromYaml) }}
{{- end }}
{{- if .Values.podAntiAffinityPreset }}
{{- $_ := set $affinity "podAntiAffinity" (include "agentregistry.affinities.podAnti" (dict "type" .Values.podAntiAffinityPreset "context" .) | fromYaml) }}
{{- end }}
{{- if and .Values.nodeAffinityPreset.type .Values.nodeAffinityPreset.key .Values.nodeAffinityPreset.values }}
{{- $_ := set $affinity "nodeAffinity" (include "agentregistry.affinities.node" (dict "type" .Values.nodeAffinityPreset.type "key" .Values.nodeAffinityPreset.key "values" .Values.nodeAffinityPreset.values) | fromYaml) }}
{{- end }}
{{- if $affinity }}
{{- toYaml $affinity }}
{{- end }}
{{- end }}
{{- end }}

{{/* ======================================================================
   Validation
   ====================================================================== */}}

{{/*
Compile hard-error validations. Any non-empty result triggers fail.
Called from templates/validate.yaml so it fires during helm template/install.
*/}}
{{- define "agentregistry.validateValues.errors" -}}
{{- $errors := list }}
{{- $hasExternalJwt := or .Values.global.existingSecret .Values.config.existingSecret }}
{{- if and (not $hasExternalJwt) (eq .Values.config.jwtPrivateKey "") }}
{{- $errors = append $errors "config.jwtPrivateKey must be set (or provide config.existingSecret / global.existingSecret containing AGENT_REGISTRY_JWT_PRIVATE_KEY)." }}
{{- else if and (not $hasExternalJwt) (not (regexMatch "^[0-9a-fA-F]+$" .Values.config.jwtPrivateKey)) }}
{{- $errors = append $errors "config.jwtPrivateKey must be a valid hex string (e.g. generated with: openssl rand -hex 32)." }}
{{- end }}
{{- if and (not (or .Values.global.existingSecret .Values.database.existingSecret)) (not .Values.database.url) (eq .Values.database.password "") }}
{{- $errors = append $errors "database.password must be set (or provide database.url, database.existingSecret, or global.existingSecret containing POSTGRES_PASSWORD)." }}
{{- end }}
{{- if and (not .Values.database.url) (not .Values.database.host) }}
{{- $errors = append $errors "database.host (or database.url) must be set. An external PostgreSQL instance with pgvector is required." }}
{{- end }}
{{- range $errors }}
{{ . }}
{{- end }}
{{- end }}

{{/*
Compile soft validation warnings into a single message.
Called from NOTES.txt (only shown during helm install/upgrade).
*/}}
{{- define "agentregistry.validateValues" -}}
{{- end }}
