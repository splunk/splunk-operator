#!/usr/bin/env bash
# Helm acceptance on an authorized disposable cluster.
set -euo pipefail
: "${NAMESPACE:?KUTTL namespace is required}"
: "${HELM_REPO_PATH:?Run make helm-package first}"
: "${KUTTL_SPLUNK_OPERATOR_IMAGE:?Supply the SOK image under test}"
: "${SPLUNK_GENERAL_TERMS:?Supply your accepted terms value; see docs/README.md}"
: "${POSTGRES_RELEASE_MANIFEST:?Run make helm-kuttl-test with this suite config}"
manifest=$POSTGRES_RELEASE_MANIFEST
image() { yq -r ".artifacts[] | select(.id == \"$1\") | .public.reference" "$manifest"; }
cnpg_image=$(image cloudnative-pg)
barman_image=$(image plugin-barman-cloud)
pg17_image=$(image postgresql-17)
pg18_image=$(image postgresql-18)
pooler_image=$(image pgbouncer)
chart="$HELM_REPO_PATH/splunk-operator"
class_name="helm-backends-$NAMESPACE"

install_sok() {
    local namespace=$1 release=$2
    shift 2
    helm install "$release" "$chart" -n "$namespace" --wait --timeout 10m \
        --set splunkOperator.clusterWideAccess=false \
        --set splunkOperator.featureGates.PostgresController=true \
        --set-string splunkOperator.image.repository="$KUTTL_SPLUNK_OPERATOR_IMAGE" \
        --set-string splunkOperator.splunkGeneralTerms="$SPLUNK_GENERAL_TERMS" "$@"
}

assert_controller_image() {
    local deployment=$1 expected=$2
    test "$(kubectl get deploy "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].image}')" = "$expected"
}

assert_controller_count() {
    test "$(kubectl get deploy -n "$1" -l "app.kubernetes.io/name=$2" -o name | wc -l | tr -d ' ')" = "$3"
}

# Require fresh backend CRDs.
for crd in clusters.postgresql.cnpg.io objectstores.barmancloud.cnpg.io; do
    if kubectl get crd "$crd" >/dev/null 2>&1; then
        echo "Fresh-cluster prerequisite failed: $crd already exists" >&2; exit 1
    fi
done
existing_release=$(helm list -a -q -n "$NAMESPACE" --filter '^sok-backends$')
if [ -n "$existing_release" ]; then
    echo "sok-backends already exists in $NAMESPACE" >&2; exit 1
fi
external_namespace="${NAMESPACE:0:54}-external"
backend_started=false
external_created=false
class_created=false

delete_fixtures() {
    local namespace=$1 failed=0
    kubectl delete pod/helm-sql postgresdatabase/helmdb -n "$namespace" --ignore-not-found --wait --timeout=120s || failed=1
    kubectl delete postgrescluster/helmdb -n "$namespace" --ignore-not-found --wait --timeout=300s || failed=1
    kubectl wait --for=delete cluster.postgresql.cnpg.io/helmdb -n "$namespace" --timeout=300s || failed=1
    return "$failed"
}

uninstall_sok() {
    helm uninstall "$2" -n "$1" --wait --timeout 300s
}

