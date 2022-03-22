{{/*
Expand the name of the chart.
*/}}
{{- define "stunner_helm.name" -}}
{{- default .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "stunner_helm.fullname" -}}
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
{{- define "stunner_helm.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "stunner_helm.labels" -}}
{{ include "stunner_helm.turnServer.selectorLabels" . }}
helm.sh/chart: {{ include "stunner_helm.chart" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels for turn server
*/}}
{{- define "stunner_helm.turnServer.selectorLabels" -}}
app: {{ .Values.turnServer.deployment.label }}
app.kubernetes.io/name: {{ include "stunner_helm.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Selector labels for application server
*/}}
{{- define "stunner_helm.applicationServer.selectorLabels" -}}
app: {{ .Values.applicationServer.deployment.name }}
{{- end }}

{{/*
Selector labels for media server
*/}}
{{- define "stunner_helm.mediaServer.selectorLabels" -}}
app: {{ .Values.mediaServer.deployment.name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "stunner_helm.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "stunner_helm.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
