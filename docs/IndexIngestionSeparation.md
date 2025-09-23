# Background

Separation between ingestion and indexing services within Splunk Operator for Kubernetes enables the operator to independently manage the ingestion service while maintaining seamless integration with the indexing service.

This separation enables:
- Independent scaling: Match resource allocation to ingestion or indexing workload.
- Data durability: Off‑load buffer management and retry logic to a durable message bus.
- Operational clarity: Separate monitoring dashboards for ingestion throughput vs indexing latency.

# IngestorCluster

IngestorCluster is introduced for high‑throughput data ingestion into a durable message bus. Its Splunk pods are configured to receive events (outputs.conf) and publish them to a message bus. 

## Spec

In addition to common spec inputs, the IngestorCluster resource provides the following Spec configuration parameters.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| replicas   | integer | The number of replicas (defaults to 3) |
| pushBus   | PushBus | Message bus configuration for publishing messages |
| pipelineConfig   | PipelineConfig | Configuration for pipeline |

PushBus inputs can be found in the table below. As of now, only SQS type of message bus is supported.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| type   | string | Type of message bus (Only sqs_smartbus as of now) |
| sqs   | SQS | SQS message bus inputs  |

SQS message bus inputs can be found in the table below.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| queueName   | string | Name of the SQS queue |
| authRegion   | string | Region where the SQS queue is located  |
| endpoint   | string | AWS SQS endpoint (e.g. https://sqs.us-west-2.amazonaws.com) |
| largeMessageStoreEndpoint   | string | AWS S3 Large Message Store endpoint (e.g. https://s3.us-west-2.amazonaws.com) |
| largeMessageStorePath   | string | S3 path for Large Message Store (e.g. s3://bucket-name/directory) |
| deadLetterQueueName   | string | Name of the SQS dead letter queue |
| maxRetriesPerPart   | integer | Max retries per part for retry policy max_count (The only one supported as of now) |
| retryPolicy   | string | Retry policy (max_retry is the only one supported as of now) |
| sendInterval   | string | Send interval (e.g. 5s) |

PipelineConfig inputs can be found in the table below.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| remoteQueueRuleset   | bool | Disable remote queue ruleset |
| ruleSet   | bool | Disable rule set  |
| remoteQueueTyping   | bool | Disable remote queue typing  |
| remoteQueueOutput   | bool | Disable remote queue output  |
| typing   | bool | Disable typing  |
| indexerPipe   | bool | Disable indexer pipe  |

## Example

The example presented below configures IngestorCluster named ingestor with Splunk 9.4.4 image that resides in a default namespace and is scaled to 3 replicas that serve the ingestion traffic. This IngestorCluster custom resource is set up with the service account named ingestion-role-sa allowing it to perform SQS and S3 operations. Push Bus and Pipeline Config inputs allow the user to specify queue and bucket settings for the ingestion process. 

In this case, it is the SQS and S3 based configuration where the messages are stored in sqs-test queue in us-west-2 region with dead letter queue set to sqs-dlq-test queue. The large message store is set to ingestion bucket in smartbus-test directory. Retry policy is set to max count with max retries per part equal to 4 and send interval set to 5 seconds. Pipeline config either enables (false) or disables (true) settings such as remote queue ruleset, ruleset, remote quee typing, typing, remote queue output and indexer pipe. Based on these inputs, default-mode.conf and outputs.conf files are configured accordingly.

Change of any of the pushBus or pipelineConfig inputs does not restart Splunk. It just updates the config values with no disruptions.

```
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: ingestor
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  serviceAccount: ingestion-sa 
  replicas: 3
  image: splunk/splunk:9.4.4
  pushBus:
    type: sqs_smartbus
    sqs:
      queueName: sqs-test
      authRegion: us-west-2
      endpoint: https://sqs.us-west-2.amazonaws.com
      largeMessageStoreEndpoint: https://s3.us-west-2.amazonaws.com
      largeMessageStorePath: s3://ingestion/smartbus-test
      deadLetterQueueName: sqs-dlq-test
      maxRetriesPerPart: 4
      retryPolicy: max_count
      sendInterval: 5s
  pipelineConfig:
    remoteQueueRuleset: false
    ruleSet: true
    remoteQueueTyping: false
    remoteQueueOutput: false
    typing: true
    indexerPipe: true
```

# IndexerCluster

IndexerCluster is enhanced to support index‑only mode enabling independent scaling, loss‑safe buffering, and simplified day‑0/day‑n management via Kubernetes CRDs. Its Splunk pods are configured to pull events from the bus (inputs.conf) and index them.

## Spec

In addition to common spec inputs, the IndexerCluster resource provides the following Spec configuration parameters.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| replicas   | integer | The number of replicas (defaults to 3) |
| pullBus   | PushBus | Message bus configuration for pulling messages |
| pipelineConfig   | PipelineConfig | Configuration for pipeline |

PullBus inputs can be found in the table below. As of now, only SQS type of message bus is supported.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| type   | string | Type of message bus (Only sqs_smartbus as of now) |
| sqs   | SQS | SQS message bus inputs  |

SQS message bus inputs can be found in the table below.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| queueName   | string | Name of SQS queue |
| authRegion   | string | Region where the SQS is located  |
| endpoint   | string | AWS SQS endpoint (e.g. https://sqs.us-west-2.amazonaws.com) |
| largeMessageStoreEndpoint   | string | AWS S3 Large Message Store endpoint (e.g. https://s3.us-west-2.amazonaws.com) |
| largeMessageStorePath   | string | S3 path for Large Message Store (e.g. s3://bucket-name/directory) |
| deadLetterQueueName   | string | Name of SQS dead letter queue |
| maxRetriesPerPart   | integer | Max retries per part for retry policy max_count (The only one supported as of now) |
| retryPolicy   | string | Retry policy (max_retry is the only one supported as of now) |
| sendInterval   | string | Send interval (e.g. 5s) |

PipelineConfig inputs can be found in the table below.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| remoteQueueRuleset   | bool | Disable remote queue ruleset |
| ruleSet   | bool | Disable rule set  |
| remoteQueueTyping   | bool | Disable remote queue typing  |
| remoteQueueOutput   | bool | Disable remote queue output  |
| typing   | bool | Disable typing  |
| indexerPipe   | bool | Disable indexer pipe  |

## Example

The example presented below configures IndexerCluster named indexer with Splunk 9.4.4 image that resides in a default namespace and is scaled to 3 replicas that serve the indexing traffic. This IndexerCluster custom resource is set up with the service account named ingestion-role-sa allowing it to perform SQS and S3 operations. Pull Bus and Pipeline Config inputs allow the user to specify queue and bucket settings for the indexing process. 

In this case, it is the SQS and S3 based configuration where the messages are stored in and retrieved from sqs-test queue in us-west-2 region with dead letter queue set to sqs-dlq-test queue. The large message store is set to ingestion bucket in smartbus-test directory. Retry policy is set to max count with max retries per part equal to 4 and send interval set to 5 seconds. Pipeline config either enables (false) or disables (true) settings such as remote queue ruleset, ruleset, remote quee typing, typing and remote queue output. Based on these inputs, default-mode.conf, inputs.conf and outputs.conf files are configured accordingly.

Change of any of the pullBus or pipelineConfig inputs does not restart Splunk. It just updates the config values with no disruptions.

```
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  serviceAccount: ingestion-sa 
  image: splunk/splunk:9.4.4
---
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: indexer
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  clusterManagerRef:
    name: cm
  serviceAccount: ingestion-role-sa
  replicas: 3 
  image: splunk/splunk:9.4.4
  pullBus:
    type: sqs_smartbus
    sqs:
      queueName: sqs-test
      authRegion: us-west-2
      endpoint: https://sqs.us-west-2.amazonaws.com
      largeMessageStoreEndpoint: https://s3.us-west-2.amazonaws.com
      largeMessageStorePath: s3://ingestion/smartbus-test
      deadLetterQueueName: sqs-dlq-test
      maxRetriesPerPart: 4
      retryPolicy: max_count
      sendInterval: 5s
  pipelineConfig:
    remoteQueueRuleset: false
    ruleSet: true
    remoteQueueTyping: false
    remoteQueueOutput: false
    typing: true
```

# Common Spec

The spec section is used to define the desired state for a resource. All custom resources provided by the Splunk Operator include the following
configuration parameters.

| Key                   | Type       | Description                                                                                                |
| --------------------- | ---------- | ---------------------------------------------------------------------------------------------------------- |
| image                 | string     | Container image to use for pod instances (overrides RELATED_IMAGE_SPLUNK_ENTERPRISE environment variable) |
| imagePullPolicy       | string     | Sets pull policy for all images (either "Always" or the default: "IfNotPresent")                           |
| livenessInitialDelaySeconds       | number     | Sets the initialDelaySeconds for liveness probe (default: 300)                           |
| readinessInitialDelaySeconds       | number     | Sets the initialDelaySeconds for readiness probe (default: 10)                           |
| extraEnv       | [EnvVar](https://v1-17.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#envvar-v1-core)     | Sets the extra environment variables to be passed to the Splunk instance containers (WARNING: Setting environment variables used by Splunk or Ansible will affect Splunk installation and operation)                          |
| schedulerName         | string     | Name of [Scheduler](https://kubernetes.io/docs/concepts/scheduling/kube-scheduler/) to use for pod placement (defaults to "default-scheduler") |
| affinity              | [Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#affinity-v1-core) | [Kubernetes Affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity) rules that control how pods are assigned to particular nodes |
| resources             | [ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | The settings for allocating [compute resource requirements](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/) to use for each pod instance (The default settings should be considered for demo/test purposes.  Please see [Hardware Resource Requirements](https://github.com/splunk/splunk-operator/blob/develop/docs/README.md#hardware-resources-requirements) for production values.) |
| serviceTemplate       | [Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#service-v1-core) | Template used to create [Kubernetes services](https://kubernetes.io/docs/concepts/services-networking/service/) |
| topologySpreadConstraint       | [TopologySpreadConstraint](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) | Template used to create [Kubernetes TopologySpreadConstraint](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) |

The following additional configuration parameters may be used for all Splunk Enterprise resources.

| Key                | Type    | Description                                                                   |
| ------------------ | ------- | ----------------------------------------------------------------------------- |
| etcVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk etc volume as described in [StorageClass](StorageClass.md) |
| varVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk var volume as described in [StorageClass](StorageClass.md) |
| volumes            | [Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volume-v1-core) | List of one or more [Kubernetes volumes](https://kubernetes.io/docs/concepts/storage/volumes/) (These will be mounted in all container pods as `/mnt/<name>`) |
| defaults           | string  | Inline map of [default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) used to initialize the environment |
| defaultsUrl        | string  | Full path or URL for one or more [default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) files (separated by commas) |
| licenseUrl         | string  | Full path or URL for a Splunk Enterprise license file                         |
| licenseManagerRef   | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectreference-v1-core) | Reference to a Splunk Operator managed LicenseManager instance (via name and optionally namespace) to use for licensing |
| clusterManagerRef  | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectreference-v1-core) | Reference to a Splunk Operator managed ClusterManager instance (via name and optionally namespace) to use for indexing |
| monitoringConsoleRef  | string     | Logical name assigned to the Monitoring Console pod (You can set the name before or after the MC pod creation) |
| serviceAccount | [ServiceAccount](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/) | Represents the service account used by the pods deployed by the CRD |
| extraEnv | [EnvVar](https://v1-17.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#envvar-v1-core) | Extra environment variables to be passed to the Splunk instance containers |
| readinessInitialDelaySeconds | number | Defines initialDelaySeconds for readiness probe |
| livenessInitialDelaySeconds | number | Defines initialDelaySeconds for the liveness probe |
| imagePullSecrets | [ImagePullSecrets](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/) | Config to pull images from private registry (Use in conjunction with image config from [common spec](#common-spec-parameters-for-all-resources)) |

# Service Account

To be able to configure ingestion and indexing resources correctly in a secure manner, it is required to provide these resources with the service account that is configured with a minimum set of permissions to complete required operations. With this provided, the right credentials are used by Splunk to peform its tasks.

## Example

The example presented below configures the ingestion-sa service account by using esctl utility. It sets up the service account for cluster-name cluster in region us-west-2 with AmazonS3FullAccess and AmazonSQSFullAccess access policies. 

```
eksctl create iamserviceaccount \                                                                                                                                          
  --name ingestor-sa \
  --cluster ind-ing-sep-demo \
  --region us-west-2 \
  --attach-policy-arn arn:aws:iam::aws:policy/AmazonS3FullAccess \
  --attach-policy-arn arn:aws:iam::aws:policy/AmazonSQSFullAccess \
  --approve \
  --override-existing-serviceaccounts
```

```
$ kubectl describe sa ingestor-sa                                                                                                                      
Name:                ingestor-sa
Namespace:           default
Labels:              app.kubernetes.io/managed-by=eksctl
Annotations:         eks.amazonaws.com/role-arn: arn:aws:iam::111111111111:role/eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123
Image pull secrets:  <none>
Mountable secrets:   <none>
Tokens:              <none>
Events:              <none>
```

```
$ aws iam get-role --role-name eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123
{
    "Role": {
        "Path": "/",
        "RoleName": "eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123",
        "RoleId": "123456789012345678901",
        "Arn": "arn:aws:iam::111111111111:role/eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123",
        "CreateDate": "2025-08-07T12:03:31+00:00",
        "AssumeRolePolicyDocument": {
            "Version": "2012-10-17",
            "Statement": [
                {
                    "Effect": "Allow",
                    "Principal": {
                        "Federated": "arn:aws:iam::111111111111:oidc-provider/oidc.eks.us-west-2.amazonaws.com/id/1234567890123456789012345678901"
                    },
                    "Action": "sts:AssumeRoleWithWebIdentity",
                    "Condition": {
                        "StringEquals": {
                            "oidc.eks.us-west-2.amazonaws.com/id/1234567890123456789012345678901:aud": "sts.amazonaws.com",
                            "oidc.eks.us-west-2.amazonaws.com/id/1234567890123456789012345678901:sub": "system:serviceaccount:default:ingestion-sa"
                        }
                    }
                }
            ]
        },
        "Description": "",
        "MaxSessionDuration": 3600,
        "Tags": [
            {
                "Key": "alpha.eksctl.io/cluster-name",
                "Value": "ind-ing-sep-demo"
            },
            {
                "Key": "alpha.eksctl.io/iamserviceaccount-name",
                "Value": "default/ingestion-sa"
            },
            {
                "Key": "alpha.eksctl.io/eksctl-version",
                "Value": "0.211.0"
            },
            {
                "Key": "eksctl.cluster.k8s.io/v1alpha1/cluster-name",
                "Value": "ind-ing-sep-demo"
            }
        ],
        "RoleLastUsed": {
            "LastUsedDate": "2025-08-18T08:47:27+00:00",
            "Region": "us-west-2"
        }
    }
}
```

```
$ aws iam list-attached-role-policies --role-name eksctl-cluster-name-addon-iamserviceac-Role1-123456789123
{
    "AttachedPolicies": [
        {
            "PolicyName": "AmazonSQSFullAccess",
            "PolicyArn": "arn:aws:iam::aws:policy/AmazonSQSFullAccess"
        },
        {
            "PolicyName": "AmazonS3FullAccess",
            "PolicyArn": "arn:aws:iam::aws:policy/AmazonS3FullAccess"
        }
    ]
}
```

## Documentation References

- [IAM Roles for Service Accounts on eksctl Docs](https://eksctl.io/usage/iamserviceaccounts/)

# Horizontal Pod Autoscaler

To automatically adjust the number of replicas to serve the ingestion traffic effectively, it is recommended to use Horizontal Pod Autoscaler which scales the workload based on the actual demand. It enables the user to provide the metrics which are used to make decisions on removing unwanted replicas if there is not too much traffic or setting up the new ones if the traffic is too big to be handled by currently running resources. 

## Example

The exmaple presented below configures HorizontalPodAutoscaler named ingestor-hpa that resides in a default namespace to scale IngestorCluster custom resource named ingestor. With average utilization set to 50, the HorizontalPodAutoscaler resource will try to keep the average utilization of the pods in the scaling target at 50%. It will be able to scale the replicas starting from the minimum number of 3 with the maximum number of 10 replicas.

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

# Grafana

In order to monitor the resources, Grafana could be installed and configured on the cluster to present the setup on a dashabord in a series of useful diagrams and metrics. 

## Example

In the following example, the dashboard presents ingestion and indexing data in the form of useful diagrams and metrics such as number of replicas or resource consumption. 

```
{
  "id": null,
  "uid": "splunk-autoscale",
  "title": "Splunk Ingestion & Indexer Autoscaling with I/O & PV",
  "schemaVersion": 27,
  "version": 12,
  "refresh": "5s",
  "time": { "from": "now-30m", "to": "now" },
  "timezone": "browser",
  "style": "dark",
  "tags": ["splunk","autoscale","ingestion","indexer","io","pv"],
  "graphTooltip": 1,
  "panels": [
    { "id": 1,  "type": "stat",       "title": "Ingestion Replicas",       "gridPos": {"x":0,"y":0,"w":4,"h":4}, "targets":[{"expr":"kube_statefulset_replicas{namespace=\"default\",statefulset=\"splunk-ingestor-ingestor\"}"}], "options": {"reduceOptions":{"calcs":["last"]},"orientation":"horizontal","colorMode":"value","graphMode":"none","textMode":"value","thresholds":{"mode":"absolute","steps":[{"value":null,"color":"#73BF69"},{"value":5,"color":"#EAB839"},{"value":8,"color":"#BF1B00"}]}}},
    { "id": 2,  "type": "stat",       "title": "Indexer Replicas",       "gridPos": {"x":4,"y":0,"w":4,"h":4}, "targets":[{"expr":"kube_statefulset_replicas{namespace=\"default\",statefulset=\"splunk-indexer-indexer\"}"}], "options": {"reduceOptions":{"calcs":["last"]},"orientation":"horizontal","colorMode":"value","graphMode":"none","textMode":"value","thresholds":{"mode":"absolute","steps":[{"value":null,"color":"#73BF69"},{"value":5,"color":"#EAB839"},{"value":8,"color":"#BF1B00"}]}}},
    { "id": 3,  "type": "timeseries","title": "Ingestion CPU (cores)","gridPos": {"x":8,"y":0,"w":8,"h":4},"targets":[{"expr":"sum(rate(container_cpu_usage_seconds_total{namespace=\"default\",pod=~\"splunk-ingestor-ingestor-.*\"}[1m]))","legendFormat":"CPU (cores)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#FFA600"}}},
    { "id": 4,  "type": "timeseries","title": "Ingestion Memory (MiB)","gridPos": {"x":16,"y":0,"w":8,"h":4},"targets":[{"expr":"sum(container_memory_usage_bytes{namespace=\"default\",pod=~\"splunk-ingestor-ingestor-.*\"}) / 1024 / 1024","legendFormat":"Memory (MiB)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#00AF91"}}},
    { "id": 5,  "type": "timeseries","title": "Ingestion Network In (KB/s)","gridPos": {"x":0,"y":8,"w":8,"h":4},"targets":[{"expr":"sum(rate(container_network_receive_bytes_total{namespace=\"default\",pod=~\"splunk-ingestor-ingestor-.*\"}[1m])) / 1024","legendFormat":"Net In (KB/s)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#59A14F"}}},
    { "id": 6,  "type": "timeseries","title": "Ingestion Network Out (KB/s)","gridPos": {"x":8,"y":8,"w":8,"h":4},"targets":[{"expr":"sum(rate(container_network_transmit_bytes_total{namespace=\"default\",pod=~\"splunk-ingestor-ingestor-.*\"}[1m])) / 1024","legendFormat":"Net Out (KB/s)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#E15759"}}},
    { "id": 7,  "type": "timeseries","title": "Indexer CPU (cores)","gridPos": {"x":16,"y":4,"w":8,"h":4},"targets":[{"expr":"sum(rate(container_cpu_usage_seconds_total{namespace=\"default\",pod=~\"splunk-indexer-indexer-.*\"}[1m]))","legendFormat":"CPU (cores)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#7D4E57"}}},
    { "id":8,  "type": "timeseries","title": "Indexer Memory (MiB)","gridPos": {"x":0,"y":12,"w":8,"h":4},"targets":[{"expr":"sum(container_memory_usage_bytes{namespace=\"default\",pod=~\"splunk-indexer-indexer-.*\"}) / 1024 / 1024","legendFormat":"Memory (MiB)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#4E79A7"}}},
    { "id":9,  "type": "timeseries","title": "Indexer Network In (KB/s)","gridPos": {"x":8,"y":12,"w":8,"h":4},"targets":[{"expr":"sum(rate(container_network_receive_bytes_total{namespace=\"default\",pod=~\"splunk-indexer-indexer-.*\"}[1m])) / 1024","legendFormat":"Net In (KB/s)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#9467BD"}}},
    { "id":10,  "type": "timeseries","title": "Indexer Network Out (KB/s)","gridPos": {"x":16,"y":12,"w":8,"h":4},"targets":[{"expr":"sum(rate(container_network_transmit_bytes_total{namespace=\"default\",pod=~\"splunk-indexer-indexer-.*\"}[1m])) / 1024","legendFormat":"Net Out (KB/s)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#8C564B"}}},
    { "id":11,  "type": "timeseries","title": "Ingestion Disk Read (KB/s)","gridPos": {"x":0,"y":16,"w":8,"h":4},"targets":[{"expr":"sum(rate(container_fs_reads_bytes_total{namespace=\"default\",pod=~\"splunk-ingestor-ingestor-.*\"}[1m])) / 1024","legendFormat":"Disk Read (KB/s)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#1F77B4"}}},
    { "id":12,  "type": "timeseries","title": "Ingestion Disk Write (KB/s)","gridPos": {"x":8,"y":16,"w":8,"h":4},"targets":[{"expr":"sum(rate(container_fs_writes_bytes_total{namespace=\"default\",pod=~\"splunk-ingestor-ingestor-.*\"}[1m])) / 1024","legendFormat":"Disk Write (KB/s)"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"},"color":{"mode":"fixed","fixedColor":"#FF7F0E"}}},
    { "id":13,  "type": "timeseries","title": "Indexer PV Usage (GiB)","gridPos": {"x":0,"y":20,"w":8,"h":4},"targets":[{"expr":"kubelet_volume_stats_used_bytes{namespace=\"default\",persistentvolumeclaim=~\".*-indexer-.*\"} / 1024 / 1024 / 1024","legendFormat":"Used GiB"},{"expr":"kubelet_volume_stats_capacity_bytes{namespace=\"default\",persistentvolumeclaim=~\".*-indexer-.*\"} / 1024 / 1024 / 1024","legendFormat":"Capacity GiB"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"}}},
    { "id":14,  "type": "timeseries","title": "Ingestion PV Usage (GiB)","gridPos": {"x":8,"y":20,"w":8,"h":4},"targets":[{"expr":"kubelet_volume_stats_used_bytes{namespace=\"default\",persistentvolumeclaim=~\".*-ingestor-.*\"} / 1024 / 1024 / 1024","legendFormat":"Used GiB"},{"expr":"kubelet_volume_stats_capacity_bytes{namespace=\"default\",persistentvolumeclaim=~\".*-ingestor-.*\"} / 1024 / 1024 / 1024","legendFormat":"Capacity GiB"}],"options":{"legend":{"displayMode":"list","placement":"bottom"},"yAxis":{"mode":"auto"}}}
  ]
}
```

## Documentation References

- [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack)

# Example

1. Install CRDs and Splunk Operator for Kubernetes.

- SOK_IMAGE_VERSION: version of the image for Splunk Operator for Kubernetes

```
$ make install
```

```
$ kubectl apply -f SOK_IMAGE_VERSION/splunk-operator-cluster.yaml --server-side
```

```
$ kubectl get po -n splunk-operator                          
NAME                                                  READY   STATUS    RESTARTS   AGE
splunk-operator-controller-manager-785b89d45c-dwfkd   2/2     Running   0          4d3h
```

2. Create a service account.

```
$ eksctl create iamserviceaccount \                                                                                                                                          
  --name ingestor-sa \
  --cluster ind-ing-sep-demo \
  --region us-west-2 \
  --attach-policy-arn arn:aws:iam::aws:policy/AmazonS3FullAccess \
  --attach-policy-arn arn:aws:iam::aws:policy/AmazonSQSFullAccess \
  --approve \
  --override-existing-serviceaccounts
```

```
$ kubectl describe sa ingestor-sa                                                                                                                      
Name:                ingestor-sa
Namespace:           default
Labels:              app.kubernetes.io/managed-by=eksctl
Annotations:         eks.amazonaws.com/role-arn: arn:aws:iam::111111111111:role/eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123
Image pull secrets:  <none>
Mountable secrets:   <none>
Tokens:              <none>
Events:              <none>
```

```
$ aws iam get-role --role-name eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123
{
    "Role": {
        "Path": "/",
        "RoleName": "eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123",
        "RoleId": "123456789012345678901",
        "Arn": "arn:aws:iam::111111111111:role/eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123",
        "CreateDate": "2025-08-07T12:03:31+00:00",
        "AssumeRolePolicyDocument": {
            "Version": "2012-10-17",
            "Statement": [
                {
                    "Effect": "Allow",
                    "Principal": {
                        "Federated": "arn:aws:iam::111111111111:oidc-provider/oidc.eks.us-west-2.amazonaws.com/id/1234567890123456789012345678901"
                    },
                    "Action": "sts:AssumeRoleWithWebIdentity",
                    "Condition": {
                        "StringEquals": {
                            "oidc.eks.us-west-2.amazonaws.com/id/1234567890123456789012345678901:aud": "sts.amazonaws.com",
                            "oidc.eks.us-west-2.amazonaws.com/id/1234567890123456789012345678901:sub": "system:serviceaccount:default:ingestion-sa"
                        }
                    }
                }
            ]
        },
        "Description": "",
        "MaxSessionDuration": 3600,
        "Tags": [
            {
                "Key": "alpha.eksctl.io/cluster-name",
                "Value": "ind-ing-sep-demo"
            },
            {
                "Key": "alpha.eksctl.io/iamserviceaccount-name",
                "Value": "default/ingestor-sa"
            },
            {
                "Key": "alpha.eksctl.io/eksctl-version",
                "Value": "0.211.0"
            },
            {
                "Key": "eksctl.cluster.k8s.io/v1alpha1/cluster-name",
                "Value": "ind-ing-sep-demo"
            }
        ],
        "RoleLastUsed": {
            "LastUsedDate": "2025-08-18T08:47:27+00:00",
            "Region": "us-west-2"
        }
    }
}
```

```
$ aws iam list-attached-role-policies --role-name eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123
{
    "AttachedPolicies": [
        {
            "PolicyName": "AmazonSQSFullAccess",
            "PolicyArn": "arn:aws:iam::aws:policy/AmazonSQSFullAccess"
        },
        {
            "PolicyName": "AmazonS3FullAccess",
            "PolicyArn": "arn:aws:iam::aws:policy/AmazonS3FullAccess"
        }
    ]
}
```

3. Install IngestorCluster resource.

```
$ cat ingestor.yaml          
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: ingestor
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  serviceAccount: ingestor-sa 
  replicas: 3
  image: splunk/splunk:9.4.4
  pushBus:
    type: sqs_smartbus
    sqs:
      queueName: ing-ind-separation-q
      authRegion: us-west-2
      endpoint: https://sqs.us-west-2.amazonaws.com
      largeMessageStoreEndpoint: https://s3.us-west-2.amazonaws.com
      largeMessageStorePath: s3://ing-ind-separation/smartbus-test
      deadLetterQueueName: ing-ind-separation-dlq
      maxRetriesPerPart: 4
      retryPolicy: max_count
      sendInterval: 5s
  pipelineConfig:
    remoteQueueRuleset: false
    ruleSet: true
    remoteQueueTyping: false
    remoteQueueOutput: false
    typing: true
    indexerPipe: true
```

```
$ kubectl apply -f ingestor.yaml     
```

```
$ kubectl get po 
NAME                         READY   STATUS    RESTARTS   AGE
splunk-ingestor-ingestor-0   1/1     Running   0          2m12s
splunk-ingestor-ingestor-1   1/1     Running   0          2m12s
splunk-ingestor-ingestor-2   1/1     Running   0          2m12s
```

```
$ kubectl describe ingestorcluster ingestor
Name:         ingestor
Namespace:    default
Labels:       <none>
Annotations:  <none>
API Version:  enterprise.splunk.com/v4
Kind:         IngestorCluster
Metadata:
  Creation Timestamp:  2025-08-18T09:49:45Z
  Generation:          1
  Resource Version:    12345678
  UID:                 12345678-1234-1234-1234-1234567890123
Spec:
  Image:  splunk/splunk:9.4.4
  Pipeline Config:
    Indexer Pipe:          true
    Remote Queue Output:   false
    Remote Queue Ruleset:  false
    Remote Queue Typing:   false
    Rule Set:              true
    Typing:                true
  Push Bus:
    Sqs:
      Auth Region:                   us-west-2
      Dead Letter Queue Name:        ing-ind-separation-dlq
      Endpoint:                      https://sqs.us-west-2.amazonaws.com
      Large Message Store Endpoint:  https://s3.us-west-2.amazonaws.com
      Large Message Store Path:      s3://ing-ind-separation/smartbus-test
      Max Retries Per Part:          4
      Queue Name:                    ing-ind-separation-q
      Retry Policy:                  max_count
      Send Interval:                 3s
    Type:                            sqs_smartbus
  Replicas:                          3
  Service Account:                   ingestor-sa
Status:
  App Context:
    App Repo:
      App Install Period Seconds:  90
      Defaults:
        Premium Apps Props:
          Es Defaults:
      Install Max Retries:  2
    Bundle Push Status:
    Is Deployment In Progress:  false
    Last App Info Check Time:   0
    Version:                    0
  Message:                      
  Phase:                        Ready
  Ready Replicas:               3
  Replicas:                     3
  Resource Rev Map:
  Selector:           app.kubernetes.io/instance=splunk-ingestor-ingestor
  Tel App Installed:  true
Events:               <none>
```

```
$ kubectl exec -it splunk-ingestor-ingestor-0 -- sh
$ kubectl exec -it splunk-ingestor-ingestor-1 -- sh
$ kubectl exec -it splunk-ingestor-ingestor-2 -- sh
sh-4.4$ env | grep AWS
AWS_DEFAULT_REGION=us-west-2
AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token
AWS_REGION=us-west-2
AWS_ROLE_ARN=arn:aws:iam::111111111111:role/eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123
AWS_STS_REGIONAL_ENDPOINTS=regional
sh-4.4$ cat /opt/splunk/etc/system/local/default-mode.conf 
[pipeline:remotequeueruleset]
disabled = false

[pipeline:ruleset]
disabled = true

[pipeline:remotequeuetyping]
disabled = false

[pipeline:remotequeueoutput]
disabled = false

[pipeline:typing]
disabled = true

[pipeline:indexerPipe]
disabled = true
    
sh-4.4$ cat /opt/splunk/etc/system/local/outputs.conf 
[remote_queue:ing-ind-separation-q]
remote_queue.max_count.sqs_smartbus.max_retries_per_part = 4
remote_queue.sqs_smartbus.auth_region = us-west-2
remote_queue.sqs_smartbus.dead_letter_queue.name = ing-ind-separation-dlq
remote_queue.sqs_smartbus.encoding_format = s2s
remote_queue.sqs_smartbus.endpoint = https://sqs.us-west-2.amazonaws.com
remote_queue.sqs_smartbus.large_message_store.endpoint = https://s3.us-west-2.amazonaws.com
remote_queue.sqs_smartbus.large_message_store.path = s3://ing-ind-separation/smartbus-test
remote_queue.sqs_smartbus.retry_policy = max_count
remote_queue.sqs_smartbus.send_interval = 5s
remote_queue.type = sqs_smartbus
```

4. Install IndexerCluster resource.

```
$ cat idxc.yaml 
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  image: splunk/splunk:9.4.4
  serviceAccount: ingestor-sa 
---
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: indexer
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  image: splunk/splunk:9.4.4
  replicas: 3
  clusterManagerRef:
    name: cm
  serviceAccount: ingestor-sa 
  pullBus:
    type: sqs_smartbus
    sqs:
      queueName: ing-ind-separation-q
      authRegion: us-west-2
      endpoint: https://sqs.us-west-2.amazonaws.com
      largeMessageStoreEndpoint: https://s3.us-west-2.amazonaws.com
      largeMessageStorePath: s3://ing-ind-separation/smartbus-test
      deadLetterQueueName: ing-ind-separation-dlq
      maxRetriesPerPart: 4
      retryPolicy: max_count
      sendInterval: 5s
  pipelineConfig:
    remoteQueueRuleset: false
    ruleSet: true
    remoteQueueTyping: false
    remoteQueueOutput: false
    typing: true
```

```
$ kubectl apply -f idxc.yaml 
```

```
$ kubectl get po
NAME                          READY   STATUS    RESTARTS   AGE
splunk-cm-cluster-manager-0   1/1     Running   0          15m
splunk-indexer-indexer-0      1/1     Running   0          12m
splunk-indexer-indexer-1      1/1     Running   0          12m
splunk-indexer-indexer-2      1/1     Running   0          12m
splunk-ingestor-ingestor-0    1/1     Running   0          27m
splunk-ingestor-ingestor-1    1/1     Running   0          29m
splunk-ingestor-ingestor-2    1/1     Running   0          31m
```

```
$ kubectl exec -it splunk-indexer-indexer-0  -- sh 
$ kubectl exec -it splunk-indexer-indexer-1  -- sh 
$ kubectl exec -it splunk-indexer-indexer-2  -- sh 
sh-4.4$ env | grep AWS
AWS_DEFAULT_REGION=us-west-2
AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token
AWS_REGION=us-west-2
AWS_ROLE_ARN=arn:aws:iam::111111111111:role/eksctl-ind-ing-sep-demo-addon-iamserviceac-Role1-123456789123
AWS_STS_REGIONAL_ENDPOINTS=regional
sh-4.4$ cat /opt/splunk/etc/system/local/inputs.conf 

[splunktcp://9997]
disabled = 0

[remote_queue:ing-ind-separation-q]
remote_queue.max_count.sqs_smartbus.max_retries_per_part = 4
remote_queue.sqs_smartbus.auth_region = us-west-2
remote_queue.sqs_smartbus.dead_letter_queue.name = ing-ind-separation-dlq
remote_queue.sqs_smartbus.endpoint = https://sqs.us-west-2.amazonaws.com
remote_queue.sqs_smartbus.large_message_store.endpoint = https://s3.us-west-2.amazonaws.com
remote_queue.sqs_smartbus.large_message_store.path = s3://ing-ind-separation/smartbus-test
remote_queue.sqs_smartbus.retry_policy = max_count
remote_queue.type = sqs_smartbus
sh-4.4$ cat /opt/splunk/etc/system/local/outputs.conf 
[remote_queue:ing-ind-separation-q]
remote_queue.max_count.sqs_smartbus.max_retries_per_part = 4
remote_queue.sqs_smartbus.auth_region = us-west-2
remote_queue.sqs_smartbus.dead_letter_queue.name = ing-ind-separation-dlq
remote_queue.sqs_smartbus.encoding_format = s2s
remote_queue.sqs_smartbus.endpoint = https://sqs.us-west-2.amazonaws.com
remote_queue.sqs_smartbus.large_message_store.endpoint = https://s3.us-west-2.amazonaws.com
remote_queue.sqs_smartbus.large_message_store.path = s3://ing-ind-separation/smartbus-test
remote_queue.sqs_smartbus.retry_policy = max_count
remote_queue.sqs_smartbus.send_interval = 5s
remote_queue.type = sqs_smartbus
sh-4.4$ cat /opt/splunk/etc/system/local/default-mode.conf 
[pipeline:remotequeueruleset]
disabled = false

[pipeline:ruleset]
disabled = true

[pipeline:remotequeuetyping]
disabled = false

[pipeline:remotequeueoutput]
disabled = false

[pipeline:typing]
disabled = true
```

5. Install Horizontal Pod Autoscaler for IngestorCluster.

```
$ cat hpa-ing.yaml 
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: ing-hpa
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

```
$ kubectl apply -f hpa-ing.yaml
```

```
$ kubectl get hpa              
NAME      REFERENCE                  TARGETS              MINPODS   MAXPODS   REPLICAS   AGE
ing-hpa   IngestorCluster/ingestor   cpu: <unknown>/50%   3         10        0          10s
```

```
kubectl top pod
NAME                             CPU(cores)   MEMORY(bytes)   
hec-locust-load-29270124-f86gj   790m         221Mi           
splunk-cm-cluster-manager-0      154m         1696Mi          
splunk-indexer-indexer-0         107m         1339Mi          
splunk-indexer-indexer-1         187m         1052Mi          
splunk-indexer-indexer-2         203m         1703Mi          
splunk-ingestor-ingestor-0       97m          517Mi           
splunk-ingestor-ingestor-1       64m          585Mi           
splunk-ingestor-ingestor-2       57m          565Mi  
```

```
$ kubectl get po   
NAME                             READY   STATUS    RESTARTS   AGE
hec-locust-load-29270126-szgv2   1/1     Running   0          30s
splunk-cm-cluster-manager-0      1/1     Running   0          41m
splunk-indexer-indexer-0         1/1     Running   0          38m
splunk-indexer-indexer-1         1/1     Running   0          38m
splunk-indexer-indexer-2         1/1     Running   0          38m
splunk-ingestor-ingestor-0       1/1     Running   0          53m
splunk-ingestor-ingestor-1       1/1     Running   0          55m
splunk-ingestor-ingestor-2       1/1     Running   0          57m
splunk-ingestor-ingestor-3       0/1     Running   0          116s
splunk-ingestor-ingestor-4       0/1     Running   0          116s
```

```
kubectl top pod
NAME                             CPU(cores)   MEMORY(bytes)   
hec-locust-load-29270126-szgv2   532m         72Mi            
splunk-cm-cluster-manager-0      91m          1260Mi          
splunk-indexer-indexer-0         112m         865Mi           
splunk-indexer-indexer-1         115m         855Mi           
splunk-indexer-indexer-2         152m         1696Mi          
splunk-ingestor-ingestor-0       115m         482Mi           
splunk-ingestor-ingestor-1       76m          496Mi           
splunk-ingestor-ingestor-2       156m         553Mi           
splunk-ingestor-ingestor-3       355m         846Mi           
splunk-ingestor-ingestor-4       1036m        979Mi   
```

```
kubectl get hpa
NAME      REFERENCE                  TARGETS         MINPODS   MAXPODS   REPLICAS   AGE
ing-hpa   IngestorCluster/ingestor   cpu: 115%/50%   3         10        10         8m54s
```

6. Generate fake load.

- HEC_TOKEN: HEC token for making fake calls

```
$ kubectl get secret splunk-default-secret -o yaml
apiVersion: v1
data:
  hec_token: HEC_TOKEN
  idxc_secret: YWJjZGVmMTIzNDU2Cg==
  pass4SymmKey: YWJjZGVmMTIzNDU2Cg==
  password: YWJjZGVmMTIzNDU2Cg==
  shc_secret: YWJjZGVmMTIzNDU2Cg==
kind: Secret
metadata:
  creationTimestamp: "2025-08-26T10:15:11Z"
  name: splunk-default-secret
  namespace: default
  ownerReferences:
  - apiVersion: enterprise.splunk.com/v4
    controller: false
    kind: IngestorCluster
    name: ingestor
    uid: 12345678-1234-1234-1234-1234567890123
  - apiVersion: enterprise.splunk.com/v4
    controller: false
    kind: ClusterManager
    name: cm
    uid: 12345678-1234-1234-1234-1234567890125
  - apiVersion: enterprise.splunk.com/v4
    controller: false
    kind: IndexerCluster
    name: indexer
    uid: 12345678-1234-1234-1234-1234567890124
  resourceVersion: "123456"
  uid: 12345678-1234-1234-1234-1234567890126
type: Opaque 
```

```
$ echo HEC_TOKEN | base64 -d         
HEC_TOKEN
```

```
cat loadgen.yaml 
apiVersion: v1
kind: ConfigMap
metadata:
  name: hec-locust-config
data:
  requirements.txt: |
    locust
    requests
    urllib3

  locustfile.py: |
    import urllib3
    from locust import HttpUser, task, between

    # disable insecure‐ssl warnings
    urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

    class HECUser(HttpUser):
        wait_time = between(1, 2)
        # use HTTPS and explicit port
        host = "https://splunk-ingestor-ingestor-service:8088"

        def on_start(self):
            # turn off SSL cert verification
            self.client.verify = False

        @task
        def send_event(self):
            token = "HEC_TOKEN"
            headers = {
                "Authorization": f"Splunk {token}",
                "Content-Type": "application/json"
            }
            payload = {"event": {"message": "load test", "value": 123}}
            # this will POST to https://…:8088/services/collector/event
            self.client.post(
                "/services/collector/event",
                json=payload,
                headers=headers,
                name="HEC POST"
            )
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: hec-locust-load
spec:
  schedule: "*/2 * * * *"
  concurrencyPolicy: Replace
  startingDeadlineSeconds: 60
  jobTemplate:
    spec:
      backoffLimit: 1
      template:
        spec:
          containers:
          - name: locust
            image: python:3.9-slim
            command:
              - sh
              - -c
              - |
                pip install --no-cache-dir -r /app/requirements.txt \
                  && exec locust \
                     -f /app/locustfile.py \
                     --headless \
                     -u 200 \
                     -r 50 \
                     --run-time 1m50s
            volumeMounts:
            - name: app
              mountPath: /app
          restartPolicy: OnFailure
          volumes:
          - name: app
            configMap:
              name: hec-locust-config
              defaultMode: 0755
```

```
kubectl apply -f loadgen.yaml
```

```
$ kubectl get cm                                  
NAME                                  DATA   AGE
hec-locust-config                     2      10s
kube-root-ca.crt                      1      5d2h
splunk-cluster-manager-cm-configmap   1      28m
splunk-default-probe-configmap        3      58m
splunk-indexer-indexer-configmap      1      28m
splunk-ingestor-ingestor-configmap    1      48m
```

```
$ kubectl get cj
NAME              SCHEDULE      TIMEZONE   SUSPEND   ACTIVE   LAST SCHEDULE   AGE
hec-locust-load   */2 * * * *   <none>     False     1        2s              26s
```

```
$ kubectl get po
NAME                             READY   STATUS    RESTARTS   AGE
hec-locust-load-29270114-zq7zz   1/1     Running   0          15s
splunk-cm-cluster-manager-0      1/1     Running   0          29m
splunk-indexer-indexer-0         1/1     Running   0          26m
splunk-indexer-indexer-1         1/1     Running   0          26m
splunk-indexer-indexer-2         1/1     Running   0          26m
splunk-ingestor-ingestor-0       1/1     Running   0          41m
splunk-ingestor-ingestor-1       1/1     Running   0          43m
splunk-ingestor-ingestor-2       1/1     Running   0          45m
```

```
$ aws s3 ls s3://ing-ind-separation/smartbus-test/
                           PRE 29DDC1B4-D43E-47D1-AC04-C87AC7298201/
                           PRE 43E16731-7146-4397-8553-D68B5C2C8634/
                           PRE C8A4D060-DE0D-4DCB-9690-01D8902825DC/
```