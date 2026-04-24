#!/bin/sh
set -eu

# Runtime contract
# - Purpose: execute GCP qualification or release validation against either the
#   staged branch image or the latest released SOK image.
# - Inputs: GCP auth and storage inputs, the runtime operator source, and the
#   repo-owned enterprise image pin or a lane override.
# - Outputs: runtime context, build log, cluster log, run log, pod logs, and
#   JUnit output under ci-output/.
# - Guardrails: private-registry staging only, ephemeral GKE by default, and
#   cleanup on success, failure, or signal.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/cloud-pipeline-common.sh"

if [ -z "${GITLAB_OIDC_TOKEN:-}" ] && [ -n "${GCP_GITLAB_OIDC_TOKEN:-}" ]; then
  export GITLAB_OIDC_TOKEN="${GCP_GITLAB_OIDC_TOKEN}"
fi

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
cleanup_log="ci-output/${WORKFLOW_SLUG}-cleanup.log"
cluster_log="ci-output/${WORKFLOW_SLUG}-cluster.log"
build_log="ci-output/${WORKFLOW_SLUG}-build.log"
run_log="ci-output/${WORKFLOW_SLUG}-run.log"
image_ref_file="ci-output/${WORKFLOW_SLUG}-image-ref.txt"
digest_file="ci-output/${WORKFLOW_SLUG}-digest.txt"
pod_log_root="ci-output/${WORKFLOW_SLUG}-pod-logs"
integration_junit="ci-output/${WORKFLOW_SLUG}-inttest-junit.xml"
gcp_key_file="$(mktemp /tmp/${WORKFLOW_SLUG}-gcp-key.XXXXXX.json)"
gcp_oidc_token_file="$(mktemp /tmp/${WORKFLOW_SLUG}-gcp-token.XXXXXX.jwt)"
gcp_oidc_cred_file="$(mktemp /tmp/${WORKFLOW_SLUG}-gcp-cred.XXXXXX.json)"
gke_kubeconfig_file="$(mktemp /tmp/${WORKFLOW_SLUG}-kubeconfig.XXXXXX)"
cluster_mode="ephemeral-gke"
cluster_created="false"
build_image_ref_file="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-image-ref.txt}"
build_image_digest_file="${BUILD_IMAGE_DIGEST_FILE:-ci-output/build-test-push-workflow-digest.txt}"
released_sok_contract_file="${RELEASED_SOK_CONTRACT_FILE:-}"
gcp_service_account_key_prepared="false"

cleanup_and_exit() {
  rc="$1"
  cleanup_rc=0

  trap - EXIT INT TERM
  set +e

  log_step "cleanup:start" | tee -a "${cleanup_log}" >/dev/null
  capture_test_logs "${CI_PROJECT_DIR}/test" "${pod_log_root}"
  capture_junit_artifact "${CI_PROJECT_DIR}/inttest-junit.xml" "${integration_junit}"

  log_step "cleanup:make-cleanup" | tee -a "${cleanup_log}" >/dev/null
  make cleanup >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  if [ "${cluster_mode}" = "ephemeral-gke" ] && [ "${cluster_created}" = "true" ]; then
    log_step "cleanup:cluster-down" | tee -a "${cleanup_log}" >/dev/null
    make cluster-down >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  else
    log_step "cleanup:cluster-down skipped mode=${cluster_mode} created=${cluster_created}" | tee -a "${cleanup_log}" >/dev/null
  fi
  log_step "cleanup:complete cleanup_rc=${cleanup_rc}" | tee -a "${cleanup_log}" >/dev/null

  rm -f "${gcp_key_file}" "${gcp_oidc_token_file}" "${gcp_oidc_cred_file}" "${gke_kubeconfig_file}"

  if [ "${rc}" -ne 0 ]; then
    exit "${rc}"
  fi
  if [ "${cleanup_rc}" -ne 0 ]; then
    exit "${cleanup_rc}"
  fi
  exit 0
}

