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
{{- default .Release.Namespace .Values.namespaceOverride }}
{{- end }}

{{/*
Format splunkOperator.featureGates map into a --feature-gates arg value.
Produces: Key1=true,Key2=false
*/}}
{{- define "splunk-operator.featureGates" -}}
{{- $pairs := list -}}
{{- range $gate, $enabled := .Values.splunkOperator.featureGates -}}
{{- $pairs = append $pairs (printf "%s=%t" $gate $enabled) -}}
{{- end -}}
{{- join "," $pairs -}}
{{- end }}

{{/*
"true" when a feature gate is on, as the operator will see it. The chart renders a
failurePolicy Fail webhook only while its gate is on, so it must agree with the
operator however the gate was set, or matching writes are rejected.
The operator applies three passes in order, each starting from the state the last
left (k8s.io/component-base featuregate): ENABLE_VALIDATION_WEBHOOK=true in
extraEnvs (deprecated, sets ValidationWebhook); FEATURE_GATES in extraEnvs;
splunkOperator.featureGates (rendered as --feature-gates). In each pass a key
naming the gate sets it, and All<stage> (AllAlpha, AllBeta) sets it only if no
earlier pass has. A gate nothing sets keeps its default.
Call with (dict "ctx" . "gate" "<name>" "stage" "<Alpha|Beta|GA>" "default" <bool>);
stage and default must match defaultFeatureGates in pkg/config/featuregates.go.
*/}}
{{- define "splunk-operator.featureGateEnabled" -}}
{{- $gate := .gate -}}
{{- $all := printf "All%s" .stage -}}
{{- $values := .ctx.Values -}}
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
{{- $flags := dict -}}
{{- range $name, $value := ($values.splunkOperator.featureGates | default dict) -}}
{{- $_ := set $flags $name (has (lower (toString $value)) $truthy) -}}
{{- end -}}
{{- $isSet := false -}}
{{- $enabled := .default -}}
{{- range (list $legacy $env $flags) -}}
{{- if hasKey . $gate -}}
{{- $enabled = index . $gate -}}
{{- $isSet = true -}}
{{- else if and (not $isSet) (hasKey . $all) -}}
{{- $enabled = index . $all -}}
{{- $isSet = true -}}
{{- end -}}
{{- end -}}
{{- if $enabled -}}
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

