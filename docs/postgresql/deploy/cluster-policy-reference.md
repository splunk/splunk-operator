---
title: PostgreSQL cluster policy reference
parent: Deploy PostgreSQL
nav_order: 5
---

# Cluster policy reference

## Choose shared cluster policy

Start with the values you chose in [Size the cluster before provisioning](cluster-policy.md#size-the-cluster-before-provisioning). Helm creates the cluster class from the release values. Do not apply the API examples below as part of the shared deployment.

| Decision | What to set before the first Helm enablement |
| --- | --- |
| Storage | Set `sharedPostgres.storageClass` to a StorageClass that exists in your cluster. An empty Helm value uses the cluster default. This differs from an explicit empty API field on a class. |
| Placement | Review `sharedPostgres.affinity`. The default requires PostgreSQL instances on separate nodes. If you keep it, make sure that each instance has an eligible node. |
| Server certificates | Keep the CNPG certificate source that the shared chart supplies. The database certificate is separate from the operator webhook certificate. The Helm values do not expose a custom database issuer. |

Keep these choices in the release values. Next, [configure backups before enabling the cluster](provision.md#configure-backups-before-enabling-the-cluster).

The remaining sections describe the underlying API. You do not need to edit the class or cluster directly.

## Class storage and placement

A class can select a StorageClass and specify where Kubernetes places PostgreSQL Pods. The following example shows the API fields:

```yaml
spec:
  cnpg:
    storageClass: encrypted-ssd
    affinity:
      enablePodAntiAffinity: true
      podAntiAffinityType: required
      topologyKey: kubernetes.io/hostname
      nodeSelector:
        pool: database
      tolerations: []
```

If you omit `storageClass`, the operator keeps the existing storage selection. On the class API, `storageClass: ""` disables use of the default StorageClass for new PVCs.

If you omit `affinity`, the operator keeps the existing placement. With `affinity: {}`, it prefers to place Pods on different hosts, using `kubernetes.io/hostname`. It clears the class-owned node selector and tolerations. It keeps other affinity fields that the class does not manage.

If you enable ValidationWebhook, admission checks Kubernetes label syntax and CNPG tolerations. Use `tolerationSeconds` only with `NoExecute`. Use an empty value with `operator: Exists`.

You cannot edit a class policy after creation. A different policy requires a new class. A new class does not migrate existing volumes. The placement policy applies to PostgreSQL instances. PgBouncer uses the provider's placement policy. If the class requires separate hosts, each PostgreSQL instance needs an eligible node.

## Database certificate policy

Select cert-manager before you create the class. To change certificate sources later, use a new class and cluster. Plan the data migration before you replace the existing cluster.

Use CNPG-default certificates if you do not need a specific issuer. The cert-manager policy below concerns PostgreSQL server certificates, not the operator webhook certificate.

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: postgres-cert-manager
spec:
  provisioner: postgresql.cnpg.io
  cnpg: {}
  tls:
    certificates:
      mode: certManager
      issuerRef:
        name: platform-ca
        kind: ClusterIssuer
      duration: 2160h
      renewBefore: 360h
      secretRetentionPolicy: Retain
```

Name an `Issuer` or `ClusterIssuer` that the operator can read. Set a duration of at least one hour. Set `renewBefore` to at least five minutes and below the certificate duration. Cert-manager defaults to 90 days if you omit the duration. Admission rejects an invalid window, a missing issuer reference, or unsupported key usages.

The controller blocks provisioning if the Certificate API is missing or the issuer is not ready. It also blocks provisioning if the issued Secret lacks valid TLS material or a required DNS name.

For a cluster named `orders` in namespace `payments`, the certificate includes
the short, namespace, service, and cluster-local names for `orders-rw`,
`orders-ro`, and `orders-r`. Enabled poolers add the analogous
`orders-pooler-rw` and `orders-pooler-ro` names. Add application names at the
cluster level:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: orders
  namespace: payments
spec:
  images:
    postgresql:
      "17": ${TEST_PG17_IMAGE}
    pgbouncer: ${TEST_PGBOUNCER_IMAGE}
    # Optional Secrets must exist in this PostgresCluster's namespace.
    # imagePullSecrets:
    #   - name: registry-reader
  class: postgres-cert-manager
  tls:
    serverAltDNSNames:
      - postgres.internal.example
```

During blue/green operations, generated service and pooler names use the
authoritative CNPG environment name. For example, if the active environment is
`orders-green`, the certificate includes `orders-green-rw` and
`orders-green-pooler-*` names.

The operator manages a Certificate and its leaf Secret, both named `<cluster>-server-tls`. It copies only `ca.crt` into the Opaque Secret `<cluster>-server-ca`. The connection ConfigMap publishes this Secret's reference in `SERVER_CA_SECRET_REF`. The operator does not copy certificate material into `PostgresCluster.status`.

### Reconciliation and readiness

Use the cluster conditions to find the step that is waiting:

| Condition | What it means |
| --- | --- |
| `CertificatesReady` | In `certManager` mode, the operator checks the issuer, issued certificate, DNS names, and CA-only Secret. |
| `ClusterReady` | In `certManager` mode, the operator waits for CNPG to report that it uses the selected certificate. In `cnpgDefault` mode, CNPG reports its own leaf and CA Secret names. |
| `ConfigMapsReady` | The operator publishes `SERVER_CA_SECRET_REF` only after the CA Secret exists and contains `ca.crt`. |

If pooling is enabled, the operator also checks the selected certificate for the required pooler DNS names.

If you temporarily disable a pooler, the operator keeps its DNS names on the Certificate to prevent an unnecessary rotation. The operator records those names in `enterprise.splunk.com/retained-pooler-sans`. They disappear when Kubernetes deletes the Certificate with the cluster.

If cluster deletion removes the backend, Kubernetes deletes the operator-owned Certificate with the `PostgresCluster`. With `clusterDeletionPolicy: Retain`, the finalizer removes the Certificate's owner reference instead. Cert-manager can then continue to renew the leaf Secret for the retained CNPG cluster.

The operator keeps the CA-only Secret by default. Set `secretRetentionPolicy: Delete` to remove the copied CA Secret when deletion removes the backend. If the cluster policy is `Retain`, the operator keeps the CA Secret because the cluster still uses it. The controller does not delete the cert-manager leaf Secret directly.