trap 'cleanup_and_exit $?' EXIT INT TERM

prepare_runtime_artifacts "${context_file}" "${cleanup_log}" "${cluster_log}" "${build_log}" "${run_log}" "${pod_log_root}"
load_repo_dotenv "${CI_PROJECT_DIR}/.env"
ensure_jq
ensure_gcloud_cli
ensure_pipeline_aws_env

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"
make setup/kubectl KUBECTL_VERSION="$(first_nonempty "${PIPELINE_KUBECTL_VERSION:-}" "${KUBECTL_VERSION:-}" "")" CI_BIN_DIR="${ci_bin_dir}"
require_commands bash aws gcloud docker make kubectl go jq base64

require_envs \
  PIPELINE_GCP_ARTIFACT_REGISTRY \
  PIPELINE_GCP_PROJECT_ID

prepare_gcp_service_account_key() {
  if [ "${gcp_service_account_key_prepared}" = "true" ]; then
    return 0
  fi

  require_envs PIPELINE_GCP_SERVICE_ACCOUNT_KEY
  materialize_json_secret "${PIPELINE_GCP_SERVICE_ACCOUNT_KEY}" "${gcp_key_file}"
  export GCP_SERVICE_ACCOUNT_KEY="$(base64 < "${gcp_key_file}" | tr -d '\n')"
  gcp_service_account_key_prepared="true"
}

gcp_auth_mode="service-account-key"
if gcp_oidc_ready; then
  gcp_auth_mode="oidc"
fi

if [ -n "${PIPELINE_GKE_KUBECONFIG:-}" ]; then
  cluster_mode="existing-gke"
  materialize_file_secret "${PIPELINE_GKE_KUBECONFIG}" "${gke_kubeconfig_file}"
  export KUBECONFIG="${gke_kubeconfig_file}"
else
  require_envs PIPELINE_GCP_REGION PIPELINE_GCP_ZONE
fi

resolve_operator_runtime_source "${build_image_ref_file}" "${released_sok_contract_file}" "splunk/splunk-operator"
if [ "${RUNTIME_OPERATOR_SOURCE_KIND}" = "official-release" ]; then
  source_operator_image="${RUNTIME_OPERATOR_SOURCE_IMAGE}"
else
  source_operator_image="${RUNTIME_OPERATOR_FULL_IMAGE_REF}"
fi
source_operator_image_tag="${source_operator_image##*:}"

operator_registry="${PIPELINE_GCP_ARTIFACT_REGISTRY}"
operator_image="${operator_registry}/splunk/splunk-operator:${source_operator_image_tag}"
resolve_runtime_enterprise_image
enterprise_source_image="${RESOLVED_ENTERPRISE_IMAGE}"
cluster_name="gke-${CI_JOB_ID}"
test_focus="$(first_nonempty "${PIPELINE_GCP_TEST_FOCUS:-}" "${JOB_CLOUD_TEST_FOCUS:-}" "s1_gcp_sanity")"
test_to_skip="$(first_nonempty "${PIPELINE_GCP_TEST_TO_SKIP:-}" "${JOB_CLOUD_TEST_TO_SKIP:-}" '^(?:[^i]+|i(?:$|[^n]|n(?:$|[^t]|t(?:$|[^e]|e(?:$|[^g]|g(?:$|[^r]|r(?:$|[^a]|a(?:$|[^t]|t(?:$|[^i]|i(?:$|[^o]|o(?:$|[^n])))))))))))*$')"
test_timeout="$(first_nonempty "${PIPELINE_GCP_TEST_TIMEOUT:-}" "${JOB_CLOUD_TEST_TIMEOUT:-}" "7h")"
gcp_registry_host="$(printf '%s' "${PIPELINE_GCP_ARTIFACT_REGISTRY}" | cut -d/ -f1)"

