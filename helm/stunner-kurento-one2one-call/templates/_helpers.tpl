
{{/*
Selector labels for application server
*/}}
{{- define "stunner-kurento-one2one-call.applicationServer.selectorLabels" -}}
app: {{ .Values.applicationServer.deployment.name }}
{{- end }}

{{/*
Selector labels for media server
*/}}
{{- define "stunner-kurento-one2one-call.mediaServer.selectorLabels" -}}
app: {{ .Values.mediaServer.deployment.name }}
{{- end }}

{{/*
Expand the name of the chart.
*/}}
{{- define "stunner-kurento-one2one-call.name" -}}
{{- default .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}