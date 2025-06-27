## Splunk AI Platform & Assistant Deployment Guide

**Audience:** This guide is written for Kubernetes users who are new to custom resources and the Splunk Operator. We explain key concepts, walk through each step clearly, and provide examples you can copy-and-paste.

### What You Will Learn

1. Key concepts and terminology
2. Prerequisites needed before you begin
3. How to install the Splunk Operator
4. Understanding the custom resources: **AIPlatform** and **AIService**
5. Field-by-field explanation of each resource spec
6. Sample YAML manifests you can use right away
7. Step-by-step deployment instructions
8. How to check if everything is working
9. Common issues and how to fix them

---

### Glossary of Terms

* **Kubernetes**: A system to run containerized applications.
* **Operator**: A Kubernetes controller that simplifies deployment and management of applications.
* **Custom Resource Definition (CRD)**: A way to extend Kubernetes with new object types, like `AIPlatform`.
* **Ray**: A framework for distributed computing (used here for AI workloads).
* **Weaviate**: A vector database for storing AI embeddings.
* **Sidecar**: An extra container that runs alongside your main application container to add functionality (logging, metrics, etc.).

---

### 1. Prerequisites

Before you start, make sure you have:

* A Kubernetes cluster (version 1.22 or newer)
* `kubectl` installed and pointed at your cluster
* Helm (optional, version 3+) if you prefer using charts
* A Splunk Enterprise or Splunk Cloud instance
* Storage (like S3, GCS, or Azure Blob) to hold AI applications and artifacts
* (Optional) Cert-Manager installed if you want automated TLS certificates

#### 1.1 Cloud Identity & Service Account Setup

To grant pods access to your object storage buckets (`s3://`, `gcs://`, `azure://`), configure a Kubernetes ServiceAccount with the appropriate cloud IAM integration:

* **AWS (IRSA)**: Annotate your ServiceAccount with an IAM role that has S3 permissions. For example:

  ```yaml
  apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: ai-platform-sa
    namespace: test
    annotations:
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/S3AccessRole
  ```
* **GCP (Workload Identity)**: Annotate with your GCP service account:

  ```yaml
  apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: ai-platform-sa
    namespace: test
    annotations:
      iam.gke.io/gcp-service-account: my-gcp-sa@project.iam.gserviceaccount.com
  ```
* **Azure (Pod Identity or MSI)**: Bind your identity to the pod via Azure Pod Identity or assign a Managed Identity. Example annotation for AAD Pod Identity:

  ```yaml
  apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: ai-platform-sa
    namespace: test
    annotations:
      aadpodidentitybinding: my-azure-identity-binding
  ```

Use this ServiceAccount in your `AIPlatform.spec.headGroupSpec.serviceAccountName` and `workerGroupSpec.serviceAccountName`, as well as in `AIService.spec.serviceAccountName`.

---

### 2. Installing the Splunk Operator Installing the Splunk Operator

The Splunk Operator (SOK) manages Splunk on Kubernetes, including our AI components.

#### Using Helm

```bash
helm repo add splunk-operator https://splunk.github.io/splunk-operator/
helm repo update
helm install splunk-operator \    # Name of the release
  splunk-operator/splunk-operator \  # Chart name
  --namespace splunk-operator \     # Dedicated namespace
  --create-namespace
```

#### Without Helm

```bash
git clone https://github.com/splunk/splunk-operator.git
cd splunk-operator/config
kubectl apply -f all-in-one.yaml
```

Verify installation:

```bash
kubectl get pods -n splunk-operator
kubectl get crds | grep splunkai
```

---

### 3. Custom Resources Overview

We introduce two new CRDs:

| CRD Name          | Short Name | Purpose                                              |
| ----------------- | ---------- | ---------------------------------------------------- |
| AIPlatform  | spai       | Runs distributed AI jobs on a Ray cluster + Weaviate |
| AIService | (none)     | Middleware service between Splunk UI and AI Platform |

These CRDs let you tell the operator how to create and manage AI workloads.

---

### 4. AIPlatform Spec Explained

Below is a simplified table of key fields. All fields are optional unless marked **required**.

| Field                    | Required? | Description                                                                 |
| ------------------------ | --------- | --------------------------------------------------------------------------- |
| `appsVolume.path`        | yes       | Storage URI (e.g., `s3://bucket/apps`) where your AI code lives             |
| `artifactsVolume.path`   | yes       | Storage URI (e.g., `s3://bucket`) for model data just the bucket name       |
| `headGroupSpec`          | yes       | Settings for the Ray head node (service account, node labels)               |
| `workerGroupSpec`        | yes       | Settings for Ray worker nodes, including GPU counts and replica limits      |
| `defaultAcceleratorType` | no        | GPU type (for example `nvidia-tesla-v100`)                                  |
| `Sidecars`               | no        | Booleans to enable Envoy, FluentBit, OpenTelemetry, and Prometheus sidecars |
| `SplunkConfiguration`    | yes       | Reference to your Splunk endpoint, token, and secret                        |
| `Weaviate.replicas`      | yes       | Number of Weaviate pods for your vector database                            |