export CLUSTER_PROVIDER="gcp"
export TEST_CLUSTER_PLATFORM="gcp"
export TEST_CLUSTER_NAME="${cluster_name}"
export CLUSTER_NAME="${cluster_name}"
export CLUSTER_WORKERS="$(first_nonempty "${PIPELINE_GCP_CLUSTER_WORKERS:-}" "${JOB_CLOUD_CLUSTER_WORKERS:-}" "5")"
export CLUSTER_NODES="$(first_nonempty "${PIPELINE_GCP_CLUSTER_NODES:-}" "${JOB_CLOUD_CLUSTER_NODES:-}" "2")"
export CLUSTER_WIDE="$(first_nonempty "${PIPELINE_GCP_CLUSTER_WIDE:-}" "${JOB_CLOUD_CLUSTER_WIDE:-}" "true")"
export DEPLOYMENT_TYPE="$(first_nonempty "${PIPELINE_GCP_DEPLOYMENT_TYPE:-}" "${JOB_CLOUD_DEPLOYMENT_TYPE:-}" "manifest")"
export GCP_PROJECT_ID="${PIPELINE_GCP_PROJECT_ID}"
export GCP_REGION="$(first_nonempty "${PIPELINE_GCP_REGION:-}" "us-west2")"
export GCP_ZONE="$(first_nonempty "${PIPELINE_GCP_ZONE:-}" "us-west2-a")"
export AWS_S3_REGION="${GCP_REGION}"
export S3_REGION="${GCP_REGION}"
export GCP_NETWORK="$(first_nonempty "${PIPELINE_GCP_NETWORK:-}" "default")"
export GCP_SUBNETWORK="$(first_nonempty "${PIPELINE_GCP_SUBNETWORK:-}" "default")"
export GCP_ARTIFACT_REGISTRY="${PIPELINE_GCP_ARTIFACT_REGISTRY}"
export GCP_CONTAINER_REGISTRY_LOGIN_SERVER="${PIPELINE_GCP_ARTIFACT_REGISTRY}"
export PRIVATE_REGISTRY="${PIPELINE_GCP_ARTIFACT_REGISTRY}"
export PRIVATE_REGISTRY_AUTH_MODE="$(first_nonempty "${PIPELINE_GCP_REGISTRY_PULL_MODE:-}" "node")"
export GCP_NODE_SERVICE_ACCOUNT_EMAIL="${PIPELINE_GKE_NODE_SERVICE_ACCOUNT_EMAIL:-}"
export SPLUNK_OPERATOR_IMAGE="${operator_image}"
export SPLUNK_ENTERPRISE_IMAGE="${enterprise_source_image}"
export TEST_FOCUS="${test_focus}"
export TEST_TO_SKIP="${test_to_skip}"
export TEST_TIMEOUT="${test_timeout}"
export TEST_BUCKET="$(first_nonempty "${PIPELINE_TEST_BUCKET:-}" "${PIPELINE_GCP_TEST_CONTAINER:-}" "")"
export TEST_S3_BUCKET="${TEST_BUCKET}"
export TEST_INDEXES_S3_BUCKET="$(first_nonempty "${PIPELINE_TEST_INDEXES_S3_BUCKET:-}" "${PIPELINE_GCP_INDEXES_CONTAINER:-}" "")"
export INDEXES_S3_BUCKET="${TEST_INDEXES_S3_BUCKET}"
export GCP_STORAGE_ACCOUNT="${PIPELINE_GCP_STORAGE_ACCOUNT:-}"
export GCP_STORAGE_ACCOUNT_KEY="${PIPELINE_GCP_STORAGE_ACCOUNT_KEY:-}"
export GCP_TEST_CONTAINER="$(first_nonempty "${PIPELINE_GCP_TEST_CONTAINER:-}" "${TEST_BUCKET}")"
export GCP_INDEXES_CONTAINER="$(first_nonempty "${PIPELINE_GCP_INDEXES_CONTAINER:-}" "${TEST_INDEXES_S3_BUCKET}")"
export GCP_SERVICE_ACCOUNT_ENABLED="$(first_nonempty "${PIPELINE_GCP_SERVICE_ACCOUNT_ENABLED:-}" "false")"
export GCP_ENTERPRISE_LICENSE_LOCATION="$(first_nonempty "${PIPELINE_GCP_ENTERPRISE_LICENSE_LOCATION:-}" "test_licenses")"
export ENTERPRISE_LICENSE_LOCATION="${GCP_ENTERPRISE_LICENSE_LOCATION}"
export COMMIT_HASH="${CI_COMMIT_SHORT_SHA:-${CI_COMMIT_SHA}}"

