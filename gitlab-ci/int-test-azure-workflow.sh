#!/bin/sh
set -eu

# Runtime contract
# - Purpose: execute Azure qualification or release validation against either
#   the staged branch image or the latest released SOK image.
# - Inputs: Azure auth and storage inputs, the runtime operator source, and the
#   repo-owned enterprise image pin or a lane override.
# - Outputs: runtime context, build log, cluster log, run log, pod logs, and
#   JUnit output under ci-output/.
# - Guardrails: private-registry staging only, ephemeral AKS by default, and
#   cleanup on success, failure, or signal.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/cloud-pipeline-common.sh"

if [ -z "${GITLAB_OIDC_TOKEN:-}" ] && [ -n "${AZURE_GITLAB_OIDC_TOKEN:-}" ]; then
  export GITLAB_OIDC_TOKEN="${AZURE_GITLAB_OIDC_TOKEN}"
fi

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
cleanup_log="ci-output/${WORKFLOW_SLUG}-cleanup.log"
cluster_log="ci-output/${WORKFLOW_SLUG}-cluster.log"
build_log="ci-output/${WORKFLOW_SLUG}-build.log"
run_log="ci-output/${WORKFLOW_SLUG}-run.log"
image_ref_file="ci-output/${WORKFLOW_SLUG}-ecr-image-ref.txt"
digest_file="ci-output/${WORKFLOW_SLUG}-digest.txt"
pod_log_dir="ci-output/${WORKFLOW_SLUG}-pod-logs"
integration_junit="ci-output/${WORKFLOW_SLUG}-inttest-junit.xml"
azure_creds_file="$(mktemp /tmp/${WORKFLOW_SLUG}-azure-creds.XXXXXX.json)"
aks_kubeconfig_file="$(mktemp /tmp/${WORKFLOW_SLUG}-kubeconfig.XXXXXX)"
cluster_mode="ephemeral-aks"
cluster_created="false"
build_image_ref_file="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-ecr-image-ref.txt}"
build_image_digest_file="${BUILD_IMAGE_DIGEST_FILE:-ci-output/build-test-push-workflow-digest.txt}"
released_sok_contract_file="${RELEASED_SOK_CONTRACT_FILE:-}"

cleanup_and_exit() {
  rc="$1"
  cleanup_rc=0

  trap - EXIT INT TERM
  set +e

  log_step "cleanup:start" | tee -a "${cleanup_log}" >/dev/null
  capture_test_logs "${CI_PROJECT_DIR}/test" "${pod_log_dir}"
  copy_integration_junit

  log_step "cleanup:make-cleanup" | tee -a "${cleanup_log}" >/dev/null
  make cleanup >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  if [ "${cluster_mode}" = "ephemeral-aks" ] && [ "${cluster_created}" = "true" ]; then
    log_step "cleanup:cluster-down" | tee -a "${cleanup_log}" >/dev/null
    make cluster-down >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  else
    log_step "cleanup:cluster-down skipped mode=${cluster_mode} created=${cluster_created}" | tee -a "${cleanup_log}" >/dev/null
  fi
  log_step "cleanup:complete cleanup_rc=${cleanup_rc}" | tee -a "${cleanup_log}" >/dev/null

  rm -f "${azure_creds_file}" "${aks_kubeconfig_file}"

  if [ "${rc}" -ne 0 ]; then
    exit "${rc}"
  fi
  if [ "${cleanup_rc}" -ne 0 ]; then
    exit "${cleanup_rc}"
  fi
  exit 0
}

trap 'cleanup_and_exit $?' EXIT INT TERM

prepare_runtime_artifacts "${context_file}" "${cleanup_log}" "${cluster_log}" "${build_log}" "${run_log}" "${pod_log_dir}"
load_repo_dotenv "${CI_PROJECT_DIR}/.env"
ensure_jq
ensure_azure_cli
ensure_pipeline_aws_env

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"
make setup/kubectl KUBECTL_VERSION="$(first_nonempty "${PIPELINE_KUBECTL_VERSION:-}" "${KUBECTL_VERSION:-}" "")" CI_BIN_DIR="${ci_bin_dir}"
require_commands bash aws az docker make kubectl go jq base64

