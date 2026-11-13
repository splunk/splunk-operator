---
title: Shared PostgreSQL with Helm
parent: PostgreSQL
nav_order: 0.3
---

# Shared PostgreSQL with Helm

`sharedPostgres.enabled` provisions one release-owned PostgreSQL cluster. It is
opt-in; Enterprise-only installations keep it disabled.

## Prerequisites

Install the SOK CRDs matching this build using the existing
[CRD procedure](../deploy/Helm.md#splunk-operator-helm-chart-repository), and install
a compatible ready [cert-manager](../deploy/Helm.md#prerequisite-cert-manager).
Defaults need three eligible nodes and 100Gi storage per PostgreSQL instance,
with 2 CPU/8Gi memory requests per instance. Select suitable storage and capacity.
From source, run `make helm-package` before installing.

## Installation

A missing class is created by a Helm pre-install/pre-upgrade hook before cluster
admission. Existing classes remain ordinary retained resources; upgrades never
delete and recreate them.

Create a protected `shared-values.yaml`:

```yaml
splunkOperator:
  featureGates:
    PostgresController: true
cloudnative-pg: {enabled: true}
plugin-barman-cloud: {enabled: true}
cert-manager: {enabled: false}
sharedPostgres:
  enabled: true
  storageClass: encrypted-ssd
  backup:
    enabled: true
    destinationPath: s3://customer-backups/postgres/
  databases:
    - name: application
```

After accepting the terms in [the installation guide](../README.md), supply your
accepted `SPLUNK_GENERAL_TERMS`. Set `BACKUP_ACCESS_KEY_FILE` and
`BACKUP_SECRET_KEY_FILE` to protected files containing your storage credentials.

```bash
: "${SPLUNK_GENERAL_TERMS:?Supply your accepted terms value}"
helm install shared ./helm-chart/splunk-operator \
  --namespace postgres-system --create-namespace --values shared-values.yaml \
  --set-string splunkOperator.splunkGeneralTerms="$SPLUNK_GENERAL_TERMS" \
  --set-file sharedPostgres.backup.credentials.accessKeyId="$BACKUP_ACCESS_KEY_FILE" \
  --set-file sharedPostgres.backup.credentials.secretAccessKey="$BACKUP_SECRET_KEY_FILE" \
  --wait --timeout 15m
```

Protect values, rendered Secrets and Helm release storage. Use an existing
credential Secret through `sharedPostgres.backup.credentials.existingSecret` if
managed externally. Omitting backups requires `backup.acknowledgeNoBackups: true`.
PG17 is the default; PG18 is an explicit initial-install option. Class policy is
immutable, major changes are guarded, and database data/credentials are retained
on uninstall. Select storage, placement and backup policy before installation.
