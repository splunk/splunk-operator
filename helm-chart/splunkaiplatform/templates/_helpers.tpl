{{/* templates/_helpers.tpl */}}
{{- define "splunkaiplatform.name" -}}
{{- default .Chart.Name .Values.nameOverride -}}
{{- end -}}

{{- define "splunkaiplatform.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" (include "splunkaiplatform.name" .) .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "splunkaiplatform.labels" -}}
app.kubernetes.io/name: {{ include "splunkaiplatform.name" . }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
