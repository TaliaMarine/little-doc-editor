---
name: eso-secret-value
description: This skill should be used when the user asks to "add a helm secret value", "wire a new credential", "add an ExternalSecret example", "support secretRef for a value", or mentions "ESO", "External Secrets Operator", or "existingSecret". Covers wiring a new credential-shaped Helm value so it supports both an inline value and a reference to an existing Kubernetes Secret, per little-doc-editor's chart convention.
---

# Wiring a new credential-shaped Helm value

little-doc-editor's Helm chart requires every credential-shaped value (Postgres DSN, S3 access key, OIDC client secret, Redis auth, and any new one) to support **both** an inline value in `values.yaml` **and** a reference to an existing Kubernetes Secret by name/key, and to be friendly to the External Secrets Operator (ESO) — the chart must never fight a Secret that ESO manages (SPEC.md Decision 19). This is the single recurring pattern across `api`, `frontend`, and `collab` values.

## The values.yaml shape

Use the same two-field shape already established for existing credentials — check `values.yaml` for the exact existing pattern (e.g. under `postgres:`, `s3:`, `oidc:`, `redis:`) before inventing a new one. The canonical shape:

```yaml
myFeature:
  secret:
    # Option A: inline value (dev/eval convenience)
    value: ""
    # Option B: reference an existing Secret (production / ESO-managed)
    existingSecret:
      name: ""
      key: ""
```

Exactly one of `value` or `existingSecret` should be set; if both are set, prefer `existingSecret` and note that in the values.yaml comment so operators aren't surprised.

## The template helper

Add (or reuse, if one already exists in `_helpers.tpl`) a named template that resolves either form to an `envFrom`/`env` entry with a `secretKeyRef`, so consumers never read `.value` directly into a Pod spec unencrypted-in-manifest when `existingSecret` is set. Pattern:

```yaml
{{- define "little-doc-editor.secretEnvVar" -}}
{{- if .existingSecret.name }}
valueFrom:
  secretKeyRef:
    name: {{ .existingSecret.name }}
    key: {{ .existingSecret.key }}
{{- else }}
valueFrom:
  secretKeyRef:
    name: {{ include "little-doc-editor.fullname" $ }}-generated
    key: {{ .key }}
{{- end }}
{{- end }}
```

If `value` is set inline, the chart should still materialize it into a Secret object it manages (never inject a raw value directly as a plain env var) — check the existing pattern for how the chart's own generated Secret is named and templated, and follow it exactly for consistency.

## Docs: the ExternalSecret example

Every new credential value needs an example `ExternalSecret` manifest in the chart's docs (README or `docs/`) showing the Secret shape the chart expects `existingSecret` to point at. Follow the existing examples for Postgres/S3/OIDC/Redis as the template:

```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: little-doc-editor-my-feature
spec:
  secretStoreRef:
    name: my-secret-store
    kind: SecretStore
  target:
    name: little-doc-editor-my-feature   # matches existingSecret.name in values.yaml
  data:
    - secretKey: my-key                  # matches existingSecret.key in values.yaml
      remoteRef:
        key: path/in/external/store
```

## Checklist before considering it done

- [ ] `values.yaml` has both `value` and `existingSecret.{name,key}` fields, with a comment explaining the choice.
- [ ] The template resolves whichever is set, never assumes one.
- [ ] No template hardcodes `helm.sh/hook` behavior on a Secret that ESO might also manage (would cause ownership conflicts).
- [ ] An example `ExternalSecret` manifest is added to chart docs, with `target.name`/`data[].secretKey` matching the values.yaml field names exactly.
- [ ] Nothing logs or `stdout`-prints the resolved value at any log level.
