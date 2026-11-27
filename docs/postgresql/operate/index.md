---
title: Operate PostgreSQL
nav_order: 11
has_children: true
---

# Operate PostgreSQL

Start here when a PostgreSQL cluster is running under the Splunk Operator. If you are setting up the shared cluster for the first time, start with [Deploy PostgreSQL](../deploy/index.md).

| What you need to do | Start here | You are done when |
|---|---|---|
| Check backup coverage, rehearse a restore, or recover data after a failure | [Protect and recover data](protection/index.md) | Backups cover the recovery window, or the application works on the recovered cluster. |
| Change instance count, CPU, memory, storage, or PostgreSQL version | [Scale and upgrade](maintenance/index.md) | The cluster is ready at the requested size or version, and clients work after the change. |
| Check health, find the cause of an incident, or add metrics | [Monitor and diagnose](observability/index.md) | You know which component needs attention, and its health signals return to normal after the fix. |
| Fix database access, inspect role Secrets, or unblock a failed database | [Manage database access](access/index.md) | The intended role can connect with the correct credentials and permissions. |
| Take a cluster out of service | [Retire a cluster](retire.md) | No client writes to the old cluster, and you know what data and backups remain. |

## Shared responsibility model

TODO: Publish the operating shared responsibility model after review and agreement. The procedures below describe actions, not support commitments or ownership assignments.

## Find the cluster you intend to change

Make sure that your Kubernetes context and namespace match the running deployment. If Helm owns the shared cluster, [find it by release label](../deploy/provision.md#enable-the-shared-helm-cluster). Reuse the same release and values for changes that the chart supports.

A status check works for both Helm-owned and independently managed clusters. A resource example that edits a `PostgresCluster` or its class does not replace the Helm values. Do not edit the CloudNativePG cluster directly to make a change that the Splunk Operator manages.
