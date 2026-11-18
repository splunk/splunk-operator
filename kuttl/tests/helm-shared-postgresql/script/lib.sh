#!/usr/bin/env bash
# Sourceable lifecycle helpers; sourcing performs no cluster operations.
# Caller contract (set before invoking the relevant helpers):
#   release, chart, major: Helm release name, chart path and PostgreSQL major.
#   cluster, database, client_image: resource names and SQL-client image.
#   tmpdir: caller-owned scratch directory; forward_pid: empty or active tunnel PID.
# Environment: HELM_POSTGRES_CONTEXT, NAMESPACE, HELM_POSTGRES_VALUES,
# KUTTL_SPLUNK_OPERATOR_IMAGE and the user's accepted SPLUNK_GENERAL_TERMS.
. "$(dirname "${BASH_SOURCE[0]}")/../../lib/postgres.sh"
k() { kubectl --context "$HELM_POSTGRES_CONTEXT" -n "$NAMESPACE" --request-timeout=30s "$@"; }
h() { helm --kube-context "$HELM_POSTGRES_CONTEXT" -n "$NAMESPACE" "$@"; }

wait_until() {
    local timeout=$1 message=$2 deadline
    shift 2
    deadline=$((SECONDS + timeout))
    until "$@"; do
        if [ "$SECONDS" -ge "$deadline" ]; then echo "$message" >&2; return 1; fi
        sleep 2
    done
}
missing_class_rejected() {
    k create --dry-run=server -f "$tmpdir/webhook-probe.yaml" >"$tmpdir/webhook-probe.log" 2>&1 || true
    grep -q 'referenced PostgresClusterClass not found' "$tmpdir/webhook-probe.log"
}
last_revision() { h history "$release" -o json | jq -er '.[-1].revision'; }
pvc_uids() { k get pvc -l "cnpg.io/cluster=$cluster" -o json | jq -c '[.items[].metadata.uid]|sort'; }
instance_pods() { k get pods -l "cnpg.io/cluster=$cluster,cnpg.io/podRole=instance" -o json | jq '.items'; }
instances_ready() {
    instance_pods | jq -e --arg image "${1:-}" --arg primary "${2:-}" '
      length==3 and ([.[].spec.nodeName]|unique|length)==3 and
      ($primary=="" or any(.[]; .metadata.name==$primary)) and
      all(.[]; .metadata.deletionTimestamp==null and any(.status.conditions[]?; .type=="Ready" and .status=="True") and
        ($image=="" or any(.spec.containers[]; .name=="postgres" and .image==$image)))' >/dev/null
}
primary_changed() {
    local current
    current=$(k get cluster.postgresql.cnpg.io "$cluster" -o jsonpath='{.status.currentPrimary}')
    [ -n "$current" ] && [ "$current" != "$1" ]
}
failover_ready() {
    local current
    current=$(k get cluster.postgresql.cnpg.io "$cluster" -o jsonpath='{.status.currentPrimary}')
    [ -n "$current" ] && [ "$current" != "$1" ] && instances_ready "$2" "$current"
}

finish() {
    local result=$?
    trap - EXIT
    if [ -n "$forward_pid" ]; then kill "$forward_pid" 2>/dev/null || true; wait "$forward_pid" 2>/dev/null || true; fi
    rm -rf "$tmpdir"
    if [ "$result" -ne 0 ]; then
        echo "Acceptance failed; retained fixtures in $HELM_POSTGRES_CONTEXT namespace $NAMESPACE for inspection." >&2
    fi
    exit "$result"
}

install_and_enable() {
    local image=$1
    # Establish an active webhook before enabling shared provisioning on upgrade.
    h install "$release" "$chart" --values "$HELM_POSTGRES_VALUES" \
        --set splunkOperator.clusterWideAccess=false \
        --set splunkOperator.featureGates.PostgresController=true \
        --set splunkOperator.featureGates.ValidationWebhook=true \
        --set-string splunkOperator.image.repository="$KUTTL_SPLUNK_OPERATOR_IMAGE" \
        --set-string splunkOperator.splunkGeneralTerms="$SPLUNK_GENERAL_TERMS" \
        --set cloudnative-pg.enabled=true --set plugin-barman-cloud.enabled=true \
        --set cert-manager.enabled=false \
        --set sharedPostgres.enabled=false --set-string sharedPostgres.postgresVersion="$major" \
        --set-string "sharedPostgres.images.postgresql.$major=$image" \
        --set sharedPostgres.instances=3 --set sharedPostgres.affinity.podAntiAffinityType=required \
        --set 'sharedPostgres.databases[0].name=acceptance' --wait --timeout=15m
    h get values "$release" --all -o yaml > "$tmpdir/initial-values.yaml"
    h template "$release" "$chart" --values "$tmpdir/initial-values.yaml" \
        --set sharedPostgres.enabled=true --show-only templates/shared_postgres.yaml | \
        yq 'select(.kind == "PostgresCluster") | .spec.class = (.metadata.name + "-absent") | .metadata.name = "shared-webhook-probe"' > "$tmpdir/webhook-probe.yaml"
    if ! wait_until 120 'The validation webhook did not reject a missing class' missing_class_rejected; then
        cat "$tmpdir/webhook-probe.log" >&2; return 1
    fi
    h upgrade "$release" "$chart" --reuse-values --set sharedPostgres.enabled=true --wait --timeout=15m
}