require_envs \
  PIPELINE_AZURE_ACR_LOGIN_SERVER \
  PIPELINE_AZURE_STORAGE_ACCOUNT \
  PIPELINE_AZURE_STORAGE_ACCOUNT_KEY \
  PIPELINE_AZURE_TEST_CONTAINER \
  PIPELINE_AZURE_INDEXES_CONTAINER

azure_client_id=""
azure_client_secret=""
azure_tenant_id=""
azure_subscription_id=""
azure_has_service_principal="false"
azure_auth_mode="acr-basic"

if [ -n "${PIPELINE_AZURE_CREDENTIALS:-}" ]; then
  materialize_json_secret "${PIPELINE_AZURE_CREDENTIALS}" "${azure_creds_file}"
  azure_client_id="$(jq -r '.clientId // empty' "${azure_creds_file}")"
  azure_client_secret="$(jq -r '.clientSecret // empty' "${azure_creds_file}")"
  azure_tenant_id="$(jq -r '.tenantId // empty' "${azure_creds_file}")"
  azure_subscription_id="$(jq -r '.subscriptionId // empty' "${azure_creds_file}")"
  if [ -z "${azure_client_id}" ] || [ -z "${azure_client_secret}" ] || [ -z "${azure_tenant_id}" ]; then
    echo "Azure credentials payload is missing clientId/clientSecret/tenantId" >&2
    exit 1
  fi
  azure_has_service_principal="true"
fi

if azure_oidc_ready; then
  azure_auth_mode="oidc"
elif [ "${azure_has_service_principal}" = "true" ]; then
  azure_auth_mode="service-principal"
fi

if [ -n "${PIPELINE_AKS_KUBECONFIG:-}" ]; then
  cluster_mode="existing-aks"
  materialize_file_secret "${PIPELINE_AKS_KUBECONFIG}" "${aks_kubeconfig_file}"
  export KUBECONFIG="${aks_kubeconfig_file}"
else
  require_envs PIPELINE_AZURE_RESOURCE_GROUP_NAME
  if [ "${azure_auth_mode}" = "acr-basic" ]; then
    echo "Ephemeral AKS mode requires GitLab OIDC variables or PIPELINE_AZURE_CREDENTIALS" >&2
    exit 1
  fi
fi

resolve_operator_runtime_source "${build_image_ref_file}" "${released_sok_contract_file}" "splunk/splunk-operator"
if [ "${RUNTIME_OPERATOR_SOURCE_KIND}" = "official-release" ]; then
  source_operator_image="${RUNTIME_OPERATOR_SOURCE_IMAGE}"
else
  source_operator_image="${RUNTIME_OPERATOR_FULL_IMAGE_REF}"
fi
source_operator_image_tag="${source_operator_image##*:}"

azure_login_service_principal() {
  az login --service-principal \
    --username "${azure_client_id}" \
    --password "${azure_client_secret}" \
    --tenant "${azure_tenant_id}" >/dev/null
  if [ -n "${azure_subscription_id}" ]; then
    az account set --subscription "${azure_subscription_id}" >/dev/null
  fi
}

azure_auth_with_oidc() {
  auth_rc=0
  set +e
  azure_login_oidc >> "${run_log}" 2>&1
  auth_rc=$?
  if [ "${auth_rc}" -eq 0 ]; then
    az acr login --name "${AZURE_CONTAINER_REGISTRY}" >> "${run_log}" 2>&1
    auth_rc=$?
  fi
  set -e
  return "${auth_rc}"
}

azure_auth_with_service_principal() {
  auth_rc=0
  set +e
  azure_login_service_principal >> "${run_log}" 2>&1
  auth_rc=$?
  if [ "${auth_rc}" -eq 0 ]; then
    az acr login --name "${AZURE_CONTAINER_REGISTRY}" >> "${run_log}" 2>&1
    auth_rc=$?
  fi
  set -e
  return "${auth_rc}"
}

