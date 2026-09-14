---
title: Index and Ingestion Separation
parent: Deploy & Configure
nav_order: 6
---

# Background

> [!IMPORTANT]
> **Please follow Splunk release information for official announcements about Index and Ingestion separation and supported configurations.**

Separation between ingestion and indexing services within Splunk Operator for Kubernetes enables the operator to manage ingestion and indexing as separate tiers while connecting them through a durable remote queue.

This separation enables:
- **Independent scaling:** Match resource allocation to ingestion or indexing workload.
- **Data durability:** Off‑load buffer management and retry logic to a durable message queue.
- **Operational clarity:** Separate monitoring dashboards for ingestion throughput vs indexing latency.

# Important Note

> [!WARNING]
> **At this time, this guide focuses on configuring new separated topologies. A supported migration strategy for existing deployments has not yet been established.**

# Queue

Queue stores the remote queue information shared by an `IngestorCluster` and an `IndexerCluster`. SOK uses this resource as configuration. It does not create or manage the external queue or dead-letter queue.

## Spec

Queue inputs can be found in the table below. The supported provider is `sqs`.

| Key        | Type    | Required | Description                                       |
| ---------- | ------- | -------- | ------------------------------------------------- |
| provider   | string | Yes | Provider of message queue (Allowed value: `sqs`) |
| sqs   | SQS | Yes if provider = `sqs` | SQS message queue inputs |

SQS message queue inputs can be found in the table below.

| Key        | Type    | Required | Description                                       |
| ---------- | ------- | -------- | ------------------------------------------------- |
| name   | string | Yes | Name of the physical queue |
| authRegion   | string | No | Region used for authentication and endpoint resolution |
| endpoint   | string | No | AWS SQS service endpoint. If omitted, SOK resolves it from `authRegion` |
| dlq   | string | Yes | Name of the physical dead-letter queue |
| secretKeyRef | object | No | Per-key selectors for AWS credentials. When not set, IRSA / workload identity is assumed. Contains `awsAccessKey` and `awsSecretKey`, each a `SecretKeySelector` (`name`, `key`). |

The provider, queue name, auth region, endpoint, and dead-letter queue are immutable after the `Queue` is created. `secretKeyRef` can be changed. When `secretKeyRef` is omitted, the pods must use IRSA or another workload identity mechanism. When it is set, the referenced Secrets must be kept in the same namespace as the resource that uses them. SOK reads the selected keys and delivers the credentials to the referenced tiers through a generated, immutable Secret. SOK watches the referenced Secrets. Changing credential data creates a new generated Secret and rolls the affected pods declaratively.

The `Queue` controller does not validate connectivity to the external queues. A `Ready` Queue status means that the Kubernetes resource has been reconciled. Access and queue health must be verified separately.

## Example
```
apiVersion: enterprise.splunk.com/v4
kind: Queue
metadata:
  name: queue
spec:
  provider: sqs
  sqs:
    name: sqs-test
    authRegion: us-west-2
    endpoint: https://sqs.us-west-2.amazonaws.com
    dlq: sqs-dlq-test
```

To use static AWS credentials instead of workload identity, add the following optional `secretKeyRef` under `spec.sqs`:

```
secretKeyRef:
  awsAccessKey:
    name: s3-secret
    key: s3_access_key
  awsSecretKey:
    name: s3-secret
    key: s3_secret_key
```

# ObjectStorage

ObjectStorage stores the large messages that exceed the underlying queue message-size limit. The same object storage configuration is shared by the `IngestorCluster` and `IndexerCluster`. SOK uses this resource as configuration. The bucket and path must already be available and accessible to the pods.

## Spec

ObjectStorage inputs can be found in the table below. The supported provider is `s3`.

| Key        | Type    | Required | Description                                       |
| ---------- | ------- | -------- | ------------------------------------------------- |
| provider   | string | Yes | Provider of object storage (Allowed value: `s3`) |
| s3   | S3 | Yes if provider = `s3` | S3 object storage inputs |

