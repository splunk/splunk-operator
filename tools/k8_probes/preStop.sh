#!/bin/bash
# PreStop lifecycle hook for Splunk pods
# Handles graceful shutdown with role-specific decommission/detention logic
#
# This script is called by Kubernetes before a pod is terminated.
# It ensures proper cleanup based on pod role and termination reason.

set -e

# Logging functions
log_info() {
    echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') - $*"
}

log_error() {
    echo "[ERROR] $(date '+%Y-%m-%d %H:%M:%S') - $*" >&2
}

log_warn() {
    echo "[WARN] $(date '+%Y-%m-%d %H:%M:%S') - $*"
}

# Configuration
SPLUNK_HOME="${SPLUNK_HOME:-/opt/splunk}"
SPLUNK_BIN="${SPLUNK_HOME}/bin/splunk"
MGMT_PORT="${SPLUNK_MGMT_PORT:-8089}"
SPLUNK_USER="admin"
SPLUNK_PASSWORD_FILE="/mnt/splunk-secrets/password"

# Track total script execution time — must be set before the timeout block so
# SCRIPT_START_TIME is anchored to when the script actually launched.
SCRIPT_START_TIME=$(date +%s)

# Split timeout budget between decommission/detention and splunk stop.
#
# TOTAL_BUDGET is computed from the actual (possibly overridden) DECOMMISSION_MAX_WAIT
# and STOP_MAX_WAIT so that env-var overrides are reflected in the cumulative guard.
#
# Grace periods vs budgets:
#   Indexers:     1020s grace = 900s decommission + 90s stop + 30s buffer → budget 990s
#   Search heads:  360s grace = 300s detention    + 50s stop + 10s buffer → budget 350s
#   Others:        120s grace =  80s operations   + 30s stop + 10s buffer → budget 110s
if [ "${SPLUNK_ROLE}" = "splunk_indexer" ]; then
    DECOMMISSION_MAX_WAIT="${PRESTOP_DECOMMISSION_WAIT:-900}"
    STOP_MAX_WAIT="${PRESTOP_STOP_WAIT:-90}"
elif [ "${SPLUNK_ROLE}" = "splunk_search_head" ]; then
    DECOMMISSION_MAX_WAIT="${PRESTOP_DECOMMISSION_WAIT:-300}"
    STOP_MAX_WAIT="${PRESTOP_STOP_WAIT:-50}"
else
    DECOMMISSION_MAX_WAIT="${PRESTOP_DECOMMISSION_WAIT:-80}"
    STOP_MAX_WAIT="${PRESTOP_STOP_WAIT:-30}"
fi

# TOTAL_BUDGET is the sum of the two phases; stop_splunk() will clamp its timeout
# to whatever budget remains, preventing decommission overrun from stealing stop time.
TOTAL_BUDGET=$((DECOMMISSION_MAX_WAIT + STOP_MAX_WAIT))

# Alias used by the search-head detention wait loop (kept for readability).
MAX_WAIT_SECONDS="${DECOMMISSION_MAX_WAIT}"

# Pod metadata — supplied by the pod spec as standard env vars.
POD_NAME="${POD_NAME:-unknown}"
POD_NAMESPACE="${POD_NAMESPACE:-default}"

log_info "Starting preStop hook for pod: ${POD_NAME}, role: ${SPLUNK_ROLE}"

# Function to read pod intent annotation
get_pod_intent() {
    # Read intent from downward API volume (mounted at /etc/podinfo/intent)
    # This file updates dynamically when the annotation changes (unlike env vars)
    # No RBAC permissions required
    local intent_file="/etc/podinfo/intent"
    local intent="serve"  # default

    if [ -f "$intent_file" ]; then
        intent=$(cat "$intent_file" 2>/dev/null || echo "serve")
        # Trim whitespace
        intent=$(echo "$intent" | tr -d '[:space:]')
    fi

    # Handle case where annotation doesn't exist (empty string)
    if [ -z "$intent" ]; then
        intent="serve"
    fi

    echo "$intent"
}

