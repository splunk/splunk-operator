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

A missing class is created before cluster admission by a pre-install/pre-upgrade
hook and retained independently of later release manifests. Existing classes are
checked for ownership and policy but never recreated or updated. With validation
already active, enable PostgresController and wait for operator readiness before
enabling shared provisioning.

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
on uninstall. Disabling `sharedPostgres.enabled` after provisioning is rejected
to preserve Helm rollback; uninstall the release to retain its data instead.
An empty release-state Secret in the Helm release namespace enforces this guard
independently of `namespaceOverride`; it is removed on uninstall.
Keep rollback targets at or after the revision that enabled provisioning: Helm
rollback reuses stored manifests and cannot run this upgrade guard.
Restore-only releases still need the object-store destination and credentials
when using `bootstrapFrom.objectStorage` or snapshot `walArchive` recovery, even
with scheduled backups disabled. Select storage, placement and backup policy
before installation.
