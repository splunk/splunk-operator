{{- define "splunk-operator.sharedKVServicePostgresName" -}}
{{- if .Values.sharedPostgres.enabled -}}
{{- if .Values.sharedKVService.postgres.clusterRef.name -}}
{{- fail "set sharedKVService.postgres.clusterRef.name only when sharedPostgres.enabled=false" -}}
{{- end -}}
{{- include "splunk-operator.sharedPostgresName" . -}}
{{- else -}}
{{- required "sharedKVService requires sharedPostgres.enabled=true or sharedKVService.postgres.clusterRef.name" .Values.sharedKVService.postgres.clusterRef.name -}}
{{- end -}}
{{- end -}}

{{- define "splunk-operator.sharedKVServiceReadinessName" -}}
{{- printf "%s-kvs-ready" (.Release.Name | trunc 48 | trimSuffix "-") -}}
{{- end -}}

{{- define "splunk-operator.sharedKVServiceValidate" -}}
{{- if not (include "splunk-operator.featureGateEnabled" (dict "ctx" . "gate" "KVService" "stage" "Alpha" "default" false)) -}}
{{- fail "sharedKVService.enabled requires the KVService feature gate" -}}
{{- end -}}
{{- if not (include "splunk-operator.postgresEnabled" .) -}}
{{- fail "sharedKVService.enabled requires the PostgresController feature gate" -}}
{{- end -}}
{{- $_ := required "sharedKVService.image must specify a qualified KVService release image" .Values.sharedKVService.image -}}
{{- $namespace := include "splunk-operator.namespace" . -}}
{{- $watched := include "splunk-operator.watchNamespaces" . -}}
{{- if and .Values.splunkOperator.clusterWideAccess $watched (not (has $namespace (splitList "," $watched))) -}}
{{- fail "splunkOperator.watchNamespaces must include the shared KVService namespace" -}}
{{- end -}}
{{- $existing := lookup "enterprise.splunk.com/v4" "KVService" $namespace .Values.sharedKVService.name -}}
{{- if $existing -}}
{{- $annotations := $existing.metadata.annotations | default dict -}}
{{- if or (ne (index $annotations "meta.helm.sh/release-name") .Release.Name) (ne (index $annotations "meta.helm.sh/release-namespace") .Release.Namespace) -}}
{{- fail "shared KVService belongs to another owner; do not adopt an existing KVService implicitly" -}}
{{- end -}}
{{- end -}}
{{- end -}}
