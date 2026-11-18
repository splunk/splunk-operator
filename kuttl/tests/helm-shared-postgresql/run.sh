#!/usr/bin/env bash
set -euo pipefail
: "${KUTTL_CONFIG:?KUTTL_CONFIG is required}"

# Entry point: docs/develop/IntegrationTesting.md#shared-postgresql-helm-lifecycle.
: "${HELM_POSTGRES_CONTEXT:?Supply the authorized disposable context}"
: "${HELM_POSTGRES_NAMESPACE:?Supply the authorized disposable namespace}"
if [ "$(kubectl config current-context)" != "$HELM_POSTGRES_CONTEXT" ]; then
    echo 'The current Kubernetes context does not match HELM_POSTGRES_CONTEXT; KUTTL was not started.' >&2
    exit 1
fi
private_dir=$(mktemp -d)
private_config="$private_dir/config"
cleanup() {
    local exit_code=$?
    trap - EXIT
    if [ -f "$private_config" ]; then unlink "$private_config"; fi
    rmdir "$private_dir"
    exit "$exit_code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
umask 077
kubectl config view --raw --minify --flatten --context "$HELM_POSTGRES_CONTEXT" > "$private_config"
kubectl --kubeconfig "$private_config" config use-context "$HELM_POSTGRES_CONTEXT" >/dev/null
export KUBECONFIG="$private_config"
test "$(kubectl config current-context)" = "$HELM_POSTGRES_CONTEXT"
if ! kubectl wait --for=condition=Established \
    crd/postgresclusterclasses.platform.splunk.com \
    crd/postgresclusters.platform.splunk.com \
    crd/postgresdatabases.platform.splunk.com \
    crd/certificates.cert-manager.io crd/issuers.cert-manager.io --timeout=60s; then
    echo 'Install the matching SOK CRDs and cert-manager before running the shared PostgreSQL suite.' >&2
    exit 1
fi
existing_namespace=$(kubectl get namespace "$HELM_POSTGRES_NAMESPACE" --request-timeout=30s --ignore-not-found -o name)
if [ -z "$existing_namespace" ]; then
    kubectl create namespace "$HELM_POSTGRES_NAMESPACE" --request-timeout=30s
fi
kubectl kuttl test --config "$KUTTL_CONFIG" --namespace "$HELM_POSTGRES_NAMESPACE" --report xml