azure_registry_login_with_docker() {
  require_envs PIPELINE_AZURE_ACR_DOCKER_USERNAME PIPELINE_AZURE_ACR_DOCKER_PASSWORD
  printf '%s' "${PIPELINE_AZURE_ACR_DOCKER_PASSWORD}" | docker login "${operator_registry}" -u "${PIPELINE_AZURE_ACR_DOCKER_USERNAME}" --password-stdin >> "${run_log}" 2>&1
}

operator_registry="${PIPELINE_AZURE_ACR_LOGIN_SERVER}"
operator_image="${operator_registry}/splunk/splunk-operator:${source_operator_image_tag}"
resolve_runtime_enterprise_image
enterprise_source_image="${RESOLVED_ENTERPRISE_IMAGE}"
cluster_name="az${CI_JOB_ID}"
test_labels="$(first_nonempty "${PIPELINE_AZURE_TEST_LABELS:-}" "${JOB_CLOUD_TEST_LABELS:-}" "tier:e2e-full && cloud:azure")"
test_timeout="$(first_nonempty "${PIPELINE_AZURE_TEST_TIMEOUT:-}" "${JOB_CLOUD_TEST_TIMEOUT:-}" "7h")"

export CLUSTER_PROVIDER="azure"
export TEST_CLUSTER_PLATFORM="azure"
export TEST_CLUSTER_NAME="${cluster_name}"
export CLUSTER_NAME="${cluster_name}"
export CLUSTER_WORKERS="$(first_nonempty "${PIPELINE_AZURE_CLUSTER_WORKERS:-}" "${JOB_CLOUD_CLUSTER_WORKERS:-}" "5")"
export CLUSTER_NODES="$(first_nonempty "${PIPELINE_AZURE_CLUSTER_NODES:-}" "${JOB_CLOUD_CLUSTER_NODES:-}" "2")"
export CLUSTER_WIDE="$(first_nonempty "${PIPELINE_AZURE_CLUSTER_WIDE:-}" "${JOB_CLOUD_CLUSTER_WIDE:-}" "true")"
export DEPLOYMENT_TYPE="$(first_nonempty "${PIPELINE_AZURE_DEPLOYMENT_TYPE:-}" "${JOB_CLOUD_DEPLOYMENT_TYPE:-}" "manifest")"
export AZURE_CONTAINER_REGISTRY="$(first_nonempty "${PIPELINE_AZURE_CONTAINER_REGISTRY:-}" "$(printf '%s' "${PIPELINE_AZURE_ACR_LOGIN_SERVER}" | cut -d. -f1)")"
export AZURE_CONTAINER_REGISTRY_LOGIN_SERVER="${PIPELINE_AZURE_ACR_LOGIN_SERVER}"
export AZURE_RESOURCE_GROUP="${PIPELINE_AZURE_RESOURCE_GROUP_NAME:-existing-cluster}"
export AZURE_STORAGE_ACCOUNT="${PIPELINE_AZURE_STORAGE_ACCOUNT}"
export AZURE_STORAGE_ACCOUNT_KEY="${PIPELINE_AZURE_STORAGE_ACCOUNT_KEY}"
export AZURE_TEST_CONTAINER="${PIPELINE_AZURE_TEST_CONTAINER}"
export AZURE_INDEXES_CONTAINER="${PIPELINE_AZURE_INDEXES_CONTAINER}"
export AZURE_REGION="$(first_nonempty "${PIPELINE_AZURE_REGION:-}" "westus")"
export AZURE_MANAGED_ID_ENABLED="$(first_nonempty "${PIPELINE_AZURE_MANAGED_ID_ENABLED:-}" "false")"
export AZURE_ENTERPRISE_LICENSE_PATH="$(first_nonempty "${PIPELINE_AZURE_ENTERPRISE_LICENSE_PATH:-}" "test_licenses")"
export PRIVATE_REGISTRY="${PIPELINE_AZURE_ACR_LOGIN_SERVER}"
export PRIVATE_REGISTRY_SERVER="${PIPELINE_AZURE_ACR_LOGIN_SERVER}"
export PRIVATE_REGISTRY_USERNAME="${PIPELINE_AZURE_ACR_DOCKER_USERNAME:-}"
export PRIVATE_REGISTRY_PASSWORD="${PIPELINE_AZURE_ACR_DOCKER_PASSWORD:-}"
export PRIVATE_REGISTRY_SECRET_NAME="$(first_nonempty "${PIPELINE_AZURE_PULL_SECRET_NAME:-}" "private-registry-credentials")"
export PRIVATE_REGISTRY_AUTH_MODE="$(first_nonempty "${PIPELINE_AZURE_REGISTRY_PULL_MODE:-}" "node")"
export SPLUNK_OPERATOR_IMAGE="${operator_image}"
export SPLUNK_ENTERPRISE_IMAGE="${enterprise_source_image}"
export TEST_LABELS="${test_labels}"
export TEST_TIMEOUT="${test_timeout}"
export COMMIT_HASH="${CI_COMMIT_SHORT_SHA:-${CI_COMMIT_SHA}}"
export TEST_CONTAINER="${PIPELINE_AZURE_TEST_CONTAINER}"
export INDEXES_CONTAINER="${PIPELINE_AZURE_INDEXES_CONTAINER}"
export REGION="${AZURE_REGION}"
export STORAGE_ACCOUNT="${AZURE_STORAGE_ACCOUNT}"
export STORAGE_ACCOUNT_KEY="${AZURE_STORAGE_ACCOUNT_KEY}"
export ENTERPRISE_LICENSE_LOCATION="$(first_nonempty "${PIPELINE_AZURE_ENTERPRISE_LICENSE_LOCATION:-}" "test_licenses")"

