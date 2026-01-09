#!/bin/bash
#
# collect-memory-profile.sh - Collect jemalloc memory profile from Splunk (C++ process)
# Based on official Splunk documentation for memory growth analysis
#
# Usage:
#   ./collect-memory-profile.sh <namespace> <pod-name> [lg_prof_interval] [collection_time]
#
# Examples:
#   # Default: 1 hour collection, 1GB interval
#   ./collect-memory-profile.sh ai-platform splunk-splunk-standalone-standalone-0
#
#   # 2 hour collection, 2GB interval
#   ./collect-memory-profile.sh ai-platform splunk-splunk-standalone-standalone-0 31 7200
#

set -e

# Configuration
NAMESPACE="${1:-ai-platform}"
POD_NAME="${2:-splunk-splunk-standalone-standalone-0}"
HEAP_DIR="/tmp/heap"
LG_PROF_INTERVAL="${3:-30}"  # 1GB default (2^30 bytes)
COLLECTION_TIME="${4:-3600}"  # 1 hour default
OUTPUT_DIR="/tmp/splunk-memory-analysis"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Calculate interval in GB
INTERVAL_GB=$(( 2**${LG_PROF_INTERVAL} / 1024 / 1024 / 1024 ))

print_info "============================================"
print_info "Splunk Memory Profiling Collection (jemalloc)"
print_info "============================================"
print_info "Namespace: ${NAMESPACE}"
print_info "Pod: ${POD_NAME}"
print_info "Profile Interval: 2^${LG_PROF_INTERVAL} bytes (${INTERVAL_GB} GB)"
print_info "Collection Time: ${COLLECTION_TIME} seconds ($(( COLLECTION_TIME / 60 )) minutes)"
print_info "Output Directory: ${OUTPUT_DIR}"
print_info "Timestamp: ${TIMESTAMP}"
print_info "============================================"

# Unset AWS credentials (from user's instructions)
unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_PROFILE

# Create output directory
mkdir -p "${OUTPUT_DIR}"

