---
title: Scale and upgrade PostgreSQL
parent: Operate PostgreSQL
nav_order: 2
has_children: true
---

# Scale and upgrade PostgreSQL

Choose what to change before opening a maintenance window. Start with a healthy cluster and [usable backups](../protection/index.md). Record the current instance count, storage size, or image reference. Make sure that the application can tolerate the interruption described in the chosen procedure.

| What needs to change | Procedure | What to watch |
|---|---|---|
| Number of instances | [Add or remove PostgreSQL instances](scaling-out.md) | Read-only endpoints need at least two ready instances. |
| CPU, memory, or volume size | [Change resources or expand storage](scaling-up.md) | CPU and memory changes restart instances. Storage cannot shrink. |
| Patch version within the same major version | [Upgrade within one major version](minor-version-upgrade.md) | Change the pinned image. A version selector alone does not change it. |
| Major version | [Choose a major upgrade strategy](major-version-upgrade.md#bluegreen-strategy) | The in-place strategy needs a write outage. Blue/green has its own prerequisites, handoff, and cleanup. |

## If Helm owns the shared cluster

Use the same release for chart-managed changes. Set `sharedPostgres.instances`, `sharedPostgres.resources`, or `sharedPostgres.storage` in the saved values. Use the [same Helm command and values files](../../deploy/provision.md#enable-the-shared-helm-cluster) that you use to manage the shared cluster. For a minor upgrade, change the pinned image under the selected major in `sharedPostgres.images.postgresql`. Keep `sharedPostgres.postgresVersion` at the current major.

If you scale down to one instance, move clients off the read-only endpoint first. Set `sharedPostgres.instances: 1` and `sharedPostgres.connectionPooler.readOnly: false` in the same Helm upgrade. Admission rejects a read-only pooler with one instance. Wait for `Ready`, then test the clients that you moved.

Use the operation pages for checks and expected behavior. Their direct resource edits are for independently managed resources. The chart renders `postgresMajorUpgradeConfig.allow: false` and does not expose the major upgrade workflow. Do not change the Helm major version or copy the direct API steps as a Helm upgrade procedure.

## Finish the change

Use the monitoring section in the chosen procedure. Make sure that the cluster is `Ready` and that the instance count, volume sizes, or running version match the requested change. Test the application path that the change could interrupt. If the change does not complete, [use status and events](../observability/index.md) before trying a second change.
