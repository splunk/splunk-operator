#!/usr/bin/env bash
# Run only on the expressly authorized disposable context. Failed fixtures are retained.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
: "${NAMESPACE:?KUTTL namespace is required}"
: "${HELM_REPO_PATH:?Run make helm-package and supply the chart directory}"
: "${HELM_POSTGRES_CONTEXT:?Supply the authorized disposable Kubernetes context}"
: "${HELM_POSTGRES_NAMESPACE:?Supply the authorized disposable Kubernetes namespace}"
: "${HELM_POSTGRES_VALUES:?Supply a protected values file with enabled backups and credentials}"
: "${KUTTL_SPLUNK_OPERATOR_IMAGE:?Supply the matching operator image under test}"
: "${SPLUNK_GENERAL_TERMS:?Supply your accepted terms value from docs/README.md}"
: "${POSTGRES_RELEASE_MANIFEST:?Run make helm-shared-postgresql-test}"
: "${HELM_POSTGRES_UPGRADE_IMAGE:?Supply a distinct approved tag@sha256 image in the selected PostgreSQL major}"
test "$(kubectl config current-context)" = "$HELM_POSTGRES_CONTEXT"
test "$NAMESPACE" = "$HELM_POSTGRES_NAMESPACE"
test -f "$HELM_POSTGRES_VALUES"
release=shared-postgres
chart="$HELM_REPO_PATH/splunk-operator"
major=${HELM_POSTGRES_MAJOR:-17}
case "$major" in 17|18) ;; *) echo 'HELM_POSTGRES_MAJOR must be 17 or 18' >&2; exit 1;; esac
upgrade_tag=${HELM_POSTGRES_UPGRADE_IMAGE%@*}
upgrade_tag=${upgrade_tag##*:}
if [[ ! "$HELM_POSTGRES_UPGRADE_IMAGE" =~ ^[^[:space:]]+:[^@[:space:]]+@sha256:[0-9a-f]{64}$ ]] || [[ "$upgrade_tag" != "$major".* ]]; then
    echo 'The upgrade image must be digest-pinned and use a numeric tag in HELM_POSTGRES_MAJOR.' >&2; exit 1
fi
client_image=$(release_image postgresql-17 "$POSTGRES_RELEASE_MANIFEST")
initial_image=$(yq -r ".sharedPostgres.images.postgresql[\"$major\"] // \"\"" "$HELM_POSTGRES_VALUES")
if [ -z "$initial_image" ]; then
    initial_image=$(release_image "postgresql-$major" "$POSTGRES_RELEASE_MANIFEST")
fi
if [ "${initial_image##*@}" = "${HELM_POSTGRES_UPGRADE_IMAGE##*@}" ]; then
    echo 'The upgrade image must have a different digest from the initial image.' >&2; exit 1
fi
for crd in clusters.postgresql.cnpg.io objectstores.barmancloud.cnpg.io; do
    existing_crd=$(k get crd "$crd" --ignore-not-found -o name)
    if [ -n "$existing_crd" ]; then
        echo "Requires a fresh disposable cluster without $crd" >&2; exit 1
    fi
done
existing_release=$(h list -a -q --filter '^shared-postgres$')
test -z "$existing_release"
test "$(yq -r '.sharedPostgres.backup.enabled' "$HELM_POSTGRES_VALUES")" = true
forward_pid=
tmpdir=$(mktemp -d)
trap finish EXIT

install_and_enable "$initial_image"
assert_guarded_transitions
cluster=$(k get postgresclusters -l "app.kubernetes.io/instance=$release" -o json | jq -er 'if (.items|length)==1 then .items[0].metadata.name else error("expected one shared cluster") end')
database="$cluster-apps"
class=$(k get postgrescluster "$cluster" -o jsonpath='{.spec.class}')
wait_ready "$cluster" "$database"
instances_ready

write_marker="CREATE TABLE IF NOT EXISTS cpi2263_marker(value text PRIMARY KEY); INSERT INTO cpi2263_marker VALUES ('retained') ON CONFLICT DO NOTHING; SELECT 1 / count(*)::int FROM cpi2263_marker WHERE value='retained';"
read_marker="SELECT 1 / count(*)::int FROM cpi2263_marker WHERE value='retained';"
sql "$cluster" "$database" CLUSTER_RW_ENDPOINT "$write_marker"
sql "$cluster" "$database" CLUSTER_POOLER_RW_ENDPOINT "$read_marker"
before_credentials=$(credential_hash)
before_pvcs=$(pvc_uids)
before_uid=$(k get postgrescluster "$cluster" -o jsonpath='{.metadata.uid}')
h upgrade "$release" "$chart" --reuse-values \
    --set-string "sharedPostgres.images.postgresql.$major=$HELM_POSTGRES_UPGRADE_IMAGE" \
    --wait --timeout=15m
wait_ready "$cluster" "$database"
k wait --for=jsonpath='{.spec.imageName}'="$HELM_POSTGRES_UPGRADE_IMAGE" "cluster.postgresql.cnpg.io/$cluster" --timeout=300s
wait_until 900 'The same-major image rollout did not complete' instances_ready "$HELM_POSTGRES_UPGRADE_IMAGE"
test "$before_credentials" = "$(credential_hash)"
test "$before_pvcs" = "$(pvc_uids)"
sql "$cluster" "$database" CLUSTER_POOLER_RW_ENDPOINT "$read_marker"
other_major=18; if [ "$major" = 18 ]; then other_major=17; fi
if h upgrade "$release" "$chart" --reuse-values --set-string "sharedPostgres.postgresVersion=$other_major" >"$tmpdir/major-change.log" 2>&1; then
    echo 'An implicit PostgreSQL major change unexpectedly succeeded' >&2; exit 1
fi
grep -q 'major-version changes require' "$tmpdir/major-change.log"

# Exercise default retention with the backend controllers removed.
h uninstall "$release" --wait --timeout=300s
test "$before_uid" = "$(k get postgrescluster "$cluster" -o jsonpath='{.metadata.uid}')"
test "$before_credentials" = "$(credential_hash)"
test "$before_pvcs" = "$(pvc_uids)"
install_and_enable "$HELM_POSTGRES_UPGRADE_IMAGE"
wait_ready "$cluster" "$database"
test "$before_uid" = "$(k get postgrescluster "$cluster" -o jsonpath='{.metadata.uid}')"
test "$before_pvcs" = "$(pvc_uids)"
# Reinstall keeps the upgraded build as well as the original release identity.
test "$before_credentials" = "$(credential_hash)"
sql "$cluster" "$database" CLUSTER_POOLER_RW_ENDPOINT "$read_marker"

# Observe a different primary, then wait for complete HA recovery and recheck SQL.
primary=$(k get cluster.postgresql.cnpg.io "$cluster" -o jsonpath='{.status.currentPrimary}')
k delete pod "$primary" --wait --timeout=120s
wait_until 300 'Failover did not select a new primary' primary_changed "$primary"
wait_until 900 'Failover did not recover healthy instances' failover_ready "$primary" "$HELM_POSTGRES_UPGRADE_IMAGE"
wait_ready "$cluster" "$database"
sql "$cluster" "$database" CLUSTER_RW_ENDPOINT "INSERT INTO cpi2263_marker VALUES ('after-failover');"
read_marker="$read_marker SELECT 1 / count(*)::int FROM cpi2263_marker WHERE value='after-failover';"
sql "$cluster" "$database" CLUSTER_POOLER_RW_ENDPOINT "$read_marker"

# A real base backup and restored marker qualify the configured destination.
backup="$cluster-acceptance"
jq -n --arg name "$backup" --arg cluster "$cluster" \
    '{apiVersion:"postgresql.cnpg.io/v1",kind:"Backup",metadata:{name:$name},spec:{cluster:{name:$cluster},method:"plugin",pluginConfiguration:{name:"barman-cloud.cloudnative-pg.io"}}}' | k apply -f -
k wait --for=jsonpath='{.status.phase}'=completed "backup/$backup" --timeout=900s
replacement="$cluster-restore"
k get postgrescluster "$cluster" -o json | jq --arg name "$replacement" --arg source "$cluster" \
    '{apiVersion,kind,metadata:{name:$name},spec:(.spec + {clusterDeletionPolicy:"Retain",bootstrapFrom:{objectStorage:{serverName:$source}}})}' | k create -f -
restored_database="$replacement-apps"
k get postgresdatabase "$database" -o json | jq --arg name "$restored_database" --arg cluster "$replacement" \
    '{apiVersion,kind,metadata:{name:$name},spec:(.spec + {clusterRef:{name:$cluster}})}' | k create -f -
wait_ready "$replacement" "$restored_database"
# The same SQL consumer now uses the replacement database's published connection and credentials.
sql "$replacement" "$restored_database" CLUSTER_RW_ENDPOINT "$read_marker"
sql "$replacement" "$restored_database" CLUSTER_POOLER_RW_ENDPOINT "$read_marker"

primary=$(k get cluster.postgresql.cnpg.io "$cluster" -o jsonpath='{.status.currentPrimary}')
scrape "$primary" 9187 cnpg_
pooler=$(k get poolers.postgresql.cnpg.io -o json | jq -er --arg cluster "$cluster" '.items[]|select(.spec.cluster.name==$cluster and .spec.type=="rw")|.metadata.name')
pooler_pod=$(k get pods -l "cnpg.io/poolerName=$pooler" -o jsonpath='{.items[0].metadata.name}')
scrape "$pooler_pod" 9127 pgbouncer


# Deliberate destructive cleanup only after all acceptance assertions pass.
for db in "$restored_database" "$database"; do
    k patch postgresdatabase "$db" --type=json -p='[{"op":"replace","path":"/spec/databases/0/deletionPolicy","value":"Delete"}]'
    k delete postgresdatabase "$db" --wait --timeout=300s
done
for pg in "$replacement" "$cluster"; do
    k patch postgrescluster "$pg" --type=merge -p='{"spec":{"clusterDeletionPolicy":"Delete"}}'
    k delete postgrescluster "$pg" --wait --timeout=300s
    k wait --for=delete "cluster.postgresql.cnpg.io/$pg" --timeout=300s
done
k delete postgresclusterclass "$class"
k delete pod/shared-sql --ignore-not-found --wait --timeout=120s
cleanup_backup_secret
h uninstall "$release" --wait --timeout=300s
echo "Shared PostgreSQL $major lifecycle acceptance passed against the configured backup destination."