*(The full API spec contains more fields. Start simple and add more as you learn.)*

---

### 5. AIService Spec Explained

This CR connects the Splunk UI, AI Platform, and Weaviate.

| Field                 | Required? | Description                                                 |
| --------------------- | --------- | ----------------------------------------------------------- |
| `taskVolume.path`     | yes       | Storage URI (e.g., `s3://bucket/tasks`)                     |
| `SplunkConfiguration` | yes       | Splunk endpoint, token, and secret                          |
| `VectorDbUrl`         | yes       | URL to your Weaviate service (e.g., `http://weaviate:8080`) |
| `AIPlatformRef` | yes       | Link to the `AIPlatform` resource that you created    |
| `replicas`            | no        | Number of middleware pods (default: 1)                      |
| `serviceAccountName`  | no        | Kubernetes service account for the assistant pods           |
| `Metrics.enabled`     | no        | Turn on Prometheus metrics scraping                         |
| `MTLS.enabled`        | no        | Enable mTLS (TLS within the cluster)                        |

---

### 6. Quick-Start Examples

Copy these manifests into files and apply them; then customize for your environment.

#### 6.1 AIPlatform Example

````yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: platform-issuer
  namespace: test
spec:
  isCA: true
  commonName: my-selfsigned-ca
  secretName: root-secret
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: selfsigned-issuer
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: my-ca-issuer
  namespace: test
spec:
  ca:
    secretName: root-secret
---
apiVersion: enterprise.splunk.com/v4
kind: AIPlatform
metadata:
  name: model-endpoints-vivekr
  namespace: test
  annotations:
    prometheus.io/path: /metrics
    prometheus.io/port: '8080'
    prometheus.io/scheme: http
    ray.io/overwrite-container-cmd: 'true'
  labels:
    app: model-endpoints-vivekr
    area: ai-platform
    team: ai-foundation
spec:
  # Object storage volume for application bundles & model artifacts
  appsVolume:
    path: s3://ai-platform-dev-vivekr/ray-services/ai-platform/applications
    region: us-west-2
    secretRef: s3-secret

  artifactsVolume:
    path: s3://ai-platform-dev-vivekr/ray-services/ai-platform/artifacts
    region: us-west-2
  defaultAcceleratorType: L40S

  headGroupSpec:
    serviceAccountName: ray-head-sa
    

    affinity:
      nodeAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 1
            preference:
              matchExpressions:
                - key: node.kubernetes.io/instance-type
                  operator: In
                  values:
                    - g6.24xlarge
    tolerations:
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule

  workerGroupSpec:
    serviceAccountName: ray-worker-sa
    gpuConfigs:
      - tier: g6.24xlarge-0-gpu
        minReplicas: 0
        maxReplicas: 10
        gpusPerPod: 1
        resources:
          requests:
            cpu: 16
            memory: 32Gi
            ephemeral-storage: 10Gi
            nvidia.com/gpu: 1
          limits:
            cpu: 16
            memory: 32Gi
            ephemeral-storage: 10Gi
            nvidia.com/gpu: 1

      - tier: g6.24xlarge-1-gpu
        minReplicas: 0
        maxReplicas: 10
        gpusPerPod: 2
        resources:
          requests:
            cpu: 16
            memory: 16Gi
            ephemeral-storage: 50Gi
            nvidia.com/gpu: 2
          limits:
            cpu: 16
            memory: 16Gi
            ephemeral-storage: 50Gi
            nvidia.com/gpu: 2

      - tier: g6.24xlarge-2-gpu
        minReplicas: 0
        maxReplicas: 10
        gpusPerPod: 2
        resources:
          requests:
            cpu: 4
            memory: 32Gi
            ephemeral-storage: 100Gi
            nvidia.com/gpu: 2
          limits:
            cpu: 4
            memory: 32Gi
            ephemeral-storage: 100Gi
            nvidia.com/gpu: 2

      - tier: g6.24xlarge-4-gpu
        minReplicas: 0
        maxReplicas: 10
        gpusPerPod: 4
        resources:
          requests:
            cpu: 8
            memory: 64Gi
            ephemeral-storage: 200Gi
            nvidia.com/gpu: 4
          limits:
            cpu: 8
            memory: 64Gi
            ephemeral-storage: 200Gi
            nvidia.com/gpu: 4

    tolerations:
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule
    affinity:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
          - matchExpressions:
            - key: node.kubernetes.io/instance-type
              operator: In
              values:
              - g6.24xlarge
  # sidecars
  sidecars:
    envoy: true
    fluentBit: true
    otel: true
    prometheusOperator: true

  # cert-manager Issuer for mTLS certificates
  certificateRef: platform-issuer

  # DNS suffix for in-cluster services
  clusterDomain: cluster.local
  splunkConfiguration:
    endpoint: https://splunk-standalone-standalone-service.test.svc.cluster.local:8088
    secretRef:
      name: splunk-test-secret
      namespace: test
  # Weaviate side-car or companion DB
  weaviate:
    replicas: 1
    resources:
      requests:
        cpu: 0.5
        memory: 1Gi
      limits:
        cpu: 1
        memory: 2Gi
    tolerations:
      - key: dedicated
        operator: Equal
        value: cpu
        effect: NoSchedule
