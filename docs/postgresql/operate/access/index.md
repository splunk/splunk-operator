---
title: Manage database access
parent: Operate PostgreSQL
nav_order: 4
has_children: true
---

# Manage database access

Use this section for a database that already exists. If you just restored the cluster, [prepare access to the restored cluster](../protection/disaster-recovery.md#prepare-access-to-the-restored-cluster) instead of reusing the source credentials. The restore disables unmanaged login roles from the backup.

## Find the access failure

| What you see | What to check |
|---|---|
| The client cannot reach the database | Make sure that it uses the current endpoint. If you scaled down, [check read-only endpoint availability](../maintenance/scaling-out.md#read-only-endpoint-unavailability-when-fewer-than-two-instances-are-ready). |
| The client reports a certificate or hostname error | Check the trust chain and the hostname that the client uses. Use the database server certificate, not the operator webhook certificate, to diagnose this connection. Do not disable TLS verification. |
| Login or privileges fail | [Check the managed roles, Secret state, and reconciliation conditions](postgresdatabase-managed-roles.md#reconciliation-status). |
| `PostgresDatabase` reports a terminal failure | [Fix the cause and let the operator retry](postgresdatabase-privileges-retry.md#fix-the-underlying-issue). Check the conditions before changing the resource. |

Finish with a connection through the application's driver. Make sure that it uses the expected endpoint and role and that the allowed operation succeeds.
