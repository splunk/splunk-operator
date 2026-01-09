# Splunk Operator for Kubernetes - CNCF Integration Analysis

**Date:** December 13, 2025  
**Analysis Target:** /Users/viveredd/Projects/splunk-operator  
**Git Branch:** develop  
**Analysis Scope:** Current operator capabilities, integration points, external dependencies, and CNCF tool alignment

---

## Executive Summary

The Splunk Operator is a mature Kubernetes controller managing Splunk Enterprise deployments across multiple topologies. It provides comprehensive lifecycle management through Custom Resource Definitions (CRDs), handles complex orchestration patterns (clustering, search head distribution), and integrates with multiple cloud storage providers. The operator exhibits strong CNCF patterns but has limited formal integration points for external tooling.

---

## 1. Current Operator Capabilities

### 1.1 Custom Resource Definitions (CRDs)

The operator defines seven primary CRDs in API versions v3 and v4:

#### **Standalone** (`api/v4/standalone_types.go`)
- Single or multiple independent Splunk instances
- Status tracking: Phase, Replicas, Ready Count, Message
- Supports SmartStore and App Framework configurations
- Resource scaling via `spec.replicas` field
- Pause/Resume capability via `standalone.enterprise.splunk.com/paused` annotation

**Key Capabilities:**
- Horizontal pod autoscaling ready (implements scale subresource)
- Status conditions: Pending → Ready/Updating/ScalingUp/ScalingDown/Error
- Telemetry app installation tracking
- Resource revision mapping for drift detection

#### **SearchHeadCluster** (`api/v4/searchheadcluster_types.go`)
- Multi-pod search head clusters with automatic captain election
- Separate Deployer pod for app distribution
- Captain management and member synchronization
- Maintenance mode support

**Status Tracking:**
- Individual member status with adhoc search capability flags
- Active search counts (historical and realtime)
- Captain ready state and cluster initialization status
- Reconciliation control via `searchheadcluster.enterprise.splunk.com/paused` annotation

#### **IndexerCluster** (`api/v4/indexercluster_types.go`)
- Multiple indexer peers with replication factor tracking
- Cluster manager coordination
- Multisite/multipart deployment support

#### **ClusterManager/ClusterMaster** (`api/v4/clustermanager_types.go`)
- Manages indexer clusters
- Bundle distribution (now ansible-controlled)
- Multisite configuration support

#### **LicenseManager** (`api/v4/licensemanager_types.go`)
- License administration and distribution
- Deprecates older LicenseMaster (v3)

#### **MonitoringConsole** (`api/v4/monitoringconsole_types.go`)
- Monitoring and alerting for other instances
- Distributed monitoring support

#### **Common Spec Structure** (`api/v4/common_types.go`)
- **Image Management:** Container image, pull policies, private registry secrets
- **Resource Management:** CPU/memory requests and limits per pod
- **Pod Placement:** Affinity rules, taints/tolerations, topology spread constraints
- **Storage:** EtcVolume (10Gi default) and VarVolume (100Gi default) with ephemeral options
- **Probes:** Customizable startup (40s), readiness (10s), and liveness (30s) probes

### 1.2 Automation & Lifecycle Management

**Reconciliation Loop:**
- Type-specific controllers in `internal/controller/` (StandaloneReconciler, SearchHeadClusterReconciler, etc.)
- Controller-runtime based with leader election support
- 15 concurrent workers (TotalWorker = 15 in common_types.go)
- Default 5-second reconciliation interval with exponential backoff

**StatefulSet Management:**
- OnDelete update strategy (manual pod recycling required)
- Parallel pod management policy (all pods created simultaneously)
- Automatic service creation (headless for DNS)
- PersistentVolumeClaim (PVC) management per pod

**Upgrade Orchestration:**
- Prometheus metrics for upgrade timing tracking
- Status phase progression: Pending → Updating → Ready
- Resource revision tracking to detect spec changes
- Finalizers for graceful deletion (`"enterprise.splunk.com/cleanup"`))

### 1.3 App Framework Implementation

Located in `pkg/splunk/enterprise/afwscheduler.go` and `configuration.go`

**Architecture:**
- Pluggable remote storage client system supporting:
  - AWS S3 and S3-compatible (MinIO)
  - Azure Blob Storage
  - Google Cloud Storage (GCS)
- Pipeline-based app deployment with multiple phases:
  1. Download from remote store
  2. Copy to pod storage
  3. Install on Splunk instance

