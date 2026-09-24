{{/*
Expand the name of the chart.
*/}}
{{- define "splunk-universalforwarder.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 44 chars because the longest suffix appended to this base is "-clusterrolebinding"
(19 chars), and 44 + 19 = 63, which is the Kubernetes DNS name limit.
If release name contains chart name it will be used as a full name.
*/}}
{{- define "splunk-universalforwarder.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 44 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 44 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 44 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "splunk-universalforwarder.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "splunk-universalforwarder.labels" -}}
helm.sh/chart: {{ include "splunk-universalforwarder.chart" . }}
{{ include "splunk-universalforwarder.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
Returns only app.kubernetes.io/name and app.kubernetes.io/instance.
These are immutable once deployed — keep minimal (D-05, D-06).
*/}}
{{- define "splunk-universalforwarder.selectorLabels" -}}
app.kubernetes.io/name: {{ include "splunk-universalforwarder.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Define namespace of release and allow for namespace override
*/}}
{{- define "splunk-universalforwarder.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "splunk-universalforwarder.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-sa" (include "splunk-universalforwarder.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
