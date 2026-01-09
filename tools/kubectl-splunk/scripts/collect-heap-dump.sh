#!/bin/bash
#
# collect-heap-dump.sh - Collect heap dumps from Splunk pods using kubectl-splunk
#
# Usage:
#   ./collect-heap-dump.sh [OPTIONS]
#
# Options:
#   -n, --namespace    Kubernetes namespace (default: splunk)
#   -p, --pod          Specific pod name (optional)
#   -l, --selector     Label selector (default: app=splunk)
#   -o, --output       Output directory (default: /tmp/splunk-heap-dumps)
#   -a, --all          Collect from all matching pods
#   -t, --threads      Also collect thread dumps
#   -h, --help         Show this help message
#
# Examples:
#   # Collect from specific pod
#   ./collect-heap-dump.sh -p splunk-idx-0 -n splunk
#
#   # Collect from all indexers
#   ./collect-heap-dump.sh -l app=splunk,role=indexer -a
#
#   # Collect with thread dumps
#   ./collect-heap-dump.sh -p splunk-idx-0 -t
#

set -e

# Default values
NAMESPACE="splunk"
POD_NAME=""
SELECTOR="app=splunk"
OUTPUT_DIR="/tmp/splunk-heap-dumps"
COLLECT_ALL=false
COLLECT_THREADS=false
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to show usage
show_usage() {
    grep "^#" "$0" | grep -v "#!/bin/bash" | sed 's/^# //' | sed 's/^#//'
    exit 0
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -p|--pod)
            POD_NAME="$2"
            shift 2
            ;;
        -l|--selector)
            SELECTOR="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        -a|--all)
            COLLECT_ALL=true
            shift
            ;;
        -t|--threads)
            COLLECT_THREADS=true
            shift
            ;;
        -h|--help)
            show_usage
            ;;
        *)
            print_error "Unknown option: $1"
            show_usage
            ;;
    esac
done

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    print_error "kubectl command not found. Please install kubectl first."
    exit 1
fi

# Create output directory
mkdir -p "${OUTPUT_DIR}"
print_info "Output directory: ${OUTPUT_DIR}"

# Function to check if kubectl-splunk is available
check_kubectl_splunk() {
    if command -v kubectl-splunk &> /dev/null; then
        return 0
    else
        print_warning "kubectl-splunk not found, using kubectl directly"
        return 1
    fi
}

# Get list of pods
get_pods() {
    if [ -n "$POD_NAME" ]; then
        echo "$POD_NAME"
    else
        kubectl get pods -n "${NAMESPACE}" -l "${SELECTOR}" \
            -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo ""
    fi
}