cleanup() {
    local result=$? failed=0
    trap - EXIT INT TERM
    set +e
    if "$external_created"; then
        delete_fixtures "$external_namespace" || failed=1
        uninstall_sok "$external_namespace" sok-external || failed=1
        kubectl delete namespace "$external_namespace" --wait --timeout=300s || failed=1
    fi
    if "$backend_started"; then
        delete_fixtures "$NAMESPACE" || failed=1
    fi
    if "$class_created"; then
        kubectl delete postgresclusterclass "$class_name" --ignore-not-found || failed=1
    fi
    if "$backend_started"; then
        uninstall_sok "$NAMESPACE" sok-backends || failed=1
    fi
    # Keep backend CRDs; KUTTL owns the original namespace. Reuse needs a new cluster.
    if [ "$result" -eq 0 ]; then result=$failed; fi
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

backend_started=true
install_sok "$NAMESPACE" sok-backends --set cloudnative-pg.enabled=true --set plugin-barman-cloud.enabled=true
for name in sok-backends-cloudnative-pg sok-backends-plugin-barman-cloud splunk-operator-controller-manager; do
    kubectl rollout status "deployment/$name" -n "$NAMESPACE" --timeout=600s
done
assert_controller_image sok-backends-cloudnative-pg "$cnpg_image"
assert_controller_image sok-backends-plugin-barman-cloud "$barman_image"
kubectl wait --for=condition=Established crd/clusters.postgresql.cnpg.io crd/objectstores.barmancloud.cnpg.io --timeout=60s
kubectl wait --for=condition=Ready certificate/barman-cloud-client certificate/barman-cloud-server -n "$NAMESPACE" --timeout=180s
cat <<EOF | kubectl create -f -
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: $class_name
spec:
  provisioner: postgresql.cnpg.io
  config:
    instances: 1
    storage: 1Gi
    postgresVersion: "17"
    connectionPooler:
      enabled: true
  cnpg:
    primaryUpdateMethod: restart
EOF
class_created=true
exercise_sql() {
    local namespace=$1 pg_image=$2
    local config cluster_config secret ca_ref ca_name ca_key
    config=$(kubectl get postgresdatabase helmdb -n "$namespace" -o jsonpath='{.status.databases[0].configMapRef.name}')
    secret=$(kubectl get postgresdatabase helmdb -n "$namespace" -o jsonpath='{.status.databases[0].adminUserSecretRef.name}')
    cluster_config=$(kubectl get postgrescluster helmdb -n "$namespace" -o jsonpath='{.status.resources.configMapRef.name}')
    ca_ref=$(kubectl get configmap "$cluster_config" -n "$namespace" -o jsonpath='{.data.SERVER_CA_SECRET_REF}')
    ca_name=${ca_ref%/*}; ca_key=${ca_ref#*/}
    # Check direct/pooled TLS using Secret references.
    for endpoint in CLUSTER_RW_ENDPOINT CLUSTER_POOLER_RW_ENDPOINT; do
        cat <<EOF | kubectl apply -n "$namespace" -f -
apiVersion: v1
kind: Pod
metadata:
  name: helm-sql
spec:
  restartPolicy: Never
  containers:
    - name: psql
      image: $pg_image
      command: [psql]
      args: ["-v", "ON_ERROR_STOP=1", "-c", "CREATE TABLE IF NOT EXISTS helmcheck (id int); INSERT INTO helmcheck VALUES (1); SELECT 1 / count(*)::int FROM helmcheck WHERE id=1;"]
      env:
        - name: PGHOST
          valueFrom: {configMapKeyRef: {name: $config, key: $endpoint}}
        - name: PGPORT
          value: "5432"
        - name: PGDATABASE
          value: appdb
        - name: PGUSER
          valueFrom: {secretKeyRef: {name: $secret, key: username}}
        - name: PGPASSWORD
          valueFrom: {secretKeyRef: {name: $secret, key: password}}
        - {name: PGSSLMODE, value: verify-full}
        - {name: PGSSLROOTCERT, value: /ca/ca.crt}
      volumeMounts:
        - {name: ca, mountPath: /ca, readOnly: true}
  volumes:
    - name: ca
      secret:
        secretName: $ca_name
        items: [{key: $ca_key, path: ca.crt}]
EOF
        kubectl wait pod/helm-sql -n "$namespace" --for=jsonpath='{.status.phase}'=Succeeded --timeout=180s
        kubectl logs helm-sql -n "$namespace"
        kubectl delete pod helm-sql -n "$namespace" --wait
    done
}

exercise_database() {
    local namespace=$1 major=$2 pg_image=$3
    cat <<EOF | kubectl apply -n "$namespace" -f -
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: helmdb
spec:
  class: $class_name
  clusterDeletionPolicy: Delete
  postgresVersion: "$major"
  images:
    postgresql:
      "$major": $pg_image
    pgbouncer: $pooler_image
---
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: helmdb
spec:
  clusterRef:
    name: helmdb
  databases:
    - name: appdb
      deletionPolicy: Delete
EOF
    kubectl wait -n "$namespace" --for=condition=Ready postgrescluster/helmdb postgresdatabase/helmdb --timeout=900s
    exercise_sql "$namespace" "$pg_image"
    delete_fixtures "$namespace"
}

exercise_database "$NAMESPACE" 17 "$pg17_image"
exercise_database "$NAMESPACE" 18 "$pg18_image"
# Isolate fixed-name chart resources while using the first release's backends.
kubectl create namespace "$external_namespace"
external_created=true
install_sok "$external_namespace" sok-external --set splunkOperator.nameOverride=external-sok
for controller in cloudnative-pg plugin-barman-cloud; do
    assert_controller_count "$NAMESPACE" "$controller" 1
    assert_controller_count "$external_namespace" "$controller" 0
done
exercise_database "$external_namespace" 17 "$pg17_image"