require_nonempty "${TEST_BUCKET}" "PIPELINE_TEST_BUCKET or PIPELINE_GCP_TEST_CONTAINER for GCP validation"
require_nonempty "${TEST_INDEXES_S3_BUCKET}" "PIPELINE_TEST_INDEXES_S3_BUCKET or PIPELINE_GCP_INDEXES_CONTAINER for GCP validation"

gcp_login_service_account_key() {
  prepare_gcp_service_account_key
  gcloud auth activate-service-account --key-file="${gcp_key_file}" >/dev/null
}

configure_gcp_application_default_credentials() {
  case "${gcp_auth_mode}" in
    oidc)
      export GOOGLE_APPLICATION_CREDENTIALS="${gcp_oidc_cred_file}"
      export CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE="${gcp_oidc_cred_file}"
      ;;
    *)
      prepare_gcp_service_account_key
      export GOOGLE_APPLICATION_CREDENTIALS="${gcp_key_file}"
      unset CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE
      ;;
  esac
  export GOOGLE_CLOUD_PROJECT="${GCP_PROJECT_ID}"
}

gcp_auth_with_oidc() {
  auth_rc=0
  set +e
  gcp_login_oidc "${gcp_oidc_token_file}" "${gcp_oidc_cred_file}" >> "${run_log}" 2>&1
  auth_rc=$?
  if [ "${auth_rc}" -eq 0 ]; then
    gcloud config set project "${GCP_PROJECT_ID}" >> "${run_log}" 2>&1
    auth_rc=$?
  fi
  if [ "${auth_rc}" -eq 0 ]; then
    gcloud auth configure-docker "${gcp_registry_host}" --quiet >> "${run_log}" 2>&1
    auth_rc=$?
  fi
  set -e
  return "${auth_rc}"
}

gcp_auth_with_service_account_key() {
  auth_rc=0
  set +e
  gcp_login_service_account_key >> "${run_log}" 2>&1
  auth_rc=$?
  if [ "${auth_rc}" -eq 0 ]; then
    gcloud config set project "${GCP_PROJECT_ID}" >> "${run_log}" 2>&1
    auth_rc=$?
  fi
  if [ "${auth_rc}" -eq 0 ]; then
    gcloud auth configure-docker "${gcp_registry_host}" --quiet >> "${run_log}" 2>&1
    auth_rc=$?
  fi
  set -e
  return "${auth_rc}"
}

log_step "gcp:auth:start" | tee -a "${run_log}" >/dev/null
if [ "${gcp_auth_mode}" = "oidc" ]; then
  if gcp_auth_with_oidc; then
    :
  else
    if ! env_present PIPELINE_GCP_SERVICE_ACCOUNT_KEY; then
      echo "GCP OIDC auth failed and PIPELINE_GCP_SERVICE_ACCOUNT_KEY is not set for fallback" >&2
      exit 1
    fi
    log_step "gcp:auth:oidc-fallback service-account-key" | tee -a "${run_log}" >/dev/null
    gcp_auth_mode="service-account-key"
    gcp_auth_with_service_account_key
  fi
else
  gcp_auth_with_service_account_key
