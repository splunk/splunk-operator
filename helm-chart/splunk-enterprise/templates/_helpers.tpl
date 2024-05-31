{{/*
Expand the name of the chart.
*/}}
{{- define "splunk-enterprise.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "splunk-enterprise.fullname" -}}
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
{{- define "splunk-enterprise.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "splunk-enterprise.labels" -}}
helm.sh/chart: {{ include "splunk-enterprise.chart" . }}
{{ include "splunk-enterprise.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "splunk-enterprise.selectorLabels" -}}
app.kubernetes.io/name: {{ include "splunk-enterprise.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "splunk-enterprise.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "splunk-enterprise.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Define namespace of release and allow for namespace override
*/}}
{{- define "splunk-enterprise.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride }}
{{- end }}

{{/*
Define the list of Cluster Maaster internal URI used for multisite config
*/}}
{{- define "cmlist" -}}
{{- $cm_name := list -}}
{{- range default (default (until 1)) .Values.sva.m4.totalCluster }}
{{- $cm_name = printf "splunk-%s-cluster-manager-service" .name | append $cm_name -}}
{{- end -}}
{{- join ","  $cm_name }}
{{- end -}}


{{/*
Return the first value from the list of indexer clusters to be configured for outputs.conf
*/}}
{{- define "cm_name_first" -}}
{{- with (first .Values.sva.m4.totalCluster) }}
{{- .name  }}
{{- end }}
{{- end -}}
