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
Noah is only supported with the sva.c3 preset, and the Noah topology has no cluster manager: the
CRDs reject noahClusterRef together with clusterManagerRef. Fail at render time with a clear
message instead of at apply time.
*/}}
{{- define "splunk-enterprise.validateNoah" -}}
{{- if .Values.noahCluster.enabled -}}
{{- if not .Values.sva.c3.enabled -}}
{{- fail "noahCluster is only available with sva.c3: set sva.c3.enabled=true" -}}
{{- end -}}
{{- if or .Values.clusterManager.enabled .Values.existingClusterManager -}}
{{- fail "noahCluster cannot be combined with clusterManager or existingClusterManager, and is only available with sva.c3: the Noah topology has no cluster manager" -}}
{{- end -}}
{{- /* noahClusterRef has no namespace field: the operator looks the NoahCluster up in the namespace of the referencing cluster, so they must match. */ -}}
{{- $noahNamespace := default .Release.Namespace .Values.noahCluster.namespaceOverride -}}
{{- $indexerNamespace := default .Release.Namespace .Values.indexerCluster.namespaceOverride -}}
{{- if ne $noahNamespace $indexerNamespace -}}
{{- fail (printf "the NoahCluster is in namespace %q but the IndexerCluster is in namespace %q: noahClusterRef has no namespace, so they must match. Set noahCluster.namespaceOverride and indexerCluster.namespaceOverride to the same value" $noahNamespace $indexerNamespace) -}}
{{- end -}}
{{- $searchHeadNamespace := default .Release.Namespace .Values.searchHeadCluster.namespaceOverride -}}
{{- if ne $noahNamespace $searchHeadNamespace -}}
{{- fail (printf "the NoahCluster is in namespace %q but the SearchHeadCluster is in namespace %q: noahClusterRef has no namespace, so they must match. Set noahCluster.namespaceOverride and searchHeadCluster.namespaceOverride to the same value" $noahNamespace $searchHeadNamespace) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Splunk configuration that indexers need when they use Noah. The restmap stanzas make the
instance-to-instance calls authenticate with the noahService key; without them the search head's
POST /services/admin/certificates to an indexer returns 401 and every indexer is excluded from
searches. Merged under the user's indexerCluster.defaults, which take precedence.
*/}}
{{- define "splunk-enterprise.noahIndexerDefaults" -}}
splunk:
  conf:
    restmap:
      directory: /opt/splunk/etc/system/local
      content:
        "admin:certificates":
          authKeyStanza: noahService
          match: /admin/certificates
        "streams:search":
          match: /streams/search
          capability: search
          authKeyStanza: noahService
        "streams:rdin":
          match: /streams/rdin
          authKeyStanza: parallelreduce noahService
{{- end }}
