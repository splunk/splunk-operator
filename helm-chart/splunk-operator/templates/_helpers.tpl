{{/*
Expand the name of the chart.
*/}}
{{- define "splunk-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "splunk-operator.fullname" -}}
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
Create standard operator fullname for uniform installation across Helm and manifest
*/}}
{{- define "splunk-operator.operator.fullname" -}}
{{- default "splunk-operator" .Values.splunkOperator.nameOverride }}
{{- end }}

{{/*
Create the name of the service account to use for splunk operator
*/}}
{{- define "splunk-operator.operator.serviceAccountName" -}}
{{- printf "%s-%s"  (include "splunk-operator.operator.fullname" .) "controller-manager" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "splunk-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "splunk-operator.labels" -}}
helm.sh/chart: {{ include "splunk-operator.chart" . }}
{{ include "splunk-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "splunk-operator.selectorLabels" -}}
control-plane: controller-manager
app.kubernetes.io/name: {{ include "splunk-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Define namespace of release and allow for namespace override
*/}}
{{- define "splunk-operator.namespace" -}}
{{- $namespace := default .Release.Namespace .Values.namespaceOverride -}}
{{- if and (not .Values.splunkOperator.clusterWideAccess) (ne $namespace .Release.Namespace) -}}
{{- fail "namespaceOverride must match the Helm release namespace when splunkOperator.clusterWideAccess is false; install with --namespace set to the operator namespace" -}}
{{- end -}}
{{- $namespace -}}
{{- end }}

{{/*
Format splunkOperator.featureGates map into a --feature-gates arg value.
Produces: Key1=true,Key2=false
*/}}
{{- define "splunk-operator.featureGates" -}}
{{- $pairs := list -}}
{{- range $gate, $enabled := (include "splunk-operator.effectiveFeatureGates" . | fromJson) -}}
{{- $pairs = append $pairs (printf "%s=%t" $gate $enabled) -}}
{{- end -}}
{{- join "," $pairs -}}
{{- end }}

{{/*
Feature gate configuration in operator application order, with shared-service
defaults added only after resolving explicit choices.
The operator applies three passes in order, each starting from the state the last
left (k8s.io/component-base featuregate): ENABLE_VALIDATION_WEBHOOK=true in
extraEnvs (deprecated, sets ValidationWebhook); FEATURE_GATES in extraEnvs;
splunkOperator.featureGates (rendered as --feature-gates). In each pass a key
naming the gate sets it, and All<stage> (AllAlpha, AllBeta) sets it only if no
earlier pass has. A gate nothing sets keeps its default.
*/}}
{{- define "splunk-operator.featureGateConfiguration" -}}
{{- $values := .Values -}}
{{- $truthy := list "1" "t" "true" -}}
{{- $legacy := dict -}}
{{- $env := dict -}}
{{- range ($values.extraEnvs | default list) -}}
{{- if and (has .name (list "FEATURE_GATES" "ENABLE_VALIDATION_WEBHOOK")) (hasKey . "valueFrom") -}}
{{- fail (printf "extraEnvs %s must set value, not valueFrom: the chart reads it to decide which ValidatingWebhookConfigurations to render; set splunkOperator.featureGates instead" .name) -}}
{{- end -}}
{{- /* A later env entry with the same name replaces an earlier one. */}}
{{- if eq .name "ENABLE_VALIDATION_WEBHOOK" -}}
{{- $legacy = dict -}}
{{- if eq (toString .value) "true" -}}
{{- $_ := set $legacy "ValidationWebhook" true -}}
{{- end -}}
{{- end -}}
{{- if eq .name "FEATURE_GATES" -}}
{{- $env = dict -}}
{{- range splitList "," (toString .value) -}}
{{- $pair := splitList "=" (trim .) -}}
{{- if eq (len $pair) 2 -}}
{{- $_ := set $env (trim (first $pair)) (has (lower (trim (last $pair))) $truthy) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $flags := deepCopy ($values.splunkOperator.featureGates | default dict) -}}
{{- $passes := list $legacy $env $flags -}}
{{- $required := dict -}}
{{- if $values.sharedKVService.enabled -}}
{{- $_ := set $required "KVService" "sharedKVService" -}}
{{- $_ := set $required "PostgresController" "sharedKVService" -}}
{{- end -}}
{{- if $values.sharedPostgres.enabled -}}
{{- $_ := set $required "PostgresController" "sharedPostgres" -}}
{{- end -}}
{{- range $gate, $workload := $required -}}
{{- $state := include "splunk-operator.resolveFeatureGate" (dict "passes" $passes "gate" $gate "stage" "Alpha" "default" false) | fromJson -}}
{{- if not $state.isSet -}}
{{- $_ := set $flags $gate true -}}
{{- end -}}
{{- end -}}
{{- toJson (dict "passes" $passes "required" $required) -}}
{{- end }}

{{/* Resolve one gate without parsing configuration or applying workload defaults. */}}
{{- define "splunk-operator.resolveFeatureGate" -}}
{{- $gate := .gate -}}
{{- $all := printf "All%s" .stage -}}
{{- $truthy := list "1" "t" "true" -}}
{{- $isSet := false -}}
{{- $enabled := .default -}}
{{- range .passes -}}
{{- if hasKey . $gate -}}
{{- $enabled = has (lower (toString (index . $gate))) $truthy -}}
{{- $isSet = true -}}
{{- else if and (not $isSet) (hasKey . $all) -}}
{{- $enabled = has (lower (toString (index . $all))) $truthy -}}
{{- $isSet = true -}}
{{- end -}}
{{- end -}}
{{- toJson (dict "isSet" $isSet "enabled" $enabled) -}}
{{- end }}

{{/*
"true" when a gate is on, as the operator will see it. Webhooks and RBAC must
agree with the operator. Call with (dict "ctx" . "gate" "<name>" "stage"
"<Alpha|Beta|GA>" "default" <bool>); stage and default must match
defaultFeatureGates in pkg/config/featuregates.go.
*/}}
{{- define "splunk-operator.featureGateEnabled" -}}
{{- $config := include "splunk-operator.featureGateConfiguration" .ctx | fromJson -}}
{{- $state := include "splunk-operator.resolveFeatureGate" (dict "passes" $config.passes "gate" .gate "stage" .stage "default" .default) | fromJson -}}
{{- if $state.enabled -}}
true
{{- end -}}
{{- end }}

{{/*
"true" when the CrossVersionFieldProtection gate is on. Beta, defaults on.
*/}}
{{- define "splunk-operator.crossVersionFieldProtectionEnabled" -}}
{{- include "splunk-operator.featureGateEnabled" (dict "ctx" . "gate" "CrossVersionFieldProtection" "stage" "Beta" "default" true) -}}
{{- end }}

{{/*
clientConfig.caBundle for the ValidatingWebhookConfigurations when cert-manager
is not injecting it: splunkOperator.webhook.caBundle, base64-encoded.
Templates cannot parse X.509, so this checks structure only: the value must be
nothing but complete PEM certificate blocks whose bodies are base64-encoded DER.
A bundle the API server cannot use would block every write the webhooks match.
*/}}
{{- define "splunk-operator.webhookCABundle" -}}
{{- $ca := .Values.splunkOperator.webhook.caBundle | default "" | replace "\r\n" "\n" | trim -}}
{{- if not $ca -}}
{{- fail "splunkOperator.webhook.caBundle is required when splunkOperator.webhook.useCertManager is false: pass the PEM CA that signed the serving certificate, e.g. --set-file splunkOperator.webhook.caBundle=ca.crt" -}}
{{- end -}}
{{- $block := "-----BEGIN CERTIFICATE-----\n[A-Za-z0-9+/=\n]+\n-----END CERTIFICATE-----" -}}
{{- if not (regexMatch (printf "^%s(\\s*%s)*$" $block $block) $ca) -}}
{{- fail "splunkOperator.webhook.caBundle must contain only complete PEM certificates, each from -----BEGIN CERTIFICATE----- to -----END CERTIFICATE-----; pass the file itself, not base64" -}}
{{- end -}}
{{- range regexFindAll $block $ca -1 -}}
{{- $body := . | trimPrefix "-----BEGIN CERTIFICATE-----" | trimSuffix "-----END CERTIFICATE-----" | nospace -}}
{{- /* A DER certificate is an ASN.1 SEQUENCE, so its first byte is 0x30, the character "0". */}}
{{- if or (not $body) (ne (b64dec $body | b64enc) $body) (not (hasPrefix "0" (b64dec $body))) -}}
{{- fail "splunkOperator.webhook.caBundle contains a PEM certificate whose body is not a base64-encoded certificate" -}}
{{- end -}}
{{- end -}}
{{- printf "%s\n" $ca | b64enc -}}
{{- end }}

{{/*
"true" when the ValidationWebhook gate is on. Alpha, defaults off.
*/}}
{{- define "splunk-operator.validationWebhookEnabled" -}}
{{- include "splunk-operator.featureGateEnabled" (dict "ctx" . "gate" "ValidationWebhook" "stage" "Alpha" "default" false) -}}
{{- end }}

{{/*
splunkOperator.watchNamespaces with whitespace and empty entries removed, e.g.
"ns1, ns2," becomes "ns1,ns2". The operator does not trim WATCH_NAMESPACE, so
the Deployment and the webhook namespaceSelector must both use this.
*/}}
{{- define "splunk-operator.watchNamespaces" -}}
{{- $namespaces := list -}}
{{- range splitList "," (.Values.splunkOperator.watchNamespaces | default "") -}}
{{- if trim . -}}
{{- $namespaces = append $namespaces (trim .) -}}
{{- end -}}
{{- end -}}
{{- join "," $namespaces -}}
{{- end }}

{{/*
namespaceSelector for the ValidatingWebhookConfigurations, limited to the
namespaces the operator watches. Empty when it watches every namespace.
*/}}
{{- define "splunk-operator.webhookNamespaceSelector" -}}
{{- $watchNamespaces := include "splunk-operator.watchNamespaces" . -}}
{{- if not .Values.splunkOperator.clusterWideAccess }}
{{- /* Release namespace, not namespaceOverride: it is WATCH_NAMESPACE and where the namespaced Role is. */}}
namespaceSelector:
  matchLabels:
    kubernetes.io/metadata.name: {{ .Release.Namespace }}
{{- else if $watchNamespaces }}
namespaceSelector:
  matchExpressions:
  - key: kubernetes.io/metadata.name
    operator: In
    values:
{{- range splitList "," $watchNamespaces }}
    - {{ . }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Give namespace-mode webhook configurations a release-namespace suffix so
multiple cluster-scoped configurations can coexist and each release can own
its configuration and webhook Service independently.
*/}}
{{- define "splunk-operator.webhookConfigurationName" -}}
{{- $baseName := printf "%s-%s" (include "splunk-operator.operator.fullname" .ctx) .suffix -}}
{{- if .ctx.Values.splunkOperator.clusterWideAccess -}}
{{- $baseName -}}
{{- else -}}
{{- printf "%s-%s" $baseName .ctx.Release.Namespace -}}
{{- end -}}
{{- end -}}

{{/* PostgreSQL RBAC follows its gate, independent of bundling. */}}
{{- define "splunk-operator.postgresEnabled" -}}
{{- include "splunk-operator.featureGateEnabled" (dict "ctx" . "gate" "PostgresController" "stage" "Alpha" "default" false) -}}
{{- end }}

{{- define "splunk-operator.postgresNamespacedRules" -}}
- apiGroups: [platform.splunk.com]
  resources: [postgresclusters, postgresdatabases]
  verbs: [get, list, watch, create, update, patch, delete]
- apiGroups: [platform.splunk.com]
  resources: [postgresclusters/status]
  verbs: [get, update, patch]
- apiGroups: [platform.splunk.com]
  resources: [postgresdatabases/status]
  verbs: [get, list, watch, update, patch]
- apiGroups: [platform.splunk.com]
  resources: [postgresclusters/finalizers, postgresdatabases/finalizers]
  verbs: [update]
- apiGroups: [postgresql.cnpg.io]
  resources: [clusters, databases, poolers, backups, scheduledbackups, publications, subscriptions]
  verbs: [get, list, watch, create, update, patch, delete]
- apiGroups: [postgresql.cnpg.io]
  resources: [clusters/status, poolers/status, backups/status, scheduledbackups/status, publications/status, subscriptions/status]
  verbs: [get]
- apiGroups: [barmancloud.cnpg.io]
  resources: [objectstores]
  verbs: [get, list, watch, create, update, patch, delete]
- apiGroups: [snapshot.storage.k8s.io]
  resources: [volumesnapshots]
  verbs: [get, list, patch]
- apiGroups: [networking.k8s.io]
  resources: [networkpolicies]
  verbs: [get, list, watch]
{{- end }}

{{- define "splunk-operator.postgresClusterRules" -}}
- apiGroups: [platform.splunk.com]
  resources: [postgresclusterclasses]
  verbs: [get, list, watch]
{{- end }}

{{/* Render only CLI choices and genuinely unset required controller gates. */}}
{{- define "splunk-operator.effectiveFeatureGates" -}}
{{- $config := include "splunk-operator.featureGateConfiguration" . | fromJson -}}
{{- range $gate, $workload := $config.required -}}
{{- $state := include "splunk-operator.resolveFeatureGate" (dict "passes" $config.passes "gate" $gate "stage" "Alpha" "default" false) | fromJson -}}
{{- if not $state.enabled -}}
{{- fail (printf "%s.enabled requires the %s feature gate" $workload $gate) -}}
{{- end -}}
{{- end -}}
{{- toJson (index $config.passes 2) -}}
{{- end -}}
