---
title: Index and Ingestion Separation
parent: Deploy & Configure
nav_order: 6
---

# Index and Ingestion Separation

## What Are Ingestors?

In a traditional Splunk deployment, every indexer both receives data from forwarders and writes that data to disk. This tight coupling means you cannot independently scale the pods that handle incoming traffic from the pods that perform the compute-intensive work of writing and searching buckets.

**Ingestors** are a dedicated tier of Splunk pods whose only job is to accept data from forwarders and publish it to a durable message queue (Amazon SQS). They hold no index data. A separate **Indexer** tier pulls messages off that queue and writes them to buckets. The two tiers are decoupled by the queue and can be scaled, upgraded, and monitored completely independently.

![Index and Ingestion Separation — SOK Architecture](images/index_ingestion_separation.png)

### Why use Ingestors on Kubernetes?

| Benefit | Detail |
|---------|--------|
| **Independent scaling** | Add ingestor pods during ingestion spikes without touching indexers, and vice-versa. |
| **Data durability** | SQS provides at-least-once delivery and a dead-letter queue. Data is never dropped if indexers are down. |
| **Zero-restart first-boot** | SOK delivers queue configuration to each ingestor pod via a Kubernetes ConfigMap and an init container. Splunk starts with the right `outputs.conf` already in place — no in-place REST restart is needed. |
| **Credential-free IRSA** | When running on EKS with IRSA, no static AWS credentials are stored. The pod identity is used directly. |
| **Operational clarity** | Separate dashboards for ingestion throughput (ingestor pods) vs. indexing latency (indexer pods). |

> **Splunk version requirement:** Splunk Enterprise 10.2 or later.

> [!WARNING]
> **For customers deploying SmartBus on CMP, SOK manages the configuration and lifecycle of the ingestor tier. This guide is primarily intended for CMP users leveraging SOK-managed ingestors.**

---

## Custom Resources

Index and Ingestion Separation introduces three new Custom Resources and enhances one existing resource. All are namespaced.

| Resource | Short name | Purpose |
|----------|-----------|---------|
| `Queue` | `queue` | Describes the SQS queue and dead-letter queue |
| `ObjectStorage` | `os` | Describes the S3 bucket used for oversized messages |
| `IngestorCluster` | `ing` | Manages the ingestor StatefulSet (receives data, publishes to queue) |
| `IndexerCluster` | `idc` / `idxc` | Enhanced to pull from the queue and write to indexes |

All four resources live in the `enterprise.splunk.com/v4` API group.

### Queue

Describes the message queue where ingestors publish events.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | string | Yes | Queue technology. Allowed: `sqs`, `sqs_cp` |
| `sqs.name` | string | Yes | SQS queue name |
| `sqs.authRegion` | string | Yes | AWS region of the queue |
| `sqs.dlq` | string | Yes | Dead-letter queue name |
| `sqs.endpoint` | string | No | Override SQS endpoint (defaults to regional endpoint) |
| `sqs.volumes` | []VolumeSpec | No | Kubernetes Secrets that provide static `s3_access_key` / `s3_secret_key` credentials. Omit when using IRSA. |

> **Immutable fields:** `provider`, `sqs.name`, `sqs.authRegion`, `sqs.dlq`, `sqs.endpoint`. Only `sqs.volumes` (credentials) can be updated after creation.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Queue
metadata:
  name: queue
  namespace: splunk
spec:
  provider: sqs
  sqs:
    name: sqs-smartbus
    authRegion: us-west-2
    dlq: sqs-smartbus-dlq
    # volumes only needed when NOT using IRSA:
    volumes:
      - name: s3-sqs-creds
        secretRef: s3-secret
```

### ObjectStorage

Describes the S3 bucket used to relay messages that exceed SQS size limits.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | string | Yes | Object storage technology. Allowed: `s3` |
| `s3.path` | string | Yes | S3 bucket path (e.g. `s3://my-bucket/prefix` or `my-bucket/prefix`) |
| `s3.endpoint` | string | No | Override S3 endpoint (defaults to regional endpoint) |

> **Immutable fields:** All ObjectStorage fields are immutable after creation.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: ObjectStorage
metadata:
  name: os
  namespace: splunk
spec:
  provider: s3
  s3:
    path: s3://my-smartbus-bucket/ingestion
    endpoint: https://s3.us-west-2.amazonaws.com
