{{- define "noah.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "noah.fullname" -}}
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

{{- define "noah.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "noah.labels" -}}
helm.sh/chart: {{ include "noah.chart" . }}
app.kubernetes.io/name: {{ include "noah.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "noah.selectorLabels" -}}
app.kubernetes.io/name: {{ include "noah.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "noah.serviceAccountName" -}}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}

{{- define "noah.databaseSecretName" -}}
{{- printf "%s-database" (include "noah.fullname" .) }}
{{- end }}

{{- define "noah.envConfigMapName" -}}
{{- printf "%s-env" (include "noah.fullname" .) }}
{{- end }}

{{- define "noah.tenantConfigMapName" -}}
{{- printf "%s-tenant-config" (include "noah.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "noah.postgresqlName" -}}
{{- printf "%s-postgresql" (include "noah.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "noah.redisName" -}}
{{- printf "%s-redis" (include "noah.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "noah.minioName" -}}
{{- printf "%s-minio" (include "noah.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "noah.minioSecretName" -}}
{{- printf "%s-minio" (include "noah.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
