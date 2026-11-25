---
title: PostgreSQL
parent: Operate & Manage
nav_order: 9
has_children: true
permalink: /docs/postgresql/
---

These guides describe how to deploy and operate PostgreSQL databases with the Splunk Operator.
They cover backups, upgrades, TLS connections, database roles, and observability.

Start with [Deploy PostgreSQL](deploy/index.md) to enable and check the shared cluster.
Use [Public PostgreSQL artifacts](deploy/public-artifacts.md) to download and verify
the database images and backend operator charts from Docker Hub.
Use [Custom PostgreSQL metrics](operate/observability/custom-metrics.md) to publish SQL query results for collection.

## Design documentation

- [Architecture overview](architecture-overview.md) describes reconciliation and resource lifecycles.