# Function to call Splunk REST API
splunk_api_call() {
    local method="$1"
    local endpoint="$2"
    local data="$3"
    local expected_status="$4"

    local url="https://localhost:${MGMT_PORT}${endpoint}"
    local response
    local http_code

    if [ -z "$SPLUNK_PASSWORD" ]; then
        log_error "SPLUNK_PASSWORD not set, cannot make API calls"
        return 1
    fi

    if [ "$method" = "POST" ]; then
        response=$(curl -s -w "\n%{http_code}" -k -u "${SPLUNK_USER}:${SPLUNK_PASSWORD}" \
            -X POST "$url" -d "$data" 2>&1)
    else
        response=$(curl -s -w "\n%{http_code}" -k -u "${SPLUNK_USER}:${SPLUNK_PASSWORD}" \
            -X GET "$url" 2>&1)
    fi

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')

    if [ "$http_code" = "$expected_status" ] || [ "$http_code" = "200" ]; then
        echo "$body"
        return 0
    else
        log_error "API call failed: $method $endpoint - HTTP $http_code"
        log_error "Response: $body"
        return 1
    fi
}

# Function to get indexer peer status from cluster manager
get_indexer_peer_status() {
    local cluster_manager_url="$1"
    local peer_name="$2"

    # Query cluster manager for peer status
    local response
    response=$(curl -s -k -u "${SPLUNK_USER}:${SPLUNK_PASSWORD}" \
        "${cluster_manager_url}/services/cluster/manager/peers?output_mode=json" 2>/dev/null)

    if [ $? -ne 0 ]; then
        log_error "Failed to query cluster manager for peer status"
        return 1
    fi

    # Extract peer status using grep (avoid jq dependency)
    local peer_status
    peer_status=$(echo "$response" | grep -o "\"label\":\"${peer_name}\"[^}]*\"status\":\"[^\"]*\"" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)

    if [ -z "$peer_status" ]; then
        log_warn "Could not find peer status for ${peer_name}, may already be removed"
        echo "Down"
    else
        echo "$peer_status"
    fi
}

# Build cluster manager API URL from available env vars.
# Prefer SPLUNK_CLUSTER_MANAGER_API_URL; otherwise derive from
# SPLUNK_CLUSTER_MANAGER_URL for backward compatibility.
get_cluster_manager_api_url() {
    local cm_api_url="${SPLUNK_CLUSTER_MANAGER_API_URL}"
    local cm_host="${SPLUNK_CLUSTER_MANAGER_URL}"

    if [ -n "$cm_api_url" ]; then
        echo "$cm_api_url"
        return
    fi

    if [ -z "$cm_host" ]; then
        echo ""
        return
    fi

    if [[ "$cm_host" == http://* || "$cm_host" == https://* ]]; then
        echo "$cm_host"
    else
        echo "https://${cm_host}:8089"
    fi
}

# Function to check if search head is in cluster
check_search_head_in_cluster() {
    local response
    response=$(splunk_api_call GET "/services/shcluster/member/info?output_mode=json" "" "200")

    if [ $? -eq 0 ] && echo "$response" | grep -q '"is_registered":true'; then
        return 0  # In cluster
    else
        return 1  # Not in cluster
    fi
}

# Function to decommission indexer
decommission_indexer() {
    local intent="$1"
    local enforce_counts

    # Determine enforce_counts based on intent
    if [ "$intent" = "scale-down" ]; then
        enforce_counts="1"  # Rebalance buckets to other peers
        log_info "Scale-down detected: decommission with enforce_counts=1 (rebalance buckets)"
    else
        enforce_counts="0"  # No rebalancing, just stop accepting data
        log_info "Restart detected: decommission with enforce_counts=0 (no rebalance)"
    fi

    # Call decommission API
    log_info "Starting decommission with enforce_counts=${enforce_counts}"
    if ! splunk_api_call POST "/services/cluster/peer/control/control/decommission" "enforce_counts=${enforce_counts}" "200"; then
        log_error "Failed to start decommission"
        return 1
    fi

    # Get cluster manager API URL from environment
    local cm_url
    cm_url=$(get_cluster_manager_api_url)
    if [ -z "$cm_url" ]; then
        log_warn "Cluster manager URL not set, cannot verify decommission status"
        log_info "Waiting 30 seconds for decommission to progress..."
        sleep 30
        return 0
    fi

    # Wait for decommission to complete
    log_info "Waiting for decommission to complete (max ${DECOMMISSION_MAX_WAIT}s)..."
    local elapsed=0
    local check_interval=10

    # Construct peer name: pod DNS name without the service suffix
    # Peer name in CM is just the pod name (e.g., "splunk-idx-indexer-0")
    local peer_name="${POD_NAME}"

    while [ $elapsed -lt $DECOMMISSION_MAX_WAIT ]; do
        local status
        status=$(get_indexer_peer_status "$cm_url" "$peer_name")

        log_info "Current peer status: $status"

        case "$status" in
            "Down"|"GracefulShutdown")
                log_info "Decommission complete after ${elapsed}s, peer status: $status"
                return 0
                ;;
            "Decommissioning"|"ReassigningPrimaries")
                log_info "Decommission in progress (${elapsed}s elapsed), status: $status"
                ;;
            "Up")
                log_warn "Peer still up (${elapsed}s elapsed), decommission may not have started"
                ;;
            *)
                log_warn "Unknown peer status (${elapsed}s elapsed): $status"
                ;;
        esac

        sleep $check_interval
        elapsed=$((elapsed + check_interval))
    done

    log_error "Decommission timeout after ${DECOMMISSION_MAX_WAIT}s - bucket migration may be incomplete"
    return 1  # Signal failure so operator/finalizer can detect incomplete decommission
}

