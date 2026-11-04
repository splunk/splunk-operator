# Splunk Heavy Forwarder Deployment on Kubernetes using Standalone Custom Resource

## Table of Contents
1. [Objective](#objective)
2. [Architecture Overview](#architecture-overview)
3. [Prerequisites](#prerequisites)
4. [Understanding the YAML Configuration](#understanding-the-yaml-configuration)
5. [Deployment Steps](#deployment-steps)
6. [Testing and Validation](#testing-and-validation)
7. [How the Splunk Operator Works](#how-the-splunk-operator-works)
8. [Troubleshooting](#troubleshooting)
9. [References](#references)

---

## Objective

The goal of this project is to deploy a **Splunk Heavy Forwarder (HF)** in Kubernetes that:

1. **Receives data** via HTTP Event Collector (HEC) on port 8088
2. **Parses and filters** the incoming data using props and transforms
3. **Forwards data** to a Splunk indexer cluster without indexing locally
4. **Operates in heavy forwarder mode** - processes data but doesn't store it locally

This configuration demonstrates how to use the **Splunk Operator** to deploy a Standalone Custom Resource configured as a heavy forwarder, with HEC automatically configured by the operator.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Data Flow Architecture                    │
└─────────────────────────────────────────────────────────────┘

         [HEC Clients / Applications]
                    │
                    │ HTTPS (Port 8088)
                    ▼
    ┌───────────────────────────────────────┐
    │  Heavy Forwarder (forwarder namespace)│
    │  - Receives HEC data                  │
    │  - Parses & filters events            │
    │  - No local indexing                  │
    └───────────────────────────────────────┘
                    │
                    │ TCP (Port 9997)
                    │ Compressed & Acknowledged
                    ▼
    ┌───────────────────────────────────────┐
    │   Indexer Cluster (stos namespace)    │
    │  - splunk-local-idxc-indexer-0        │
    │  - splunk-local-idxc-indexer-1        │
    │  - splunk-local-idxc-indexer-2        │
    │  - Stores & indexes data              │
    └───────────────────────────────────────┘
```

---

## Prerequisites

Before deploying, ensure you have:

1. **Kubernetes cluster** with kubectl access
2. **Splunk Operator** installed and running
3. **Two namespaces**:
   - `forwarder` - for the heavy forwarder
   - `stos` - with running indexer cluster (`splunk-local-idxc`)
4. **Indexer service** accessible at:
   - `splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997`

### Create the forwarder namespace if it doesn't exist:

```bash
kubectl create namespace forwarder
```

---

## Understanding the YAML Configuration

The `forwarder.yaml` file contains two Kubernetes resources:

1. **ConfigMap** - Contains Splunk configuration files
2. **Standalone Custom Resource** - Defines the heavy forwarder deployment

### Part 1: ConfigMap (splunk-defaults)

The ConfigMap stores Splunk configuration that will be mounted into the pod.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: splunk-defaults
data:
  default.yml: |-
    splunk:
      conf:
        # Configuration sections...
```

#### ConfigMap Structure Explained

The ConfigMap contains a `default.yml` file with three main configuration sections:

##### 1. outputs.conf - Forwarding Configuration (Lines 9-22)

This is the **most critical section** for heavy forwarder functionality:

```yaml
- key: outputs
  value:
    directory: /opt/splunk/etc/system/local
    content:
      tcpout:
        defaultGroup: idx_svc              # Default forwarding group
        useACK: true                       # Enable acknowledgment for reliability
        indexAndForward: false             # CRITICAL: False = Heavy Forwarder mode
        autoLBFrequency: 30                # Load balance every 30 seconds
        compressed: true                   # Compress data during transmission
      "tcpout:idx_svc":
        server: "splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997"
        sslVerifyServerCert: true          # Verify SSL certificates
      "tcpout-server://splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997": {}
```

**Key Settings:**
- `indexAndForward: false` - This is what makes it a heavy forwarder (parses but doesn't index)
- `useACK: true` - Ensures data isn't lost (indexer acknowledges receipt)
- `compressed: true` - Reduces network bandwidth
- `autoLBFrequency: 30` - Distributes load across indexers every 30 seconds

##### 2. props.conf - Data Parsing Rules (Lines 23-30)

Defines how to process incoming data based on sourcetype or host:

```yaml
- key: props
  value:
    directory: /opt/splunk/etc/system/local
    content:
      my_noisy_sourcetype:
        TRANSFORMS-null: drop_noise        # Apply drop_noise transform
      "host::db-*.example.com":
        TRANSFORMS-routing: to_idx_svc     # Apply routing transform
```

**Purpose:**
- Routes data to appropriate transforms based on patterns
- Can filter out unwanted events
- Can route to specific indexer groups

##### 3. transforms.conf - Data Transformation (Lines 31-42)

Defines actions to take on matched events:

```yaml
- key: transforms
  value:
    directory: /opt/splunk/etc/system/local
    content:
      drop_noise:
        REGEX: "DEBUG"                     # Match lines containing DEBUG
        DEST_KEY: queue
        FORMAT: nullQueue                  # Send to null queue (drop)
      to_idx_svc:
        REGEX: "."                         # Match all events
        DEST_KEY: _TCP_ROUTING
        FORMAT: idx_svc                    # Route to idx_svc group
```

**Transforms Explained:**
- `drop_noise` - Filters out DEBUG messages to reduce noise
- `to_idx_svc` - Routes specific host data to the indexer cluster

### Part 2: Standalone Custom Resource (Lines 44-55)

This is the Splunk Operator Custom Resource Definition:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: hf-standalone
  finalizers:
    - enterprise.splunk.com/delete-pvc    # Clean up PVCs on deletion
spec:
  volumes:
    - name: defaults
      configMap:
        name: splunk-defaults              # Mount the ConfigMap
  defaultsUrl: /mnt/defaults/default.yml   # Path to config file in pod
```

**Key Components:**

1. **apiVersion**: `enterprise.splunk.com/v4` - Uses Splunk Operator v4 API
2. **kind**: `Standalone` - Single instance (not clustered)
3. **finalizers**: Ensures PVCs are cleaned up when CR is deleted
4. **volumes**: Mounts the ConfigMap as a volume
5. **defaultsUrl**: Points to the configuration file inside the container

**Important Notes:**
- The operator automatically creates a Kubernetes Secret with:
  - Admin password
  - pass4SymmKey
  - HEC token (auto-generated UUID)
- The operator automatically configures HEC (no manual inputs.conf needed)
- The docker-splunk Ansible playbook reads the secret and configures Splunk

---

## Deployment Steps

### Step 1: Deploy the Configuration

Apply the YAML file to the forwarder namespace:

```bash
kubectl apply -f forwarder.yaml -n forwarder
```

**Expected Output:**
```
configmap/splunk-defaults created
standalone.enterprise.splunk.com/hf-standalone created
```

### Step 2: Monitor Pod Creation

Watch the pod startup process:

```bash
kubectl get pods -n forwarder -w
```

**Expected Status Progression:**
```
NAME                                READY   STATUS              RESTARTS   AGE
splunk-hf-standalone-standalone-0   0/1     ContainerCreating   0          10s
splunk-hf-standalone-standalone-0   0/1     Running             0          30s
splunk-hf-standalone-standalone-0   1/1     Running             0          90s
```

The pod takes approximately 60-90 seconds to become ready.

### Step 3: Check Standalone CR Status

```bash
kubectl get standalone -n forwarder
```

**Expected Output:**
```
NAME            PHASE   DESIRED   READY   AGE
hf-standalone   Ready   1         1       2m
```

### Step 4: Verify Services Created

```bash
kubectl get svc -n forwarder
```

**Expected Output:**
```
NAME                                       TYPE        CLUSTER-IP     PORT(S)
splunk-hf-standalone-standalone-headless   ClusterIP   None           8000,8088,8089,9997
splunk-hf-standalone-standalone-service    ClusterIP   10.0.137.210   8000,8088,8089,9997
```

### Step 5: Check Logs for Successful Startup

```bash
kubectl logs splunk-hf-standalone-standalone-0 -n forwarder --tail=20
```

Look for:
```
Ansible playbook complete, will begin streaming splunkd_stderr.log
```

---

## Testing and Validation

This section walks through the comprehensive testing performed to validate the heavy forwarder configuration.

### Test 1: Verify HEC Token Auto-Generation

**Purpose:** Confirm the Splunk Operator automatically generated a HEC token.

**Command:**
```bash
kubectl get secret splunk-hf-standalone-standalone-secret-v1 -n forwarder -o yaml
```

**What to Look For:**
```yaml
data:
  hec_token: <base64-encoded-token>
  password: <base64-encoded-password>
  pass4SymmKey: <base64-encoded-key>
```

**Decode the HEC Token:**
```bash
kubectl get secret splunk-hf-standalone-standalone-secret-v1 -n forwarder \
  -o jsonpath='{.data.hec_token}' | base64 -d
```

**Example Output:**
```
0B0A9A6B-0537-492B-48B7-09FD91091B5A
```

**✓ Result:** HEC token was automatically generated by the operator.

---

### Test 2: Verify HEC Configuration in inputs.conf

**Purpose:** Confirm the Splunk Ansible playbook (running in docker-splunk) configured HEC using the auto-generated token.

**Command:**
```bash
kubectl exec -n forwarder splunk-hf-standalone-standalone-0 -- \
  cat /opt/splunk/etc/apps/splunk_httpinput/local/inputs.conf
```

**Expected Output:**
```ini
[http://splunk_hec_token]
disabled = 0
token = 0B0A9A6B-0537-492B-48B7-09FD91091B5A

[http]
disabled = 0
```

**What This Means:**
- The operator created a Kubernetes secret with the HEC token
- The Splunk Ansible playbook read the secret from `/mnt/splunk-secrets/hec_token`
- Ansible automatically configured `inputs.conf` with the token
- HEC is enabled and listening on port 8088

**✓ Result:** HEC was automatically configured without manual inputs.conf in ConfigMap.

---

### Test 3: Verify outputs.conf Forwarding Configuration

**Purpose:** Confirm the heavy forwarder is configured to forward data to indexers.

**Command:**
```bash
kubectl exec -n forwarder splunk-hf-standalone-standalone-0 -- \
  cat /opt/splunk/etc/system/local/outputs.conf
```

**Expected Output:**
```ini
[tcpout]
defaultGroup = idx_svc
useACK = True
indexAndForward = False    # Heavy forwarder mode confirmed
autoLBFrequency = 30
compressed = True

[tcpout:idx_svc]
server = splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997
sslVerifyServerCert = True

[tcpout-server://splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997]
```

**Key Validation Points:**
- `indexAndForward = False` - Confirms heavy forwarder mode (no local indexing)
- `useACK = True` - Ensures reliable delivery with acknowledgments
- `server` points to the indexer cluster service in the stos namespace

**✓ Result:** Heavy forwarder configuration is correct.

---

### Test 4: Verify Forward Server Connection

**Purpose:** Confirm the heavy forwarder has established a connection to the indexer cluster.

**Command:**
```bash
kubectl exec -n forwarder splunk-hf-standalone-standalone-0 -- \
  bash -c 'HF_PASS=$(cat /mnt/splunk-secrets/password) && \
  /opt/splunk/bin/splunk list forward-server -auth admin:$HF_PASS'
```

**Expected Output:**
```
Active forwards:
    splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997
Configured but inactive forwards:
    None
```

**What This Means:**
- The heavy forwarder has successfully connected to the indexer cluster
- No inactive forwards (all configured targets are reachable)

**✓ Result:** Forward server connection is active.

---

### Test 5: Verify Indexer is Receiving Connections

**Purpose:** Confirm the indexer cluster is receiving connections from the heavy forwarder.

**Get Heavy Forwarder Pod IP:**
```bash
kubectl get pod splunk-hf-standalone-standalone-0 -n forwarder \
  -o jsonpath='{.status.podIP}'
```

**Example Output:**
```
10.244.6.12
```

**Get Indexer Admin Password:**
```bash
kubectl get secret splunk-local-idxc-indexer-secret-v1 -n stos \
  -o jsonpath='{.data.password}' | base64 -d
```

**Check Indexer for HF Connection:**
```bash
kubectl exec -n stos splunk-local-idxc-indexer-0 -- \
  /opt/splunk/bin/splunk list inputstatus -auth admin:<INDEXER_PASSWORD> | \
  grep "10.244.6.12"
```

**Expected Output:**
```
9997:10.244.6.12:8089
    time opened = 2025-10-21T05:02:03+0000
```

**What This Means:**
- The indexer is receiving connections from the heavy forwarder pod
- Connection is on port 9997 (Splunk-to-Splunk forwarding)
- The connection is established and active

**✓ Result:** Indexer is receiving connections from the heavy forwarder.

---

### Test 6: Send Test Event via HEC

**Purpose:** Send a test event through HEC to verify end-to-end data flow.

**Command:**
```bash
HEC_TOKEN=$(kubectl get secret splunk-hf-standalone-standalone-secret-v1 -n forwarder \
  -o jsonpath='{.data.hec_token}' | base64 -d)

kubectl run test-curl --rm -i --restart=Never --image=curlimages/curl -- \
  curl -k -X POST \
  https://splunk-hf-standalone-standalone-service.forwarder.svc.cluster.local:8088/services/collector/event \
  -H "Authorization: Splunk $HEC_TOKEN" \
  -d '{"event":"test event with auto-generated HEC token", "sourcetype":"hec:json", "source":"test", "host":"test-host"}'
```

**Expected Response:**
```json
{"text":"Success","code":0}
```

**What This Means:**
- HEC endpoint is accessible on port 8088
- The auto-generated token is valid and accepted
- The heavy forwarder accepted the event

**✓ Result:** HEC endpoint is working correctly.

---

### Test 7: Verify Data Reached Indexers

**Purpose:** Confirm the test event was forwarded to and indexed by the indexer cluster.

**Wait for Indexing (10 seconds):**
```bash
sleep 10
```

**Search for the Test Event:**
```bash
INDEXER_PASS=$(kubectl get secret splunk-local-idxc-indexer-secret-v1 -n stos \
  -o jsonpath='{.data.password}' | base64 -d)

kubectl exec -n stos splunk-local-idxc-indexer-0 -- \
  /opt/splunk/bin/splunk search \
  'index=main earliest=-5m "test event with auto-generated HEC token" | table _time host sourcetype _raw' \
  -auth admin:$INDEXER_PASS
```

**Expected Output:**
```
test event with auto-generated HEC token
```

**What This Means:**
- The heavy forwarder received the event via HEC
- The event was parsed and forwarded to the indexer cluster
- The indexer successfully indexed the event
- End-to-end data flow is working

**✓ Result:** Data successfully flows from HEC → Heavy Forwarder → Indexer.

---

### Test 8: Count Events in Last 5 Minutes

**Purpose:** Verify continuous data flow.

**Command:**
```bash
kubectl exec -n stos splunk-local-idxc-indexer-0 -- \
  /opt/splunk/bin/splunk search \
  'index=main earliest=-5m | stats count' \
  -auth admin:$INDEXER_PASS
```

**Example Output:**
```
count
-----
    1
```

---

## Testing Summary

| Test # | Test Description | Status | Notes |
|--------|-----------------|--------|-------|
| 1 | HEC Token Auto-Generation | ✓ Pass | Operator created token: `0B0A9A6B-0537-492B-48B7-09FD91091B5A` |
| 2 | HEC Configuration in inputs.conf | ✓ Pass | Ansible automatically configured HEC from secret |
| 3 | outputs.conf Forwarding Config | ✓ Pass | Heavy forwarder mode confirmed (`indexAndForward=False`) |
| 4 | Forward Server Connection | ✓ Pass | Active connection to indexer service |
| 5 | Indexer Receiving Connections | ✓ Pass | Connection from HF pod IP `10.244.6.12` confirmed |
| 6 | HEC Endpoint Test | ✓ Pass | HEC responded with `{"text":"Success","code":0}` |
| 7 | Data in Indexers | ✓ Pass | Test event successfully indexed |
| 8 | Event Count Verification | ✓ Pass | Events visible in main index |

---

## How the Splunk Operator Works

Understanding the operator's workflow helps troubleshoot issues:

### 1. Resource Creation Flow

```
User applies forwarder.yaml
        ↓
Operator detects Standalone CR
        ↓
Operator creates:
  - StatefulSet (for pod)
  - Service (for endpoints)
  - Secret (with auto-generated credentials)
        ↓
StatefulSet creates Pod
        ↓
Pod starts docker-splunk container
        ↓
Ansible playbook runs inside container
        ↓
Ansible reads:
  - /mnt/splunk-secrets/* (from Kubernetes secret)
  - /mnt/defaults/default.yml (from ConfigMap)
        ↓
Ansible configures Splunk:
  - Creates admin user with password from secret
  - Configures HEC with token from secret
  - Applies outputs.conf from ConfigMap
  - Applies props.conf from ConfigMap
  - Applies transforms.conf from ConfigMap
        ↓
Splunk starts and becomes ready
```

### 2. Auto-Generated Credentials

The operator automatically generates and stores in Kubernetes secrets:

| Secret Key | Purpose | Example Value |
|------------|---------|---------------|
| `password` | Splunk admin password | `mc53LauDYtXxnpbhrfnwwfcy` |
| `hec_token` | HEC authentication token | `0B0A9A6B-0537-492B-48B7-09FD91091B5A` |
| `pass4SymmKey` | Encryption key | `lFrkeBwjtdi3rHK5iA9cpJeR` |
| `idxc_secret` | Indexer cluster secret | `dYE40VP5pWhRaW31M8tz5SDR` |
| `shc_secret` | Search head cluster secret | `vBbuEE5JKeVf43PC7Xn39xQi` |

### 3. Why HEC Configuration is Not in ConfigMap

The operator follows this design pattern:

1. **Security**: HEC tokens should be treated as secrets, not stored in ConfigMaps
2. **Automation**: The operator generates unique tokens for each deployment
3. **Secret Management**: Kubernetes secrets are more secure than ConfigMaps
4. **Ansible Integration**: The docker-splunk Ansible playbook automatically configures HEC

**What you configure in ConfigMap:**
- Forwarding settings (outputs.conf)
- Data parsing rules (props.conf)
- Data transformations (transforms.conf)
- Custom apps and configurations

**What the operator handles automatically:**
- Admin credentials
- HEC tokens
- Encryption keys
- Cluster secrets
- Basic inputs.conf for HEC

---

## Troubleshooting

### Issue 1: Pod Stuck in CrashLoopBackOff

**Symptom:**
```bash
kubectl get pods -n forwarder
NAME                                READY   STATUS             RESTARTS   AGE
splunk-hf-standalone-standalone-0   0/1     CrashLoopBackOff   3          2m
```

**Diagnosis:**
```bash
kubectl logs splunk-hf-standalone-standalone-0 -n forwarder --tail=100
```

**Common Causes:**
1. **YAML Syntax Error in ConfigMap**
   - Look for: `yaml.scanner.ScannerError`
   - Fix: Validate YAML syntax (colons on same line as keys)

2. **Invalid Configuration in default.yml**
   - Look for: `ERROR` or `FAILED` in Ansible playbook output
   - Fix: Verify configuration syntax matches Splunk documentation

### Issue 2: HEC Returns 401 Unauthorized

**Symptom:**
```bash
curl test returns: {"text":"Token disabled","code":3}
```

**Diagnosis:**
```bash
kubectl exec -n forwarder splunk-hf-standalone-standalone-0 -- \
  cat /opt/splunk/etc/apps/splunk_httpinput/local/inputs.conf
```

**Solutions:**
1. Verify token in secret matches token used in curl
2. Check if HEC is disabled: `disabled = 1` (should be 0)
3. Restart Splunk to reload configuration

### Issue 3: Data Not Reaching Indexers

**Symptom:**
Events sent to HEC return success, but not found in indexers.

**Diagnosis Steps:**

1. **Check forward server status:**
```bash
kubectl exec -n forwarder splunk-hf-standalone-standalone-0 -- \
  bash -c 'HF_PASS=$(cat /mnt/splunk-secrets/password) && \
  /opt/splunk/bin/splunk list forward-server -auth admin:$HF_PASS'
```

2. **Check for inactive forwards:**
If forwards are inactive, check network connectivity:
```bash
kubectl exec -n forwarder splunk-hf-standalone-standalone-0 -- \
  nc -zv splunk-local-idxc-indexer-service.stos.svc.cluster.local 9997
```

3. **Check indexer is listening:**
```bash
kubectl exec -n stos splunk-local-idxc-indexer-0 -- \
  netstat -tuln | grep 9997
```

4. **Check HF internal logs:**
```bash
kubectl exec -n forwarder splunk-hf-standalone-standalone-0 -- \
  tail -f /opt/splunk/var/log/splunk/splunkd.log
```

Look for:
- `TcpOutputProc` - Forwarding processor logs
- `Connection to host:port failed` - Network issues
- `SSL certificate validation failed` - Certificate issues

### Issue 4: SSL Certificate Verification Failures

**Symptom:**
```
SSL certificate validation failed
```

**Solution:**
Set `sslVerifyServerCert: false` in outputs.conf (for testing only):

```yaml
"tcpout:idx_svc":
  server: "splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997"
  sslVerifyServerCert: false  # Set to false for testing
```

**For Production:**
Configure proper SSL certificates or use mutual TLS.

### Issue 5: Pod Stuck in ContainerCreating

**Symptom:**
```bash
kubectl get pods -n forwarder
NAME                                READY   STATUS              RESTARTS   AGE
splunk-hf-standalone-standalone-0   0/1     ContainerCreating   0          5m
```

**Diagnosis:**
```bash
kubectl describe pod splunk-hf-standalone-standalone-0 -n forwarder
```

**Common Causes:**
1. **PVC not bound** - Check PersistentVolumeClaim status
2. **ConfigMap not found** - Ensure ConfigMap exists in same namespace
3. **Image pull errors** - Check image pull secrets

---

## Configuration Reference

### Complete forwarder.yaml

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: splunk-defaults
data:
  default.yml: |-
    splunk:
      conf:
        - key: outputs
          value:
            directory: /opt/splunk/etc/system/local
            content:
              tcpout:
                defaultGroup: idx_svc
                useACK: true
                indexAndForward: false
                autoLBFrequency: 30
                compressed: true
              "tcpout:idx_svc":
                server: "splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997"
                sslVerifyServerCert: true
              "tcpout-server://splunk-local-idxc-indexer-service.stos.svc.cluster.local:9997": {}
        - key: props
          value:
            directory: /opt/splunk/etc/system/local
            content:
              my_noisy_sourcetype:
                TRANSFORMS-null: drop_noise
              "host::db-*.example.com":
                TRANSFORMS-routing: to_idx_svc
        - key: transforms
          value:
            directory: /opt/splunk/etc/system/local
            content:
              drop_noise:
                REGEX: "DEBUG"
                DEST_KEY: queue
                FORMAT: nullQueue
              to_idx_svc:
                REGEX: "."
                DEST_KEY: _TCP_ROUTING
                FORMAT: idx_svc
---
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: hf-standalone
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: defaults
      configMap:
        name: splunk-defaults
  defaultsUrl: /mnt/defaults/default.yml
```

---

## References

### Official Documentation
- [Splunk Operator GitHub](https://github.com/splunk/splunk-operator)
- [Splunk Operator Documentation](https://splunk.github.io/splunk-operator/)
- [Splunk Heavy Forwarder Documentation](https://docs.splunk.com/Documentation/Forwarder/latest/Forwarder/Aboutforwardingandreceivingdata)
- [HTTP Event Collector (HEC) Documentation](https://docs.splunk.com/Documentation/Splunk/latest/Data/UsetheHTTPEventCollector)

### Configuration Files Reference
- [outputs.conf](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Outputsconf)
- [inputs.conf](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Inputsconf)
- [props.conf](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Propsconf)
- [transforms.conf](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Transformsconf)

### Splunk Operator Custom Resources
- [Standalone CR Specification](https://splunk.github.io/splunk-operator/StandaloneSpec.html)
- [Common Spec Parameters](https://splunk.github.io/splunk-operator/CommonSpec.html)

---

## Cleanup

To remove the heavy forwarder deployment:

```bash
# Delete the Standalone CR and ConfigMap
kubectl delete -f forwarder.yaml -n forwarder

# Verify resources are deleted
kubectl get pods -n forwarder
kubectl get standalone -n forwarder

# Optional: Delete PVCs (data will be lost)
kubectl delete pvc -n forwarder -l app.kubernetes.io/instance=splunk-hf-standalone-standalone
```

---

## Conclusion

This guide demonstrates how to deploy a Splunk Heavy Forwarder in Kubernetes using the Splunk Operator with the following key features:

- **Automatic HEC Configuration**: The operator handles HEC token generation and configuration
- **Heavy Forwarder Mode**: Data is parsed but not indexed locally
- **Secure Credential Management**: All sensitive data stored in Kubernetes secrets
- **Reliable Data Forwarding**: Uses acknowledgments and compression
- **Data Processing**: Supports props and transforms for filtering and routing
- **Production Ready**: Follows Kubernetes and Splunk best practices

The testing demonstrated successful end-to-end data flow from HEC through the heavy forwarder to the indexer cluster.