```yaml
apiVersion: v4.splunk.cloud/v1
kind: AIPlatform
metadata:
  name: my-ai-platform
spec:
  appsVolume:
    path: s3://my-bucket/apps
  artifactsVolume:
    path: s3://my-bucket/artifacts
  headGroupSpec:
    serviceAccountName: ai-head-sa
  workerGroupSpec:
    serviceAccountName: ai-worker-sa
    gpuConfigs:
      - tier: default
        minReplicas: 1
        maxReplicas: 2
        gpusPerPod: 1
  SplunkConfiguration:
    crName: splunk-config
    crNamespace: splunk
    secretRef:
      name: splunk-secret
  Weaviate:
    replicas: 1
````

#### 6.2 AIService Example

````yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: test-issuer
  namespace: test
spec:
  selfSigned: {}

---
apiVersion: enterprise.splunk.com/v4
kind: AIService
metadata:
  name: my-ai-assistant
  namespace: test
spec:
  taskVolume:
    path: ai-assistant-prod-1
    region: us-west-2

  # 1) Reference an existing SplunkConfiguration CR
  splunkConfiguration:
    crName: standalone-sample
    crNamespace: test

  # 2) Point to your AI platform (which also sets VectorDbUrl for you)
  # aiPlatformRef:
  #   name: example-ai-platform
  #   namespace: test
  aiPlatformUrl: model-endpoints-vivekr-head-svc.test.svc.cluster.local:8000
  vectorDbUrl: model-endpoints-vivekr-weaviate.test.svc.cluster.local

  # 3) Deploy two replicas
  replicas: 1

  # 4) Node scheduling constraints
  tolerations:
    - key: dedicated
      operator: Equal
      value: cpu
      effect: NoSchedule
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node.kubernetes.io/instance-type
                operator: In
                values:
                  - m5.xlarge

  # 5) Enable Prometheus scraping via ServiceMonitor
  metrics:
    enabled: false
    path: /metrics
    port: 8088

  # 6) Turn on operator-managed mTLS for the SAIA Service
  mtls:
    enabled: true
    termination: operator
    issuerRef:
      name: test-issuer
      kind: Issuer
    secretName: my-ai-assistant-tls
    dnsNames:
      - my-ai-assistant.test.svc.cluster.local

  # 7) (optional) override the ServiceAccount name
  serviceAccountName: ray-worker-sa

  # 8) (optional) pass in extra env vars
  env:
    LOG_LEVEL: DEBUG
    FEATURE_FLAG: true


```yaml
apiVersion: v4.splunk.cloud/v1
kind: AIService
metadata:
  name: my-ai-assistant
spec:
  taskVolume:
    path: s3://my-bucket/tasks
  SplunkConfiguration:
    crName: splunk-config
    crNamespace: splunk
    secretRef:
      name: splunk-secret
  VectorDbUrl: http://my-ai-platform-ray-service:8080
  AIPlatformRef:
    kind: AIPlatform
    name: my-ai-platform
  replicas: 1
````

---

### 7. Deploying Your Resources

1. **Apply CRDs**

   ```bash
   kubectl apply -f config/crd/bases/v4.splunk.cloud_aiplatforms.yaml
   kubectl apply -f config/crd/bases/v4.splunk.cloud_aiservices.yaml
   ```
2. **Create Secrets** for Splunk credentials and storage access
3. **Deploy the AI Platform**

   ```bash
   kubectl apply -f my-ai-platform.yaml
   ```
4. **Wait for Status**

   ```bash
   kubectl wait --for=condition=Ready spai/my-ai-platform --timeout=5m
   ```
5. **Deploy the AI Assistant**

   ```bash
   kubectl apply -f my-ai-assistant.yaml
   ```
6. **Check Readiness**

   ```bash
   kubectl wait --for=condition=Ready aiservice/my-ai-assistant --timeout=5m
   ```

---

### 8. How to Check Status

Use these commands to see pod status, logs, and conditions:

* List pods: `kubectl get pods`
* View conditions in the CR status:

  ```bash
  kubectl get spai/my-ai-platform -o yaml | grep -A3 conditions
  ```
* View logs:

  ```bash
  kubectl logs deployment/my-ai-assistant
  ```

---

### 9. Troubleshooting Tips

* **Pod never starts**: Check your storage permissions and secret names.
* **CR stuck in Pending**: Ensure ray-operator and cert-manager are installed (or disabled in Helm).
* **Errors in logs**: Use `kubectl logs` on the pod and look for missing environment variables or incorrect URLs.

---

### 10. Where to Go Next

* Read the full API reference in `api/v4`
* Explore tuning Ray and Weaviate settings
* Learn more about sidecar logging and metrics
* Join the Splunk Operator community for tips and examples

Happy deploying! Feel free to ask questions as you explore.