**Features:**
- Configurable polling interval (1 hour default, 1 min to 1 day range)
- App installation scheduler with yield intervals (90s default, min 30s)
- Concurrent download limits (configurable)
- Max retry configuration (default 2 retries)
- Scope-based deployment: local, cluster, clusterWithPreConfig, premiumApps
- Enterprise Security app special handling with SSL options

**App Deployment Status Tracking:**
```
AppPkgDownloadPending → AppPkgDownloadInProgress → AppPkgDownloadComplete
AppPkgPodCopyPending → AppPkgPodCopyInProgress → AppPkgPodCopyComplete  
AppPkgInstallPending → AppPkgInstallInProgress → AppPkgInstallComplete
```

### 1.4 SmartStore & Remote Storage Integration

**SmartStore Capabilities** (`api/v4/common_types.go`):
- Multi-volume configuration with different providers
- Cache manager settings (eviction policies, concurrent operations)
- Index-specific configuration (maxGlobalDataSizeMB, hotlist recency)
- Global defaults for index settings

**Remote Storage Providers** (`pkg/splunk/client/`):
- **AWS S3:** Full support with region configuration
- **MinIO:** S3-compatible implementation
- **Azure Blob:** Azure SDK v2 integration
- **GCS:** Google Cloud Storage support

**Client Architecture:**
- Abstracted RemoteDataClient interface
- GetAppsList() - enumerate remote apps
- DownloadApp() - retrieve app packages
- RegisterRemoteDataClient() - dynamic provider registration
- Credential management via Kubernetes Secrets (SecretRef)

### 1.5 Monitoring & Observability Integration

**Prometheus Metrics** (`pkg/splunk/client/metrics/metrics.go`):
```go
- splunk_operator_reconcile_total
- splunk_operator_reconcile_error_total
- splunk_operator_error_total
- splunk_operator_module_duration_in_milliseconds
- splunk_upgrade_start_time / splunk_upgrade_end_time
- splunk_active_historical_search_count
- splunk_active_realtime_search_count
```

**Metrics Server:**
- Metrics endpoint: `:8080` (default) or `:8443` (secure)
- Standard controller-runtime metrics integration
- Search head cluster member metrics with pod labeling

### 1.6 Event System

**Kubernetes Event Publishing** (`pkg/splunk/enterprise/events.go`):
- K8EventPublisher creates Kubernetes events for:
  - Resource status changes
  - Validation failures
  - App deployment warnings
  - Configuration issues
- Event types: Normal and Warning
- Reason and message fields for audit trails

---

## 2. Integration Points

### 2.1 Webhook Integration

**Conversion Webhooks** (`config/crd/patches/webhook_in_*.yaml`):
- Configured for all CRDs (Standalones, SearchHeadClusters, IndexerClusters, etc.)
- Uses K8s ConversionReview API
- Path: `/convert` on webhook-service
- Enables version migration (v3 → v4)

**Webhook Configuration Pattern:**
```yaml
spec:
  conversion:
    strategy: Webhook
    webhook:
      clientConfig:
        service:
          namespace: system
          name: webhook-service
          path: /convert
```

**Integration Opportunity:**
- Webhooks disabled/not implemented in current codebase
- Can be extended for:
  - Custom validation (ValidatingAdmissionWebhook)
  - Resource mutation (MutatingAdmissionWebhook)
  - Version conversion automation

### 2.2 Event Handling

**Watch Types** (in controllers):
- Primary resource watch (CRD instances)
- Secondary resource watches (owned StatefulSets, Services, ConfigMaps, Secrets, PVCs)
- Pod owner references for lifecycle management
- Integrated via handler.EnqueueRequestForOwner()

**Integration Opportunity:**
- Events can trigger external webhooks
- External tools can create K8s events
- Annotation-based triggering patterns exist (`paused` annotations)

### 2.3 Status Reporting

**Structured Status Fields:**
- Phase (Pending, Ready, Updating, ScalingUp, ScalingDown, Terminating, Error)
- Replica counts (desired vs ready)
- Resource version maps for drift detection
- App context with deployment status
- SmartStore configuration state
- Member-level status for clusters

**Integration Opportunity:**
- Status subresources enable external status writers
- Can be queried by external monitoring tools
- Message field for human-readable status

### 2.4 RBAC & Service Accounts

**RBAC Configuration** (internal/controller/standalone_controller.go):
```go
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=standalones,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/exec,verbs=get;list;watch;create;update;patch;delete
```

**Service Account Support:**
- Configurable via `spec.serviceAccount`
- Defaults to namespace default service account
- Used for pod authentication to Splunk services