# Function to collect heap dump from a pod
collect_heap_dump() {
    local pod=$1
    local pod_dir="${OUTPUT_DIR}/${pod}"

    print_info "Processing pod: ${pod}"
    mkdir -p "${pod_dir}"

    # Check if pod exists and is running
    local pod_status
    pod_status=$(kubectl get pod -n "${NAMESPACE}" "${pod}" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

    if [ "$pod_status" != "Running" ]; then
        print_error "Pod ${pod} is not running (status: ${pod_status})"
        return 1
    fi

    # Find Java process ID
    print_info "Finding Java process in pod ${pod}..."
    local java_pid
    java_pid=$(kubectl exec -n "${NAMESPACE}" "${pod}" -- \
        sh -c 'pgrep -f "java.*splunkd" | head -1' 2>/dev/null || echo "")

    if [ -z "$java_pid" ]; then
        print_warning "No Java process found in pod ${pod}"
        # Try finding any java process
        java_pid=$(kubectl exec -n "${NAMESPACE}" "${pod}" -- \
            sh -c 'pgrep java | head -1' 2>/dev/null || echo "")

        if [ -z "$java_pid" ]; then
            print_error "No Java processes found in pod ${pod}"
            return 1
        fi
        print_info "Found Java process with PID: ${java_pid}"
    else
        print_info "Found splunkd Java process with PID: ${java_pid}"
    fi

    # Generate heap dump file name
    local heap_file="heap-${pod}-${TIMESTAMP}.hprof"
    local remote_heap_path="/tmp/${heap_file}"

    print_info "Generating heap dump (this may take a few minutes)..."

    # Try using Splunk's jmap wrapper first
    if kubectl exec -n "${NAMESPACE}" "${pod}" -- \
        test -f /opt/splunk/bin/splunk 2>/dev/null; then

        print_info "Using Splunk jmap wrapper..."
        if kubectl exec -n "${NAMESPACE}" "${pod}" -- \
            /opt/splunk/bin/splunk cmd jmap \
            -dump:live,format=b,file="${remote_heap_path}" "${java_pid}" 2>&1; then
            print_success "Heap dump generated successfully"
        else
            print_warning "Splunk jmap failed, trying direct jmap..."
            # Try direct jmap
            if ! kubectl exec -n "${NAMESPACE}" "${pod}" -- \
                jmap -dump:live,format=b,file="${remote_heap_path}" "${java_pid}" 2>&1; then
                print_error "Failed to generate heap dump for pod ${pod}"
                return 1
            fi
        fi
    else
        # Use jmap directly
        print_info "Using jmap directly..."
        if ! kubectl exec -n "${NAMESPACE}" "${pod}" -- \
            jmap -dump:live,format=b,file="${remote_heap_path}" "${java_pid}" 2>&1; then
            print_error "Failed to generate heap dump for pod ${pod}"
            return 1
        fi
    fi

    # Check heap dump size
    local heap_size
    heap_size=$(kubectl exec -n "${NAMESPACE}" "${pod}" -- \
        stat -c %s "${remote_heap_path}" 2>/dev/null || echo "0")

    if [ "$heap_size" -eq "0" ]; then
        print_error "Heap dump file is empty or was not created"
        return 1
    fi

    print_info "Heap dump size: $(numfmt --to=iec-i --suffix=B ${heap_size})"

    # Copy heap dump to local machine
    print_info "Copying heap dump to local machine..."
    if kubectl cp "${NAMESPACE}/${pod}:${remote_heap_path}" \
        "${pod_dir}/${heap_file}" 2>/dev/null; then
        print_success "Heap dump saved to: ${pod_dir}/${heap_file}"
    else
        print_error "Failed to copy heap dump from pod ${pod}"
        # Try to clean up remote file anyway
        kubectl exec -n "${NAMESPACE}" "${pod}" -- \
            rm -f "${remote_heap_path}" 2>/dev/null || true
        return 1
    fi

    # Clean up remote heap dump
    print_info "Cleaning up remote heap dump..."
    kubectl exec -n "${NAMESPACE}" "${pod}" -- \
        rm -f "${remote_heap_path}" 2>/dev/null || true

    # Collect thread dump if requested
    if [ "$COLLECT_THREADS" = true ]; then
        print_info "Collecting thread dump..."
        local thread_file="${pod_dir}/threads-${pod}-${TIMESTAMP}.txt"

        if kubectl exec -n "${NAMESPACE}" "${pod}" -- \
            test -f /opt/splunk/bin/splunk 2>/dev/null; then
            # Use Splunk's jstack wrapper
            kubectl exec -n "${NAMESPACE}" "${pod}" -- \
                /opt/splunk/bin/splunk cmd jstack "${java_pid}" \
                > "${thread_file}" 2>/dev/null || true
        else
            # Use jstack directly
            kubectl exec -n "${NAMESPACE}" "${pod}" -- \
                jstack "${java_pid}" \
                > "${thread_file}" 2>/dev/null || true
        fi

        if [ -f "${thread_file}" ] && [ -s "${thread_file}" ]; then
            print_success "Thread dump saved to: ${thread_file}"
        else
            print_warning "Failed to collect thread dump"
        fi
    fi

    # Collect basic pod info
    print_info "Collecting pod information..."
    kubectl get pod -n "${NAMESPACE}" "${pod}" -o yaml \
        > "${pod_dir}/pod-info.yaml" 2>/dev/null || true
    kubectl top pod -n "${NAMESPACE}" "${pod}" \
        > "${pod_dir}/pod-metrics.txt" 2>/dev/null || true

    return 0
}

# Main execution
print_info "=========================================="
print_info "Splunk Heap Dump Collection Tool"
print_info "=========================================="
print_info "Namespace: ${NAMESPACE}"
print_info "Selector: ${SELECTOR}"
print_info "Timestamp: ${TIMESTAMP}"

if [ -n "$POD_NAME" ]; then
    print_info "Target Pod: ${POD_NAME}"
fi

print_info "=========================================="

# Get pods to process
print_info "Finding Splunk pods..."
PODS=$(get_pods)

if [ -z "$PODS" ]; then
    print_error "No pods found matching criteria"
    exit 1
fi

pod_count=$(echo "$PODS" | wc -w | tr -d ' ')
print_info "Found ${pod_count} pod(s): ${PODS}"

# If multiple pods found and --all not specified, ask user
if [ "$pod_count" -gt 1 ] && [ "$COLLECT_ALL" != true ] && [ -z "$POD_NAME" ]; then
    echo ""
    print_warning "Multiple pods found. Use --all to collect from all pods, or specify a pod with --pod"
    echo ""
    echo "Available pods:"
    i=1
    for pod in $PODS; do
        echo "  ${i}. ${pod}"
        i=$((i + 1))
    done
    echo ""
    read -p "Select pod number (1-${pod_count}) or 'a' for all: " selection

    if [ "$selection" = "a" ]; then
        COLLECT_ALL=true
    elif [ "$selection" -ge 1 ] && [ "$selection" -le "$pod_count" ]; then
        PODS=$(echo "$PODS" | tr ' ' '\n' | sed -n "${selection}p")
    else
        print_error "Invalid selection"
        exit 1
    fi
fi

# Collect heap dumps
success_count=0
failure_count=0

for pod in $PODS; do
    echo ""
    if collect_heap_dump "$pod"; then
        ((success_count++))
    else
        ((failure_count++))
    fi
done

# Generate summary
echo ""
print_info "=========================================="
print_info "Collection Summary"
print_info "=========================================="
print_info "Total pods processed: ${pod_count}"
print_success "Successful collections: ${success_count}"
if [ "$failure_count" -gt 0 ]; then
    print_error "Failed collections: ${failure_count}"
fi
print_info "Output directory: ${OUTPUT_DIR}"
print_info "=========================================="

# Create summary file
summary_file="${OUTPUT_DIR}/collection-summary-${TIMESTAMP}.txt"
cat > "${summary_file}" <<EOF
Splunk Heap Dump Collection Summary
====================================
Generated: $(date)
Namespace: ${NAMESPACE}
Selector: ${SELECTOR}
Timestamp: ${TIMESTAMP}

Pods processed: ${pod_count}
Successful: ${success_count}
Failed: ${failure_count}

Output directory: ${OUTPUT_DIR}

Heap Dumps Collected:
EOF

find "${OUTPUT_DIR}" -name "*.hprof" -exec ls -lh {} \; >> "${summary_file}"

print_success "Summary saved to: ${summary_file}"

# Show next steps
echo ""
print_info "Next Steps:"
echo "  1. Analyze heap dumps with Eclipse MAT or VisualVM"
echo "  2. Review thread dumps if collected"
echo "  3. Compare with resource metrics: kubectl top pod"
echo "  4. Check Splunk logs: kubectl logs <pod> | grep -i memory"
echo ""
echo "To analyze with jhat:"
echo "  jhat -J-Xmx4g ${OUTPUT_DIR}/<pod-name>/heap-*.hprof"
echo "  Then open: http://localhost:7000"

exit $( [ "$failure_count" -eq 0 ] && echo 0 || echo 1 )