S3 object storage inputs can be found in the table below.

| Key        | Type    | Required | Description                                       |
| ---------- | ------- | -------- | ------------------------------------------------- |
| path   | string | Yes | Remote storage location for messages that are larger than the underlying maximum message size |
| endpoint   | string | No | S3-compatible service endpoint. If omitted, SOK resolves the endpoint from the Queue `authRegion` |
| encryptionScheme | string | No | Remote-storage encryption scheme: `sse-s3`, `sse-c`, or `none` |
| kmsEndpoint | string | No | KMS endpoint used with `encryptionScheme: sse-c`. Resolved from the Queue region when omitted |
| kmsKeyId | string | No | KMS key identifier |

All `ObjectStorage` spec inputs are immutable after the resource is created. `kmsKeyId` is required when `encryptionScheme` is `sse-c`.

The `ObjectStorage` controller does not create or validate connectivity to the external bucket. A `Ready` ObjectStorage status means that the Kubernetes resource has been reconciled.

## Example
```
apiVersion: enterprise.splunk.com/v4
kind: ObjectStorage
metadata:
  name: os
spec:
  provider: s3
  s3:
    path: ingestion/smartbus-test
    endpoint: https://s3.us-west-2.amazonaws.com
```

# IngestorCluster

IngestorCluster manages the ingestion tier. Its Splunk pods receive events and publish them to the remote queue through `outputs.conf`.

## Spec

In addition to common spec inputs, the IngestorCluster resource provides the following Spec configuration parameters.

| Key        | Type    | Required | Description                                       |
| ---------- | ------- | -------- | ------------------------------------------------- |
| replicas   | integer | No | The number of ingestion pods (defaults to 1) |
| queueRef   | corev1.ObjectReference | Yes | Message queue reference |
| objectStorageRef   | corev1.ObjectReference | Yes | Object storage reference |

## Example

The example presented below configures an `IngestorCluster` named `ingestor` in the default namespace with three replicas serving ingestion traffic. The `Queue` and `ObjectStorage` references provide the queue and bucket settings. Credentials are configured on the referenced `Queue`. This example assumes the service account provides access through IRSA.

In this case, the setup uses SQS and S3 configuration: messages are written to `sqs-test` in `us-west-2`, with `sqs-dlq-test` as the dead-letter queue, and large messages are written to `ingestion/smartbus-test`.

```
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: ingestor
spec:
  serviceAccount: ingestor-sa 
  replicas: 3
  queueRef:
    name: queue
  objectStorageRef:
    name: os
```

# IndexerCluster

IndexerCluster supports index-only mode. When both references are set, its Splunk pods consume events from the remote queue and index them. When neither reference is set, the IndexerCluster uses its regular configuration.

## Spec

In addition to common spec inputs, the IndexerCluster resource provides the following Spec configuration parameters.

| Key        | Type    | Required | Description                                       |
| ---------- | ------- | -------- | ------------------------------------------------- |
| replicas   | integer | Yes | The number of indexer peers. Must be at least 3 |
| queueRef   | corev1.ObjectReference | No | Message queue reference |
| objectStorageRef   | corev1.ObjectReference | No | Object storage reference |

## Example

The example presented below configures an `IndexerCluster` named `indexer` with three peers. It references the same `Queue` and `ObjectStorage` resources as the ingestor. Credentials are configured on the referenced `Queue`, so the indexer pods use the same workload-identity or static-credential configuration.

In this case, the setup uses the SQS and S3 configuration described above.

```
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
spec: {}
---
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: indexer
spec:
  replicas: 3
  clusterManagerRef:
    name: cm
  serviceAccount: ingestor-sa
  queueRef:
    name: queue
  objectStorageRef:
    name: os
```

# Update

