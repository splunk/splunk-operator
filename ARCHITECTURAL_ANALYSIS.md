# Splunk Operator Architectural Analysis

## Executive Summary

The Splunk Operator is a Kubernetes controller that manages Splunk Enterprise deployments. It uses Custom Resource Definitions (CRDs) to define declarative configurations and translates these into Kubernetes StatefulSets, Services, Secrets, and ConfigMaps. The operator heavily relies on the `splunk-docker` container image which runs ansible-based initialization scripts during pod startup.

## 1. CRD Definitions and Specs

### Main CRD Types (api/v4/)

**File:** `/Users/viveredd/Projects/splunk-operator/api/v4/`

1. **Standalone** (`standalone_types.go`)
   - Single Splunk instance
   - Supports SmartStore and App Framework configurations
   - Replicas field allows multiple standalone instances

2. **SearchHeadCluster** (`searchheadcluster_types.go`)
   - Multiple search head pods with captain election
   - Includes separate Deployer pod for app distribution
   - Tracks captain status, maintenance mode, and member synchronization

3. **IndexerCluster** (`indexercluster_types.go`)
   - Multiple indexer peers with cluster manager coordination
   - Tracks replication factor (RF) and cluster initialization
   - Supports multisite/multipart deployments

4. **ClusterManager** / **ClusterMaster** (`clustermanager_types.go`)
   - Manages indexer clusters
   - Handles bundle push to peers (now controlled by ansible)

5. **LicenseManager** (`licensemanager_types.go`)
   - License administration and distribution

6. **MonitoringConsole** (`monitoringconsole_types.go`)
   - Monitoring and alerting for other instances

### Common Spec Structure (`common_types.go`)

**Key Configuration Areas:**

- **Image Management** (line 92-96)
  - `Image`: Splunk docker image (defaults to env var or "splunk/splunk")
  - `ImagePullPolicy`: Always or IfNotPresent
  - `ImagePullSecrets`: Private registry credentials

- **Resource Management** (line 107-108)
  - `Resources`: CPU and memory requests/limits
  - Default: 0.1 CPU request, 512Mi memory request; 4 CPU limit, 8Gi memory limit

- **Affinity and Pod Placement** (line 101-114)
  - `Affinity`: Node affinity rules
  - `Tolerations`: Node taint tolerations
  - `TopologySpreadConstraints`: Pod distribution across zones
  - `SchedulerName`: Custom scheduler specification

- **Storage Configuration** (line 167-170)
  - `EtcVolumeStorageConfig`: /opt/splunk/etc storage (default 10Gi)
  - `VarVolumeStorageConfig`: /opt/splunk/var storage (default 100Gi)
  - Both support ephemeral (emptyDir) storage option

- **Configuration Management** (line 175-186)
  - `Defaults`: Inline YAML configuration (default.yml)
  - `DefaultsURL`: Remote configuration files (comma-separated URLs)
  - `DefaultsURLApps`: App-specific defaults (for deployer/CM/LC)
  - All URLs are passed to Splunk via SPLUNK_DEFAULTS_URL env var

- **License Management** (line 187-196)
  - `LicenseURL`: URL to license file
  - `LicenseMasterRef`/`LicenseManagerRef`: Reference to license manager CR
  - Cross-namespace references supported with ObjectReference

- **Cluster Configuration** (line 198-205)
  - `ClusterMasterRef`/`ClusterManagerRef`: Reference to cluster manager
  - `MonitoringConsoleRef`: Reference to monitoring console
  - All are ObjectReference types allowing cross-namespace deployment

- **Probe Configuration** (line 219-236)
  - `LivenessProbe`, `ReadinessProbe`, `StartupProbe`: Custom probe settings
  - `LivenessInitialDelaySeconds`, `ReadinessInitialDelaySeconds`: Probe delays
  - Defaults: Startup=40s, Readiness=10s, Liveness=30s

- **Extra Environment Variables** (line 215-217)
  - `ExtraEnv`: User-provided environment variables
  - WARNING: Documented to affect Splunk installation and operation

### SmartStore Configuration (lines 257-359)