assert_guarded_transitions() {
    local revision
    # Repeated rollbacks must preserve the retained class and unchanged workloads.
    revision=$(last_revision)
    if h upgrade "$release" "$chart" --reuse-values --set sharedPostgres.enabled=false >"$tmpdir/disable.log" 2>&1; then
        echo 'Disabling a retained shared cluster unexpectedly succeeded' >&2; return 1
    fi
    grep -q 'sharedPostgres.enabled cannot be disabled' "$tmpdir/disable.log"
    test "$revision" = "$(last_revision)"
    h upgrade "$release" "$chart" --reuse-values --wait --timeout=15m
    h rollback "$release" "$revision" --wait --timeout=15m
    h rollback "$release" "$((revision + 1))" --wait --timeout=15m
}

wait_ready() {
    local generation
    generation=$(k get postgrescluster "$1" -o jsonpath='{.metadata.generation}')
    k wait --for=jsonpath='{.status.observedGeneration}'="$generation" "postgrescluster/$1" --timeout=900s
    k wait --for=condition=Ready "postgrescluster/$1" "postgresdatabase/$2" --timeout=900s
}

sql() { run_sql "$@" "$client_image" acceptance shared-sql --context "$HELM_POSTGRES_CONTEXT" -n "$NAMESPACE" --request-timeout=30s; }
credential_hash() {
    local secret
    secret=$(k get postgresdatabase "$database" -o jsonpath='{.status.databases[0].adminUserSecretRef.name}')
    k get secret "$secret" -o json | jq -cS '.data' | shasum -a 256
}

scrape() {
    local pod=$1 port=$2 pattern=$3 forwarded deadline
    # Background kubectl directly: k() would leave a shell-wrapper PID in $!.
    kubectl --context "$HELM_POSTGRES_CONTEXT" -n "$NAMESPACE" --request-timeout=30s \
        port-forward --address=127.0.0.1 "pod/$pod" ":$port" >"$tmpdir/forward.log" 2>&1 &
    forward_pid=$!
    deadline=$((SECONDS + 30))
    while ! grep -q 'Forwarding from 127.0.0.1:' "$tmpdir/forward.log"; do
        kill -0 "$forward_pid"
        test "$SECONDS" -lt "$deadline"
        sleep 1
    done
    forwarded=$(sed -n 's/.*127\.0\.0\.1:\([0-9]*\).*/\1/p' "$tmpdir/forward.log" | head -1)
    curl --fail --silent --show-error --max-time 30 "http://127.0.0.1:$forwarded/metrics" | grep "$pattern" >/dev/null
    kill "$forward_pid"; wait "$forward_pid" 2>/dev/null || true; forward_pid=
}

cleanup_backup_secret() {
    local existing_secret secret
    existing_secret=$(h get values "$release" --all -o json | jq -r '.sharedPostgres.backup.credentials.existingSecret // ""')
    if [ -n "$existing_secret" ]; then return; fi
    secret=$(k get secret "$cluster-backup" --ignore-not-found -o json)
    if [ -z "$secret" ]; then return; fi
    if ! printf '%s' "$secret" | jq -e --arg release "$release" --arg namespace "$NAMESPACE" \
        '.metadata.labels["app.kubernetes.io/managed-by"] == "Helm" and
         .metadata.annotations["meta.helm.sh/release-name"] == $release and
         .metadata.annotations["meta.helm.sh/release-namespace"] == $namespace' >/dev/null; then
        echo 'Refusing to delete a backup Secret not owned by this Helm release' >&2; return 1
    fi
    k delete secret "$cluster-backup" --ignore-not-found --wait --timeout=120s
}