# Function to detain search head (remove from cluster)
detain_search_head() {
    local intent="$1"

    log_info "Starting search head detention (removal from cluster)"

    # Check if already removed from cluster
    if ! check_search_head_in_cluster; then
        log_info "Search head already removed from cluster"
        return 0
    fi

    # Call detention API (remove from consensus)
    if ! splunk_api_call POST "/services/shcluster/member/consensus/default/remove_server" "" "200"; then
        # Check for expected 503 errors (member not in config = already removed)
        log_warn "Detention API returned error, checking if already removed..."

        if ! check_search_head_in_cluster; then
            log_info "Search head successfully removed from cluster"
            return 0
        fi

        log_error "Failed to remove search head from cluster"
        return 1
    fi

    # Wait for removal to complete
    log_info "Waiting for removal from cluster (max ${MAX_WAIT_SECONDS}s)..."
    local elapsed=0
    local check_interval=5

    while [ $elapsed -lt $MAX_WAIT_SECONDS ]; do
        if ! check_search_head_in_cluster; then
            log_info "Search head successfully removed from cluster after ${elapsed}s"
            return 0
        fi

        log_info "Still registered in cluster (${elapsed}s elapsed), waiting..."
        sleep $check_interval
        elapsed=$((elapsed + check_interval))
    done

    log_error "Detention timeout after ${MAX_WAIT_SECONDS}s - member may still be registered"
    return 1  # Signal failure so operator/finalizer can detect incomplete detention
}