- Remote storage volume management (S3, Azure Blob, GCS)
- Index-specific configuration
- Cache manager settings
- Supports multiple remote volumes with different providers

### App Framework Configuration (lines 414-449)

**AppFrameworkSpec:**
- Remote app repository management
- App polling interval (1 hour default, 1 min to 1 day range)
- App installation scheduler yield interval (default 90s, min 30s)
- Maximum retries for app installation (default 2)
- Concurrent app download limits

## 2. Pod and StatefulSet Creation Logic

### StatefulSet Creation Function

**File:** `/Users/viveredd/Projects/splunk-operator/pkg/splunk/enterprise/configuration.go`
**Function:** `getSplunkStatefulSet()` (line 681-788)

**StatefulSet Configuration:**

```
- Name: splunk-{instanceType}-{crName}
- ServiceName: headless service for DNS
- PodManagementPolicy: Parallel (all pods created simultaneously)
- UpdateStrategy: OnDelete (manual pod recycling required)
- Replicas: From CR spec
```

**Key Design Decisions:**

1. **OnDelete Strategy** (line 731)
   - Pods are NOT automatically replaced on StatefulSet updates
   - Operator manually handles pod recycling
   - Allows controlled cluster upgrades and configuration changes

2. **Pod Template Configuration** (lines 733-787)

   a. **Base Container** (lines 745-750)
   ```
   Image: From spec
   ImagePullPolicy: Always or IfNotPresent
   Name: "splunk"
   Ports: Splunk standard ports (web:8000, splunkd:8089, hec:8088, s2s:9997)
   ```

   b. **Security Context** (lines 896-906)
   ```
   RunAsUser: 41812 (splunk user)
   FSGroup: 41812
   RunAsNonRoot: true
   FSGroupChangePolicy: OnRootMismatch
   ```

   c. **Container Security Context** (lines 1092-1108)
   ```
   Capabilities: Drop ALL, Add NET_BIND_SERVICE
   AllowPrivilegeEscalation: false
   SeccompProfile: RuntimeDefault
   ```

### Volume Mounting Strategy

**File:** `updateSplunkPodTemplateWithConfig()` (lines 804-1110)

**Volumes Mounted:**

1. **Storage Volumes** (from `addStorageVolumes()`)
   - `pvc-etc`: /opt/splunk/etc (PersistentVolumeClaim or emptyDir)
   - `pvc-var`: /opt/splunk/var (PersistentVolumeClaim or emptyDir)

2. **Secrets Volume** (lines 837-842)
   - `mnt-splunk-secrets`: /mnt/splunk-secrets
   - Contains `default.yml` with Splunk admin password
   - Mounted as secret with mode 420 (0644)

3. **Defaults ConfigMap** (lines 847-869)
   - `mnt-splunk-defaults`: /mnt/splunk-defaults
   - Contains user-provided inline defaults
   - Resource version tracked in annotations for pod recycling on ConfigMap changes

4. **SmartStore ConfigMap** (lines 871-894)
   - `mnt-splunk-operator`: /mnt/splunk-operator/local/
   - Contains indexes.conf and server.conf for SmartStore
   - For Standalone/ClusterManager only (not propagated to peers via bundle)

5. **Custom Volumes** (lines 823-833)
   - User-defined volumes from spec.Volumes
   - Mounted at /mnt/{volumeName}

6. **Probe ConfigMap** (implicit, from `getProbeConfigMap()`)
   - Contains probe scripts for health checks

### Environment Variable Injection

**File:** `updateSplunkPodTemplateWithConfig()` (lines 925-1082)

**Critical Environment Variables:**

```
SPLUNK_HOME: "/opt/splunk"
SPLUNK_START_ARGS: "--accept-license"
SPLUNK_DEFAULTS_URL: Comma-separated list of defaults files
SPLUNK_HOME_OWNERSHIP_ENFORCEMENT: "false" (allows different pod UID)
SPLUNK_ROLE: Instance role (splunk_standalone, splunk_search_head, etc.)
SPLUNK_DECLARATIVE_ADMIN_PASSWORD: "true" (uses SPLUNK_PASSWORD env var)
SPLUNK_GENERAL_TERMS: From operator env var
SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH: "true" (CRITICAL WORKAROUND - line 943)
```