### 2.5 Configuration Management Hooks

**Configuration Sources:**
- Inline YAML (spec.defaults)
- Remote URLs (spec.defaultsUrl - comma-separated)
- App-specific defaults (spec.defaultsUrlApps)
- All passed to Splunk via SPLUNK_DEFAULTS_URL environment variable

**Integration Opportunity:**
- External tools can:
  - Host configuration files (HTTP/HTTPS)
  - Dynamically generate defaults based on cluster state
  - Provide environment-specific overrides

---

## 3. External Dependencies

### 3.1 Cloud Services & Storage

**Direct Cloud Service Integration:**
- **AWS:** EC2 IAM (IRSA), S3, OptionalConfiguration via AWS SDK v2
- **Azure:** Azure Blob Storage, Azure SDKs with managed identity support
- **Google Cloud:** GCS (Cloud Storage), GCP service account credentials
- **MinIO:** S3-compatible on-premises storage

**Credential Patterns:**
- Kubernetes Secrets referenced by SecretRef
- IAM role-based authentication (AWS IRSA)
- Managed identities (Azure)
- Service account keys (GCP)

**Environment Variables:**
```
SPLUNK_DEFAULTS_URL - Configuration file URLs
RELATED_IMAGE_SPLUNK_ENTERPRISE - Container image override
AWS_PROFILE, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY - AWS credentials
AZURE_STORAGE_ACCOUNT, AZURE_STORAGE_KEY - Azure credentials
```

### 3.2 Storage Patterns

**Persistent Storage:**
- EtcVolume (StorageClass-based PVC)
- VarVolume (StorageClass-based PVC)
- Optional ephemeral storage (emptyDir)
- Supports any Kubernetes StorageClass

**Remote Storage:**
- S3/MinIO for apps and SmartStore buckets
- Azure Blob for apps and SmartStore
- GCS for apps and SmartStore
- Separate endpoint configuration per volume

### 3.3 Container Image Management

**Image Dependencies:**
- Splunk Enterprise Docker image (splunk/splunk or custom)
- Private registry support via imagePullSecrets
- Image pull policies: Always or IfNotPresent
- Environment variable override: RELATED_IMAGE_SPLUNK_ENTERPRISE

**Ansible-Based Initialization:**
- Container runs ansible scripts during startup
- Configuration via /opt/splunk/etc default.yml
- Initialization environment variables
- Log output to /opt/splunk/var/log/

### 3.4 No Direct Database/Queue Integration

**Finding:** The operator does NOT directly integrate with:
- Databases (PostgreSQL, MySQL, etc.)
- Message queues (RabbitMQ, Kafka, etc.)
- Distributed tracing (Jaeger, etc.)
- Logging aggregation (ELK, Loki, etc.)

These would be indirect via:
- Splunk forwarding to external systems
- Container logs captured by kubelet
- Prometheus scraping metrics endpoint

---

## 4. CNCF Tool Compatibility & Opportunities

### 4.1 Current CNCF Tool Usage

**Tools Already Integrated:**

| Tool | Usage | File | Status |
|------|-------|------|--------|
| **Kubernetes** | Core dependency | cmd/main.go | Primary |
| **controller-runtime** | Operator framework | go.mod v0.19.0 | Primary |
| **Prometheus** | Metrics | pkg/splunk/client/metrics/ | Integrated |
| **logr/zap** | Structured logging | cmd/main.go | Integrated |

**Tools in Consideration:**

| Tool | Current Status | Potential Integration |
|------|-----------------|----------------------|
| **cert-manager** | Directory present but unused | Certificate provisioning for webhooks |
| **Kyverno** | Not present | Policy enforcement (image validation, etc.) |
| **OPA/Gatekeeper** | Not present | Authorization policies |
| **KEDA** | Not present | Metric-driven autoscaling |
| **Flux/ArgoCD** | Not present | GitOps deployment |

### 4.2 Potential Integration Points for CNCF Tools

#### A. **Kyverno** - Policy as Code
**Opportunity:** Validate Splunk deployments against policies
```yaml
- Enforce image registries
- Require resource requests/limits
- Mandate pod disruption budgets
- Control namespace labeling
```

#### B. **KEDA** - Kubernetes Event-Driven Autoscaling
**Opportunity:** Scale SearchHeadCluster replicas based on:
- Active search count metrics (currently exposed)
- Search queue depth from Splunk API
- Custom Prometheus queries

