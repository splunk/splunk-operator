#!/usr/bin/env bash
# run make install make run in a separate terminal to have the operator running while this test executes
# this test verifies that when a PostgresCluster with clusterDeletionPolicy=Retain is deleted, the underlying CNPG Cluster and superuser Secret are not deleted and can be re-attached to a new PostgresCluster with the same name (simulating a major version upgrade flow where the cluster needs to be recreated). 
# then, in a separate terminal, run: NAMESPACE=your-namespace UPGRADE_POSTGRES_VERSION=16 ./test/postgrescluster-retain-upgrade-flow.sh


set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$ROOT_DIR/test"
SAMPLES_DIR="$ROOT_DIR/config/samples"

CLUSTER_MANIFEST="${CLUSTER_MANIFEST:-$SAMPLES_DIR/enterprise_v4_postgrescluster_dev.yaml}"
DATABASE_MANIFEST="${DATABASE_MANIFEST:-$SAMPLES_DIR/enterprise_v4_postgresdatabase.yaml}"
CONNECT_SCRIPT="${CONNECT_SCRIPT:-$TEST_DIR/connect-to-postgres-cluster.sh}"
UPGRADE_POSTGRES_VERSION="${UPGRADE_POSTGRES_VERSION:-16}"
POLL_INTERVAL="${POLL_INTERVAL:-5}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-900}"
REQUIRE_POSTGRESDATABASE_READY="${REQUIRE_POSTGRESDATABASE_READY:-0}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    echo -e "${YELLOW}[$(date '+%Y-%m-%d %H:%M:%S')] $*${NC}"
}

pass() {
    echo -e "${GREEN}[PASS] $*${NC}"
}

fail() {
    echo -e "${RED}[FAIL] $*${NC}" >&2
    exit 1
}

require_file() {
    local path="$1"
    [[ -f "$path" ]] || fail "Required file not found: $path"
}

require_command() {
    local cmd="$1"
    command -v "$cmd" >/dev/null 2>&1 || fail "Required command not found: $cmd"
}

current_namespace() {
    local ns
    ns="$(kubectl config view --minify --output 'jsonpath={..namespace}' 2>/dev/null || true)"
    if [[ -z "$ns" ]]; then
        ns="default"
    fi
    printf '%s' "$ns"
}

preflight_namespace() {
    local deletion_ts phase
    deletion_ts="$(kubectl get ns "$NAMESPACE" -o jsonpath='{.metadata.deletionTimestamp}' 2>/dev/null || true)"
    phase="$(kubectl get ns "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    if [[ -n "$deletion_ts" || "$phase" == "Terminating" ]]; then
        fail "Namespace $NAMESPACE is terminating (deletionTimestamp=$deletion_ts phase=$phase). Use a non-terminating namespace."
    fi
}

preflight_cluster_dns() {
    local host
    host="${CLUSTER_NAME}-rw.${NAMESPACE}.svc.cluster.local"
    if getent hosts "$host" >/dev/null 2>&1; then
        return 0
    fi

    log "Cluster DNS name is not resolvable from this machine: $host"
    log "This does not block local connection tests (we use kubectl port-forward), but it blocks PostgresDatabase DB-connection/privilege phases when the operator runs out-of-cluster (make run)."
    log "Fix: run the operator in-cluster or use telepresence/kubefwd to get cluster DNS/networking on your machine."

    SKIP_POSTGRESDATABASE_READY_CHECK=1
    if [[ "$REQUIRE_POSTGRESDATABASE_READY" == "1" ]]; then
        fail "PostgresDatabase readiness required (REQUIRE_POSTGRESDATABASE_READY=1) but cluster DNS is not available."
    fi

    log "Continuing with degraded PostgresDatabase checks (readiness will not be required)."
}

resource_exists() {
    local resource="$1"
    local name="$2"
    kubectl get "$resource" "$name" -n "$NAMESPACE" >/dev/null 2>&1
}

jsonpath_value() {
    local resource="$1"
    local name="$2"
    local jsonpath="$3"
    kubectl get "$resource" "$name" -n "$NAMESPACE" -o "jsonpath=${jsonpath}" 2>/dev/null
}