# Verify pod exists and is running
print_info "Verifying pod status..."
POD_STATUS=$(kubectl get pod -n ${NAMESPACE} ${POD_NAME} -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
if [ "$POD_STATUS" != "Running" ]; then
  print_error "Pod ${POD_NAME} is not running (status: ${POD_STATUS})"
  exit 1
fi
print_success "Pod is running"

# Step 1: Backup configuration
print_info "[1/9] Backing up splunk-launch.conf..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  cp /opt/splunk/etc/splunk-launch.conf \
     /opt/splunk/etc/splunk-launch.conf.noheap
print_success "Configuration backed up"

# Step 2: Create heap directory
print_info "[2/9] Creating heap output directory..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  sh -c "mkdir -p ${HEAP_DIR} && chmod 777 ${HEAP_DIR}"
print_success "Heap directory created: ${HEAP_DIR}"

# Step 3: Verify jemalloc exists
print_info "[3/9] Verifying jemalloc library..."
if kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  test -f /opt/splunk/opt/jemalloc-4k-stats/lib/libjemalloc.so; then
  JEMALLOC_PATH="/opt/splunk/opt/jemalloc-4k-stats/lib/libjemalloc.so"
  print_success "Found jemalloc at: ${JEMALLOC_PATH}"
elif kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  test -f /opt/splunk/opt/jemalloc-64k-stats/lib/libjemalloc.so; then
  JEMALLOC_PATH="/opt/splunk/opt/jemalloc-64k-stats/lib/libjemalloc.so"
  print_warning "Found jemalloc at: ${JEMALLOC_PATH} (aarch64)"
elif kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  test -f /opt/splunk/lib/with_stats/libjemalloc.so; then
  JEMALLOC_PATH="/opt/splunk/lib/with_stats/libjemalloc.so"
  print_warning "Found jemalloc at: ${JEMALLOC_PATH} (older version)"
else
  print_error "jemalloc library not found in expected locations"
  print_error "This might not be a standard Splunk installation"
  exit 1
fi

# Step 4: Configure jemalloc
print_info "[4/9] Configuring jemalloc profiling..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- sh -c "
  echo '' >> /opt/splunk/etc/splunk-launch.conf
  echo 'LD_PRELOAD=\"${JEMALLOC_PATH}\"' >> /opt/splunk/etc/splunk-launch.conf
  echo 'MALLOC_CONF=\"prof:true,prof_accum:true,prof_leak:true,lg_prof_interval:${LG_PROF_INTERVAL},prof_prefix:${HEAP_DIR}/heap_data\"' >> /opt/splunk/etc/splunk-launch.conf
"
print_success "Configuration updated"

# Show configuration
print_info "Configuration added to splunk-launch.conf:"
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- tail -3 /opt/splunk/etc/splunk-launch.conf

# Step 5: Restart Splunk
print_warning "[5/9] Restarting Splunk to enable profiling..."
print_warning "This will cause brief service interruption!"
read -p "Continue? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  print_warning "Aborted. Restoring configuration..."
  kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    cp /opt/splunk/etc/splunk-launch.conf.noheap \
       /opt/splunk/etc/splunk-launch.conf
  exit 1
fi

kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  /opt/splunk/bin/splunk restart

print_info "Waiting for Splunk to restart (60 seconds)..."
sleep 60

# Step 6: Verify Splunk is running
print_info "[6/9] Verifying Splunk is running..."
if kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  /opt/splunk/bin/splunk status > /dev/null 2>&1; then
  print_success "Splunk is running with profiling enabled"
else
  print_error "Splunk failed to start after configuration"
  print_error "Checking logs..."
  kubectl logs -n ${NAMESPACE} ${POD_NAME} --tail=50
  print_warning "Restoring configuration..."
  kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    cp /opt/splunk/etc/splunk-launch.conf.noheap \
       /opt/splunk/etc/splunk-launch.conf
  kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    /opt/splunk/bin/splunk restart
  exit 1
fi

# Step 7: Monitor collection
print_info "[7/9] Collecting heap profiles for ${COLLECTION_TIME} seconds..."
print_info "Monitoring memory usage and heap file generation..."
echo ""

START_TIME=$(date +%s)
LAST_FILE_COUNT=0

while [ $(( $(date +%s) - START_TIME )) -lt ${COLLECTION_TIME} ]; do
  ELAPSED=$(( $(date +%s) - START_TIME ))
  REMAINING=$(( COLLECTION_TIME - ELAPSED ))

  # Check heap files
  FILE_COUNT=$(kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    sh -c "ls ${HEAP_DIR}/*.heap 2>/dev/null | wc -l" || echo "0")

  # Check memory
  MEM_KB=$(kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
    sh -c "ps aux | grep 'splunkd -p' | grep -v grep | awk '{print \$6}'" || echo "0")
  MEM_MB=$(( MEM_KB / 1024 ))

  # Check pod resources
  POD_MEM=$(kubectl top pod -n ${NAMESPACE} ${POD_NAME} --no-headers 2>/dev/null | awk '{print $3}' || echo "N/A")

  # Print status
  printf "\r[%d/%ds] Heap files: %d (+%d) | splunkd RSS: %d MB | Pod Memory: %s | Remaining: %ds   " \
    ${ELAPSED} ${COLLECTION_TIME} ${FILE_COUNT} $((FILE_COUNT - LAST_FILE_COUNT)) ${MEM_MB} "${POD_MEM}" ${REMAINING}

  # Alert if new files created
  if [ ${FILE_COUNT} -gt ${LAST_FILE_COUNT} ]; then
    echo ""
    print_success "New heap file(s) created! Total: ${FILE_COUNT}"
  fi

  LAST_FILE_COUNT=${FILE_COUNT}
  sleep 30
done

echo ""
print_success "Collection period complete!"
print_info "Total heap files collected: ${FILE_COUNT}"

# Step 8: Collect data
print_info "[8/9] Collecting heap profile data and diag..."

# Tar up heap files
print_info "Archiving heap files..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  tar czf /tmp/heap_data.tar.gz -C /tmp heap/

HEAP_SIZE=$(kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  stat -c %s /tmp/heap_data.tar.gz 2>/dev/null || echo "0")
HEAP_SIZE_MB=$(( HEAP_SIZE / 1024 / 1024 ))

print_info "Heap data archive size: ${HEAP_SIZE_MB} MB"

# Download heap data
print_info "Downloading heap data..."
HEAP_OUTPUT="${OUTPUT_DIR}/heap_data-${POD_NAME}-${TIMESTAMP}.tar.gz"
kubectl cp ${NAMESPACE}/${POD_NAME}:/tmp/heap_data.tar.gz "${HEAP_OUTPUT}"
print_success "Heap data saved to: ${HEAP_OUTPUT}"

# Collect diag
print_info "Collecting Splunk diag (this may take a few minutes)..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  /opt/splunk/bin/splunk diag > /dev/null 2>&1

DIAG_FILE=$(kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  sh -c 'ls -t /opt/splunk/diag-*.tar.gz | head -1')

print_info "Downloading diag..."
DIAG_OUTPUT="${OUTPUT_DIR}/diag-${POD_NAME}-${TIMESTAMP}.tar.gz"
kubectl cp ${NAMESPACE}/${POD_NAME}:${DIAG_FILE} "${DIAG_OUTPUT}"
print_success "Diag saved to: ${DIAG_OUTPUT}"

# Step 9: Restore and cleanup
print_info "[9/9] Restoring configuration and cleaning up..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  cp /opt/splunk/etc/splunk-launch.conf.noheap \
     /opt/splunk/etc/splunk-launch.conf

print_info "Restarting Splunk to disable profiling..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  /opt/splunk/bin/splunk restart

# Clean up remote files
print_info "Cleaning up remote files..."
kubectl exec -n ${NAMESPACE} ${POD_NAME} -- \
  sh -c "rm -rf /tmp/heap /tmp/heap_data.tar.gz /opt/splunk/diag-*.tar.gz /opt/splunk/etc/splunk-launch.conf.noheap"

print_success "Cleanup complete"

# Summary
echo ""
print_info "============================================"
print_success "Memory Profiling Collection Complete!"
print_info "============================================"
echo ""
print_info "Collected Files:"
ls -lh ${OUTPUT_DIR}/*-${POD_NAME}-${TIMESTAMP}* 2>/dev/null
echo ""
print_info "Summary:"
echo "  - Heap files collected: ${FILE_COUNT}"
echo "  - Heap data size: ${HEAP_SIZE_MB} MB"
echo "  - Collection duration: $(( COLLECTION_TIME / 60 )) minutes"
echo "  - Output directory: ${OUTPUT_DIR}"
echo ""
print_info "Next Steps:"
echo "  1. Upload ${HEAP_OUTPUT} to Splunk Support case"
echo "  2. Upload ${DIAG_OUTPUT} to Splunk Support case"
echo "  3. Provide description of memory issue and timeline"
echo "  4. Include these details:"
echo "     - When memory growth started"
echo "     - Symptoms observed"
echo "     - Recent configuration changes"
echo "     - Workload characteristics"
echo ""
print_info "Splunk Support Portal:"
echo "  https://splunkcommunity.force.com/customers/home/home.jsp"
print_info "============================================"