#### C. **Flux/ArgoCD** - GitOps
**Opportunity:** Manage Splunk CR lifecycle via Git
- Store Standalone, SearchHeadCluster CRs in Git
- Automated reconciliation with GitOps
- Version control for Splunk configurations

#### D. **cert-manager** - Certificate Management
**Opportunity:** Automate SSL/TLS certificates
- Use directory `/Users/viveredd/Projects/splunk-operator/cert-manager/`
- Generate certs for Splunk services
- Webhook certificate automation
- Splunk web UI certificates

#### E. **DAPR** - Distributed Application Runtime
**Opportunity:** Event-driven app deployments
- Use DAPR pubsub for app update notifications
- Binding to remote storage change events
- Distributed tracing integration

#### F. **Jaeger/OpenTelemetry** - Distributed Tracing
**Opportunity:** Instrument operator reconciliation
- Trace app framework pipeline stages
- Cross-pod dependency tracking
- Performance profiling of reconciliation

#### G. **Loki/Promtail** - Log Aggregation
**Opportunity:** Centralized operator logging
- Aggregate operator and Splunk logs
- Query across multiple deployments
- Alert on error patterns

#### H. **OPA/Gatekeeper** - Policy Enforcement
**Opportunity:** Authorization & compliance
- Enforce ClusterManager references exist before IndexerCluster creation
- Prevent invalid Splunk topology combinations
- License manager quota enforcement

### 4.3 Recommended Integration Priority

**High Priority:**
1. **Kyverno** - Low risk, high compliance value
2. **KEDA** - Leverages existing metrics
3. **Flux/ArgoCD** - Standard GitOps pattern

**Medium Priority:**
4. **cert-manager** - Infrastructure already present
5. **OpenTelemetry** - Observability enhancement

**Lower Priority (Splunk-specific needs):**
6. **OPA/Gatekeeper** - Custom policy rules required
7. **DAPR** - Architectural redesign needed

---

## 5. Detailed Architecture Insights

### 5.1 Reconciliation Pattern

**Controller Entry Point** (`internal/controller/standalone_controller.go`):
```
StandaloneReconciler.Reconcile()
  ├── Fetch CR instance
  ├── Check pause annotation
  ├── Validate spec
  ├── Call enterprise.ApplyStandalone()
  ├── Record metrics
  └── Return Result{Requeue, RequeueAfter}
```

**Enterprise Logic** (`pkg/splunk/enterprise/standalone.go`):
```
ApplyStandalone()
  ├── Initialize event publisher
  ├── Validate spec
  ├── Check SmartStore changes
  ├── Initialize app sources
  ├── Check app framework status
  ├── Apply SmartStore ConfigMap
  ├── Create/update StatefulSet
  ├── Monitor pod readiness
  └── Update CR status
```

### 5.2 Pod Lifecycle

**Initialization Process:**
1. StatefulSet creates pod with `splunk/splunk` image
2. Container entrypoint runs ansible playbooks
3. Splunk initialization from environment variables
4. Mounts PVCs for /opt/splunk/etc (10Gi) and /opt/splunk/var (100Gi)
5. Joins cluster (captain election, peer registration)
6. Reports ready status

**Security Context:**
```go
RunAsUser: 41812 (splunk user)
FSGroup: 41812
RunAsNonRoot: true
FSGroupChangePolicy: OnRootMismatch
Capabilities: Drop ALL, Add NET_BIND_SERVICE
AllowPrivilegeEscalation: false
SeccompProfile: RuntimeDefault
```

### 5.3 Configuration Propagation

**Default YAML Configuration:**
1. Operator mounts defaults via ConfigMap
2. Passed via SPLUNK_DEFAULTS_URL
3. Splunk ansible merges with image defaults
4. Custom overrides via spec.defaults (inline YAML)

**Runtime Configuration Updates:**
- OnDelete StatefulSet update strategy requires manual pod deletion
- Operator does NOT automatically recycle pods on spec changes
- This allows controlled cluster upgrades

---

## 6. Resource Tracking & State Management

### 6.1 Resource Revision Tracking
Located in `pkg/splunk/enterprise/util.go`:
- Maintains `ResourceRevMap` per CR
- Detects remote volume key changes
- Tracks SmartStore configuration versions
- Used for drift detection

### 6.2 Global Resource Tracker
```go
operatorResourceTracker
  ├── commonResourceTracker (mutex per resource)
  ├── storageTracker (disk space monitoring)
  └── appInstallPipeline (concurrent app deployment)
```

---

## 7. Testing & Validation Infrastructure