Although the `Queue` and `ObjectStorage` configuration values are immutable after creation, these references can be changed. Changing either reference causes SOK to regenerate the content-addressed defaults resources and update the corresponding StatefulSet declaratively.

There is no supported migration strategy for moving data from previously referenced resources, which means that the existing data will not be available through the new configuration.

# Configuration Flow

When an `IngestorCluster` or `IndexerCluster` references a `Queue` and an `ObjectStorage` resource, SOK resolves those references and builds the SmartBus configuration for the selected tier. The external queue, dead-letter queue, bucket, and path remain customer-managed resources.

It is the user's responsibility to configure the connections between these resources so that traffic is routed correctly. Ensure that the participating resources reference the intended shared `Queue` and `ObjectStorage` resources so data can be served by the deployment and shared across the connected tiers.

- For an `IngestorCluster`, SOK configures the tier to publish events to the remote queue.
- For an `IndexerCluster`, SOK configures the tier to consume events from the remote queue and index them. Dead-letter queue handling is configured on the indexer side only.

SOK delivers the structural configuration separately from credentials. When `secretKeyRef` is configured, the referenced Secrets must be kept in the same namespace as the resource that uses them. When it is omitted, the pods are expected to use IRSA or another workload identity mechanism. The exact configuration files, resources, and delivery paths are implementation details managed by SOK.

Changes to the `Queue` or `ObjectStorage` references regenerate the configuration and update the corresponding StatefulSet declaratively. Changes to a source credential Secret are watched, and SOK creates a new credential configuration and rolls the affected pods.

# Configuration File References

For additional information about the relevant Splunk configuration files, see:

- [default-mode.conf configuration file reference](https://help.splunk.com/en/data-management/splunk-enterprise-admin-manual/10.4/configuration-file-reference/10.4.3-configuration-file-reference/default-mode.conf)
- [inputs.conf configuration file reference](https://help.splunk.com/en/data-management/splunk-enterprise-admin-manual/10.4/configuration-file-reference/10.4.3-configuration-file-reference/inputs.conf)
- [outputs.conf configuration file reference](https://help.splunk.com/en/data-management/splunk-enterprise-admin-manual/10.4/configuration-file-reference/10.4.3-configuration-file-reference/outputs.conf)

These links use a version-specific documentation path. Select the configuration-file reference that matches the Splunk Enterprise version used in your deployment.

# Common Spec

Common spec values for all SOK Custom Resources can be found in [CustomResources doc](../operate/CustomResources.md).

# Helm Charts

Queue, ObjectStorage, and IngestorCluster are supported by the `splunk/splunk-enterprise` Helm chart. IndexerCluster is enhanced with the references required for separated ingestion.

The Helm chart is one way to provision these resources. You can also create and apply the corresponding Kubernetes manifest files directly, as shown in the examples above.

## Example

Below examples describe how to define values for Queue, ObjectStorage, IngestorCluster and IndexerCluster similarly to the above yaml files specifications.

```
queue:
  enabled: true
  name: queue
  provider: sqs
  sqs:
    name: sqs-test
    authRegion: us-west-2
    endpoint: https://sqs.us-west-2.amazonaws.com
    dlq: sqs-dlq-test
```

To use static AWS credentials instead of workload identity, add the following optional block under `queue.sqs`:

```
secretKeyRef:
  awsAccessKey:
    name: s3-secret
    key: s3_access_key
  awsSecretKey:
    name: s3-secret
    key: s3_secret_key
```

```
objectStorage:
  enabled: true
  name: os
  provider: s3
  s3:
    endpoint: https://s3.us-west-2.amazonaws.com
    path: ingestion/smartbus-test
```

```
ingestorCluster:
  enabled: true
  name: ingestor
  replicaCount: 3
  serviceAccount: ingestor-sa 
  queueRef:
    name: queue
  objectStorageRef:
    name: os
```

```
clusterManager:
  enabled: true
  name: cm
  replicaCount: 1

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

# Service Account

To access the configured SQS queue and S3 bucket, provide both tiers with a service account backed by workload identity, or configure static AWS credentials through `Queue.spec.sqs.secretKeyRef`. Grant only the permissions required for the configured queue, dead-letter queue, and object-storage path. If an encryption key is provided for object storage, also grant the permissions required to use that key. It is up to the user to identify the minimum set of permissions needed for their environment and use case.

## Example

The example presented below creates the `ingestor-sa` service account by using the `eksctl` utility and attaches a customer-managed policy named `ExamplePolicy`. Define the policy with the minimum permissions required for your environment and use case.

```
eksctl create iamserviceaccount \                                                                                                                                          
  --name ingestor-sa \
  --cluster ind-ing-sep-demo \
  --region us-west-2 \
  --attach-policy-arn arn:aws:iam::<account-id>:policy/ExamplePolicy \
  --approve \
  --override-existing-serviceaccounts
```

## Documentation References

- [IAM Roles for Service Accounts on eksctl Docs](https://eksctl.io/usage/iamserviceaccounts/)

# Horizontal Pod Autoscaler

To automatically adjust the number of replicas to serve the ingestion traffic effectively, you can use Horizontal Pod Autoscaler, which scales the workload based on the actual demand. It enables the user to provide the metrics which are used to make decisions on removing unwanted replicas if there is not too much traffic or setting up the new ones if the traffic is too big to be handled by currently running resources.

HorizontalPodAutoscaler is a Kubernetes resource and is not managed by SOK. It can be used with `IngestorCluster`, but the manifest below is an example only. Configure the replica limits and metrics according to your workload.

## Example

The example presented below configures a HorizontalPodAutoscaler named ingestor-hpa in the default namespace (the same namespace as the resource it manages) to scale the `IngestorCluster` named `ingestor`. With average utilization set to 50%, the HPA scales the target between 3 and 10 replicas.

```                             
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: ingestor-hpa
spec:
  scaleTargetRef:
    apiVersion: enterprise.splunk.com/v4
    kind: IngestorCluster
    name: ingestor
  minReplicas: 3
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 50
```

## Documentation References

- [Horizontal Pod Autoscaling on Kubernetes Docs](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)

# App Installation for Ingestor Cluster Instances

Application installation is supported for `IngestorCluster` instances using local scope. SOK tracks application deployment status and polls the ingestor pods for `restart_required`. When a restart is required, SOK performs a rolling, PodDisruptionBudget-gated eviction of the ingestor pods. During an App Framework deployment, the eviction loop is paused so that transient restart signals from configuration writes do not cause unintended restarts.

# Example

1. Install Splunk Operator.

2. Create a service account, if applicable.

See the [Service Account](#service-account) section for workload-identity setup and required permissions.

3. Install Queue resource.

Create the external SQS queue and dead-letter queue. Then apply the Queue manifest described in the [Queue](#queue) section. This example uses workload identity. For static credentials, see the separate `secretKeyRef` example in that section.

4. Install ObjectStorage resource.

Create the S3 bucket and grant the configured workload identity or credential Secret access to the path, then apply the ObjectStorage manifest described in the [ObjectStorage](#objectstorage) section. The `ObjectStorage` CR does not create the bucket.

5. Install IngestorCluster resource.

Apply the IngestorCluster manifest described in the [IngestorCluster](#ingestorcluster) section.

6. Install IndexerCluster resource, if applicable.

Apply the ClusterManager and IndexerCluster manifests described in the [IndexerCluster](#indexercluster) section. Refer to [Configuration Flow](#configuration-flow) for the generated configuration files and resource delivery flow.

7. Install Horizontal Pod Autoscaler, if applicable.

See the [Horizontal Pod Autoscaler](#horizontal-pod-autoscaler) section for an example HPA manifest and configuration guidance. HPA is not managed by SOK.