```

### IngestorCluster

Manages the Splunk ingestor StatefulSet. Each pod receives data from forwarders and publishes it to the queue.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `replicas` | integer | Yes | Number of ingestor pods (min 1, default 1) |
| `queueRef.name` | string | Yes | Name of the `Queue` CR in the same namespace |
| `objectStorageRef.name` | string | Yes | Name of the `ObjectStorage` CR in the same namespace |
| `serviceAccount` | string | No | Kubernetes ServiceAccount with SQS + S3 IAM permissions (required for IRSA) |
| `image` | string | No | Splunk Enterprise container image |
| `appRepo` | AppFrameworkSpec | No | App Framework configuration for deploying Splunk apps to ingestors |

> **Immutable fields:** `queueRef`, `objectStorageRef`. These cannot be changed after the IngestorCluster is created.

**How SOK configures ingestor pods:**
SOK builds a Kubernetes ConfigMap named `splunk-<name>-ingestor-queue-config` containing the Splunk app `100-sok-ingestorcluster` with `outputs.conf`, `default-mode.conf`, `app.conf`, and `local.meta`. An init container (`init-ingestor-queue-config`) runs before Splunk starts and symlinks these files from the ConfigMap mount into the Splunk app directory. Splunk boots with the correct queue configuration already in place — no restart is required.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: ingestor
  namespace: splunk
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  replicas: 3
  image: splunk/splunk:${SPLUNK_IMAGE_VERSION}
  serviceAccount: ingestor-sa   # omit if using static credentials via Queue volumes
  queueRef:
    name: queue
  objectStorageRef:
    name: os
```

### IndexerCluster

An existing SOK resource, enhanced to pull events from the queue. When `queueRef` and `objectStorageRef` are set, the indexer pods receive their `inputs.conf`, `outputs.conf`, and `default-mode.conf` configuration to drain the queue and write events to indexes.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `replicas` | integer | Yes | Number of indexer pods |
| `clusterManagerRef.name` | string | Yes | Name of the `ClusterManager` CR |
| `queueRef.name` | string | No | Name of the `Queue` CR (must set both or neither) |
| `objectStorageRef.name` | string | No | Name of the `ObjectStorage` CR (must set both or neither) |

> **Immutable fields:** `queueRef`, `objectStorageRef` are immutable once set.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  namespace: splunk
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  image: splunk/splunk:${SPLUNK_IMAGE_VERSION}
  serviceAccount: ingestor-sa
---
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: indexer
  namespace: splunk
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  clusterManagerRef:
    name: cm
  replicas: 3
  image: splunk/splunk:${SPLUNK_IMAGE_VERSION}
  serviceAccount: ingestor-sa
  queueRef:
    name: queue
  objectStorageRef:
    name: os
```

---

## Critical User Journey

This section walks through the complete lifecycle from zero to a running index-ingestion-separated deployment, from the user's point of view.

### Prerequisites

- SOK installed and running in your cluster (see [Install](Install.md))
- An EKS cluster (or equivalent) with IAM IRSA configured, **or** static AWS credentials available in a Kubernetes Secret
- An Amazon SQS queue, dead-letter queue, and S3 bucket provisioned in AWS

### Step 1 — Set Up IAM Permissions

Ingestors and indexers need permission to put and receive SQS messages and to read/write the S3 bucket for oversized messages.

**Option A — IRSA (recommended for EKS):** Create a Kubernetes ServiceAccount annotated with an IAM role ARN. The role needs `AmazonSQSFullAccess` and `AmazonS3FullAccess` (or equivalent least-privilege policies).

```bash
eksctl create iamserviceaccount \
  --name ingestor-sa \
  --namespace splunk \
  --cluster my-cluster \
  --region us-west-2 \
  --attach-policy-arn arn:aws:iam::aws:policy/AmazonSQSFullAccess \
  --attach-policy-arn arn:aws:iam::aws:policy/AmazonS3FullAccess \
  --approve \
  --override-existing-serviceaccounts
```

Verify the ServiceAccount is annotated with the role ARN:

```bash
kubectl describe sa ingestor-sa -n splunk
# Annotations: eks.amazonaws.com/role-arn: arn:aws:iam::111111111111:role/...
```

**Option B — Static credentials:** Create a Kubernetes Secret with `s3_access_key` and `s3_secret_key` keys. Reference this secret in `Queue.spec.sqs.volumes`. No ServiceAccount annotation is needed.

```bash
kubectl create secret generic s3-secret \
  --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE \
  --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  -n splunk
