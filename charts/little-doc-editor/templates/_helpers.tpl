{{/*
Chart name and fullname, standard helm create conventions.
*/}}
{{- define "little-doc-editor.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{- define "little-doc-editor.fullname" -}}
{{- .Release.Name -}}-{{- .Chart.Name -}}
{{- end -}}

{{- define "little-doc-editor.labels" -}}
app.kubernetes.io/name: {{ include "little-doc-editor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Resolve a credential-shaped value (the { value, existingSecret: {name, key} }
shape used throughout values.yaml, per Decision 19) to a Secret name/key pair
for use in a secretKeyRef. If `existingSecret.name` is set, it's used
directly (ESO-friendly: the chart never touches that Secret). Otherwise the
chart's own generated Secret (see secrets.yaml) is used.

Usage: {{ include "little-doc-editor.secretRef" (dict "root" $ "field" .Values.postgres.dsn "generatedKey" "postgres-dsn") }}
Returns a map with .name and .key for use in secretKeyRef.
*/}}
{{- define "little-doc-editor.secretRef" -}}
{{- $field := .field -}}
{{- if $field.existingSecret.name -}}
name: {{ $field.existingSecret.name }}
key: {{ $field.existingSecret.key }}
{{- else -}}
name: {{ include "little-doc-editor.fullname" .root }}-generated
key: {{ .generatedKey }}
{{- end -}}
{{- end -}}

{{/*
Renders a full env var entry (name + secretKeyRef) for a credential-shaped
value. Usage:
  {{ include "little-doc-editor.secretEnvVar" (dict "root" $ "name" "DATABASE_URL" "field" .Values.postgres.dsn "generatedKey" "postgres-dsn") }}
*/}}
{{- define "little-doc-editor.secretEnvVar" -}}
- name: {{ .name }}
  valueFrom:
    secretKeyRef:
      {{- include "little-doc-editor.secretRef" . | nindent 6 }}
{{- end -}}

{{/*
Public API base URL handed to the SPA at runtime (frontend API_BASE_URL).
Precedence: frontend.config.apiBaseUrl, then the ingress api host (scheme from
its tls toggle), then the first gatewayApi api hostname (https assumed: the
Gateway listener's TLS isn't visible to the chart). Empty = same-origin
/api/v1 default inside the image.
*/}}
{{- define "little-doc-editor.frontendApiBaseUrl" -}}
{{- if .Values.frontend.config.apiBaseUrl -}}
{{- .Values.frontend.config.apiBaseUrl -}}
{{- else if and .Values.ingress.enabled .Values.ingress.api.host -}}
{{- if .Values.ingress.api.tls.enabled }}https{{ else }}http{{ end }}://{{ .Values.ingress.api.host }}/api/v1
{{- else if and .Values.gatewayApi.enabled .Values.gatewayApi.api.hostnames -}}
https://{{ first .Values.gatewayApi.api.hostnames }}/api/v1
{{- end -}}
{{- end -}}

{{/* Public collab WebSocket URL for the SPA (frontend COLLAB_URL); same precedence as above. */}}
{{- define "little-doc-editor.frontendCollabUrl" -}}
{{- if .Values.frontend.config.collabUrl -}}
{{- .Values.frontend.config.collabUrl -}}
{{- else if and .Values.ingress.enabled .Values.ingress.collab.host -}}
{{- if .Values.ingress.collab.tls.enabled }}wss{{ else }}ws{{ end }}://{{ .Values.ingress.collab.host }}
{{- else if and .Values.gatewayApi.enabled .Values.gatewayApi.collab.hostnames -}}
wss://{{ first .Values.gatewayApi.collab.hostnames }}
{{- end -}}
{{- end -}}

{{/*
Browser origin(s) the SPA is served from, comma-joined, derived from
ingress.frontend.host (scheme from its tls toggle) and
gatewayApi.frontend.hostnames (https assumed). Empty when neither is set.
*/}}
{{- define "little-doc-editor.frontendOrigins" -}}
{{- $o := list -}}
{{- if and .Values.ingress.enabled .Values.ingress.frontend.host -}}
{{- $h := lower .Values.ingress.frontend.host -}}
{{- $o = append $o (printf "https://%s" $h) -}}
{{- if not .Values.ingress.frontend.tls.enabled -}}
{{- /* TLS may be terminated upstream of the cluster: browsers then send https. */ -}}
{{- $o = append $o (printf "http://%s" $h) -}}
{{- end -}}
{{- end -}}
{{- if .Values.gatewayApi.enabled -}}
{{- range .Values.gatewayApi.frontend.hostnames -}}
{{- if not (contains "*" .) -}}
{{- $o = append $o (printf "https://%s" (lower .)) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- join "," (uniq $o) -}}
{{- end -}}

{{/*
True ("true") when the api is published on a different host than the
frontend, i.e. the SPA's calls to it are cross-origin and need CORS.
*/}}
{{- define "little-doc-editor.apiIsCrossOrigin" -}}
{{- $cross := false -}}
{{- if and .Values.ingress.enabled .Values.ingress.api.host (ne (lower .Values.ingress.api.host) (lower .Values.ingress.frontend.host)) -}}
{{- $cross = true -}}
{{- end -}}
{{- if .Values.gatewayApi.enabled -}}
{{- range .Values.gatewayApi.api.hostnames -}}
{{- if not (has . $.Values.gatewayApi.frontend.hostnames) -}}{{- $cross = true -}}{{- end -}}
{{- end -}}
{{- end -}}
{{- $cross -}}
{{- end -}}

{{/*
api CORS_ALLOWED_ORIGINS: api.corsAllowedOrigins when set, else the frontend
origin(s) if the api host differs from the frontend host, else empty (CORS off).
Never "*": credentials-bearing (Authorization) API.
*/}}
{{- define "little-doc-editor.apiCorsOrigins" -}}
{{- if .Values.api.corsAllowedOrigins -}}
{{- range .Values.api.corsAllowedOrigins -}}
{{- if contains "*" . -}}{{- fail "api.corsAllowedOrigins must list explicit origins, not wildcards" -}}{{- end -}}
{{- end -}}
{{- join "," .Values.api.corsAllowedOrigins -}}
{{- else if eq (include "little-doc-editor.apiIsCrossOrigin" .) "true" -}}
{{- include "little-doc-editor.frontendOrigins" . -}}
{{- end -}}
{{- end -}}

{{/* collab COLLAB_ALLOWED_ORIGINS: collab.allowedOrigins when set, else the frontend origin(s). */}}
{{- define "little-doc-editor.collabAllowedOrigins" -}}
{{- if .Values.collab.allowedOrigins -}}
{{- range .Values.collab.allowedOrigins -}}
{{- if contains "*" . -}}{{- fail "collab.allowedOrigins must list explicit origins, not wildcards" -}}{{- end -}}
{{- end -}}
{{- join "," .Values.collab.allowedOrigins -}}
{{- else -}}
{{- include "little-doc-editor.frontendOrigins" . -}}
{{- end -}}
{{- end -}}
