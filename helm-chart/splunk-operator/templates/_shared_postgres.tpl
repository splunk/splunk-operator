{{/* Include namespace and the full release identity in the hash: the class is cluster scoped. */}}
{{- define "splunk-operator.sharedPostgresName" -}}
{{- printf "%s-pg-%s" (.Release.Name | trunc 10 | trimSuffix "-") (printf "%s/%s" (include "splunk-operator.namespace" .) .Release.Name | sha256sum | trunc 10) -}}
{{- end -}}

{{- define "splunk-operator.sharedPostgresOwner" -}}
{{- $object := .object -}}
{{- if $object -}}
{{- $annotations := $object.metadata.annotations | default dict -}}
{{- if or (ne (index $annotations "meta.helm.sh/release-name") .ctx.Release.Name) (ne (index $annotations "meta.helm.sh/release-namespace") .ctx.Release.Namespace) -}}
{{- fail (printf "shared PostgreSQL resource %s belongs to another release; use its original release name and namespace" $object.metadata.name) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* Keep credentials while referenced by immutable policy, including restore-only releases. */}}
{{- define "splunk-operator.sharedPostgresObjectStore" -}}
{{- $cfg := .Values.sharedPostgres -}}
{{- if or $cfg.backup.enabled $cfg.backup.destinationPath (hasKey $cfg.bootstrapFrom "objectStorage") (hasKey ($cfg.bootstrapFrom.volumeSnapshot | default dict) "walArchive") -}}true{{- end -}}
{{- end -}}

{{- define "splunk-operator.sharedPostgresValidate" -}}
{{- $cfg := .Values.sharedPostgres -}}
{{- if not (include "splunk-operator.postgresEnabled" .) -}}
{{- fail "sharedPostgres.enabled requires the PostgresController feature gate" -}}
{{- end -}}
{{- $major := toString $cfg.postgresVersion -}}
{{- if not (has $major (list "17" "18")) -}}
{{- fail "sharedPostgres.postgresVersion must be 17 or 18" -}}
{{- end -}}
{{- if not (index $cfg.images.postgresql $major) -}}
{{- fail "sharedPostgres.images.postgresql must contain the selected PostgreSQL major version" -}}
{{- end -}}
{{- if and $cfg.monitoring.connectionPoolerMetrics (not $cfg.connectionPooler.enabled) -}}
{{- fail "sharedPostgres.monitoring.connectionPoolerMetrics requires connectionPooler.enabled" -}}
{{- end -}}
{{- if and $cfg.kvService.enabled (not (include "splunk-operator.featureGateEnabled" (dict "ctx" . "gate" "KVService" "stage" "Alpha" "default" false))) -}}
{{- fail "sharedPostgres.kvService.enabled requires the KVService feature gate" -}}
{{- end -}}
{{- if and $cfg.kvService.enabled (not $cfg.kvService.spec.image) -}}
{{- fail "sharedPostgres.kvService.spec.image must supply an approved KVService image" -}}
{{- end -}}
{{- if $cfg.backup.enabled -}}
{{- $_ := required "sharedPostgres.backup.schedule is required" $cfg.backup.schedule -}}
{{- else if not $cfg.backup.acknowledgeNoBackups -}}
{{- fail "configure sharedPostgres.backup.enabled or explicitly set sharedPostgres.backup.acknowledgeNoBackups=true" -}}
{{- end -}}
{{- if include "splunk-operator.sharedPostgresObjectStore" . -}}
{{- $_ := required "sharedPostgres.backup.destinationPath is required" $cfg.backup.destinationPath -}}
{{- if not (hasPrefix "s3://" $cfg.backup.destinationPath) -}}
{{- fail "sharedPostgres.backup.destinationPath must use an S3-compatible s3:// destination" -}}
{{- end -}}
{{- $creds := $cfg.backup.credentials -}}
{{- if eq $creds.accessKeyIdKey $creds.secretAccessKeyKey -}}
{{- fail "backup accessKeyIdKey and secretAccessKeyKey must be different" -}}
{{- end -}}
{{- if and $creds.existingSecret (or $creds.accessKeyId $creds.secretAccessKey) -}}
{{- fail "set existingSecret or backup credential values, not both" -}}
{{- end -}}
{{- if not $creds.existingSecret -}}
{{- $secret := lookup "v1" "Secret" (include "splunk-operator.namespace" .) (printf "%s-backup" (include "splunk-operator.sharedPostgresName" .)) -}}
{{- if not $secret -}}
{{- $_ := required "backup credentials.accessKeyId is required without existingSecret" $creds.accessKeyId -}}
{{- $_ := required "backup credentials.secretAccessKey is required without existingSecret" $creds.secretAccessKey -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $namespace := include "splunk-operator.namespace" . -}}
{{- $name := include "splunk-operator.sharedPostgresName" . -}}
{{- with lookup "v1" "Secret" .Release.Namespace (printf "%s-shared-postgres" .Release.Name) -}}
{{- include "splunk-operator.sharedPostgresOwner" (dict "ctx" $ "object" .) -}}
{{- $annotations := .metadata.annotations | default dict -}}
{{- $previousNamespace := required "shared PostgreSQL state is missing its backend namespace; verify the existing backend before continuing" (index $annotations "platform.splunk.com/shared-postgres-namespace") -}}
{{- $previousName := required "shared PostgreSQL state is missing its backend name; verify the existing backend before continuing" (index $annotations "platform.splunk.com/shared-postgres-name") -}}
{{- if or (ne $previousNamespace $namespace) (ne $previousName $name) -}}
{{- fail "shared PostgreSQL backend identity cannot change; restore namespaceOverride or use an explicit replacement/restore procedure" -}}
{{- end -}}
{{- end -}}
{{- $watched := include "splunk-operator.watchNamespaces" . -}}
{{- if and .Values.splunkOperator.clusterWideAccess $watched (not (has $namespace (splitList "," $watched))) -}}
{{- fail "splunkOperator.watchNamespaces must include the shared PostgreSQL namespace" -}}
{{- end -}}
{{- $cluster := lookup "platform.splunk.com/v1alpha1" "PostgresCluster" $namespace $name -}}
{{- include "splunk-operator.sharedPostgresOwner" (dict "ctx" . "object" $cluster) -}}
{{- if and $cluster (ne (toString $cluster.spec.postgresVersion) $major) -}}
{{- fail "shared PostgreSQL major-version changes require the documented explicit major-upgrade workflow; restore the existing sharedPostgres.postgresVersion value" -}}
{{- end -}}
{{- $class := lookup "platform.splunk.com/v1alpha1" "PostgresClusterClass" "" $name -}}
{{- include "splunk-operator.sharedPostgresOwner" (dict "ctx" . "object" $class) -}}
{{- if $class -}}
{{- $annotations := $class.metadata.annotations | default dict -}}
{{- if ne (index $annotations "platform.splunk.com/helm-class-policy") (include "splunk-operator.sharedPostgresClassPolicy" .) -}}
{{- fail "shared PostgreSQL class policy is immutable: keep storageClass, affinity, pooler policy and backup destination/Secret references unchanged, or restore into a replacement cluster" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* Operational overrides belong on PostgresCluster; this policy is immutable. */}}
{{- define "splunk-operator.sharedPostgresClassSpec" -}}
{{- $cfg := .Values.sharedPostgres -}}
{{- $name := include "splunk-operator.sharedPostgresName" . -}}
provisioner: postgresql.cnpg.io
config:
  postgresVersion: "17"