```

### Step 2 — Create the Queue CR

Tell SOK about your SQS queue. The Queue CR is a shared reference — both the IngestorCluster and IndexerCluster will point to it.

```bash
kubectl apply -f - <<EOF
apiVersion: enterprise.splunk.com/v4
kind: Queue
metadata:
  name: queue
  namespace: splunk
spec:
  provider: sqs
  sqs:
    name: sqs-smartbus
    authRegion: us-west-2
    dlq: sqs-smartbus-dlq
EOF
```

Check it is Ready:
```bash
kubectl get queue -n splunk
# NAME    PHASE   AGE
# queue   Ready   10s
```

### Step 3 — Create the ObjectStorage CR

Tell SOK about your S3 bucket for oversized messages.

```bash
kubectl apply -f - <<EOF
apiVersion: enterprise.splunk.com/v4
kind: ObjectStorage
metadata:
  name: os
  namespace: splunk
spec:
  provider: s3
  s3:
    path: s3://my-smartbus-bucket/ingestion
    endpoint: https://s3.us-west-2.amazonaws.com
EOF
```

Check it is Ready:
```bash
kubectl get objectstorage -n splunk
# NAME   PHASE   AGE
# os     Ready   5s
```

### Step 4 — Deploy the IngestorCluster

SOK will create the ingestor StatefulSet. On first boot, the operator builds the queue config ConfigMap and each pod's init container writes the Splunk app before Splunk starts.

```bash
kubectl apply -f - <<EOF
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: ingestor
  namespace: splunk
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  replicas: 3
  image: splunk/splunk:${SPLUNK_IMAGE_VERSION}
  serviceAccount: ingestor-sa
  queueRef:
    name: queue
  objectStorageRef:
    name: os
EOF
```

Watch progress:
```bash
kubectl get ingestorcluster -n splunk -w
# NAME       PHASE      DESIRED   READY   AGE
# ingestor   Pending    3         0       5s
# ingestor   Ready      3         3       4m
```

What SOK does automatically:
1. Creates a ConfigMap `splunk-ingestor-ingestor-queue-config` containing the Splunk app `100-sok-ingestorcluster` with the correct `outputs.conf` (queue endpoint, DLQ, S3 path) and `default-mode.conf` (pipeline routing).
2. Adds an init container `init-ingestor-queue-config` to each pod that symlinks the ConfigMap contents into the Splunk app directory before Splunk starts.
3. Sets the `ingestorQueueConfigRev` annotation on the pod template so future credential or topology changes are detected and trigger a rolling restart.

### Step 5 — Deploy the Indexer Cluster

```bash
kubectl apply -f - <<EOF
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  namespace: splunk
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  image: splunk/splunk:${SPLUNK_IMAGE_VERSION}
  serviceAccount: ingestor-sa
---
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: indexer
  namespace: splunk
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  clusterManagerRef:
    name: cm
  replicas: 3
  image: splunk/splunk:${SPLUNK_IMAGE_VERSION}
  serviceAccount: ingestor-sa
  queueRef:
    name: queue
  objectStorageRef:
    name: os
EOF
```

Watch progress:
```bash
kubectl get indexercluster -n splunk -w
# NAME      PHASE   MANAGER   DESIRED   READY   AGE
# indexer   Ready   Ready     3         3       6m
```

### Step 6 — Verify Data Flow

Point a Splunk Universal Forwarder or HEC client at the ingestor service:

```bash
kubectl get svc -n splunk | grep ingestor
# splunk-ingestor-ingestor-headless   ClusterIP   None   ...   9997/TCP
# splunk-ingestor-ingestor            ClusterIP   ...    ...   9997/TCP,8089/TCP
```

Confirm events are flowing through SQS by checking the queue depth in the AWS console, or search the indexers for the data.

---

## Day-2 Operations

### Scaling Ingestors

To handle more ingestion throughput, increase replicas. New pods pick up the existing ConfigMap automatically — no reconfiguration is needed.

```bash
kubectl patch ingestorcluster ingestor -n splunk \
  --type=merge -p '{"spec":{"replicas":5}}'
```

### Scaling Indexers

```bash
kubectl patch indexercluster indexer -n splunk \
  --type=merge -p '{"spec":{"replicas":5}}'