**Test Frameworks:**
- Ginkgo v2 (behavior-driven testing)
- Gomega (assertions)
- KinD (Kubernetes in Docker) via kuttl tests

**Test Coverage:**
- Unit tests for each component
- Integration tests in `test/` directory
- KUTTL end-to-end tests in `kuttl/` directory
- AWS/Azure/GCP-specific tests

---

## 8. Recommendations for CNCF Tool Integration

### 8.1 Immediate Actions

1. **Enable cert-manager** - Use existing cert-manager directory
   - Automate webhook certificate provisioning
   - Add cert-manager dependency to go.mod

2. **Export HPA Metrics** - Already have scale subresource
   - Document autoscaling patterns
   - Create example HPA configurations

3. **Standardize Webhook Implementation**
   - Implement ValidatingAdmissionWebhook
   - Implement MutatingAdmissionWebhook
   - Test webhook framework

### 8.2 Medium-Term Roadmap

1. **KEDA Integration**
   - Create scalers for active search counts
   - Document search-queue-based scaling
   - Add examples to docs

2. **GitOps Support**
   - Create Flux/ArgoCD application templates
   - Document CR management via Git
   - Add FluxCD notifications

3. **OpenTelemetry Instrumentation**
   - Add trace exporting to controller-runtime
   - Instrument app deployment pipeline
   - Create observability dashboards

### 8.3 Long-Term Vision

1. **Policy-as-Code Framework**
   - Kyverno policies for topology validation
   - OPA Gatekeeper for compliance
   - Multi-cluster policy orchestration

2. **Advanced Event Handling**
   - DAPR pubsub for cross-cluster events
   - Splunk app deployment notifications
   - SmartStore bucket migration events

3. **Cross-Operator Collaboration**
   - Strimzi (Kafka) for audit logs
   - Prometheus Operator for metrics management
   - Vault integration for secrets

---

## 9. API Surface Analysis

### 9.1 Watch Hooks
- StatefulSets (owned resources)
- Services (managed endpoints)
- ConfigMaps (configuration distribution)
- Secrets (credential management)
- PersistentVolumeClaims (storage)
- Pods (lifecycle events)

### 9.2 Webhook Opportunities
**Validation:**
- ClusterManager references must exist
- SmartStore volumes must be properly configured
- App sources must reference valid remote storage

**Mutation:**
- Default SmartStore settings
- Auto-inject resource limits
- Add pod disruption budgets
- Inject monitoring labels

---

## 10. Key Findings

### Strengths
- Well-structured controller-runtime implementation
- Multi-cloud storage support (AWS, Azure, GCP)
- Comprehensive metrics for observability
- Event-driven architecture
- Kubernetes-native patterns (finalizers, owner references)

### Limitations
- Webhooks configured but not implemented
- Limited external tool integration points
- No GitOps or policy engine support
- OnDelete update strategy requires manual orchestration
- No built-in autoscaling (only via scale subresource)

### Opportunities
- Kyverno for policy validation
- KEDA for intelligent autoscaling
- cert-manager for SSL automation
- Flux/ArgoCD for GitOps workflows
- OpenTelemetry for distributed tracing
- DAPR for event-driven integrations

---

## 11. Conclusion

The Splunk Operator is a production-ready Kubernetes controller with strong CNCF alignment. It provides excellent lifecycle management for Splunk Enterprise deployments and exposes several integration points through its CRD status, event system, and metrics. The operator would benefit significantly from:

1. **Formal CNCF tool integration** (Kyverno, KEDA, cert-manager)
2. **GitOps workflow support** (Flux/ArgoCD integration)
3. **Enhanced observability** (OpenTelemetry instrumentation)
4. **Policy enforcement** (Kyverno/OPA integration)

Current external dependencies are limited to cloud storage providers (S3, Azure Blob, GCS) and Kubernetes itself, making it suitable for multi-cloud deployments. The operator's architecture is extensible and ready for deeper CNCF tool integration.

---

**Files Analyzed:**
- `/Users/viveredd/Projects/splunk-operator/api/v4/*.go` - CRD definitions
- `/Users/viveredd/Projects/splunk-operator/internal/controller/*.go` - Controllers
- `/Users/viveredd/Projects/splunk-operator/pkg/splunk/enterprise/*.go` - Business logic
- `/Users/viveredd/Projects/splunk-operator/pkg/splunk/client/*.go` - Storage clients
- `/Users/viveredd/Projects/splunk-operator/cmd/main.go` - Operator entry point
- `/Users/viveredd/Projects/splunk-operator/go.mod` - Dependencies

