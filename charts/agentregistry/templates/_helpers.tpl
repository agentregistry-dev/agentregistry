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
   PostgreSQL helpers
   ====================================================================== */}}

{{/*
PostgreSQL fully qualified name.
*/}}
{{- define "agentregistry.postgresql.fullname" -}}
{{- printf "%s-postgresql" (include "agentregistry.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
PostgreSQL labels — stable set for all PG resources.
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
PostgreSQL selector labels.
*/}}
{{- define "agentregistry.postgresql.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agentregistry.name" . }}-postgresql
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: database
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
Return the proper PostgreSQL image name.
*/}}
{{- define "agentregistry.postgresql.image" -}}
{{- $registry := .Values.postgresql.image.registry -}}
{{- if .Values.global }}
  {{- if .Values.global.imageRegistry }}
    {{- $registry = .Values.global.imageRegistry -}}
  {{- end }}
{{- end }}
{{- if .Values.postgresql.image.digest }}
{{- printf "%s/%s@%s" $registry .Values.postgresql.image.repository .Values.postgresql.image.digest }}
{{- else }}
{{- printf "%s/%s:%s" $registry .Values.postgresql.image.repository .Values.postgresql.image.tag }}
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
Return the secret name containing credentials.
*/}}
{{- define "agentregistry.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- include "agentregistry.fullname" . }}
{{- end }}
{{- end }}

{{/*
Return the secret name containing kubeconfig.
*/}}
{{- define "agentregistry.kubeconfigSecretName" -}}
{{- if .Values.kubeconfig.existingSecret }}
{{- .Values.kubeconfig.existingSecret }}
{{- else }}
{{- printf "%s-kubeconfig" (include "agentregistry.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Generate a random hex string of the specified length (number of hex characters).
Uses concatenated UUIDs (which are hex) with dashes stripped, then truncated.
Each UUID yields 32 hex chars, so 3 UUIDs give 96 hex chars (enough for 64).

Usage: include "agentregistry.randHex" <int>
*/}}
{{- define "agentregistry.randHex" -}}
{{- $hex := printf "%s%s%s" (uuidv4 | replace "-" "") (uuidv4 | replace "-" "") (uuidv4 | replace "-" "") -}}
{{- $hex | trunc (. | int) -}}
{{- end }}

{{/*
Generate a secret value. If autoGenerate is true and the secret already exists
in the cluster, reuse the existing value to guarantee stability across upgrades.
Otherwise fall back to the user-supplied value or generate a random one.

Set format to "hex" to generate a hex-encoded random string instead of
alphanumeric (required for keys that must be valid hex, e.g. JWT private keys).

Usage: include "agentregistry.secretValue" (dict "current" <existing-value> "provided" <user-value> "autoGenerate" <bool> "length" <int> "format" <string>)
*/}}
{{- define "agentregistry.secretValue" -}}
{{- if .current }}
{{- .current }}
{{- else if and .provided (ne .provided "") }}
{{- .provided }}
{{- else if .autoGenerate }}
{{- if eq (default "" .format) "hex" }}
{{- include "agentregistry.randHex" (.length | default 64) }}
{{- else }}
{{- randAlphaNum (.length | default 32) }}
{{- end }}
{{- else }}
{{- .provided }}
{{- end }}
{{- end }}

{{/* ======================================================================
   Database URL
   ====================================================================== */}}

{{/*
Return the PostgreSQL database URL.
When postgresql.enabled is true, builds the URL from bundled PG values.
When postgresql.enabled is false, uses externalDatabase settings.
In both cases, the password is injected at runtime via the $(POSTGRES_PASSWORD)
env-var expansion.
*/}}
{{- define "agentregistry.databaseUrl" -}}
{{- if .Values.postgresql.enabled }}
{{- printf "postgres://%s:$(%s)@%s:%s/%s?sslmode=%s"
      .Values.postgresql.auth.username
      "POSTGRES_PASSWORD"
      (include "agentregistry.postgresql.fullname" .)
      (toString .Values.postgresql.service.port)
      .Values.postgresql.auth.database
      .Values.postgresql.sslMode }}
{{- else }}
  {{- if .Values.externalDatabase.url }}
{{- .Values.externalDatabase.url }}
  {{- else }}
{{- printf "postgres://%s:$(%s)@%s:%s/%s?sslmode=%s"
      .Values.externalDatabase.username
      "POSTGRES_PASSWORD"
      .Values.externalDatabase.host
      (toString .Values.externalDatabase.port)
      .Values.externalDatabase.database
      .Values.externalDatabase.sslMode }}
  {{- end }}
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
   StorageClass helper
   ====================================================================== */}}

{{/*
Return the proper StorageClass name.
Uses global.storageClass as override, then per-component, then empty (default).
Usage: include "agentregistry.storageClass" (dict "storageClass" .Values.postgresql.persistence.storageClass "global" .Values.global)
*/}}
{{- define "agentregistry.storageClass" -}}
{{- $sc := "" }}
{{- if .global }}
  {{- if .global.storageClass }}
    {{- $sc = .global.storageClass }}
  {{- end }}
{{- end }}
{{- if .storageClass }}
  {{- $sc = .storageClass }}
{{- end }}
{{- if $sc }}
{{- if eq $sc "-" }}
storageClassName: ""
{{- else }}
storageClassName: {{ $sc | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/* ======================================================================
   Validation
   ====================================================================== */}}

{{/*
Compile all validation warnings into a single message.
Call from NOTES.txt: include "agentregistry.validateValues" .
*/}}
{{- define "agentregistry.validateValues" -}}
{{- $messages := list }}
{{- if and (not .Values.postgresql.enabled) (not .Values.externalDatabase.url) (not .Values.externalDatabase.host) }}
{{- $messages = append $messages "WARNING: postgresql.enabled is false but no externalDatabase.url or externalDatabase.host is set. The application will fail to start without a database connection." }}
{{- end }}
{{- if and .Values.rbac.create .Values.rbac.clusterAdminBinding }}
{{- $messages = append $messages "WARNING: rbac.clusterAdminBinding is true. This grants cluster-admin privileges to the ServiceAccount. This is intended for development/demo environments only." }}
{{- end }}
{{- if and (not .Values.existingSecret) (not .Values.secrets.autoGenerate) (or (eq .Values.secrets.postgresPassword "") (eq .Values.secrets.jwtPrivateKey "")) }}
{{- $messages = append $messages "WARNING: Secrets are not auto-generated and no values or existingSecret were provided. Set secrets.autoGenerate=true or provide explicit secret values." }}
{{- end }}
{{- if and .Values.hostVolumes.dockerSocket }}
{{- $messages = append $messages "WARNING: hostVolumes.dockerSocket is enabled. This exposes the Docker socket inside the pod and is a significant security risk." }}
{{- end }}
{{- if and .Values.hostVolumes.hostTmp }}
{{- $messages = append $messages "WARNING: hostVolumes.hostTmp is enabled. Mounting host /tmp is insecure and should only be used for development." }}
{{- end }}
{{- range $messages }}
{{ . }}
{{- end }}
{{- end }}