# Function to gracefully stop Splunk
stop_splunk() {
    if [ ! -x "$SPLUNK_BIN" ]; then
        log_error "Splunk binary not found at ${SPLUNK_BIN}"
        return 1
    fi

    # Calculate actual remaining time based on total budget and elapsed time
    local current_time elapsed_time remaining_time actual_stop_timeout
    current_time=$(date +%s)
    elapsed_time=$((current_time - SCRIPT_START_TIME))
    remaining_time=$((TOTAL_BUDGET - elapsed_time))

    # Use the smaller of STOP_MAX_WAIT or remaining_time to stay within budget
    if [ $remaining_time -le 0 ]; then
        log_warn "Total budget exhausted (${elapsed_time}s elapsed), attempting immediate stop"
        actual_stop_timeout=5  # Minimal grace for stop
    elif [ $remaining_time -lt $STOP_MAX_WAIT ]; then
        actual_stop_timeout=$remaining_time
        log_info "Adjusting stop timeout from ${STOP_MAX_WAIT}s to ${actual_stop_timeout}s (remaining budget)"
    else
        actual_stop_timeout=$STOP_MAX_WAIT
    fi

    log_info "Stopping Splunk gracefully (timeout: ${actual_stop_timeout}s, elapsed: ${elapsed_time}s, budget: ${TOTAL_BUDGET}s)..."

    # Stop Splunk with calculated timeout to ensure we stay within grace period
    if timeout ${actual_stop_timeout} "$SPLUNK_BIN" stop; then
        log_info "Splunk stopped successfully"
        return 0
    else
        log_warn "Splunk stop timed out or failed after ${actual_stop_timeout}s, may need forceful termination"
        return 1
    fi
}

# Main logic
main() {
    # Validate required environment variables
    if [ -z "$POD_NAME" ]; then
        log_error "POD_NAME environment variable not set"
        exit 1
    fi

    if [ -z "$POD_NAMESPACE" ]; then
        log_error "POD_NAMESPACE environment variable not set"
        exit 1
    fi

    if [ -z "$SPLUNK_ROLE" ]; then
        log_error "SPLUNK_ROLE environment variable not set"
        exit 1
    fi

    # Read Splunk admin password from mounted secret
    if [ ! -f "$SPLUNK_PASSWORD_FILE" ]; then
        log_error "Splunk password file not found at ${SPLUNK_PASSWORD_FILE}"
        exit 1
    fi

    SPLUNK_PASSWORD=$(cat "$SPLUNK_PASSWORD_FILE")
    if [ -z "$SPLUNK_PASSWORD" ]; then
        log_error "Splunk password file is empty"
        exit 1
    fi

    # Role-specific validation
    if [ "$SPLUNK_ROLE" = "splunk_indexer" ] && [ -z "$(get_cluster_manager_api_url)" ]; then
        log_warn "Cluster manager URL env vars are not set for indexer - decommission status verification will be skipped"
    fi

    local pod_intent
    pod_intent=$(get_pod_intent)
    log_info "Pod intent: ${pod_intent}"

    # Handle based on Splunk role
    case "$SPLUNK_ROLE" in
        "splunk_indexer")
            log_info "Detected indexer role"
            if ! decommission_indexer "$pod_intent"; then
                log_error "Indexer decommission failed, stopping Splunk anyway"
            fi
            stop_splunk
            ;;

        "splunk_search_head")
            log_info "Detected search head role"
            if ! detain_search_head "$pod_intent"; then
                log_error "Search head detention failed, stopping Splunk anyway"
            fi
            stop_splunk
            ;;

        "splunk_cluster_manager"|"splunk_cluster_master")
            log_info "Detected cluster manager role, graceful stop only"
            stop_splunk
            ;;

        "splunk_license_manager"|"splunk_license_master")
            log_info "Detected license manager role, graceful stop only"
            stop_splunk
            ;;

        "splunk_monitoring_console")
            log_info "Detected monitoring console role, graceful stop only"
            stop_splunk
            ;;

        "splunk_deployer")
            log_info "Detected deployer role, graceful stop only"
            stop_splunk
            ;;

        "splunk_standalone")
            log_info "Detected standalone role, graceful stop only"
            stop_splunk
            ;;

        "splunk_ingestor")
            log_info "Detected ingestor role, graceful stop only"
            stop_splunk
            ;;

        *)
            log_warn "Unknown Splunk role: ${SPLUNK_ROLE}, attempting graceful stop"
            stop_splunk
            ;;
    esac

    local exit_code=$?
    log_info "PreStop hook completed with exit code: ${exit_code}"
    return $exit_code
}

# Execute main function
main
exit $?