**Licensing:**
```
SPLUNK_LICENSE_URI: License file URL (if configured)
SPLUNK_LICENSE_MASTER_URL or SPLUNK_LICENSE_MANAGER_URL: License manager service
```

**Clustering:**
```
SPLUNK_CLUSTER_MASTER_URL or SPLUNK_CLUSTER_MANAGER_URL: CM service
SPLUNK_SEARCH_HEAD_URL: All search heads (for SHC)
SPLUNK_SEARCH_HEAD_CAPTAIN_URL: Captain pod-0 (for SHC)
SPLUNK_DEPLOYER_URL: Deployer service (for SHC members)
```

**Monitoring:**
```
SPLUNK_MONITORING_CONSOLE_REF: MC reference (pod reconciliation trigger)
```

**Extra Environment Variables:**
- User-provided via `spec.ExtraEnv`
- Merged with standard variables
- Duplicates handled by `removeDuplicateEnvVars()` (lines 1112-1122)
- User vars take precedence (added first, line 1070)

## 3. Secret and ConfigMap Management

### Namespace-Scoped Secret

**File:** `pkg/splunk/util/secrets.go`
**Function:** `ApplyNamespaceScopedSecretObject()`

- Single secret per namespace: `splunk-secrets`
- Contains admin password for all instances in namespace
- Versioned with `-v1`, `-v2` suffixes to force pod restarts on password changes
- Mounted at `/mnt/splunk-secrets` as `default.yml`

### Configuration Files

1. **Inline Defaults ConfigMap**
   - Name: `splunk-{crName}-{instanceType}-defaults`
   - Contains user-provided YAML configuration
   - Mounted at `/mnt/splunk-defaults/default.yml`

2. **SmartStore ConfigMap**
   - Name: `splunk-{crName}-{kind}-smartstore`
   - Contains `indexes.conf` and `server.conf` in INI format
   - Only for Standalone and ClusterManager (line 794)
   - Reason: Peer configs distributed via cluster manager bundle push

3. **App Framework ConfigMap**
   - Per CR type (Standalone, SearchHead, Indexer, etc.)
   - Tracks app deployment status and phases
   - Internal use by app deployment scheduler

### Defaults URL Handling

**Priority Order (line 918-923):**
```
1. spec.DefaultsURL (remote files)
2. /mnt/splunk-defaults/default.yml (inline YAML)
3. /mnt/splunk-secrets/default.yml (admin password)
```

All joined as comma-separated SPLUNK_DEFAULTS_URL value. Splunk processes in order, later values override earlier ones.

## 4. Key Workarounds and Limitations

### Critical Workaround: SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH=true

**Location:** `/Users/viveredd/Projects/splunk-operator/pkg/splunk/enterprise/configuration.go` line 943

**Issue:** Splunk's cluster manager bundle push feature conflicts with Kubernetes pod management
**Solution:** Disable automatic bundle push in Splunk, let ansible handle it
**Impact:** Operator must manually trigger bundle updates when cluster manager or deployer configuration changes

### Known Issue CSPL-1880: Splunk 9.0.0 Bundle Push Encryption

**Location:** `/Users/viveredd/Projects/splunk-operator/pkg/splunk/enterprise/indexercluster.go` lines 166-180

**Problem:**
- Splunk 9.0.0 uses encrypted bundle transfers
- Splunk 8.2.x instances cannot decrypt → cluster manager fails
- During version upgrade, some pods get new image, others still old
- Splunk 9.0.0 splunkd fails to start with old version peers

**Workaround:**
- Check if any pods have mismatched image version during reconciliation (line 197)
- If version mismatch detected AND upgrading to 9.x, delete entire StatefulSet
- Forces all pods to be recreated with new image version
- All pods must have compatible versions for bundle push to work

### CSPL-2242: FailureThreshold Default Value

**Location:** `/Users/viveredd/Projects/splunk-operator/pkg/splunk/enterprise/configuration.go` line 1179

