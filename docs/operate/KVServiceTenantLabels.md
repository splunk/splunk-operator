---
title: KV Service tenant labels
parent: Operate & Manage
nav_order: 11
---

# KV Service tenant labels

Tenant names and KV Service discovery are configured independently with labels
on Kubernetes Namespace objects. The `tenant` label identifies the tenant for
any operator feature that needs a tenant name. The `kvservice` label opts a
namespace into KV Service discovery only.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: sok-tenant
  labels:
    platform.splunk.com/kvservice: enabled
    platform.splunk.com/tenant: tenant
  annotations:
    platform.splunk.com/kvservice-addr: "splunk-shared-kvservice-service.backend:8066"
```

`platform.splunk.com/tenant` optionally sets the tenant name for the namespace.
This label is independent of KV Service and applies wherever the operator needs
to identify a tenant. If it is absent or empty, the namespace name is used.

`platform.splunk.com/kvservice: enabled` opts the namespace into KV Service
discovery. It is not required to set a tenant name. The operator's service
account can read, list, and watch namespaces to support discovery. The operator only
accepts tenant names for namespaces included in its `WATCH_NAMESPACE` scope; an
empty `WATCH_NAMESPACE` means all namespaces are watched.

`platform.splunk.com/kvservice-addr` sets the KVService client address for
Enterprise clients, for example `splunk-shared-kvservice-service.backend:8066`.
This selects KVService `backend/shared`, regardless of which workload namespace
contains the annotation. It is required when KVService is enabled and belongs
under the workload Namespace's `metadata.annotations`.

Only the short Service DNS form
`splunk-<kvservice-name>-kvservice-service.<hosting-namespace>:8066` is supported.
The Service name and port must match the operator-managed backend. Full DNS names
(including `.svc` and `.svc.cluster.local`), single-label names, IP addresses,
aliases, missing ports, and custom ports are rejected. Do not append a DNS suffix.

An explicit `http://` or `https://` scheme and an optional trailing root slash are
preserved; a bare address uses `http://`. Credentials, query strings, fragments,
and non-root paths are rejected. The shared parser returns the same backend
identity for client configuration, tenant discovery, and source-Secret routing.
Invalid address metadata is reported with the namespace and annotation to repair;
the supplied annotation value is not logged.

Changes to the enable or tenant label, or address annotation, update KVService consumers
automatically. Missing or empty address annotations in enabled namespaces cause
reconciliation errors. See
[Enterprise client configuration](KVService.md#enterprise-client-configuration).
