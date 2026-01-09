# Quick Start: Collecting Heap Dumps from Splunk on Kubernetes

Based on [Splunk's official documentation](https://splunk.my.site.com/customer/s/article/Steps-for-collecting-heap-dump-linux) adapted for Kubernetes environments.

## TL;DR - Quick Commands

### Method 1: Using the Provided Script (Easiest)

```bash
# Single pod
./scripts/collect-heap-dump.sh -p splunk-idx-0 -n splunk

# All pods with threads
./scripts/collect-heap-dump.sh -n splunk -a -t

# Interactive selection
./scripts/collect-heap-dump.sh -n splunk
```

### Method 2: Using kubectl-splunk (Direct Commands)

```bash
# Step 1: Find Java PID
kubectl splunk -P splunk-idx-0 -n splunk exec ps aux | grep java

# Step 2: Generate heap dump (replace <PID>)
kubectl splunk -P splunk-idx-0 -n splunk exec \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/tmp/heap.hprof <PID>

# Step 3: Copy heap dump locally
kubectl splunk -P splunk-idx-0 -n splunk cp \
    :/tmp/heap.hprof ./heap-dump-$(date +%Y%m%d).hprof

# Step 4: Clean up remote file
kubectl exec -n splunk splunk-idx-0 -- rm /tmp/heap.hprof
```

### Method 3: One-Liner (for emergencies)

```bash
kubectl exec -n splunk splunk-idx-0 -- sh -c '
  PID=$(pgrep -f "java.*splunkd" | head -1);
  /opt/splunk/bin/splunk cmd jmap -dump:live,format=b,file=/tmp/heap.hprof $PID
' && kubectl cp splunk/splunk-idx-0:/tmp/heap.hprof ./heap.hprof
```

---

## Official Splunk Method (Kubernetes Adapted)

According to Splunk's official documentation, here's the recommended approach:

### Step 1: Identify the Java Process

```bash
# Get Java process ID for splunkd
kubectl exec -n splunk splunk-idx-0 -- ps aux | grep java

# Or more directly
kubectl exec -n splunk splunk-idx-0 -- pgrep -f "java.*splunkd"
```

Look for processes like:
- `splunkd -p <port> start` (main Splunk daemon)
- Java processes running search heads
- KV Store processes

### Step 2: Generate Heap Dump Using jmap

Splunk recommends using `jmap` which comes bundled with the JVM:

```bash
# Replace <PID> with the actual process ID from Step 1
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/tmp/splunk-heap-$(date +%Y%m%d-%H%M%S).hprof \
    <PID>
```

**Options explained:**
- `-dump:live` - Only dump live objects (excludes garbage)
- `format=b` - Binary format (standard .hprof format)
- `file=...` - Output file path

### Step 3: Verify Heap Dump Created

```bash
# Check file exists and size
kubectl exec -n splunk splunk-idx-0 -- ls -lh /tmp/*.hprof

# Expected: File should be several hundred MB to several GB
```

### Step 4: Download Heap Dump

```bash
# Copy from pod to local machine
kubectl cp splunk/splunk-idx-0:/tmp/splunk-heap-*.hprof \
    ./heap-dumps/

# Verify local copy
ls -lh ./heap-dumps/
```

### Step 5: Clean Up Remote Files

```bash
# Remove heap dump from pod to free space
kubectl exec -n splunk splunk-idx-0 -- rm /tmp/splunk-heap-*.hprof
```

---

## Additional Diagnostics (Per Splunk Docs)

### Thread Dump (Recommended alongside heap dump)

```bash
# Collect thread dump using jstack
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd jstack <PID> \
    > thread-dump-$(date +%Y%m%d-%H%M%S).txt
```

### Memory Statistics

```bash
# Get JVM memory statistics
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd jstat -gc <PID>

# Get detailed heap histogram
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd jmap -histo:live <PID> \
    > heap-histogram-$(date +%Y%m%d-%H%M%S).txt
```

### Java Version Info

```bash
# Check Java version being used
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd java -version
```

---

## When to Collect Heap Dumps

As per Splunk best practices:

1. **High Memory Usage**: When container memory usage is consistently >80%
2. **OutOfMemoryError**: After OOM events (if not automatically captured)
3. **Memory Leaks**: Suspected memory leaks causing gradual memory increase
4. **Performance Issues**: Slow searches or indexing related to memory
5. **Before/After**: Compare heap dumps before and after configuration changes

---

## Important Considerations

### Disk Space Requirements

Heap dumps can be **very large** (same size as JVM heap):
- 4GB heap = ~4GB heap dump
- 16GB heap = ~16GB heap dump

**Check available space first:**
```bash
# Check disk space in pod
kubectl exec -n splunk splunk-idx-0 -- df -h /tmp

# Check disk space on var volume
kubectl exec -n splunk splunk-idx-0 -- df -h /opt/splunk/var
```

### Performance Impact

**Warning**: Generating heap dumps:
- Pauses the JVM (Stop-The-World event)
- Duration: seconds to minutes depending on heap size
- May cause temporary service disruption

**Recommendations:**
- Collect during maintenance windows if possible
- Use `-dump:live` to reduce dump size
- Monitor pod during collection: `kubectl top pod -n splunk splunk-idx-0`

### Multiple Heap Dumps

For memory leak analysis, collect **2-3 heap dumps** over time:

```bash
# Collect heap dump #1
./scripts/collect-heap-dump.sh -p splunk-idx-0 -n splunk

# Wait 1 hour
sleep 3600

# Collect heap dump #2
./scripts/collect-heap-dump.sh -p splunk-idx-0 -n splunk

# Compare to see what's growing
```

---

## Using Splunk Diag (Comprehensive Alternative)

Splunk's `diag` command collects heap dumps automatically:

```bash
# Generate full diagnostic bundle (includes heap dumps)
kubectl splunk -P splunk-idx-0 -n splunk exec diag

# Wait for diag to complete (~5-10 minutes)
# Check for diag file
kubectl exec -n splunk splunk-idx-0 -- \
    ls -lh /opt/splunk/var/run/splunk/ | grep diag

# Copy diag bundle
DIAG_FILE=$(kubectl exec -n splunk splunk-idx-0 -- \
    ls -t /opt/splunk/var/run/splunk/diag-*.tar.gz | head -1)

kubectl cp splunk/splunk-idx-0:${DIAG_FILE} ./splunk-diag.tar.gz

# Extract and find heap dumps
tar -tzf splunk-diag.tar.gz | grep -i heap
```

**Diag includes:**
- Heap dumps
- Thread dumps
- Configuration files
- Log files
- System metrics
- Cluster status

---

## Analyzing Heap Dumps

### Eclipse Memory Analyzer (MAT) - Recommended

```bash
# Download MAT from: https://eclipse.dev/mat/

# Open heap dump in MAT
# File > Open Heap Dump > Select .hprof file

# Run automatic leak suspects report
# MAT will analyze and show:
# - Largest objects
# - Potential memory leaks
# - Duplicate strings
# - Object retention paths
```

### VisualVM (Lightweight)

```bash
# Included with JDK or download from: https://visualvm.github.io/

# Open heap dump
jvisualvm --openfile heap-dump.hprof

# Navigate to:
# - Classes view: See object counts
# - Instances view: Inspect individual objects
# - OQL Console: Query heap dump
```

### Command-Line Quick Analysis

```bash
# View heap summary
jmap -heap heap-dump.hprof

# View object histogram (top memory consumers)
jmap -histo heap-dump.hprof | head -30

# Find specific class instances
jmap -histo heap-dump.hprof | grep -i "YourClassName"
```

---

## Troubleshooting

### Error: "Unable to open socket file"

```bash
# Run jmap as the same user as the Java process
kubectl exec -n splunk splunk-idx-0 -- \
    su -s /bin/bash splunk -c \
    "/opt/splunk/bin/splunk cmd jmap -dump:file=/tmp/heap.hprof <PID>"
```

### Error: "Insufficient memory to create heap dump"

```bash
# Use non-live dump (includes garbage, but smaller)
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:format=b,file=/tmp/heap.hprof <PID>

# Or dump to a larger volume
kubectl exec -n splunk splunk-idx-0 -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/opt/splunk/var/log/heap.hprof <PID>
```

### Error: "Connection refused" when copying

```bash
# Verify pod is running
kubectl get pod -n splunk splunk-idx-0

# Check file exists
kubectl exec -n splunk splunk-idx-0 -- ls -lh /tmp/heap.hprof

# Try compression before copying
kubectl exec -n splunk splunk-idx-0 -- gzip /tmp/heap.hprof
kubectl cp splunk/splunk-idx-0:/tmp/heap.hprof.gz ./heap.hprof.gz
```

---

## Complete Example: Production Troubleshooting

Real-world scenario collecting heap dumps from a search head cluster:

```bash
#!/bin/bash
# Production heap dump collection with safety checks

NAMESPACE="splunk-prod"
SELECTOR="app=splunk,role=search-head"
OUTPUT_DIR="/mnt/diagnostics/heap-dumps-$(date +%Y%m%d)"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

echo "=== Splunk Production Heap Dump Collection ==="
echo "Namespace: ${NAMESPACE}"
echo "Timestamp: ${TIMESTAMP}"
echo ""

# 1. Create output directory
mkdir -p "${OUTPUT_DIR}"

# 2. Get all search head pods
echo "Finding search head pods..."
PODS=$(kubectl get pods -n ${NAMESPACE} -l ${SELECTOR} \
    -o jsonpath='{.items[*].metadata.name}')

echo "Found pods: ${PODS}"
echo ""

# 3. Collect from each pod
for pod in ${PODS}; do
    echo "=== Processing ${pod} ==="

    # Check disk space
    DISK_AVAIL=$(kubectl exec -n ${NAMESPACE} ${pod} -- \
        df /tmp | tail -1 | awk '{print $4}')

    echo "Available disk space: ${DISK_AVAIL}KB"

    if [ "${DISK_AVAIL}" -lt 5242880 ]; then  # Less than 5GB
        echo "WARNING: Insufficient disk space, skipping ${pod}"
        continue
    fi

    # Get Java PID
    JAVA_PID=$(kubectl exec -n ${NAMESPACE} ${pod} -- \
        pgrep -f "java.*splunkd" | head -1)

    if [ -z "${JAVA_PID}" ]; then
        echo "WARNING: No Java process found in ${pod}"
        continue
    fi

    echo "Java PID: ${JAVA_PID}"

    # Generate heap dump
    echo "Generating heap dump..."
    kubectl exec -n ${NAMESPACE} ${pod} -- \
        /opt/splunk/bin/splunk cmd jmap \
        -dump:live,format=b,file=/tmp/heap-${pod}-${TIMESTAMP}.hprof \
        ${JAVA_PID}

    # Generate thread dump
    echo "Generating thread dump..."
    kubectl exec -n ${NAMESPACE} ${pod} -- \
        /opt/splunk/bin/splunk cmd jstack ${JAVA_PID} \
        > ${OUTPUT_DIR}/threads-${pod}-${TIMESTAMP}.txt

    # Copy heap dump
    echo "Copying heap dump..."
    kubectl cp ${NAMESPACE}/${pod}:/tmp/heap-${pod}-${TIMESTAMP}.hprof \
        ${OUTPUT_DIR}/heap-${pod}-${TIMESTAMP}.hprof

    # Clean up
    echo "Cleaning up remote files..."
    kubectl exec -n ${NAMESPACE} ${pod} -- \
        rm /tmp/heap-${pod}-${TIMESTAMP}.hprof

    # Collect pod metrics
    kubectl top pod -n ${NAMESPACE} ${pod} \
        > ${OUTPUT_DIR}/metrics-${pod}-${TIMESTAMP}.txt

    echo "✓ Completed ${pod}"
    echo ""
done

echo "=== Collection Complete ==="
echo "Output directory: ${OUTPUT_DIR}"
ls -lh ${OUTPUT_DIR}/
```

---

## References

- [Official Splunk Heap Dump Guide](https://splunk.my.site.com/customer/s/article/Steps-for-collecting-heap-dump-linux)
- [HEAP_DUMP_GUIDE.md](HEAP_DUMP_GUIDE.md) - Detailed guide
- [Eclipse MAT](https://eclipse.dev/mat/) - Heap dump analyzer
- [VisualVM](https://visualvm.github.io/) - JVM monitoring tool

---

## Need Help?

If you encounter issues:

1. Check the detailed guide: [HEAP_DUMP_GUIDE.md](HEAP_DUMP_GUIDE.md)
2. Review Splunk logs: `kubectl logs -n splunk <pod-name>`
3. Check pod status: `kubectl describe pod -n splunk <pod-name>`
4. Contact Splunk Support with heap dumps for analysis