**Issue:** Kubernetes has no default for probe FailureThreshold
**Solution:** Explicitly set default values to prevent unnecessary StatefulSet updates

### Multisite/Multipart Cluster Manager Limitations

**Location:** `/Users/viveredd/Projects/splunk-operator/pkg/splunk/enterprise/indexercluster.go` (comment in code)

**Limitation:** Cross-namespace cluster manager references not supported for multipart clusters
**Reason:** Service and Secret limitations in Kubernetes
**Impact:** All parts of multisite/multipart indexer clusters must be in same namespace

## 5. Reconciliation Logic and State Management

### Apply Functions

**Main reconciliation functions (pkg/splunk/enterprise/):**

1. **ApplyStandalone()** (standalone.go line 38)
   - Creates/updates StatefulSet for standalone instances
   - Manages SmartStore configuration
   - Handles app framework deployment

2. **ApplySearchHeadCluster()** (searchheadcluster.go line 42)
   - Creates deployer StatefulSet
   - Creates search head cluster StatefulSet
   - Uses SearchHeadClusterPodManager for cluster-aware pod updates
   - Tracks captain, member status, maintenance mode

3. **ApplyIndexerClusterManager()** (indexercluster.go line 49)
   - Creates indexer cluster StatefulSet
   - References cluster manager for settings
   - Uses IndexerClusterPodManager for cluster-aware pod updates
   - Performs replication factor verification

### Phase Lifecycle

**Phases defined in api/v4/common_types.go (lines 118-142):**

```
Pending      - Resource just created
Ready        - All replicas ready and configured
Updating     - Configuration changes being applied
ScalingUp    - Pods being added
ScalingDown  - Pods being removed
Terminating  - Resource deletion in progress
Error        - Error occurred, requires investigation
```

### Pod Manager Pattern

**Interfaces:** `pkg/splunk/common/types.go` (lines 43-59)

**StatefulSetPodManager interface methods:**
```
Update()         - Orchestrate all pod updates
PrepareScaleDown() - Decommission pod before removal
PrepareRecycle()   - Prepare pod for configuration update
FinishRecycle()    - Complete pod update
FinishUpgrade()    - Finalize upgrade process
```

**Implementations:**
1. **DefaultStatefulSetPodManager** - Generic implementation
2. **SearchHeadClusterPodManager** - SHC-aware (captain election, etc.)
3. **IndexerClusterPodManager** - Indexer cluster-aware (RF verification, etc.)

### Configuration Change Detection

**Methods:**
1. **Resource Version Tracking** (lines 865, 892 in configuration.go)
   - ConfigMap/Secret resource versions stored in pod annotations
   - Pod recycled when version changes

2. **SmartStore Configuration Comparison**
   - CR spec vs status comparison (line 79 in standalone.go)
   - `reflect.DeepEqual()` used for struct comparison

3. **Status Update Pattern**
   - Deferred function updates CR status on reconciliation completion (line 60 in standalone.go)
   - Ensures status reflects current cluster state

## 6. Clustering Implementation Details

### Search Head Cluster

**Pod Manager:** SearchHeadClusterPodManager (searchheadclusterpodmanager.go)

**Cluster Features:**
- Captain election and tracking
- Minimum peers joined verification
- Shc secret change detection (line 104-109 in searchheadcluster_types.go)
- Admin password change tracking (line 110-115)
- Maintenance mode detection

**Deployment Strategy:**
- Deployer as separate StatefulSet with 1 replica
- SHC members as StatefulSet with configurable replicas
- Pod-0 is preferentially the captain (via statefulset ordering)

### Indexer Cluster

**Pod Manager:** IndexerClusterPodManager (indexercluster.go)

**Cluster Features:**
- Replication factor (RF) validation
- Cluster initialization tracking
- Peer status monitoring
- Bundle ID tracking
- Maintenance mode detection
- Version upgrade handling

**Multisite Support:**
- Multiple IndexerCluster CRs can reference same ClusterManager
- Operator doesn't directly handle multisite configuration
- Multisite setup configured via defaults and ansible

## 7. App Deployment and Management

### App Framework Architecture

**Files:** `pkg/splunk/enterprise/afwscheduler.go`, related files