if [ "${cluster_mode}" = "ephemeral-aks" ]; then
  cluster_config_summary="$(validate_and_print_cloud_cluster_config \
    "azure" \
    "${TEST_CLUSTER_NAME}" \
    "${CLUSTER_WORKERS}" \
    "${SPLUNK_OPERATOR_IMAGE}" \
    "${TEST_LABELS}" \
    "${TEST_TIMEOUT}")"
  printf '%s\n' "${cluster_config_summary}" | tee -a "${cluster_log}"
fi

append_operator_runtime_context "${context_file}"
append_context "${context_file}" "cluster_mode" "${cluster_mode}"
append_context "${context_file}" "cluster_provider" "${CLUSTER_PROVIDER}"
append_context "${context_file}" "test_cluster_name" "${TEST_CLUSTER_NAME}"
append_context "${context_file}" "cluster_workers" "${CLUSTER_WORKERS}"
append_context "${context_file}" "cluster_nodes" "${CLUSTER_NODES}"
append_context "${context_file}" "cluster_wide" "${CLUSTER_WIDE}"
append_context "${context_file}" "deployment_type" "${DEPLOYMENT_TYPE}"
append_context "${context_file}" "operator_image" "${operator_image}"
append_context "${context_file}" "source_operator_image" "${source_operator_image}"
append_context "${context_file}" "enterprise_source_image" "${enterprise_source_image}"
append_context "${context_file}" "azure_resource_group" "${AZURE_RESOURCE_GROUP}"
append_context "${context_file}" "azure_container_registry" "${AZURE_CONTAINER_REGISTRY}"
append_context "${context_file}" "azure_region" "${AZURE_REGION}"
append_context "${context_file}" "azure_auth_mode_requested" "${azure_auth_mode}"
append_context "${context_file}" "azure_registry_pull_mode_requested" "${PRIVATE_REGISTRY_AUTH_MODE}"
append_context "${context_file}" "test_labels" "${TEST_LABELS}"
append_context "${context_file}" "test_timeout" "${TEST_TIMEOUT}"

# creds-helper selects DOCKER_CONFIG. Configure source auth before Azure auth
# so the ACR login is written to the same Docker config.
login_enterprise_source_registry_if_needed "${enterprise_source_image}" >> "${run_log}" 2>&1