cnpg:
  # Restart supports both single-instance development and multi-instance HA clusters.
  primaryUpdateMethod: restart
  {{- if $cfg.storageClass }}
  storageClass: {{ $cfg.storageClass | quote }}
  {{- end }}
  affinity:
    {{- toYaml $cfg.affinity | nindent 4 }}
  connectionPooler:
    instances: {{ $cfg.connectionPooler.instances }}
    mode: {{ $cfg.connectionPooler.mode | quote }}
    config:
      {{- toYaml $cfg.connectionPooler.config | nindent 6 }}
  {{- if $cfg.backup.destinationPath }}
  backup:
    target: {{ $cfg.backup.target | quote }}
    barmanObjectStore:
      destinationPath: {{ $cfg.backup.destinationPath | quote }}
      {{- with $cfg.backup.endpointURL }}
      endpointURL: {{ . | quote }}
      {{- end }}
      retentionPolicy: {{ $cfg.backup.retentionPolicy | quote }}
      wal:
        {{- toYaml $cfg.backup.wal | nindent 8 }}
      s3Credentials:
        accessKeyId:
          name: {{ default (printf "%s-backup" $name) $cfg.backup.credentials.existingSecret | quote }}
          key: {{ $cfg.backup.credentials.accessKeyIdKey | quote }}
        secretAccessKey:
          name: {{ default (printf "%s-backup" $name) $cfg.backup.credentials.existingSecret | quote }}
          key: {{ $cfg.backup.credentials.secretAccessKeyKey | quote }}
  {{- end }}
{{- end -}}

{{- define "splunk-operator.sharedPostgresClassPolicy" -}}
{{- include "splunk-operator.sharedPostgresClassSpec" . | fromYaml | toJson | sha256sum -}}
{{- end -}}
