---
title: Postgres Operator Quickstart
parent: PostgreSQL
nav_order: 0
---

# Postgres Operator Quickstart

This is the fastest path to a connected PostgreSQL database managed through the Postgres Operator.
It uses the Postgres Operator API to create a small development PostgreSQL cluster and one
application database in a new namespace. Run the commands in order from a terminal with `kubectl`
access to the Kubernetes cluster.

In this guide, **Postgres Operator** means the PostgreSQL API and controllers delivered in the
Splunk Operator binary. The Splunk Operator image is the packaging and deployment vehicle; this
workflow does not deploy or configure Splunk Enterprise. CloudNativePG (CNPG) is the backend
provisioner used by the Postgres Operator, not the consumer-facing API.

All database provisioning in this workflow must go through `PostgresClusterClass`,
`PostgresCluster`, and `PostgresDatabase`. Do not create or modify CNPG `Cluster`, `Database`, or
`Pooler` resources directly: doing so bypasses the Postgres Operator's policy, lifecycle, status,
and generated connection-resource contracts.

See [Setup and prerequisites](#setup-and-prerequisites) below to choose the short or full setup path
before continuing.

The quickstart is intentionally a development configuration: one PostgreSQL instance, no automated
backups, no PgBouncer, and a Postgres Operator deletion policy that cleans up its managed backend
cluster. Do not use it unchanged for a workload that stores data you need to retain.

For choosing a production, shared-cluster, or pooled-connection configuration after the first
connection works, see [Integration & Onboarding Guide](integration-patterns.md).

## Setup and prerequisites

This guide starts with an existing Kubernetes cluster; creating the cluster itself is outside its
scope. Before beginning either setup path, you need a current `kubectl` context that points to the
target cluster and permission to create namespaces and PostgreSQL resources. Before continuing to
the database steps, the cluster must also have the Postgres Operator CRDs and controllers, plus the
CNPG backend operator.

Check the target before making changes:

```bash
kubectl config current-context
kubectl cluster-info
```

Choose only one setup path:

- **Platform-prepared cluster:** use the short path to verify those components when a platform team
  has already installed and owns the operators and CRDs.
- **Fresh cluster:** use the full path to install those components when you administer an empty
  cluster and the required operators and CRDs are not present.

### Short path: verify a platform-provided Postgres Operator

<details>

<summary> <b><i> Click here to see details</i> </b> </summary>

Confirm the three consumer-facing Postgres Operator CRDs and the backend CNPG CRD are registered:

```bash
kubectl get crd \
  postgresclusterclasses.platform.splunk.com \
  postgresclusters.platform.splunk.com \
  postgresdatabases.platform.splunk.com \
  clusters.postgresql.cnpg.io
```

Check the Postgres Operator host Deployment and the CNPG backend Deployment. Ask the platform team
for their names and namespaces if the installation uses a different layout.

```bash
kubectl get deployment splunk-operator-controller-manager -n splunk-operator
kubectl get deployment cnpg-controller-manager -n cnpg-system
```

The Postgres Operator controllers are an alpha feature and are disabled by default in the Splunk
Operator binary. Verify that its Deployment explicitly enables them:

```bash
kubectl get deployment splunk-operator-controller-manager -n splunk-operator \
  -o jsonpath='{.spec.template.spec.containers[0].args}{"\n"}'
```

The output must contain `--feature-gates=PostgresController=true`. If a CRD is absent, a deployment
is not ready, or the feature gate is not enabled, stop and ask the platform team to finish the
installation. Having the CRDs alone is not sufficient: without the feature gate, the Postgres
Operator does not reconcile `PostgresClusterClass`, `PostgresCluster`, or `PostgresDatabase`.

Continue at [Create a namespace](#1-create-a-namespace) after all checks pass.

</details>

### Full path: running in a fresh cluster

<details>

<summary> <b><i> Click here to see details</i> </b> </summary>

This path is for cluster administrators working from a checkout of this repository. It requires
Docker, GNU Make, Go, `kubectl`, internet access to retrieve dependencies, and cluster-admin access.
The tested local path additionally requires [kind](https://kind.sigs.k8s.io/). Do not use this
source deployment path on a shared or production cluster: the repository's `make deploy` target
uninstalls and reinstalls its CRDs.

> **Why kind, and what is in scope:** `kind` is the tested local reference environment for this guide.
> This repository already uses kind in its
> [local cluster bootstrap](../../test/deploy-kind-cluster.sh) and
> [KUTTL test configuration](../../kuttl/kuttl-test-kind.yaml), so it provides a reproducible,
> disposable Kubernetes cluster using the same Docker-based workflow as the locally built operator
> image. It also requires no cloud account or provider-specific infrastructure. This is a
> documentation and testing choice, not a requirement imposed by the Postgres Operator or a
> compatibility statement about other Kubernetes distributions.
>
> Local tools such as `k3d` and `minikube`, or managed services such as Amazon EKS, differ in how they
> make local or private images available, provision storage, expose networking, and configure
> cluster access. Documenting and validating every variation would turn this document
> into a platform installation guide. Setting up or troubleshooting those environments is therefore
> outside its scope. On another platform, follow its documentation to prepare the cluster and make
> the operator image available to every node, then continue with the common operator and database
> steps below.

#### A. Install the CNPG backend

The Postgres Operator currently uses CNPG as its backend provisioner and requires the CNPG 1.29 API.
Install the current patch release from that release line and wait for its controller. Consumers
will use the Postgres Operator CRs later in this guide rather than creating CNPG resources directly.

```bash
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.29/releases/cnpg-1.29.2.yaml
kubectl rollout status deployment/cnpg-controller-manager \
  -n cnpg-system --timeout=2m
```

See the [CNPG installation documentation](https://cloudnative-pg.io/documentation/current/installation_upgrade/)
for other installation methods.

#### B. Install cert-manager

The webhook-enabled Postgres Operator deployment in
[Deploy the Postgres Operator controllers](#d-deploy-the-postgres-operator-controllers) requires
cert-manager:

```bash
kubectl apply -f \
  https://github.com/cert-manager/cert-manager/releases/download/v1.21.0/cert-manager.yaml
kubectl rollout status deployment/cert-manager \
  -n cert-manager --timeout=2m
kubectl rollout status deployment/cert-manager-cainjector \
  -n cert-manager --timeout=2m
kubectl rollout status deployment/cert-manager-webhook \
  -n cert-manager --timeout=2m
```

See the [cert-manager installation documentation](https://cert-manager.io/docs/installation/)
before changing the pinned version or installation method.

#### C. Make the Postgres Operator image available

The Postgres Operator controllers are packaged in the Splunk Operator image. For a local kind
cluster, build that image from this checkout and load it directly into the kind nodes. Set
`KIND_CLUSTER_NAME` to the name shown by `kind get clusters`, not the `kind-` prefixed `kubectl`
context name.

```bash
export OPERATOR_IMAGE=splunk-operator:postgres-quickstart
kind get clusters
export KIND_CLUSTER_NAME=<kind-cluster-name>

make docker-build IMG="$OPERATOR_IMAGE"
kind load docker-image "$OPERATOR_IMAGE" --name "$KIND_CLUSTER_NAME"
```

See kind's [local image-loading documentation](https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster)
for more detail.

On another Kubernetes platform, use its documented mechanism to make the image available to every
node. That might be a platform-specific local import or a registry; those commands are outside this
guide's tested path. When using a registry, set `OPERATOR_IMAGE` to its fully qualified reference,
such as `registry.example.com/team/splunk-operator:<tag>`, follow the registry's authentication and
push instructions, and configure the cluster to pull from it. Relevant public references include
[Docker image push](https://docs.docker.com/reference/cli/docker/image/push/),
[Kubernetes private-registry credentials](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/),
and [kind private registries](https://kind.sigs.k8s.io/docs/user/private-registries/). Only configure
registry credentials in Kubernetes when the registry requires them.

#### D. Deploy the Postgres Operator controllers

Deploy the Splunk Operator binary as the host for the Postgres Operator controllers. The
`PostgresController` feature gate is required; `MANAGER_EXTRA_ARG` is singular:

```bash
make deploy IMG="$OPERATOR_IMAGE" \
  ENVIRONMENT=default-with-webhook \
  MANAGER_EXTRA_ARG="--feature-gates=PostgresController=true"
```

> **Known source-deployment side effects:** `make deploy` records `OPERATOR_IMAGE` in
> `config/manager/kustomization.yaml`. In addition, `WATCH_NAMESPACE` and `SPLUNK_GENERAL_TERMS` are
> empty by default. With both values empty, the target's cleanup substitutions can leave
> `config/default-with-webhook/kustomization.yaml` with `WATCH_NAMESPACE_VALUE` in the
> `SPLUNK_GENERAL_TERMS` entry instead of restoring `SPLUNK_GENERAL_TERMS_VALUE`. This happens after
> the manifests are applied and does not affect the running deployment, but these source-tree
> changes leave the local checkout modified and the incorrect placeholder can affect a later
> deployment. Check the diff after deployment:
>
> ```bash
> git diff -- \
>   config/manager/kustomization.yaml \
>   config/default-with-webhook/kustomization.yaml
> ```
>
> If those files had no intentional changes before this procedure and the diff shows only the image
> and placeholder replacements described above, restore them before another deployment or commit:
>
> ```bash
> git restore \
>   config/manager/kustomization.yaml \
>   config/default-with-webhook/kustomization.yaml
> ```

If the image was loaded directly into kind, change the generated Deployment's pull policy so
Kubernetes uses that node-local image. Skip this patch for a registry-hosted image:

```bash
kubectl patch deployment splunk-operator-controller-manager \
  -n splunk-operator --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
```

Wait for the operator and confirm both the feature gate and required CRDs:

```bash
kubectl rollout status deployment/splunk-operator-controller-manager \
  -n splunk-operator --timeout=2m
kubectl get deployment splunk-operator-controller-manager -n splunk-operator \
  -o jsonpath='{.spec.template.spec.containers[0].args}{"\n"}'
kubectl get crd \
  postgresclusterclasses.platform.splunk.com \
  postgresclusters.platform.splunk.com \
  postgresdatabases.platform.splunk.com \
  clusters.postgresql.cnpg.io
```

The deployment arguments must contain `--feature-gates=PostgresController=true`, all four CRDs
must be returned, and both the Postgres Operator host and CNPG backend Deployments must be ready
before continuing. If the rollout times out, see
[Operator rollout does not complete](#operator-rollout-does-not-complete).

</details>

## 1. Create a namespace

`PostgresCluster` and every `PostgresDatabase` that references it must be in the same namespace.

```bash
export NAMESPACE=postgres-quickstart
kubectl create namespace "$NAMESPACE"
```

## 2. Select or apply the development class

`PostgresClusterClass` is cluster-scoped, so it has no namespace. It is immutable after creation;
create a new class rather than editing this one later. In production, classes are normally owned by
the platform team because they set shared policy for resources, backups, and pooling. First check
whether the platform has already provided an approved development class:

```bash
export CLASS=postgresql-dev
kubectl get postgresclusterclass
```

If the platform has provided a class suitable for this quickstart, set `CLASS` to its exact name and
skip the manifest below. Do not apply a second class with the same name. If no suitable development
class exists and you are allowed to create one, apply this development-only class:

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: postgresql-dev
spec:
  # CNPG is the currently supported provisioner.
  provisioner: postgresql.cnpg.io
  config:
    # Why: one instance keeps this first database small and inexpensive. It has no HA.
    instances: 1
    # Why: set an explicit small PVC size instead of relying on the class default.
    storage: 10Gi
    postgresVersion: "18"
    # Resource requests and limits apply to the single PostgreSQL pod.
    resources:
      requests:
        cpu: "500m"
        memory: "1Gi"
      limits:
        cpu: "1"
        memory: "2Gi"
    # Why: keep the quickstart direct; enable PgBouncer only when the workload needs it.
    connectionPooler:
      enabled: false
    # Why: make the no-backup development intent explicit.
    backup:
      enabled: false
  # Required whenever the provisioner is postgresql.cnpg.io.
  cnpg:
    # Why: a single instance cannot switch over to a replica.
    primaryUpdateMethod: restart
EOF
```

The class's configuration is shared by every cluster that refers to it. For a production workload,
use the platform-approved class rather than creating or changing one yourself.

## 3. Create a PostgreSQL cluster

Apply a `PostgresCluster` to ask the Postgres Operator to provision and manage the PostgreSQL
cluster through the class policy selected in step 2:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: quickstart-postgres
  namespace: $NAMESPACE
spec:
  # The class name is immutable after the cluster is created.
  class: $CLASS
  # Why: a throwaway quickstart should clean up its managed backend when deleted.
  # Use Retain for any cluster whose data must survive deletion of this CR.
  clusterDeletionPolicy: Delete
EOF

kubectl wait --namespace "$NAMESPACE" \
  --for=jsonpath='{.status.phase}'=Ready \
  postgrescluster/quickstart-postgres --timeout=5m
```

The first provisioning run can take several minutes while the Postgres Operator provisions its
backend and CNPG initializes the data volume. If the wait times out, see
[`PostgresCluster` does not reach `Ready`](#postgrescluster-does-not-reach-ready).

## 4. Create an application database

Apply a `PostgresDatabase` to ask the Postgres Operator to create the `appdb` database,
`appdb_admin` and `appdb_rw` PostgreSQL roles (the default names), their credential Secrets, and a database connection
ConfigMap.

```bash
cat <<EOF | kubectl apply -f -
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: quickstart-db
  namespace: $NAMESPACE
spec:
  # Must name a PostgresCluster in this same namespace.
  clusterRef:
    name: quickstart-postgres
  databases:
    # Database names must start with a lowercase letter and use only lowercase letters and digits.
    - name: appdb
      # Why: remove the database too when the quickstart resource is deleted.
      # Use Retain when deleting the PostgresDatabase must leave its data in place.
      deletionPolicy: Delete
EOF

kubectl wait --namespace "$NAMESPACE" \
  --for=jsonpath='{.status.phase}'=Ready \
  postgresdatabase/quickstart-db --timeout=5m
```

## 5. Retrieve connection details and connect

The Postgres Operator-generated read-write Secret is named `<PostgresDatabase>-<database>-rw`; this
example produces `quickstart-db-appdb-rw`. The database ConfigMap is named
`<PostgresDatabase>-<database>-config`; it contains the endpoint, port, database name, and role
name. The temporary client Pod below references those generated resources directly. Its Pod spec
contains Secret and ConfigMap references, not the decoded password.

Database names must begin with a lowercase letter and contain only lowercase letters and digits.
Underscores are invalid in the derived Kubernetes resource names, while hyphens do not fit the
PostgreSQL identifiers and roles derived from the same value. Both are rejected when the
`PostgresDatabase` is submitted. The exact generated Secret and ConfigMap names are also published
in `status.databases[].adminUserSecretRef`, `rwUserSecretRef`, and `configMapRef`.

```bash
export DATABASE_CONFIG=quickstart-db-appdb-config
export RW_SECRET=quickstart-db-appdb-rw

kubectl delete pod postgres-client -n "$NAMESPACE" --ignore-not-found

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: postgres-client
  namespace: $NAMESPACE
spec:
  restartPolicy: Never
  containers:
    - name: psql
      image: postgres:18
      command: ["psql"]
      args: ["-c", "select current_database(), current_user;"]
      env:
        - name: PGHOST
          valueFrom:
            configMapKeyRef:
              name: $DATABASE_CONFIG
              key: CLUSTER_RW_ENDPOINT
        - name: PGPORT
          valueFrom:
            configMapKeyRef:
              name: $DATABASE_CONFIG
              key: DEFAULT_CLUSTER_PORT
        - name: PGDATABASE
          valueFrom:
            configMapKeyRef:
              name: $DATABASE_CONFIG
              key: DATABASE_NAME
        - name: PGUSER
          valueFrom:
            secretKeyRef:
              name: $RW_SECRET
              key: username
        - name: PGPASSWORD
          valueFrom:
            secretKeyRef:
              name: $RW_SECRET
              key: password
EOF

kubectl wait --namespace "$NAMESPACE" \
  --for=jsonpath='{.status.phase}'=Succeeded \
  pod/postgres-client --timeout=1m
```

The completed Pod remains available until it is explicitly deleted. After it reaches `Succeeded`,
read the query result:

```bash
kubectl logs postgres-client -n "$NAMESPACE"
```

After reviewing the output, delete the temporary Pod:

```bash
kubectl delete pod postgres-client -n "$NAMESPACE"
```

A successful result shows `appdb` and `appdb_rw`. Your application should use the read-write
credential Secret and database access ConfigMap in the same way, ideally by mounting them rather
than copying their values into a workload manifest. See [Connecting to PostgreSQL with TLS](connecting-to-postgres-with-TLS.md)
for the complete ConfigMap and certificate contract.

## 6. Troubleshoot the quickstart

Use these checks for failures in the tested workflow. Inspect Secret metadata and references, but
do not decode credentials into terminal output, logs, Pod specifications, or support bundles.

### Operator rollout does not complete

Inspect the controller Pod because Deployment events alone do not show container-level failures:

```bash
kubectl get pods -n splunk-operator
kubectl describe pod -n splunk-operator \
  -l control-plane=controller-manager
kubectl get events -n splunk-operator --sort-by='.lastTimestamp'
```

- `ErrImagePull` or `ImagePullBackOff`: for the tested kind path, verify the image reference, the
  cluster selected by `KIND_CLUSTER_NAME`, `kind load docker-image`, and
  `imagePullPolicy: IfNotPresent`. On another platform, use its image-distribution documentation.
- `FailedMount` for `webhook-server-cert`: confirm cert-manager is ready and the Secret exists with
  `kubectl get secret webhook-server-cert -n splunk-operator`.
- `FailedScheduling`: inspect the event for unavailable CPU, memory, or storage and increase the
  local cluster capacity before retrying.

### PostgreSQL resource kinds are unavailable

An error such as `no matches for kind "PostgresCluster"` means the Postgres Operator CRDs are not
installed in the current cluster or `kubectl` is using the wrong context. Recheck both:

```bash
kubectl config current-context
kubectl get crd \
  postgresclusterclasses.platform.splunk.com \
  postgresclusters.platform.splunk.com \
  postgresdatabases.platform.splunk.com
```

Use the appropriate [setup path](#setup-and-prerequisites) if a CRD is missing.

### `PostgresClusterClass` cannot be created

`PostgresClusterClass` is cluster-scoped and immutable. Check whether the class already exists and
whether the current user can create classes:

```bash
kubectl get postgresclusterclass postgresql-dev
kubectl auth can-i create postgresclusterclasses \
  --api-group=platform.splunk.com
```

Use an approved existing class when one is available. For `Forbidden`, request the required
permission from the platform team. For an immutable-field error, create a class with a new name
rather than editing the existing class.

### `PostgresCluster` does not reach `Ready`

Inspect the Postgres Operator resource first, then use its managed CNPG resources only for
diagnostics:

```bash
kubectl describe postgrescluster quickstart-postgres -n "$NAMESPACE"
kubectl get cluster.postgresql.cnpg.io -n "$NAMESPACE"
kubectl get pods,pvc -n "$NAMESPACE"
kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp'
```

Common causes include a missing or unprovisionable StorageClass, insufficient node resources, and
backend image-pull failures. Resolve the reported Kubernetes event, then repeat the wait command;
do not create or modify the managed CNPG resources directly.

### `PostgresDatabase` does not reach `Ready`

Confirm that `clusterRef.name` points to a ready `PostgresCluster` in the same namespace:

```bash
kubectl describe postgresdatabase quickstart-db -n "$NAMESPACE"
kubectl get postgrescluster quickstart-postgres -n "$NAMESPACE"
```

Correct the reference or namespace if they differ. If the cluster is not ready, resolve that
failure first.

### Connection Pod does not succeed

Inspect the Pod and verify that the generated connection resources exist without printing their
contents:

```bash
kubectl describe pod postgres-client -n "$NAMESPACE"
kubectl logs postgres-client -n "$NAMESPACE"
kubectl get configmap "$DATABASE_CONFIG" -n "$NAMESPACE"
kubectl get secret "$RW_SECRET" -n "$NAMESPACE"
```

`ImagePullBackOff` means the node cannot obtain `postgres:18`. Missing ConfigMap or Secret errors
mean the `PostgresDatabase` has not finished reconciling or the names do not match the documented
contract. Connection timeouts can also indicate a NetworkPolicy blocking access to the published
endpoint. Resolve the reported condition, delete the failed Pod, and apply the client manifest
again.

## 7. Clean up

These commands ask the Postgres Operator to remove the application database and PostgreSQL cluster,
then remove the quickstart namespace. With `clusterDeletionPolicy: Delete`, deleting the
`PostgresCluster` also deletes its managed backend cluster.

```bash
kubectl delete postgresdatabase quickstart-db -n "$NAMESPACE"
kubectl delete postgrescluster quickstart-postgres -n "$NAMESPACE"
kubectl delete namespace "$NAMESPACE"
# Delete the class only if you created it in step 2 and no other clusters use it.
# kubectl delete postgresclusterclass postgresql-dev
```

## Configuration examples

The rest of this page is a compact catalog for five Postgres Operator deployment shapes. Each
snippet uses only fields present in the current Postgres Operator API. Apply a class before a
cluster that refers it, and keep the cluster and its `PostgresDatabase` resources in the same
namespace. Do not translate these examples into direct CNPG resources.

### `postgresql-dev`: lightweight single-instance development

The class in [step 2](#2-select-or-apply-the-development-class) is the complete `postgresql-dev` example:
one instance, 10Gi storage, minimal resources, no automated backups, and no pooler. Its corresponding
`PostgresCluster` is in [step 3](#3-create-a-postgresql-cluster) and its `PostgresDatabase` is in
[step 4](#4-create-an-application-database).

### `postgresql-prod`: HA with volume-snapshot backups and PgBouncer

Use this only where the platform has already created the referenced `VolumeSnapshotClass`. It has
three database instances, transaction-mode PgBouncer, and a daily volume snapshot. `switchover` and
the read-only pooler require more than one effective database instance, so do not override this
class to `instances: 1`.

This is an adapted quickstart profile, not a verbatim copy of the repository's
[`postgresql-prod` sample](../../config/samples/platform_v1alpha1_postgresclusterclass_prod.yaml): it
uses PostgreSQL `18` rather than the sample's pinned `18.1`, omits its workload-specific OLTP
`postgresqlConfig` tuning, and explicitly enables both PgBouncer endpoints rather than relying on
their `true` defaults. It adds the backup configuration from the
[`postgresql-backup` sample](../../config/samples/platform_v1alpha1_postgresclusterclass_backup.yaml).
Use the production sample as the baseline when its version pin and tuning match the workload.

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: postgresql-prod
spec:
  provisioner: postgresql.cnpg.io
  config:
    # Why: three instances provide a primary and replicas for HA.
    instances: 3
    storage: 100Gi
    postgresVersion: "18"
    resources:
      requests:
        cpu: "2"
        memory: "8Gi"
      limits:
        cpu: "4"
        memory: "16Gi"
    pgHBA:
      # Reject plaintext. Scope any additional hostssl rules to intended application CIDRs.
      - "hostnossl all all 0.0.0.0/0 reject"
    # Why: production workloads commonly need to absorb concurrent application connections.
    connectionPooler:
      enabled: true
      readWrite: true
      readOnly: true
    # Why: production data needs a scheduled recovery point.
    backup:
      enabled: true
      schedule: "0 2 * * *"
  cnpg:
    # Why: replicas make a switchover possible during primary updates.
    primaryUpdateMethod: switchover
    connectionPooler:
      instances: 3
      mode: transaction
      config:
        max_client_conn: "100"
        default_pool_size: "20"
    backup:
      target: prefer-standby
      volumeSnapshot:
        # This must name a CSI VolumeSnapshotClass in the target cluster.
        # This repository's backup sample uses csi-hostpath-snapclass.
        className: csi-hostpath-snapclass
        # Keep snapshots after the managed backend cluster is deleted.
        snapshotOwnerReference: none
        # Take hot snapshots without database downtime.
        online: true
```

The `volumeSnapshot` fields come from the current
[`CNPGVolumeSnapshotConfig` API](../../api/platform/v1alpha1/postgresclusterclass_types.go) and match
the repository's [`postgresql-backup` sample](../../config/samples/platform_v1alpha1_postgresclusterclass_backup.yaml).
Choose the snapshot class approved for the target environment; `csi-hostpath-snapclass` is the
repository's development sample, not a portable production default. See
[Automated Backups via Volume Snapshots](backup-volume-snapshots.md) for the required platform
setup.

### `postgresql-shared`: one cluster for several application databases

This class and cluster serve multiple `PostgresDatabase` resources in one namespace. Use separate
database resources per workload so their generated credentials can be scoped independently with
Kubernetes RBAC.

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: postgresql-shared
spec:
  provisioner: postgresql.cnpg.io
  config:
    # Why: a shared failure domain should have replicas.
    instances: 3
    storage: 100Gi
    postgresVersion: "18"
    resources:
      requests:
        cpu: "2"
        memory: "8Gi"
      limits:
        cpu: "4"
        memory: "16Gi"
    connectionPooler:
      # Why: several workloads can otherwise create independent connection spikes.
      enabled: true
      readWrite: true
      readOnly: true
  cnpg:
    primaryUpdateMethod: switchover
    connectionPooler:
      instances: 3
      mode: transaction
      config:
        max_client_conn: "300"
        default_pool_size: "30"
---
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: shared-postgres
  namespace: shared-ns
spec:
  class: postgresql-shared
  # Why: deleting the CR should not immediately delete data shared by several teams.
  clusterDeletionPolicy: Retain
---
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: team-a-db
  namespace: shared-ns
spec:
  clusterRef:
    name: shared-postgres
  databases:
    - name: teamaapp
      # Why: delete the database, roles, generated Secrets, and ConfigMap with this CR.
      deletionPolicy: Delete
---
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: team-b-db
  namespace: shared-ns
spec:
  clusterRef:
    name: shared-postgres
  databases:
    - name: teambapp
      # Why: keep the PostgreSQL database and roles if this CR is deleted.
      deletionPolicy: Retain
```

`databases[].deletionPolicy` controls what happens only when its `PostgresDatabase` resource is
deleted. Team A uses `Delete`, so the Postgres Operator removes the managed backend database,
generated Secrets, ConfigMap, and managed PostgreSQL roles. Team B uses `Retain`, so deletion
orphans the application resources and leaves its PostgreSQL database and roles in place. Neither
per-database choice protects data if the `PostgresCluster` itself is deleted; that is controlled by
`clusterDeletionPolicy`. See [shared-cluster guidance](integration-patterns.md#shared-cluster-vs-dedicated-cluster)
for the retention and namespace-isolation details.

### `postgresql-dedicated`: one cluster for one workload

Use a dedicated cluster when the workload needs independent capacity, maintenance timing, backups,
or connection policy. This example enforces TLS at the PostgreSQL access-policy layer and keeps the
cluster when its CR is deleted.

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: postgresql-dedicated
spec:
  provisioner: postgresql.cnpg.io
  config:
    # Why: production dedicated workloads need a replica for HA.
    instances: 3
    storage: 200Gi
    postgresVersion: "18"
    resources:
      requests:
        cpu: "4"
        memory: "16Gi"
      limits:
        cpu: "8"
        memory: "32Gi"
    # Why: reject plaintext network connections to this workload's cluster.
    pgHBA:
      # Scope any additional hostssl rules to intended application CIDRs.
      - "hostnossl all all 0.0.0.0/0 reject"
  cnpg:
    primaryUpdateMethod: switchover
---
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: orders-postgres
  namespace: orders
spec:
  class: postgresql-dedicated
  # Why: preserve the independently managed cluster if this CR is removed.
  clusterDeletionPolicy: Retain
---
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: orders-db
  namespace: orders
spec:
  clusterRef:
    name: orders-postgres
  databases:
    - name: orders
      # Why: preserve the application data if the declaration is removed.
      deletionPolicy: Retain
```

The current API does not provide custom server-certificate fields. The PostgreSQL certificate
management workflow is in progress and will be documented separately when it is available. Until
then, `pgHBA` is the supported way to enforce TLS, using the server certificate lifecycle provided
by CNPG. See [Connecting to PostgreSQL with TLS](connecting-to-postgres-with-TLS.md).

### `postgresql-pooler-transaction`: transaction-mode read-write pooler

There is no standalone `PgBouncer` custom resource in this API. PgBouncer is configured as
immutable class policy under `spec.cnpg.connectionPooler`, while a `PostgresCluster` can only
enable or disable the inherited pooler endpoints. This complete class creates only a read-write
pooler, which is appropriate when replicas are reserved for HA rather than application reads.

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: postgresql-pooler-transaction
spec:
  provisioner: postgresql.cnpg.io
  config:
    # Why: pooler replicas and a switchover policy need database replicas.
    instances: 3
    storage: 100Gi
    postgresVersion: "18"
    resources:
      requests:
        cpu: "2"
        memory: "8Gi"
      limits:
        cpu: "4"
        memory: "16Gi"
    connectionPooler:
      # Why: protect PostgreSQL from large numbers of short-lived client connections.
      enabled: true
      readWrite: true
      readOnly: false
  cnpg:
    # Why: move the primary role to a replica before updates to reduce interruption.
    primaryUpdateMethod: switchover
    connectionPooler:
      instances: 3
      # Why: transaction mode is the recommended default for most web workloads.
      mode: transaction
      config:
        max_client_conn: "300"
        default_pool_size: "30"
```

Use `mode: session` in a separate class only when the client requires session state such as
session variables or advisory locks across transactions. Do not use `transaction` mode for those
clients. The database access ConfigMap publishes `CLUSTER_POOLER_RW_ENDPOINT` when the pooler is
enabled; use that hostname with `DEFAULT_CLUSTER_PORT` instead of the direct RW endpoint.

`primaryUpdateMethod` has two supported values. `restart` is the default and restarts the current
primary in place, which is appropriate for development, test, or a maintenance window where a brief
write interruption is acceptable. `switchover` promotes a replica before updating the old primary,
which reduces client-visible interruption and is appropriate for production; it requires at least
two PostgreSQL instances. The method is class policy, so select the class before creating the
cluster. See [Vertical Scaling](scaling-up.md#cpu-and-memory-changes) for update behavior in more
detail.

## Next steps

- Use the full [Integration & Onboarding Guide](integration-patterns.md) to choose shared versus
  dedicated topology, apply RBAC, and tune PgBouncer.
- Read [Connecting to PostgreSQL with TLS](connecting-to-postgres-with-TLS.md) before deploying an
  application that requires certificate verification.
- See [PostgresDatabase Managed Roles](postgresdatabase-managed-roles.md) for generated Secret names,
  role ownership, and deletion behavior.
