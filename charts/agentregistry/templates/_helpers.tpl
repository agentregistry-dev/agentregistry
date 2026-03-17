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
Priority: global.existingSecret → database.external.existingSecret → chart-managed secret.
When database.bundled.enabled, external.existingSecret is not meaningful; the chart-managed secret holds the bundled password.
*/}}
{{- define "agentregistry.passwordSecretName" -}}
{{- .Values.global.existingSecret | default .Values.database.external.existingSecret | default (include "agentregistry.fullname" .) }}
{{- end }}

{{/* ======================================================================
   Database URL
   ====================================================================== */}}

{{/*
Return the PostgreSQL database URL.
When database.bundled.enabled, points at the in-chart PostgreSQL service.
Otherwise: if database.external.url is set, use it directly; else build from individual external fields.
*/}}
{{- define "agentregistry.databaseUrl" -}}
{{- if .Values.database.bundled.enabled }}
{{- printf "postgres://%s:$(%s)@%s:%s/%s?sslmode=%s"
      .Values.database.bundled.auth.username
      "POSTGRES_PASSWORD"
      (include "agentregistry.postgresql.fullname" .)
      (toString .Values.database.bundled.service.port)
      .Values.database.bundled.auth.database
      .Values.database.bundled.sslMode }}
{{- else if .Values.database.external.url }}
{{- .Values.database.external.url }}
{{- else }}
{{- printf "postgres://%s:$(%s)@%s:%s/%s?sslmode=%s"
      .Values.database.external.username
      "POSTGRES_PASSWORD"
      .Values.database.external.host
      (toString .Values.database.external.port)
      .Values.database.external.database
      .Values.database.external.sslMode }}
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
   Bundled PostgreSQL helpers
   ====================================================================== */}}

{{/*
Full name for the bundled PostgreSQL resources.
*/}}
{{- define "agentregistry.postgresql.fullname" -}}
{{- printf "%s-postgresql" (include "agentregistry.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Standard labels for bundled PostgreSQL resources.
*/}}
{{- define "agentregistry.postgresql.labels" -}}
helm.sh/chart: {{ include "agentregistry.chart" . }}
{{ include "agentregistry.postgresql.selectorLabels" . }}
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
Selector labels for bundled PostgreSQL resources.
*/}}
{{- define "agentregistry.postgresql.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agentregistry.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: database
{{- end }}

{{/*
Return the bundled PostgreSQL image string.
Respects global.imageRegistry override.
*/}}
{{- define "agentregistry.postgresql.image" -}}
{{- $registry := .Values.database.bundled.image.registry -}}
{{- if .Values.global }}
  {{- if .Values.global.imageRegistry }}
    {{- $registry = .Values.global.imageRegistry -}}
  {{- end }}
{{- end }}
{{- printf "%s/%s:%s" $registry .Values.database.bundled.image.repository .Values.database.bundled.image.tag }}
{{- end }}

{{/* ======================================================================
   Validation
   ====================================================================== */}}

{{/*
Compile hard-error validations. Any non-empty result triggers fail.
Called from templates/validate.yaml so it fires during helm template/install.
When database.bundled.enabled, external database host/password validation is skipped.
*/}}
{{- define "agentregistry.validateValues.errors" -}}
{{- $errors := list }}
{{- $hasExternalJwt := or .Values.global.existingSecret .Values.config.existingSecret }}
{{- if and (not $hasExternalJwt) (eq .Values.config.jwtPrivateKey "") }}
{{- $errors = append $errors "config.jwtPrivateKey must be set (or provide config.existingSecret / global.existingSecret containing AGENT_REGISTRY_JWT_PRIVATE_KEY)." }}
{{- else if and (not $hasExternalJwt) (not (regexMatch "^[0-9a-fA-F]+$" .Values.config.jwtPrivateKey)) }}
{{- $errors = append $errors "config.jwtPrivateKey must be a valid hex string (e.g. generated with: openssl rand -hex 32)." }}
{{- end }}
{{- if not .Values.database.bundled.enabled }}
{{- if and (not (or .Values.global.existingSecret .Values.database.external.existingSecret)) (not .Values.database.external.url) (eq .Values.database.external.password "") }}
{{- $errors = append $errors "database.external.password must be set (or provide database.external.url, database.external.existingSecret, or global.existingSecret containing POSTGRES_PASSWORD)." }}
{{- end }}
{{- if and (not .Values.database.external.url) (not .Values.database.external.host) }}
{{- $errors = append $errors "database.external.host (or database.external.url) must be set when database.bundled.enabled=false. An external PostgreSQL instance with pgvector is required." }}
{{- end }}
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
