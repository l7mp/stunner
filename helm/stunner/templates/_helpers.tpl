{{/*
Expand the name of the chart.
*/}}
{{- define "stunner.name" -}}
{{- default .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
# {{- define "stunner.fullname" -}}
# {{- if .Values.fullnameOverride }}
# {{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
# {{- else }}
# {{- $name := default .Chart.Name .Values.nameOverride }}
# {{- if contains $name .Release.Name }}
# {{- .Release.Name | trunc 63 | trimSuffix "-" }}
# {{- else }}
# {{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
# {{- end }}
# {{- end }}
# {{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "stunner.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "stunner.labels" -}}
{{ include "stunner.stunner.selectorLabels" . }}
helm.sh/chart: {{ include "stunner.chart" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels for turn server
*/}}
{{- define "stunner.stunner.selectorLabels" -}}
app: {{ .Values.global.stunner.deployment.label }}
app.kubernetes.io/name: {{ include "stunner.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "stunner.serviceAccountName" -}}
{{- if .Values.global.serviceAccount.create }}
{{- default (include "stunner.fullname" .) .Values.global.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.global.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Generate the proper args for stunnerd
*/}}
{{- define "stunner.stunnerGatewayOperator.args" -}}
{{- if .Values.global.stunnerGatewayOperator.enabled }}
command: ["stunnerd"]
args: ["-w", "-c", "/etc/stunnerd/stunnerd.conf"]
env:
  - name: STUNNER_ADDR
    valueFrom:
      fieldRef:
        apiVersion: v1
        fieldPath: status.podIP
volumeMounts:
  - name: stunnerd-config-volume
    mountPath: /etc/stunnerd
{{- else }}
command: ["stunnerd"]
args: ["-c", "/stunnerd.conf"]
envFrom:
  - configMapRef:
      name: stunner-config
env:
- name: STUNNER_ADDR  # we use the POD IP
  valueFrom:
    fieldRef:
      fieldPath: status.podIP
{{- end }}
{{- end }}

{{/*
Generate the proper args for stunnerd
*/}}
{{- define "stunner.stunnerGatewayOperator.volume" -}}
{{- if .Values.global.stunnerGatewayOperator.enabled }}
volumes:
  - name: stunnerd-config-volume
    configMap:
      name: stunnerd-configmap
{{- end }}
{{- end }}


