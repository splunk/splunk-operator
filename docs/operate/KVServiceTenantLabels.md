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
```

`platform.splunk.com/tenant` optionally sets the tenant name for the namespace.
This label is independent of KV Service and applies wherever the operator needs
to identify a tenant. If it is absent or empty, the namespace name is used.

`platform.splunk.com/kvservice: enabled` opts the namespace into KV Service
discovery. It is not required to set a tenant name. The operator's service
account can read and list namespaces to support discovery. The operator only
accepts tenant names for namespaces included in its `WATCH_NAMESPACE` scope; an
empty `WATCH_NAMESPACE` means all namespaces are watched.