```

### Rotating Credentials (Static Credential Mode)

Update the Kubernetes Secret referenced in `Queue.spec.sqs.volumes`. SOK detects the Secret's ResourceVersion change on the next reconcile, rebuilds the ConfigMap with new credentials, and triggers a rolling restart of ingestor pods.

```bash
kubectl create secret generic s3-secret \
  --from-literal=s3_access_key=NEW_ACCESS_KEY \
  --from-literal=s3_secret_key=NEW_SECRET_KEY \
  -n splunk \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Pausing Reconciliation

To temporarily prevent SOK from making changes to an IngestorCluster (e.g. during a maintenance window):

```bash
kubectl annotate ingestorcluster ingestor -n splunk \
  ingestorcluster.enterprise.splunk.com/paused=true
```

Remove the annotation to resume:
```bash
kubectl annotate ingestorcluster ingestor -n splunk \
  ingestorcluster.enterprise.splunk.com/paused-
```

### Deploying Apps to Ingestors

Use the `appRepo` field on the IngestorCluster spec to install Splunk apps via the App Framework. See [AppFramework](AppFramework.md) for configuration details.

### Deleting the Deployment

Delete in reverse dependency order to allow SOK to cleanly remove finalizers and deregister resources:

```bash
kubectl delete ingestorcluster ingestor -n splunk
kubectl delete indexercluster indexer -n splunk
kubectl delete clustermanager cm -n splunk
kubectl delete objectstorage os -n splunk
kubectl delete queue queue -n splunk
```

---

## Troubleshooting

### Ingestor pods stuck in Init state

The init container `init-ingestor-queue-config` failed to run. Check why:

```bash
kubectl describe pod splunk-ingestor-ingestor-0 -n splunk | grep -A 10 "Init Containers"
kubectl logs splunk-ingestor-ingestor-0 -n splunk -c init-ingestor-queue-config
```

Common causes:
- The ConfigMap `splunk-ingestor-ingestor-queue-config` was not yet created (wait for operator reconcile)
- The `serviceAccount` does not exist in the namespace

### Queue config ConfigMap not found

Verify the ConfigMap was created:
```bash
kubectl get cm splunk-ingestor-ingestor-queue-config -n splunk -o yaml
```

If missing, check operator logs for errors from `buildAndApplyIngestorQueueConfigMap`:
```bash
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager | grep -i "ingestor-queue-config"
```

### Ingestors cannot reach SQS

Check that the pod's ServiceAccount has the correct IAM role annotation and that the role policy includes SQS permissions:
```bash
kubectl describe pod splunk-ingestor-ingestor-0 -n splunk | grep -i "service-account\|role-arn"
```

### Status and events

```bash
# Overall status
kubectl get ingestorcluster,indexercluster,queue,objectstorage -n splunk

# Detailed status with conditions and events
kubectl describe ingestorcluster ingestor -n splunk

# Operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager --tail=100
```

---

## Helm Chart

Queue, ObjectStorage, and IngestorCluster are included in the `splunk/splunk-enterprise` Helm chart. IndexerCluster has been extended with `queueRef` and `objectStorageRef` inputs.

```yaml
queue:
  enabled: true
  name: queue
  provider: sqs
  sqs:
    name: sqs-smartbus
    authRegion: us-west-2
    dlq: sqs-smartbus-dlq
    volumes:
      - name: s3-sqs-creds
        secretRef: s3-secret

objectStorage:
  enabled: true
  name: os
  provider: s3
  s3:
    endpoint: https://s3.us-west-2.amazonaws.com
    path: s3://my-smartbus-bucket/ingestion

ingestorCluster:
  enabled: true
  name: ingestor
  replicaCount: 3
  serviceAccount: ingestor-sa
  queueRef:
    name: queue
  objectStorageRef:
    name: os

clusterManager:
  enabled: true
  name: cm
  serviceAccount: ingestor-sa

indexerCluster:
  enabled: true
  name: indexer
  replicaCount: 3
  serviceAccount: ingestor-sa
  clusterManagerRef:
    name: cm
  queueRef:
    name: queue
  objectStorageRef:
    name: os
```

---

## Reference

- [Custom Resources](CustomResources.md) — CommonSplunkSpec fields shared by all CRs
- [App Framework](AppFramework.md) — Deploying Splunk apps to ingestor pods
- [SmartStore](SmartStore.md) — S3-backed index storage (separate from ingestion S3 bucket)
- [Security](Security.md) — RBAC, pod security, network policies