wait_for_jsonpath() {
    local resource="$1"
    local name="$2"
    local jsonpath="$3"
    local expected="$4"
    local timeout="${5:-$TIMEOUT_SECONDS}"
    local deadline=$((SECONDS + timeout))
    local value=""

    while (( SECONDS < deadline )); do
        value="$(jsonpath_value "$resource" "$name" "$jsonpath" || true)"
        if [[ "$value" == "$expected" ]]; then
            pass "$resource/$name reached ${jsonpath}=${expected}"
            return 0
        fi
        sleep "$POLL_INTERVAL"
    done

    fail "Timed out waiting for $resource/$name to reach ${jsonpath}=${expected}. Last value: ${value:-<empty>}"
}

wait_for_contains() {
    local resource="$1"
    local name="$2"
    local jsonpath="$3"
    local expected_substring="$4"
    local timeout="${5:-$TIMEOUT_SECONDS}"
    local deadline=$((SECONDS + timeout))
    local value=""

    while (( SECONDS < deadline )); do
        value="$(jsonpath_value "$resource" "$name" "$jsonpath" || true)"
        if [[ "$value" == *"$expected_substring"* ]]; then
            pass "$resource/$name contains ${expected_substring} in ${jsonpath}"
            return 0
        fi
        sleep "$POLL_INTERVAL"
    done

    fail "Timed out waiting for $resource/$name to contain ${expected_substring} in ${jsonpath}. Last value: ${value:-<empty>}"
}

wait_for_absence() {
    local resource="$1"
    local name="$2"
    local timeout="${3:-$TIMEOUT_SECONDS}"
    local deadline=$((SECONDS + timeout))

    while (( SECONDS < deadline )); do
        if ! resource_exists "$resource" "$name"; then
            pass "$resource/$name is absent"
            return 0
        fi
        sleep "$POLL_INTERVAL"
    done

    fail "Timed out waiting for $resource/$name to be deleted"
}

wait_for_presence() {
    local resource="$1"
    local name="$2"
    local timeout="${3:-$TIMEOUT_SECONDS}"
    local deadline=$((SECONDS + timeout))

    while (( SECONDS < deadline )); do
        if resource_exists "$resource" "$name"; then
            pass "$resource/$name exists"
            return 0
        fi
        sleep "$POLL_INTERVAL"
    done

    fail "Timed out waiting for $resource/$name to exist"
}

wait_for_owner_reference() {
    local resource="$1"
    local name="$2"
    local owner_kind="$3"
    local owner_name="$4"
    local owner_uid="$5"
    local timeout="${6:-$TIMEOUT_SECONDS}"
    local deadline=$((SECONDS + timeout))
    local owners=""
    local expected="${owner_kind}:${owner_name}:${owner_uid}"

    while (( SECONDS < deadline )); do
        owners="$(jsonpath_value "$resource" "$name" '{range .metadata.ownerReferences[*]}{.kind}:{.name}:{.uid}{"\n"}{end}' || true)"
        if [[ "$owners" == *"$expected"* ]]; then
            pass "$resource/$name is owned by ${owner_kind}/${owner_name}"
            return 0
        fi
        sleep "$POLL_INTERVAL"
    done

    fail "Timed out waiting for $resource/$name to be owned by ${owner_kind}/${owner_name}. Owners: ${owners:-<none>}"
}

run_connection_check() {
    log "Checking superuser connection with $CONNECT_SCRIPT"
    printf 'SELECT current_user;\n\\q\n' | bash "$CONNECT_SCRIPT" "$CLUSTER_NAME" "$NAMESPACE"
    pass "Superuser connection succeeded"
}

patch_cluster() {
    local deletion_policy="$1"
    local pooler_enabled="$2"
    kubectl patch postgrescluster "$CLUSTER_NAME" -n "$NAMESPACE" --type merge \
        -p "{\"spec\":{\"clusterDeletionPolicy\":\"${deletion_policy}\",\"connectionPoolerEnabled\":${pooler_enabled}}}" >/dev/null
}

apply_upgraded_cluster_manifest() {
    local tmp_manifest
    tmp_manifest="$(mktemp)"

    sed \
        -e "s/^\([[:space:]]*clusterDeletionPolicy:\).*/\1 Retain/" \
        -e "s/^\([[:space:]]*postgresVersion:\).*/\1 \"${UPGRADE_POSTGRES_VERSION}\"/" \
        "$CLUSTER_MANIFEST" > "$tmp_manifest"

    kubectl apply -n "$NAMESPACE" -f "$tmp_manifest" >/dev/null
    rm -f "$tmp_manifest"
}

