---
title: Connecting to PostgreSQL with TLS
parent: PostgreSQL
nav_order: 2
---

# Connecting to PostgreSQL with TLS

This guide describes how **application workloads** connect to a managed `**PostgresCluster`** using TLS: where to read **non-secret** connection metadata, where **passwords and CA PEM** live, and how to use `**sslmode=verify-full`** safely inside Kubernetes.

**Shared schema:** both PostgreSQL access ConfigMap families use **UPPER_SNAKE_CASE** keys and publish Service hostnames as full Kubernetes FQDNs: `<service>.<namespace>.svc.cluster.local`.

**Separation of concerns:** the **cluster access ConfigMap** (same namespace as the `PostgresCluster`, often `<cluster-name>-configmap`) owns **infrastructure-level** data such as shared endpoints, port, superuser access, and CA discovery. The **database access ConfigMap** created per `PostgresDatabase.spec.databases[]` extends that same connection schema with **application-level** data such as `DATABASE_NAME`, `ADMIN_USER_NAME`, and `RW_USER_NAME`.

Certificate lifecycle and server-side behaviour follow **[CloudNativePG — Certificates](https://cloudnative-pg.io/docs/1.30/certificates)** (pick the doc version that matches your CNPG release).

---

## Finding the cluster access ConfigMap

1. **By convention:** many installs use `**metadata.name`** = `**<PostgresCluster.metadata.name>-configmap**` in the same namespace as the `PostgresCluster`.
2. **From status:** after reconciliation, `**status.resources.configMapRef.name`** points at the published ConfigMap (useful for scripts and GitOps that avoid hardcoding derived names).
3. **Example:**
  ```bash
   kubectl get postgrescluster -n <namespace> <name> -o jsonpath='{.status.resources.configMapRef.name}{"\n"}'
   kubectl get configmap -n <namespace> <configmap-name> -o yaml
  ```

---

## ConfigMap keys (what apps read)


| Key                                                                 | Scope              | Use                                                               |
| ------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------- |
| `CLUSTER_RW_ENDPOINT`                                               | Cluster + Database | Primary / read-write endpoint                                     |
| `CLUSTER_RO_ENDPOINT`                                               | Cluster + Database | Read-only replica traffic                                         |
| `CLUSTER_R_ENDPOINT`                                                | Cluster + Database | Any instance                                                      |
| `DEFAULT_CLUSTER_PORT`                                              | Cluster + Database | Shared client port (currently `5432`) for the published endpoints  |
| `CLUSTER_POOLER_RW_ENDPOINT` / `CLUSTER_POOLER_RO_ENDPOINT`         | Cluster + Database | PgBouncer hosts — only if poolers are enabled                     |
| `SUPER_USER_NAME`                                                   | Cluster only       | Bootstrap superuser (often `postgres`)                            |
| `SUPER_USER_SECRET_REF`                                             | Cluster only       | Secret name for the superuser password                            |
| `SERVER_CA_SECRET_REF`                                              | Cluster only       | Serialized `SecretKeySelector` for the server CA Secret + key     |
| `DATABASE_NAME`                                                     | Database only      | Logical database name for the application                         |
| `ADMIN_USER_NAME`                                                   | Database only      | Application admin role name                                       |
| `RW_USER_NAME`                                                      | Database only      | Application read-write role name                                  |


If `SERVER_CA_*` is missing, the database may still be starting, CNPG has not yet published CA metadata, or the operator has not validated the Secret yet—check `PostgresCluster` events and CNPG cluster status, then retry.

---

## Secrets to mount


| Purpose                      | Source                                                                                                                                                           |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Trust anchor (server CA)** | `SERVER_CA_SECRET_REF` publishes a serialized `SecretKeySelector`; use it to locate the CA Secret and key (typically `ca.crt`), then mount read-only. |
| **Password**                 | Secret named by `SUPER_USER_SECRET_REF` — typically key `password` (follow your cluster’s conventions if documented elsewhere).                       |


Never copy PEM or passwords into the ConfigMap; keep them in Secrets and restrict RBAC to workloads that need them.

---

## Connect with `verify-full` (direct Postgres)

Use the **RW or RO endpoint** from the ConfigMap for strict certificate verification.

1. Resolve the Secret name and key from `SERVER_CA_SECRET_REF`, then mount that CA file read-only (for example as `/etc/postgres-ca/ca.crt`).
2. Load the password from the Secret named by `SUPER_USER_SECRET_REF` (convention: key `password`).
3. Point your client at the endpoint and port from the ConfigMap. With `**verify-full**`, the client validates both the CA chain and server certificate identity.

Example environment:

```text
PGSSLMODE=verify-full
PGSSLROOTCERT=/etc/postgres-ca/ca.crt
PGHOST=<CLUSTER_RW_ENDPOINT>
PGPORT=<DEFAULT_CLUSTER_PORT>
PGUSER=<SUPER_USER_NAME>
PGPASSWORD=<from superuser Secret>
```

Equivalent libpq connection string parameters: `**sslmode=verify-full**`, `**sslrootcert=...**`, `**host=**`, `**port=**`, `**user=**`, `**password=**`.

---

## Pooler and `verify-full`

The pooler is a separate Service from direct Postgres. With `**sslmode=verify-full`**, the client checks that the name it connects to matches the server certificate. If that name is the pooler endpoint from `**CLUSTER_POOLER_***`, the certificate presented on that path must include a matching identity.

Pooler SAN identities are managed by the operator/CNPG reconcile flow when pooler is enabled. Users do not manage individual pooler SAN entries directly. When the pooler is disabled, pooler-derived SANs stay on the server certificate so disabling the pooler does not force an extra certificate rotation.

**Practical defaults:**

- Prefer `**CLUSTER_RW_ENDPOINT`** / `**CLUSTER_RO_ENDPOINT**` when you need `**verify-full**` and want the simplest path.
- If you use pooler endpoints, wait for SAN reconciliation to converge before expecting stable `verify-full` success.

**Operational note:** after server certificate material changes, pooler Pods may need to roll so PgBouncer picks up the new leaf; expect brief TLS errors until clients and poolers converge.

---

## Require TLS on the server (`pg_hba`)

PostgreSQL decides **whether a connection may use plaintext or must use TLS** using `**pg_hba.conf`** rules. In Splunk Operator, you express those rules on `**PostgresClusterClass**` under `**spec.config.pgHBA**`: each entry is one line, in the same order PostgreSQL will evaluate them.

**Typical pattern:** reject anything that tries to skip SSL, then allow password login only over SSL:

```yaml
# PostgresClusterClass (fragment)
spec:
  config:
    pgHBA:
      # Decline connections that do not negotiate TLS
      - "hostnossl all all 0.0.0.0/0 reject"
      # Allow all databases/users from any IPv4 address when using TLS + SCRAM password auth
      - "hostssl all all 0.0.0.0/0 scram-sha-256"
```

If you also need IPv6 clients, add equivalent rules for `**::/0**` (see the [PostgreSQL `**pg_hba.conf**` documentation](https://www.postgresql.org/docs/current/auth-pg-hba-conf.html)).

**Together with client TLS:** set your application to `**sslmode=verify-full`** (or `**PGSSLMODE=verify-full**`) and mount the server CA as described above. The server presents a certificate managed by the platform; `**pg_hba**` is what **forces** clients to use SSL for password auth on those lines.

**Do not** put `**ssl`**, `**ssl_cert_file**`, or similar server certificate settings in `**postgresqlConfig**` here—the platform already provisions server TLS. Use `**pgHBA**` (and general non-TLS `**postgresqlConfig**` tuning) for policy and performance.

For allowed PostgreSQL parameters in this setup, see **[CloudNativePG — PostgreSQL configuration](https://cloudnative-pg.io/documentation/1.30/postgresql_conf/)**.

---

## Quick troubleshooting


| Symptom                                      | Things to check                                                                                                                                                                                              |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `**SERVER_CA_SECRET_REF`** missing in ConfigMap | CNPG not ready yet; CA Secret not published or not readable by the operator; requeue/reconcile.                                                                                                              |
| `**verify-full` fails with hostname errors** | Use an endpoint whose name appears in the server cert (often direct RW/RO); or align CNPG server certificate identities with the pooler host per CNPG docs; pooler rollout may be needed after cert changes. |
| **Password auth fails**                      | Correct Secret name/key; user exists for that database; `pg_hba` allows your client network and SSL method.                                                                                                  |
