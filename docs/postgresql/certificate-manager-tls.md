# cert-manager TLS for PostgresCluster

`PostgresClusterClass` is immutable. Select cert-manager when creating the
class; migrate by creating a new class and a new `PostgresCluster`, rather than
changing an existing class.

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

The class must name a readable cert-manager `Issuer` or `ClusterIssuer`. The
admission webhook rejects a missing issuer reference, unsupported key usages,
durations shorter than one hour, renewal windows shorter than five minutes, and
a `renewBefore` value that is not shorter than the configured duration (or
cert-manager's default 90-day duration). The controller fails closed if the
Certificate API is not installed, the issuer is not ready, or the issued Secret
does not contain valid TLS material and all required DNS names.

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
  class: postgres-cert-manager
  tls:
    serverAltDNSNames:
      - postgres.internal.example
```

During blue/green operations, generated service and pooler names use the
authoritative CNPG environment name. For example, if the active environment is
`orders-green`, the certificate includes `orders-green-rw` and
`orders-green-pooler-*` names.

The operator manages a Certificate and leaf Secret named
`<cluster>-server-tls`. It copies only `ca.crt` into the Opaque Secret
`<cluster>-server-ca`; this is the Secret advertised by the existing
`SERVER_CA_SECRET_REF` connection ConfigMap entry. Certificate material is
never copied into `PostgresCluster.status`.

## Reconciliation and readiness

The controller first reconciles the TLS workflow. In `certManager` mode it
validates the issuer, Certificate, issued leaf Secret, required DNS names, and
the CA-only Secret before reporting `CertificatesReady`. It then produces an
internal TLS plan; the plan contains no Kubernetes client or runtime I/O.

`clusterModel` is the only writer of the CNPG `Cluster` spec. It applies that
plan to the in-memory desired CNPG spec and, after CNPG reconciliation,
compares CNPG status with the plan. `ClusterReady` waits for this adoption in
cert-manager mode. With `cnpgDefault`, CNPG reports the selected leaf and CA
Secret names, which are then used by the same downstream path.

The pooler validates the CNPG-selected leaf certificate against the plan's
required pooler DNS names. The connection ConfigMap publishes
`SERVER_CA_SECRET_REF` only after the resolved CA Secret exists and contains
`ca.crt`; that publication is owned by `ConfigMapsReady`. These conditions are
intentionally separate so waiting for CNPG adoption or ConfigMap publication
does not block the earlier TLS workflow.

If a pooler is temporarily disabled, its previously issued pooler DNS names
remain on the Certificate to avoid an unnecessary certificate rotation. They
are recorded in the Certificate annotation
`enterprise.splunk.com/retained-pooler-sans` and disappear when the cluster is
permanently deleted with its Certificate.

For delete-style cleanup, the Certificate remains controller-owned and is
garbage-collected with the PostgresCluster. When
`clusterDeletionPolicy: Retain` is selected, the finalizer removes that owner
reference instead, allowing cert-manager to continue renewing the leaf Secret
for the retained CNPG cluster. The CA-only Secret is retained by default. Set
`secretRetentionPolicy: Delete` to remove that copied CA Secret during
delete-style finalization; Retain preserves it because the retained CNPG
cluster continues to reference it. The controller does not delete the
cert-manager leaf Secret directly.