assert_cluster_ready() {
    wait_for_jsonpath postgrescluster "$CLUSTER_NAME" '{.status.phase}' 'Ready'
    wait_for_jsonpath postgrescluster "$CLUSTER_NAME" '{.status.conditions[?(@.type=="ClusterReady")].status}' 'True'
    wait_for_jsonpath postgrescluster "$CLUSTER_NAME" '{.status.conditions[?(@.type=="ConfigMapReady")].status}' 'True'
}

assert_database_created() {
    wait_for_presence postgresdatabase "$DATABASE_NAME"
    for db in "${DATABASES[@]}"; do
        wait_for_presence databases.postgresql.cnpg.io "${DATABASE_NAME}-${db}"
    done
    pass "PostgresDatabase CR exists and CNPG Database CRs are present"
}

assert_database_ready() {
    if [[ "${SKIP_POSTGRESDATABASE_READY_CHECK:-0}" == "1" ]]; then
        assert_database_created
        return 0
    fi
    wait_for_jsonpath postgresdatabase "$DATABASE_NAME" '{.status.phase}' 'Ready'
    wait_for_jsonpath postgresdatabase "$DATABASE_NAME" '{.status.observedGeneration}' \
        "$(jsonpath_value postgresdatabase "$DATABASE_NAME" '{.metadata.generation}')"
}

record_cluster_artifacts() {
    SUPERUSER_SECRET_NAME="$(jsonpath_value postgrescluster "$CLUSTER_NAME" '{.status.resources.secretRef.name}')"
    CONFIGMAP_NAME="$(jsonpath_value postgrescluster "$CLUSTER_NAME" '{.status.resources.configMapRef.name}')"

    [[ -n "$SUPERUSER_SECRET_NAME" ]] || fail "PostgresCluster status.resources.secretRef.name is empty"
    [[ -n "$CONFIGMAP_NAME" ]] || fail "PostgresCluster status.resources.configMapRef.name is empty"
}

cleanup_database_cr() {
    if resource_exists postgresdatabase "$DATABASE_NAME"; then
        log "Deleting PostgresDatabase/$DATABASE_NAME to leave the namespace clean"
        kubectl delete postgresdatabase "$DATABASE_NAME" -n "$NAMESPACE" --wait=false >/dev/null
        wait_for_absence postgresdatabase "$DATABASE_NAME"
    fi
}

require_command kubectl
require_file "$CLUSTER_MANIFEST"
require_file "$DATABASE_MANIFEST"
require_file "$CONNECT_SCRIPT"

NAMESPACE="${NAMESPACE:-$(current_namespace)}"
CLUSTER_NAME="${CLUSTER_NAME:-$(kubectl create --dry-run=client -f "$CLUSTER_MANIFEST" -o jsonpath='{.metadata.name}')}"
DATABASE_NAME="${DATABASE_NAME:-$(kubectl create --dry-run=client -f "$DATABASE_MANIFEST" -o jsonpath='{.metadata.name}')}"
DATABASES_STR="$(kubectl create --dry-run=client -f "$DATABASE_MANIFEST" -o jsonpath='{range .spec.databases[*]}{.name}{" "}{end}')"
read -r -a DATABASES <<< "${DATABASES_STR:-}"
RW_POOLER_NAME="${CLUSTER_NAME}-pooler-rw"
RO_POOLER_NAME="${CLUSTER_NAME}-pooler-ro"

log "Using namespace: $NAMESPACE"
log "Cluster manifest: $CLUSTER_MANIFEST"
log "Database manifest: $DATABASE_MANIFEST"
log "Upgrade target postgresVersion: $UPGRADE_POSTGRES_VERSION"

preflight_namespace
preflight_cluster_dns

log "1. Creating PostgresCluster from sample manifest"
kubectl apply -n "$NAMESPACE" -f "$CLUSTER_MANIFEST"

log "2. Creating PostgresDatabase from sample manifest"
kubectl apply -n "$NAMESPACE" -f "$DATABASE_MANIFEST"

