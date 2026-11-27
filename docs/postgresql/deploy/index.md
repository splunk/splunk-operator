---
title: Deploy PostgreSQL
nav_order: 10
has_children: true
---

# Deploy PostgreSQL

Use this guide to enable shared PostgreSQL on a Kubernetes cluster you administer or have permission to use. Start with an existing cluster. This guide does not cover a Splunk-operated database service.

## Shared responsibility model

TODO: Publish the shared responsibility model after review and agreement. The deployment steps below describe actions, not support commitments or ownership assignments.

## Choose the deployment configuration

Before you install the controllers, choose an artifact source and a webhook certificate. Decide whether you need backups so you know whether to install Barman. Before you enable the shared cluster, choose its capacity, placement, and connection policy.

| Decision | What to configure |
| --- | --- |
| Webhook certificate | Use cert-manager or [supply your own certificate](../../deploy/Helm.md#prerequisite-cert-manager). Kubernetes uses this certificate to verify the webhook that checks resource requests. |
| PostgreSQL server certificate | The shared cluster uses the default certificates from CloudNativePG (CNPG) for database connections. |
| Backups and recovery | [Configure shared PostgreSQL backups](provision.md#configure-backups-before-enabling-the-cluster). Choose the destination, credentials, schedule, and retention before enabling the cluster. |
| Initial cluster sizing | [Size the cluster](cluster-policy.md#size-the-cluster-before-provisioning) for instance count, CPU, memory, storage, and connections. Choose a StorageClass and check node capacity. |
| Connections | Decide whether the application needs PgBouncer. If it does, choose a mode that supports the driver's behavior. Review `sharedPostgres.connectionPooler` before enabling the cluster. |
| Monitoring | Prepare a metrics collector. See the [dashboard requirements](../operate/observability/postgresql-observability-dashboard.md#assumptions) and [custom-metrics guide](../operate/observability/custom-metrics.md). |

Start with [controller installation](install.md) to choose an artifact source. Use the [release catalog and verification guide](public-artifacts.md) when you need exact versions or verification steps. Retention on deletion is not a backup.

## Follow the deployment path

1. [Prepare the controllers](install.md). Use bundled CNPG or reuse a compatible installation.
2. [Size the shared cluster](cluster-policy.md#size-the-cluster-before-provisioning) and [choose its class policy in Helm](cluster-policy-reference.md#choose-shared-cluster-policy). Set the storage class and placement before you enable the cluster.
3. [Configure backups](provision.md#configure-backups-before-enabling-the-cluster), then [enable shared PostgreSQL](provision.md#enable-the-shared-helm-cluster) in the same Helm release.
4. [Check the shared deployment](verify.md). If you configured backups, test recovery. If you configured monitoring, make sure that your collector receives metrics.

If both controllers are ready, start at step 2. If the Helm-owned cluster already exists, start at step 4. Do not create a separate cluster or application database as part of Deploy.

## After deployment

After the shared cluster is ready and the applicable checks pass, use [Operate PostgreSQL](../operate/index.md) to maintain it. If setup fails, start with the diagnostics on the page for the failed step.
