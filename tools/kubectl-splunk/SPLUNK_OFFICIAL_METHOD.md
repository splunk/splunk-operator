# Official Splunk Heap Dump Collection Method (Kubernetes Adapted)

## Reference
Based on: https://splunk.my.site.com/customer/s/article/Steps-for-collecting-heap-dump-linux

## Important Discovery from Testing

⚠️ **Critical Finding**: After testing on the `ai-platform` namespace, we discovered that:

**Splunkd is a C++ binary, NOT a Java process**

This means:
- Traditional Java heap dumps (jmap, .hprof files) are **NOT applicable** to splunkd
- Heap dumps only work for Java-based Splunk components
- The main Splunk process requires different memory diagnostic tools

## When Java Heap Dumps ARE Applicable

Heap dumps are useful for:

1. **Search Head Pools with Java Searches**
   - Complex scheduled searches running in Java
   - Custom Java-based search commands
   - Java SPL processors

2. **Custom Java Apps**
   - Splunk apps written in Java
   - Java-based modular inputs
   - Custom Java search commands

3. **Splunk Add-ons with Java Components**
   - DB Connect (uses Java JDBC)
   - Hadoop Connect (Java-based)
   - Custom Java integrations

## Standard Splunk Documentation Method

### For Java Components (When Applicable)

#### Step 1: Identify Java Processes

```bash
# In Kubernetes environment
kubectl exec -n <namespace> <pod-name> -- ps aux | grep java

# Look for processes like:
# - Search processes with Java runtime
# - DB Connect Java processes
# - Custom Java apps
```

#### Step 2: Get Process ID (PID)

```bash
# Find specific Java process
kubectl exec -n <namespace> <pod-name> -- pgrep -f "java.*splunk"

# Or list all Java processes
kubectl exec -n <namespace> <pod-name> -- ps -ef | grep java | grep -v grep
```

#### Step 3: Generate Heap Dump Using jmap

**Standard Splunk Command:**
```bash
# Via Splunk's jmap wrapper
kubectl exec -n <namespace> <pod-name> -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/tmp/heap-dump.hprof <PID>
```

**Alternative - Direct Java jmap:**
```bash
# If Splunk wrapper doesn't work
kubectl exec -n <namespace> <pod-name> -- \
    jmap -dump:live,format=b,file=/tmp/heap-dump.hprof <PID>
```

**Options Explained:**
- `-dump:live` - Only dump live objects (excludes garbage-collectable objects)
- `format=b` - Binary format (.hprof format)
- `file=/tmp/heap-dump.hprof` - Output file path

#### Step 4: Verify Heap Dump Created

```bash
# Check file size (should match heap size roughly)
kubectl exec -n <namespace> <pod-name> -- ls -lh /tmp/heap-dump.hprof

# Expected: Several hundred MB to several GB depending on heap size
```

#### Step 5: Download Heap Dump

```bash
# Copy from pod to local machine
kubectl cp <namespace>/<pod-name>:/tmp/heap-dump.hprof \
    ./heap-dump-$(date +%Y%m%d-%H%M%S).hprof
```

#### Step 6: Clean Up Remote File

```bash
# Free up space in pod
kubectl exec -n <namespace> <pod-name> -- rm /tmp/heap-dump.hprof
```

## For Splunkd (C++ Process) - The Correct Approach

Since splunkd is C++, use **Splunk Diag** instead:

### Method 1: Full Diagnostic Bundle (Recommended)

```bash
# Generate comprehensive diagnostic bundle
kubectl exec -n <namespace> <pod-name> -- \
    /opt/splunk/bin/splunk diag

# Find the generated file
DIAG_FILE=$(kubectl exec -n <namespace> <pod-name> -- \
    sh -c 'ls -t /opt/splunk/diag-*.tar.gz | head -1')

# Download it
kubectl cp <namespace>/<pod-name>:${DIAG_FILE} \
    ./splunk-diag-$(date +%Y%m%d).tar.gz

# Clean up
kubectl exec -n <namespace> <pod-name> -- rm /opt/splunk/diag-*.tar.gz
```

**What Splunk Diag Includes:**
- Memory usage statistics
- Process information (ps output)
- Resource utilization
- All log files
- Configuration files
- Index information
- System information
- Network configuration

### Method 2: Core Dump (For Crashes)

If splunkd crashes or has severe memory issues:

```bash
# Enable core dumps in the pod
kubectl exec -n <namespace> <pod-name> -- \
    ulimit -c unlimited

# Set core dump pattern
kubectl exec -n <namespace> <pod-name> -- \
    sh -c 'echo "/tmp/core.%e.%p.%t" > /proc/sys/kernel/core_pattern'

# If splunkd crashes, core dump will be in /tmp/
# Download it for analysis with gdb
kubectl cp <namespace>/<pod-name>:/tmp/core.* ./core-dump.core
```

### Method 3: Memory Profiling with pmap

```bash
# Get detailed memory map of splunkd process
SPLUNKD_PID=$(kubectl exec -n <namespace> <pod-name> -- pgrep splunkd | head -1)

kubectl exec -n <namespace> <pod-name> -- \
    pmap -x ${SPLUNKD_PID} > splunkd-memory-map.txt
```

### Method 4: Resource Usage Snapshot

```bash
# Get current memory usage
kubectl exec -n <namespace> <pod-name> -- \
    ps aux | grep splunkd

# Get detailed memory info
kubectl exec -n <namespace> <pod-name> -- \
    cat /proc/$(pgrep splunkd)/status | grep -i mem

# Get memory maps
kubectl exec -n <namespace> <pod-name> -- \
    cat /proc/$(pgrep splunkd)/smaps > splunkd-memory-details.txt
```

## Common Splunk Heap Dump Scenarios