fi
configure_gcp_application_default_credentials
append_context "${context_file}" "google_application_credentials" "${GOOGLE_APPLICATION_CREDENTIALS}"
append_context "${context_file}" "gcp_auth_mode_effective" "${gcp_auth_mode}"
log_step "gcp:auth:complete" | tee -a "${run_log}" >/dev/null

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
append_context "${context_file}" "gcp_project_id" "${GCP_PROJECT_ID}"
append_context "${context_file}" "gcp_region" "${GCP_REGION}"
append_context "${context_file}" "gcp_zone" "${GCP_ZONE}"
append_context "${context_file}" "gcp_registry_pull_mode_effective" "${PRIVATE_REGISTRY_AUTH_MODE}"
append_context "${context_file}" "gcp_node_service_account_email" "${GCP_NODE_SERVICE_ACCOUNT_EMAIL:-}"
append_context "${context_file}" "test_focus" "${TEST_FOCUS}"
append_context "${context_file}" "test_to_skip" "${TEST_TO_SKIP}"
append_context "${context_file}" "test_timeout" "${TEST_TIMEOUT}"

log_step "gcp:operator-image:promote:start source=${source_operator_image} target=${operator_image}" | tee -a "${build_log}" >/dev/null
login_source_registry_for_image "${source_operator_image}" >> "${build_log}" 2>&1
promote_image_to_private_registry "${source_operator_image}" "${operator_image}" >> "${build_log}" 2>&1
printf '%s\n' "${operator_image}" > "${image_ref_file}"
copy_if_exists "${build_image_digest_file}" "${digest_file}" >/dev/null 2>&1 || true
log_step "gcp:operator-image:promote:complete" | tee -a "${build_log}" >/dev/null

log_step "gcp:registry-enterprise-image:start" | tee -a "${run_log}" >/dev/null
PRIVATE_SPLUNK_ENTERPRISE_IMAGE="$(bash "${CI_PROJECT_DIR}/test/get-private-registry-enterprise.sh" | tail -n 1)"
export SPLUNK_ENTERPRISE_IMAGE="${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
append_context "${context_file}" "private_splunk_enterprise_image" "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
log_step "gcp:registry-enterprise-image:complete ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}" | tee -a "${run_log}" >/dev/null

if [ "${cluster_mode}" = "ephemeral-gke" ]; then
  log_step "gcp:cluster-up:start ${TEST_CLUSTER_NAME}" | tee -a "${cluster_log}" >/dev/null
  make cluster-up 2>&1 | tee -a "${cluster_log}"
  cluster_created="true"
  log_step "gcp:cluster-up:complete" | tee -a "${cluster_log}" >/dev/null
else
  log_step "gcp:cluster-up:skipped mode=${cluster_mode}" | tee -a "${cluster_log}" >/dev/null
fi
kubectl get nodes -o wide 2>&1 | tee -a "${cluster_log}"
kubectl get pods -A 2>&1 | tee -a "${cluster_log}"

if [ -f "${CI_PROJECT_DIR}/test/gcp-storageclass.yaml" ]; then
  log_step "gcp:storageclass:apply" | tee -a "${cluster_log}" >/dev/null
  kubectl apply -f "${CI_PROJECT_DIR}/test/gcp-storageclass.yaml" 2>&1 | tee -a "${cluster_log}"
fi

log_step "gcp:deploy-operator:start" | tee -a "${run_log}" >/dev/null
bash "${CI_PROJECT_DIR}/test/deploy-operator.sh" "${operator_image}" "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}" >> "${run_log}" 2>&1
log_step "gcp:deploy-operator:complete" | tee -a "${run_log}" >/dev/null

log_step "gcp:trigger-tests:start focus=${TEST_FOCUS}" | tee -a "${run_log}" >/dev/null
bash "${CI_PROJECT_DIR}/test/trigger-tests.sh" "${operator_image}" "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}" >> "${run_log}" 2>&1
log_step "gcp:trigger-tests:complete" | tee -a "${run_log}" >/dev/null

capture_test_logs "${CI_PROJECT_DIR}/test" "${pod_log_root}"
capture_junit_artifact "${CI_PROJECT_DIR}/inttest-junit.xml" "${integration_junit}"
log_step "gcp:workflow:complete" | tee -a "${run_log}" >/dev/null
