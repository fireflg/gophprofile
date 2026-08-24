{{- define "avatars-service.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "avatars-service.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "avatars-service.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "avatars-service.selectorLabels" -}}
app.kubernetes.io/name: {{ include "avatars-service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "avatars-service.labels" -}}
helm.sh/chart: {{ include "avatars-service.chart" . }}
{{ include "avatars-service.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: {{ include "avatars-service.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "avatars-service.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "avatars-service.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "avatars-service.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "avatars-service.fullname" . -}}
{{- end -}}
{{- end -}}

{{- define "avatars-service.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}

{{- define "avatars-service.envFrom" -}}
- configMapRef:
    name: {{ include "avatars-service.fullname" . }}-config
{{- if not .Values.vault.enabled }}
- secretRef:
    name: {{ include "avatars-service.secretName" . }}
{{- end }}
{{- end -}}

{{- define "avatars-service.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: {{ .Values.podSecurity.runAsUser }}
runAsGroup: {{ .Values.podSecurity.runAsGroup }}
fsGroup: {{ .Values.podSecurity.fsGroup }}
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "avatars-service.containerSecurityContext" -}}
allowPrivilegeEscalation: false
privileged: false
readOnlyRootFilesystem: true
runAsNonRoot: true
runAsUser: {{ .Values.podSecurity.runAsUser }}
capabilities:
  drop:
    - ALL
{{- end -}}

{{- define "avatars-service.vaultInitContainer" -}}
- name: vault-agent
  image: {{ .Values.vault.image }}
  imagePullPolicy: {{ .Values.image.pullPolicy }}
  args:
    - agent
    - -config=/vault/config/vault-agent.hcl
  securityContext:
    {{- include "avatars-service.containerSecurityContext" . | nindent 4 }}
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      cpu: 200m
      memory: 128Mi
  volumeMounts:
    - name: vault-config
      mountPath: /vault/config
      readOnly: true
    - name: vault-secrets
      mountPath: /vault/secrets
    - name: tmp
      mountPath: /tmp
{{- end -}}

{{- define "avatars-service.migrationsInitContainer" -}}
- name: migrations
  image: {{ .Values.migrations.image }}
  imagePullPolicy: {{ .Values.image.pullPolicy }}
  command:
    - /bin/sh
    - -c
  args:
    - |
      {{- if .Values.vault.enabled }}
      migrate -path /migrations -database "$$(cat /vault/secrets/postgres.dsn)" up
      {{- else }}
      migrate -path /migrations -database "$POSTGRES_DSN" up
      {{- end }}
  {{- if not .Values.vault.enabled }}
  envFrom:
    - secretRef:
        name: {{ include "avatars-service.secretName" . }}
  {{- end }}
  securityContext:
    {{- include "avatars-service.containerSecurityContext" . | nindent 4 }}
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      cpu: 200m
      memory: 128Mi
  volumeMounts:
    - name: migrations
      mountPath: /migrations
      readOnly: true
    {{- if .Values.vault.enabled }}
    - name: vault-secrets
      mountPath: /vault/secrets
      readOnly: true
    {{- end }}
{{- end -}}

{{- define "avatars-service.volumes" -}}
- name: tmp
  emptyDir: {}
{{- if .Values.vault.enabled }}
- name: vault-config
  configMap:
    name: {{ include "avatars-service.fullname" . }}-vault-agent
- name: vault-secrets
  emptyDir:
    medium: Memory
{{- end }}
{{- end -}}

{{- define "avatars-service.volumeMounts" -}}
- name: tmp
  mountPath: /tmp
{{- if .Values.vault.enabled }}
- name: vault-secrets
  mountPath: /vault/secrets
  readOnly: true
{{- end }}
{{- end -}}

{{- define "avatars-service.checksums" -}}
checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
{{- if and .Values.secrets.create (not .Values.vault.enabled) }}
checksum/secret: {{ include (print $.Template.BasePath "/secret.yaml") . | sha256sum }}
{{- end }}
{{- end -}}
