# Collecting Heap Dumps from Splunk with kubectl-splunk

## Overview

Heap dumps are critical for diagnosing memory issues in Splunk, including memory leaks, high memory usage, and OutOfMemory errors. This guide shows how to collect heap dumps from Splunk instances running in Kubernetes using `kubectl-splunk`.

## Table of Contents

- [Methods to Collect Heap Dumps](#methods-to-collect-heap-dumps)
- [Method 1: Using Splunk Diag (Recommended)](#method-1-using-splunk-diag-recommended)
- [Method 2: Using jmap Directly](#method-2-using-jmap-directly)
- [Method 3: Triggering on OutOfMemory](#method-3-triggering-on-outofmemory)
- [Method 4: Using REST API](#method-4-using-rest-api)
- [Analyzing Heap Dumps](#analyzing-heap-dumps)
- [Automated Collection Script](#automated-collection-script)
- [Troubleshooting](#troubleshooting)

---

## Methods to Collect Heap Dumps

Splunk Enterprise uses Java for several components (especially search processes), so heap dumps are Java heap dumps. There are multiple ways to collect them:

1. **Splunk's built-in `diag` command** - Includes heap dumps and other diagnostics
2. **Direct jmap usage** - For immediate heap snapshots
3. **Automatic on OOM** - Pre-configured to dump on OutOfMemory
4. **REST API** - Trigger diagnostics via Splunk's REST endpoint

---

## Method 1: Using Splunk Diag (Recommended)

The Splunk `diag` command creates a comprehensive diagnostic bundle that includes heap dumps, logs, and configuration files.

### 1.1 Generate Diagnostic Bundle

```bash
# Generate diag for a specific pod
kubectl splunk -P splunk-idx-0 -n splunk exec diag

# Generate diag with specific components
kubectl splunk -P splunk-idx-0 exec diag --collect memory

# Check diag generation status
kubectl splunk -P splunk-idx-0 exec diag --status
```

### 1.2 Full Example with Download

```bash
#!/bin/bash
# Script: collect-splunk-diag.sh

NAMESPACE="splunk"
POD_NAME="splunk-idx-0"
LOCAL_DIR="/tmp/splunk-diags"

echo "==> Generating Splunk diagnostic bundle on pod: ${POD_NAME}"

# Generate the diagnostic bundle
kubectl splunk -P ${POD_NAME} -n ${NAMESPACE} exec diag

echo "==> Waiting for diag to complete (this may take 5-10 minutes)..."
sleep 60

# Find the generated diag file
DIAG_FILE=$(kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    ls -t /opt/splunk/var/run/splunk/ | grep "diag-.*\.tar\.gz" | head -1)

if [ -z "$DIAG_FILE" ]; then
    echo "ERROR: Diag file not found"
    exit 1
fi

echo "==> Found diag file: ${DIAG_FILE}"

# Create local directory
mkdir -p ${LOCAL_DIR}

# Copy the diag bundle from pod to local
echo "==> Copying diag bundle to local machine..."
kubectl splunk -P ${POD_NAME} -n ${NAMESPACE} cp \
    :/opt/splunk/var/run/splunk/${DIAG_FILE} \
    ${LOCAL_DIR}/${POD_NAME}-${DIAG_FILE}

echo "==> Diag bundle saved to: ${LOCAL_DIR}/${POD_NAME}-${DIAG_FILE}"
echo "==> Extract with: tar -xzf ${LOCAL_DIR}/${POD_NAME}-${DIAG_FILE}"
```

### 1.3 What's Included in Splunk Diag

The diag bundle includes:
- Java heap dumps (`.hprof` files)
- Thread dumps
- Configuration files
- Log files
- System information
- Resource usage stats

---

## Method 2: Using jmap Directly

For immediate heap dumps without waiting for full diag generation, use `jmap` directly.

### 2.1 Find Java Process ID

```bash
# Find the splunkd Java process
kubectl splunk -P splunk-idx-0 exec ps aux | grep java

# Or more precisely
kubectl splunk -P splunk-idx-0 exec pgrep -f splunkd
```

### 2.2 Generate Heap Dump with jmap

```bash
# Get the Java PID
JAVA_PID=$(kubectl exec -n splunk splunk-idx-0 -- pgrep -f "java.*splunkd")

echo "Java PID: ${JAVA_PID}"

# Generate heap dump
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/tmp/heap-dump-$(date +%Y%m%d-%H%M%S).hprof \
    ${JAVA_PID}
```

### 2.3 Complete Script with Download

```bash
#!/bin/bash
# Script: collect-heap-dump.sh

NAMESPACE="splunk"
POD_NAME="splunk-idx-0"
LOCAL_DIR="/tmp/heap-dumps"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
HEAP_FILE="heap-dump-${TIMESTAMP}.hprof"

echo "==> Collecting heap dump from pod: ${POD_NAME}"

# Find Java process
echo "==> Finding Java process..."
JAVA_PID=$(kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    sh -c 'pgrep -f "java.*splunkd" | head -1')

if [ -z "$JAVA_PID" ]; then
    echo "ERROR: Could not find Java process"
    exit 1
fi

echo "==> Found Java PID: ${JAVA_PID}"

# Generate heap dump in the pod
echo "==> Generating heap dump (this may take a few minutes)..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/tmp/${HEAP_FILE} ${JAVA_PID}

# Check if heap dump was created
HEAP_SIZE=$(kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    stat -c %s /tmp/${HEAP_FILE} 2>/dev/null || echo "0")

if [ "$HEAP_SIZE" -eq "0" ]; then
    echo "ERROR: Heap dump was not created"
    exit 1
fi

echo "==> Heap dump created: ${HEAP_FILE} ($(numfmt --to=iec-i --suffix=B ${HEAP_SIZE}))"

# Copy heap dump to local machine
mkdir -p ${LOCAL_DIR}
echo "==> Copying heap dump to local machine..."
kubectl cp ${NAMESPACE}/${POD_NAME}:/tmp/${HEAP_FILE} \
    ${LOCAL_DIR}/${POD_NAME}-${HEAP_FILE}

# Clean up heap dump from pod
echo "==> Cleaning up heap dump from pod..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- rm /tmp/${HEAP_FILE}

echo "==> Heap dump saved to: ${LOCAL_DIR}/${POD_NAME}-${HEAP_FILE}"
echo "==> Size: $(ls -lh ${LOCAL_DIR}/${POD_NAME}-${HEAP_FILE} | awk '{print $5}')"
```

---

## Method 3: Triggering on OutOfMemory

Configure Splunk to automatically generate heap dumps when OutOfMemory errors occur.

### 3.1 Configure OOM Heap Dumps

Add JVM options to capture heap dumps on OOM:

```bash
# Edit jvm.options in the pod
kubectl splunk -P splunk-idx-0 exec vi /opt/splunk/etc/splunk-launch.conf

# Or use exec to add the options
kubectl exec -n splunk splunk-idx-0 -- \
    sh -c 'echo "JAVA_OPTS=\$JAVA_OPTS -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/tmp" >> /opt/splunk/etc/splunk-launch.conf'

# Restart Splunk to apply changes
kubectl splunk -P splunk-idx-0 exec restart
```

### 3.2 Configure via StatefulSet

Better approach: Configure in the StatefulSet/Pod spec:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: splunk-idx
spec:
  template:
    spec:
      containers:
      - name: splunk
        env:
        - name: JAVA_TOOL_OPTIONS
          value: "-XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/splunk/var/log/heap-dumps"
        volumeMounts:
        - name: heap-dumps
          mountPath: /opt/splunk/var/log/heap-dumps
      volumes:
      - name: heap-dumps
        emptyDir:
          sizeLimit: 10Gi
```

### 3.3 Monitor for Heap Dumps

```bash
# Check for OOM heap dumps
kubectl exec -n splunk splunk-idx-0 -- \
    find /opt/splunk/var/log/heap-dumps -name "*.hprof" -mtime -1

# Monitor logs for OOM events
kubectl logs -n splunk splunk-idx-0 --tail=100 | grep -i "OutOfMemoryError"
```

---

## Method 4: Using REST API

Trigger diagnostic collection via Splunk's REST API.

### 4.1 Trigger Diag via REST

```bash
# Start diagnostic collection
kubectl splunk -P splunk-idx-0 rest POST \
    /services/server/diagnostics/diag \
    --data "components=memory" \
    --insecure

# Check status
kubectl splunk -P splunk-idx-0 rest GET \
    /services/server/diagnostics/diag \
    --insecure
```

### 4.2 Get Diag Job Status

```bash
# List all diagnostic jobs
kubectl splunk -P splunk-idx-0 rest GET \
    /services/server/diagnostics/diag \
    --insecure

# Get specific job status (replace <job-id>)
kubectl splunk -P splunk-idx-0 rest GET \
    /services/server/diagnostics/diag/<job-id> \
    --insecure
```

---

## Analyzing Heap Dumps

Once you've collected heap dumps, you need to analyze them.

### 5.1 Tools for Analysis

1. **Eclipse Memory Analyzer (MAT)** - Free, powerful
   ```bash
   # Download from: https://eclipse.dev/mat/
   # Open the .hprof file in MAT
   ```

2. **VisualVM** - Lightweight, included with JDK
   ```bash
   # Open heap dump
   jvisualvm --openfile /path/to/heap-dump.hprof
   ```

3. **JProfiler** - Commercial, comprehensive

4. **jhat** - Command-line tool (deprecated but still useful)
   ```bash
   jhat -J-Xmx4g heap-dump.hprof
   # Open browser to http://localhost:7000
   ```

### 5.2 Quick Analysis with jmap

```bash
# View heap summary
jmap -heap /path/to/heap-dump.hprof

# View histogram of object types
jmap -histo /path/to/heap-dump.hprof | head -50

# Find top memory consumers
jmap -histo /path/to/heap-dump.hprof | sort -k3 -n -r | head -20
```

### 5.3 Common Issues to Look For

- **Memory Leaks**: Objects that should be garbage collected but aren't
- **Large Collections**: Oversized HashMaps, ArrayLists, etc.
- **Duplicate Strings**: String interning issues
- **Cached Objects**: Overly aggressive caching
- **Search-Related Objects**: Search head memory issues

---

## Automated Collection Script

Complete script for collecting heap dumps from multiple pods:

```bash
#!/bin/bash
# Script: collect-all-heap-dumps.sh
# Collects heap dumps from all Splunk pods in a namespace

set -e

NAMESPACE="${1:-splunk}"
SELECTOR="${2:-app=splunk}"
OUTPUT_DIR="${3:-/tmp/splunk-heap-dumps}"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

echo "==========================================="
echo "Splunk Heap Dump Collection"
echo "==========================================="
echo "Namespace: ${NAMESPACE}"
echo "Selector: ${SELECTOR}"
echo "Output Directory: ${OUTPUT_DIR}"
echo "Timestamp: ${TIMESTAMP}"
echo "==========================================="

# Create output directory
mkdir -p "${OUTPUT_DIR}"

# Get all Splunk pods
echo "==> Finding Splunk pods..."
PODS=$(kubectl get pods -n ${NAMESPACE} -l ${SELECTOR} \
    -o jsonpath='{.items[*].metadata.name}')

if [ -z "$PODS" ]; then
    echo "ERROR: No pods found with selector ${SELECTOR} in namespace ${NAMESPACE}"
    exit 1
fi

echo "==> Found pods: $PODS"

# Function to collect heap dump from a pod
collect_heap_dump() {
    local pod_name=$1
    local pod_output_dir="${OUTPUT_DIR}/${pod_name}"

    echo ""
    echo "==> Processing pod: ${pod_name}"
    mkdir -p "${pod_output_dir}"

    # Find Java process
    echo "    Finding Java process..."
    local java_pid
    java_pid=$(kubectl exec -n ${NAMESPACE} ${pod_name} -- \
        sh -c 'pgrep -f "java.*splunkd" | head -1' 2>/dev/null || echo "")

    if [ -z "$java_pid" ]; then
        echo "    WARNING: No Java process found, skipping ${pod_name}"
        return 1
    fi

    echo "    Java PID: ${java_pid}"

    # Generate heap dump
    local heap_file="heap-${pod_name}-${TIMESTAMP}.hprof"
    echo "    Generating heap dump..."

    if kubectl exec -n ${NAMESPACE} ${pod_name} -- \
        /opt/splunk/bin/splunk cmd jmap \
        -dump:live,format=b,file=/tmp/${heap_file} ${java_pid} 2>/dev/null; then

        echo "    Heap dump generated successfully"

        # Get heap dump size
        local heap_size
        heap_size=$(kubectl exec -n ${NAMESPACE} ${pod_name} -- \
            stat -c %s /tmp/${heap_file} 2>/dev/null || echo "0")

        echo "    Heap dump size: $(numfmt --to=iec-i --suffix=B ${heap_size})"

        # Copy to local machine
        echo "    Copying heap dump to local machine..."
        kubectl cp ${NAMESPACE}/${pod_name}:/tmp/${heap_file} \
            ${pod_output_dir}/${heap_file}

        # Clean up from pod
        kubectl exec -n ${NAMESPACE} ${pod_name} -- rm /tmp/${heap_file}

        echo "    ✓ Heap dump saved to: ${pod_output_dir}/${heap_file}"

        # Also collect thread dump
        echo "    Collecting thread dump..."
        kubectl exec -n ${NAMESPACE} ${pod_name} -- \
            /opt/splunk/bin/splunk cmd jstack ${java_pid} \
            > ${pod_output_dir}/threads-${pod_name}-${TIMESTAMP}.txt 2>/dev/null || true

        return 0
    else
        echo "    ERROR: Failed to generate heap dump for ${pod_name}"
        return 1
    fi
}

# Collect heap dumps from all pods
success_count=0
failure_count=0

for pod in $PODS; do
    if collect_heap_dump "$pod"; then
        ((success_count++))
    else
        ((failure_count++))
    fi
done

echo ""
echo "==========================================="
echo "Collection Summary"
echo "==========================================="
echo "Total pods: $(echo $PODS | wc -w)"
echo "Successful: ${success_count}"
echo "Failed: ${failure_count}"
echo "Output directory: ${OUTPUT_DIR}"
echo "==========================================="

# Create a summary file
cat > ${OUTPUT_DIR}/collection-summary-${TIMESTAMP}.txt <<EOF
Splunk Heap Dump Collection Summary
Generated: $(date)
Namespace: ${NAMESPACE}
Selector: ${SELECTOR}

Pods processed: $(echo $PODS | wc -w)
Successful collections: ${success_count}
Failed collections: ${failure_count}

Output directory: ${OUTPUT_DIR}

Files collected:
$(find ${OUTPUT_DIR} -name "*.hprof" -exec ls -lh {} \;)
EOF

echo "Summary saved to: ${OUTPUT_DIR}/collection-summary-${TIMESTAMP}.txt"
```

### Usage

```bash
# Collect from default namespace
./collect-all-heap-dumps.sh splunk app=splunk /tmp/heaps

# Collect from specific namespace
./collect-all-heap-dumps.sh my-splunk-namespace app=splunk,role=indexer

# Collect from all Splunk pods
./collect-all-heap-dumps.sh splunk app=splunk
```

---

## Troubleshooting

### Issue 1: jmap Command Not Found

**Problem**: `/opt/splunk/bin/splunk cmd jmap` fails

**Solution**:
```bash
# Find Java home
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk show java-home

# Use jmap directly
kubectl exec -n splunk splunk-idx-0 -- \
    /path/to/java/bin/jmap -dump:live,format=b,file=/tmp/heap.hprof <PID>
```

### Issue 2: Insufficient Memory to Generate Heap Dump

**Problem**: Heap dump generation fails due to OOM

**Solution**:
```bash
# Use jmap with no live objects (includes garbage)
kubectl exec -n splunk splunk-idx-0 -- \
    jmap -dump:format=b,file=/tmp/heap.hprof <PID>

# Or increase memory limits before collection
kubectl patch statefulset splunk-idx -p '{"spec":{"template":{"spec":{"containers":[{"name":"splunk","resources":{"limits":{"memory":"16Gi"}}}]}}}}'
```

### Issue 3: Heap Dump Too Large to Copy

**Problem**: Heap dump is too large to copy directly

**Solution**:
```bash
# Compress before copying
kubectl exec -n splunk splunk-idx-0 -- \
    gzip /tmp/heap-dump.hprof

# Copy compressed file
kubectl cp splunk/splunk-idx-0:/tmp/heap-dump.hprof.gz \
    /tmp/heap-dump.hprof.gz

# Or use streaming
kubectl exec -n splunk splunk-idx-0 -- \
    cat /tmp/heap-dump.hprof | gzip > /tmp/heap-dump.hprof.gz
```

### Issue 4: Permission Denied

**Problem**: Cannot write heap dump to /tmp

**Solution**:
```bash
# Write to Splunk's var directory instead
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/opt/splunk/var/log/heap-dump.hprof <PID>
```

---

## Best Practices

1. **Schedule Regular Collections**: For trending analysis
2. **Collect During Peak Load**: To see real-world memory patterns
3. **Collect Thread Dumps**: Along with heap dumps for complete picture
4. **Monitor Disk Space**: Heap dumps can be very large
5. **Automate Cleanup**: Remove old heap dumps to save space
6. **Document Collection Time**: Note what was happening when collected
7. **Compare Over Time**: Look for trends in memory usage

---

## Next Steps

After collecting heap dumps:

1. Analyze with MAT or VisualVM
2. Look for memory leaks and large objects
3. Review Splunk configurations (search limits, caching)
4. Adjust resource limits if needed
5. Open support ticket with Splunk if needed

For more information, see:
- [ENHANCEMENT_PROPOSAL.md](ENHANCEMENT_PROPOSAL.md) - Future diagnostic features
- [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) - Automated collection plans
- [Splunk Documentation](https://docs.splunk.com/) - Official troubleshooting guides
