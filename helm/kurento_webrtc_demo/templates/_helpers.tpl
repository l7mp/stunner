
{{/*
Selector labels for application server
*/}}
{{- define "kurento_webrtc_demo.applicationServer.selectorLabels" -}}
app: {{ .Values.applicationServer.deployment.name }}
{{- end }}

{{/*
Selector labels for media server
*/}}
{{- define "kurento_webrtc_demo.mediaServer.selectorLabels" -}}
app: {{ .Values.mediaServer.deployment.name }}
{{- end }}

{{/*
Expand the name of the chart.
*/}}
{{- define "kurento_webrtc_demo.name" -}}
{{- default .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}