**Three-Phase Pipeline:**
1. **Download Phase** - Download apps from remote storage to operator pod
2. **Pod Copy Phase** - Copy apps from operator to Splunk pod
3. **Install Phase** - Install apps in Splunk (local scope only)

**Key Components:**
```
AppInstallPipeline - Three-phase pipeline manager
PipelinePhase      - Single phase execution
PipelineWorker     - Worker for app package processing
```

**Deployment Status Tracking:**
- DeployStatusPending
- DeployStatusInProgress  
- DeployStatusComplete
- DeployStatusError

**Remote Storage Support:**
- S3 (AWS)
- Azure Blob Storage
- Google Cloud Storage (GCS)
- MinIO (S3-compatible)

### Bundle Push for Clusters

**For Cluster Manager and Deployer:**
- BundlePushStage tracks: Uninitialized → Pending → InProgress → Complete
- Bundle push is ansible-driven (not Operator code)
- All member apps copied to manager
- Manager triggers Splunk to propagate via bundle push

**Limitations:**
- App Framework Phase 2 (download/copy only)
- Phase 3 adds install capabilities
- Per CSPL-1639: Stdin handling for large app payloads

## 8. Storage and Persistent Volume Management

### Volume Claim Templates

**File:** `/Users/viveredd/Projects/splunk-operator/pkg/splunk/enterprise/configuration.go` (lines 101-179)

**Two Storage Volumes:**

1. **etc volume** (default 10Gi)
   - Stores configuration at /opt/splunk/etc
   - Survives pod restarts
   - Ephemeral storage option available

2. **var volume** (default 100Gi)
   - Stores data/indexes at /opt/splunk/var
   - Critical for data persistence
   - Ephemeral storage option available

**Ephemeral Storage Option:**
- Uses emptyDir instead of PersistentVolumeClaim
- Data lost on pod restart
- Useful for testing/dev or stateless deployments
- Trade-off: No persistent state vs. simplified deployment

**Admin-Managed PV Option:**
- Allows manual PV management
- Uses Selector to match user-created PVs
- Requires manual cleanup

## 9. Resource Management and Validation

### Default Resources (lines 367-376)

**Default Requests:**
- CPU: 0.1 cores (100m)
- Memory: 512Mi

**Default Limits:**
- CPU: 4 cores
- Memory: 8Gi

### Validation

**Image Handling:**
1. RELATED_IMAGE_SPLUNK_ENTERPRISE environment variable (operator namespace)
2. spec.Image in CR
3. Fall back to "splunk/splunk" if not specified

**Image Pull Policy:**
- Defaults to "IfNotPresent"
- Can override with "Always"
- Validated in ValidateImagePullPolicy() (lines 277-293)

### Probe Configuration

**Defaults (configuration.go lines 46-86):**

1. **Liveness Probe** (line 46)
   - InitialDelay: 30s
   - Timeout: 30s
   - Period: 30s
   - FailureThreshold: 3
   - Command: /mnt/probes/livenessProbe.sh

2. **Readiness Probe** (line 60)
   - InitialDelay: 10s
   - Timeout: 5s
   - Period: 5s
   - FailureThreshold: 3
   - Command: /mnt/probes/readinessProbe.sh

3. **Startup Probe** (line 74)
   - InitialDelay: 40s
   - Timeout: 30s
   - Period: 30s
   - FailureThreshold: 12
   - Command: /mnt/probes/startupProbe.sh

**Customization:**
- User can override via spec.LivenessProbe, spec.ReadinessProbe, spec.StartupProbe
- Operator may increase InitialDelaySeconds if needed
- Probe scripts mounted from ConfigMap

## 10. Gaps in Kubernetes-Native Support

### Major Gaps Identified

1. **Manual Pod Recycling**
   - OnDelete update strategy requires operator to manually delete pods
   - Cannot leverage Kubernetes-native rolling updates
   - Must implement custom pod update logic in PodManagers

2. **Cluster Coordination Limitations**
   - SHC and IDXC clusters managed by ansible inside Splunk
   - Operator acts as orchestrator only
   - No native Kubernetes cluster management
   - Manual intervention sometimes required (e.g., maintenance mode)