log "3. Waiting for PostgresCluster and PostgresDatabase to become ready"
assert_cluster_ready
assert_database_ready
record_cluster_artifacts
pass "PostgresCluster and PostgresDatabase were created successfully"

log "4. Verifying superuser connection to PostgresCluster"
run_connection_check

log "5. Setting clusterDeletionPolicy=Retain and connectionPoolerEnabled=false"
patch_cluster "Retain" "false"
wait_for_jsonpath postgrescluster "$CLUSTER_NAME" '{.spec.clusterDeletionPolicy}' 'Retain'
wait_for_jsonpath postgrescluster "$CLUSTER_NAME" '{.spec.connectionPoolerEnabled}' 'false'
wait_for_absence pooler.postgresql.cnpg.io "$RW_POOLER_NAME"
wait_for_absence pooler.postgresql.cnpg.io "$RO_POOLER_NAME"
assert_cluster_ready

log "6. Setting connectionPoolerEnabled=true and waiting for poolers"
patch_cluster "Retain" "true"
wait_for_jsonpath postgrescluster "$CLUSTER_NAME" '{.spec.connectionPoolerEnabled}' 'true'
wait_for_presence pooler.postgresql.cnpg.io "$RW_POOLER_NAME"
wait_for_presence pooler.postgresql.cnpg.io "$RO_POOLER_NAME"
wait_for_jsonpath postgrescluster "$CLUSTER_NAME" '{.status.conditions[?(@.type=="PoolerReady")].status}' 'True'
assert_cluster_ready

log "7. Deleting PostgresCluster with retention enabled"
kubectl delete postgrescluster "$CLUSTER_NAME" -n "$NAMESPACE" --wait=false >/dev/null
wait_for_absence postgrescluster "$CLUSTER_NAME"
wait_for_presence cluster.postgresql.cnpg.io "$CLUSTER_NAME"
wait_for_presence secret "$SUPERUSER_SECRET_NAME"
pass "CNPG cluster and superuser secret remained after PostgresCluster deletion"

log "8. Recreating PostgresCluster with a major version upgrade"
apply_upgraded_cluster_manifest
wait_for_presence postgrescluster "$CLUSTER_NAME"
wait_for_contains cluster.postgresql.cnpg.io "$CLUSTER_NAME" '{.spec.imageName}' ":${UPGRADE_POSTGRES_VERSION}"
assert_cluster_ready
record_cluster_artifacts

log "9. Checking that retained resources were re-attached to the recreated PostgresCluster"
POSTGRES_CLUSTER_UID="$(jsonpath_value postgrescluster "$CLUSTER_NAME" '{.metadata.uid}')"
wait_for_owner_reference cluster.postgresql.cnpg.io "$CLUSTER_NAME" "PostgresCluster" "$CLUSTER_NAME" "$POSTGRES_CLUSTER_UID"
wait_for_owner_reference secret "$SUPERUSER_SECRET_NAME" "PostgresCluster" "$CLUSTER_NAME" "$POSTGRES_CLUSTER_UID"

log "10. Verifying superuser connection after recreate/upgrade"
run_connection_check

log "11. Setting clusterDeletionPolicy=Delete"
kubectl patch postgrescluster "$CLUSTER_NAME" -n "$NAMESPACE" --type merge \
    -p '{"spec":{"clusterDeletionPolicy":"Delete"}}' >/dev/null
wait_for_jsonpath postgrescluster "$CLUSTER_NAME" '{.spec.clusterDeletionPolicy}' 'Delete'

log "12. Deleting the PostgresCluster"
kubectl delete postgrescluster "$CLUSTER_NAME" -n "$NAMESPACE" --wait=false >/dev/null
wait_for_absence postgrescluster "$CLUSTER_NAME"

log "13. Checking that no cluster leftovers remain"
cleanup_database_cr
wait_for_absence cluster.postgresql.cnpg.io "$CLUSTER_NAME"
wait_for_absence pooler.postgresql.cnpg.io "$RW_POOLER_NAME"
wait_for_absence pooler.postgresql.cnpg.io "$RO_POOLER_NAME"
wait_for_absence secret "$SUPERUSER_SECRET_NAME"
wait_for_absence configmap "$CONFIGMAP_NAME"
pass "No PostgresCluster leftovers remain in namespace $NAMESPACE"

log "Flow finished successfully"
