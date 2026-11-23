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