3. **No Native StatefulSet Ordering**
   - Relies on pod ordinals and service discovery
   - Captain election in SHC not Kubernetes-native
   - Multi-node coordination happens inside Splunk, not Kubernetes

4. **Configuration Management Complexity**
   - Defaults URL handling requires complex coordination
   - Multiple config sources merged in complex order
   - Changes require pod recycling (no reload signal support)

5. **Storage Flexibility vs. Simplicity**
   - Must support ephemeral, PVC, and admin-managed PV modes
   - Adds complexity to volume handling
   - SmartStore adds additional storage layer

6. **Bundle Push Disabled by Default**
   - SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH=true workaround
   - Operator can't rely on Splunk's native cluster coordination
   - Must track and manually trigger bundle operations

7. **Cross-Namespace Limitations**
   - Multisite/multipart clusters can't span namespaces
   - Service and Secret scoping restrictions
   - Limits multi-tenant deployments

8. **App Framework Complexity**
   - Three-phase pipeline manually implemented
   - Operator must manage remote storage clients (S3, Azure, GCS)
   - No native Kubernetes app deployment mechanism

9. **Pod Identity and DNS**
   - Relies on Kubernetes service DNS for discovery
   - Pod ordinals critical for role assignment
   - No way to customize pod names or identities

10. **Upgrade Path Complexity**
    - CSPL-1880 workaround for Splunk 9.0.0 compatibility
    - Image version mismatches require full StatefulSet restart
    - No support for canary or blue-green deployments

### What Splunk-Docker Could Improve

1. **Support for Kubernetes Configuration**
   - Accept configuration via environment variables beyond current set
   - Support for config reload without full restart
   - Native startup probes specific to Kubernetes

2. **Cluster Coordination**
   - Native support for Kubernetes service discovery
   - Leader/captain election via Kubernetes constructs
   - Built-in health checks for cluster operations

3. **Stateless Operation Option**
   - Allow purely stateless container (ephemeral by default)
   - External state storage support (etcd, etc.)
   - Config from ConfigMaps without requiring defaults URL

4. **Bundle Management**
   - Explicit bundle push commands for Kubernetes reconciliation
   - Webhook support for operator-driven updates
   - Better version compatibility handling

5. **Metrics and Observability**
   - Prometheus metrics endpoint
   - Structured logging for Kubernetes
   - Init container status endpoints

## File Structure Summary

```
/Users/viveredd/Projects/splunk-operator/
├── api/v4/                              # CRD definitions
│   ├── common_types.go                  # Shared spec/status (216-620 lines)
│   ├── standalone_types.go              # Standalone CRD
│   ├── searchheadcluster_types.go       # SHC CRD
│   ├── indexercluster_types.go          # IDXC CRD
│   ├── clustermanager_types.go          # CM CRD
│   ├── licensemanager_types.go          # LM CRD
│   └── monitoringconsole_types.go       # MC CRD
│
├── pkg/splunk/enterprise/               # Core operator logic
│   ├── configuration.go                 # Pod/StatefulSet/Secret creation (2100+ lines)
│   ├── standalone.go                    # Standalone controller
│   ├── searchheadcluster.go             # SHC controller
│   ├── searchheadclusterpodmanager.go   # SHC-specific pod management
│   ├── indexercluster.go                # IDXC controller
│   ├── util.go                          # Utilities (environment vars, etc.)
│   ├── afwscheduler.go                  # App framework scheduler
│   └── ... (other instance types)
│
└── pkg/splunk/common/                   # Common interfaces and utilities
    ├── types.go                         # MetaObject, ControllerClient interfaces
    └── ... (other utilities)
```

## Conclusion

The Splunk Operator represents a sophisticated Kubernetes controller that abstracts Splunk Enterprise deployment complexity. However, it must work around several `splunk-docker` limitations through environment variable configuration, manual pod management, and complex state tracking. The architecture prioritizes stability and data integrity over native Kubernetes patterns, resulting in a mature but complex system that would benefit from closer integration with Kubernetes-native APIs.