if [ "${azure_auth_mode}" = "oidc" ]; then
  log_step "azure:auth:start mode=oidc" | tee -a "${run_log}" >/dev/null
  if azure_auth_with_oidc; then
    :
  elif [ "${cluster_mode}" = "ephemeral-aks" ] && [ "${azure_has_service_principal}" = "true" ]; then
    log_step "azure:auth:oidc-fallback service-principal" | tee -a "${run_log}" >/dev/null
    azure_auth_mode="service-principal"
    azure_auth_with_service_principal
  else
    exit 1
  fi
  log_step "azure:auth:complete" | tee -a "${run_log}" >/dev/null
elif [ "${cluster_mode}" = "ephemeral-aks" ]; then
  log_step "azure:auth:start mode=service-principal" | tee -a "${run_log}" >/dev/null
  azure_auth_with_service_principal
  log_step "azure:auth:complete" | tee -a "${run_log}" >/dev/null
else
  log_step "azure:auth:skipped mode=${cluster_mode}" | tee -a "${run_log}" >/dev/null
  log_step "azure:registry-login:start ${operator_registry}" | tee -a "${run_log}" >/dev/null
  azure_registry_login_with_docker
  log_step "azure:registry-login:complete" | tee -a "${run_log}" >/dev/null
fi
append_context "${context_file}" "azure_auth_mode_effective" "${azure_auth_mode}"

log_step "azure:operator-image:promote:start source=${source_operator_image} target=${operator_image}" | tee -a "${build_log}" >/dev/null
login_source_registry_for_image "${source_operator_image}" >> "${build_log}" 2>&1
promote_image_to_private_registry "${source_operator_image}" "${operator_image}" >> "${build_log}" 2>&1
printf '%s\n' "${operator_image}" > "${image_ref_file}"
copy_if_exists "${build_image_digest_file}" "${digest_file}" >/dev/null 2>&1 || true
log_step "azure:operator-image:promote:complete" | tee -a "${build_log}" >/dev/null

log_step "azure:registry-enterprise-image:start" | tee -a "${run_log}" >/dev/null
PRIVATE_SPLUNK_ENTERPRISE_IMAGE="$(bash "${CI_PROJECT_DIR}/test/get-private-registry-enterprise.sh")"
export SPLUNK_ENTERPRISE_IMAGE="${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
append_context "${context_file}" "private_splunk_enterprise_image" "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
log_step "azure:registry-enterprise-image:complete ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}" | tee -a "${run_log}" >/dev/null

if [ "${cluster_mode}" = "ephemeral-aks" ]; then
  log_step "azure:cluster-up:start ${TEST_CLUSTER_NAME}" | tee -a "${cluster_log}" >/dev/null
  make cluster-up 2>&1 | tee -a "${cluster_log}"
  cluster_created="true"
  log_step "azure:cluster-up:complete" | tee -a "${cluster_log}" >/dev/null
else
  log_step "azure:cluster-up:skipped mode=${cluster_mode}" | tee -a "${cluster_log}" >/dev/null
fi
kubectl get nodes -o wide 2>&1 | tee -a "${cluster_log}"
kubectl get pods -A 2>&1 | tee -a "${cluster_log}"

append_context "${context_file}" "azure_registry_pull_mode_effective" "${PRIVATE_REGISTRY_AUTH_MODE}"
log_step "azure:registry-pull-mode ${PRIVATE_REGISTRY_AUTH_MODE}" | tee -a "${run_log}" >/dev/null

log_step "azure:deploy-operator:start" | tee -a "${run_log}"
run_and_tee "${run_log}" bash "${CI_PROJECT_DIR}/test/deploy-operator.sh" "${operator_image}" "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
log_step "azure:deploy-operator:complete" | tee -a "${run_log}"

log_step "azure:trigger-tests:start labels=${TEST_LABELS}" | tee -a "${run_log}"
run_and_tee "${run_log}" bash "${CI_PROJECT_DIR}/test/trigger-tests.sh" "${operator_image}" "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
log_step "azure:trigger-tests:complete" | tee -a "${run_log}"

capture_test_logs "${CI_PROJECT_DIR}/test" "${pod_log_dir}"
copy_integration_junit
log_step "azure:workflow:complete" | tee -a "${run_log}" >/dev/null