### Scenario 1: DB Connect High Memory

**DB Connect uses Java JDBC drivers:**

```bash
# Find DB Connect Java process
kubectl exec -n <namespace> <pod-name> -- \
    ps aux | grep "dbx.*java"

# Get PID and collect heap dump
DBCONNECT_PID=$(kubectl exec -n <namespace> <pod-name> -- \
    pgrep -f "dbx.*java")

kubectl exec -n <namespace> <pod-name> -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/tmp/dbconnect-heap.hprof ${DBCONNECT_PID}
```

### Scenario 2: Custom Java Search Command

**For custom Java-based search commands:**

```bash
# Find the search process
kubectl exec -n <namespace> <pod-name> -- \
    ps aux | grep "chunked.*java"

# Collect heap dump during execution
kubectl exec -n <namespace> <pod-name> -- \
    /opt/splunk/bin/splunk cmd jmap \
    -dump:live,format=b,file=/tmp/search-heap.hprof <PID>
```

### Scenario 3: Scheduled Search Memory Issue

**When scheduled searches consume too much memory:**

1. Identify the search job:
```bash
kubectl exec -n <namespace> <pod-name> -- \
    /opt/splunk/bin/splunk list search-jobs
```

2. Check if it's Java-based:
```bash
kubectl exec -n <namespace> <pod-name> -- \
    ps aux | grep "java.*search"
```

3. If Java process found, collect heap dump

## Official Splunk Recommendations

### When to Collect

1. **High Memory Usage**
   - JVM heap usage consistently > 80%
   - OutOfMemoryError in logs
   - Slow performance related to memory

2. **Memory Leaks**
   - Gradual memory increase over time
   - Memory doesn't free after operations complete
   - Requires multiple heap dumps over time

3. **Support Cases**
   - Splunk Support requests heap dump
   - Reproducing specific issues
   - Performance analysis

### Best Practices

1. **Multiple Heap Dumps**
   - Collect 2-3 dumps over time (1 hour apart)
   - Compare to identify leaks
   - Shows growth trends

2. **Timing**
   - Collect during peak load
   - Collect when issue is occurring
   - Don't collect during startup (heap not stable)

3. **Disk Space**
   - Ensure 2x heap size available
   - Check with `df -h` first
   - Clean up after download

4. **Performance Impact**
   - Heap dump causes JVM pause (Stop-The-World)
   - Can take seconds to minutes
   - Plan for brief service interruption

## Splunk Support Submission

When submitting to Splunk Support:

### Required Information

1. **Heap Dump File(s)**
   - At least 2 dumps if investigating leak
   - Note timestamp of each

2. **Splunk Diag Bundle**
   - Full diagnostic bundle
   - Collected at same time as heap dump

3. **Description**
   - What was happening when collected
   - Symptoms observed
   - Recent changes

4. **Environment Details**
   - Splunk version
   - Java version (`java -version`)
   - OS information
   - Available memory

### Submission Command

```bash
# Upload to Splunk Support case
# (After downloading files locally)
# Use Splunk Support portal to upload files
```

## Comparison: What to Use When

| Symptom | Component | Tool to Use | File Type |
|---------|-----------|-------------|-----------|
| High splunkd memory | splunkd (C++) | Splunk diag | .tar.gz |
| DB Connect OOM | DB Connect (Java) | jmap heap dump | .hprof |
| Search hanging | Search (Java) | jmap heap dump | .hprof |
| Custom app issue | App-dependent | Check if Java first | Varies |
| General diagnostics | Any | Splunk diag | .tar.gz |
| Crash analysis | splunkd (C++) | Core dump + diag | .core + .tar.gz |

## Automated Collection (Using Our Scripts)

### For Java Components

Use the provided script:
```bash
./scripts/collect-heap-dump.sh -n <namespace> -p <pod-name> -t
```

### For Splunkd (C++)

Use Splunk diag:
```bash
# Generate
kubectl exec -n <namespace> <pod-name> -- /opt/splunk/bin/splunk diag

# Download
kubectl cp <namespace>/<pod-name>:/opt/splunk/diag-*.tar.gz ./diag.tar.gz
```

## Key Differences: Splunk on Linux vs Kubernetes

| Aspect | Traditional Linux | Kubernetes |
|--------|------------------|------------|
| Access | SSH to server | `kubectl exec` |
| File paths | Direct filesystem | Pod filesystem |
| Download | scp/rsync | `kubectl cp` |
| Permissions | Root/splunk user | Pod user context |
| Cleanup | Delete locally | Delete in pod + local |
| Networking | Direct access | Through API server |

## Tested and Verified

✅ **Tested on**: `ai-platform` namespace, `splunk-standalone` CR
✅ **Findings**: splunkd is C++, use Splunk diag
✅ **Diag size**: 282 MB (10,876 files)
✅ **Collection time**: ~60 seconds
✅ **Download time**: ~30 seconds

See [TEST_RESULTS.md](TEST_RESULTS.md) for complete test report.

## Additional Resources

- **HEAP_DUMP_QUICKSTART.md** - Quick reference for heap dumps
- **HEAP_DUMP_GUIDE.md** - Comprehensive guide with troubleshooting
- **TEST_RESULTS.md** - Test results from ai-platform environment
- **scripts/collect-heap-dump.sh** - Automated collection script

---

## Summary

**The official Splunk method is implemented in our guides**, but with important clarifications:

1. ✅ Heap dumps work for **Java components only**
2. ✅ Splunkd (main process) is **C++** - use Splunk diag instead
3. ✅ All commands adapted for Kubernetes (kubectl exec/cp)
4. ✅ Automated scripts provided for both methods
5. ✅ Tested and verified on real Splunk deployment

**For your environment (ai-platform):** Use Splunk diag, as confirmed by testing.
