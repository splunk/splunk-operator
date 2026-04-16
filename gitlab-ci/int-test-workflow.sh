#!/bin/sh
set -eu

# Runtime contract
# - Purpose: execute the primary staging EKS integration path against the operator image built in GitLab.
# - Inputs: build artifact image ref, staging AWS/EKS/S3 variables, focus selector, and repo .env defaults.
# - Outputs: runtime context, cluster logs, cleanup log, copied pod logs, and JUnit output under ci-output/.
# - Guardrails: ephemeral cluster naming, staging-only registries and buckets, cleanup on success, failure, or signal.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
cleanup_log="ci-output/${WORKFLOW_SLUG}-cleanup.log"
cluster_log="ci-output/${WORKFLOW_SLUG}-cluster.log"
pod_log_dir="ci-output/${WORKFLOW_SLUG}-pod-logs"
integration_junit="ci-output/${WORKFLOW_SLUG}-inttest-junit.xml"
integration_skip_regex='^(?:[^i]+|i(?:$|[^n]|n(?:$|[^t]|t(?:$|[^e]|e(?:$|[^g]|g(?:$|[^r]|r(?:$|[^a]|a(?:$|[^t]|t(?:$|[^i]|i(?:$|[^o]|o(?:$|[^n])))))))))))*$'

log_step() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

: > "${context_file}"
: > "${cleanup_log}"
: > "${cluster_log}"
mkdir -p "${pod_log_dir}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
load_optional_release_controller_env "${CI_PROJECT_DIR}/ci-output/release-controller/release-cycle.env"
resolve_enterprise_source_image

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"

BUILD_IMAGE_REF_FILE="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-image-ref.txt}"
require_file "${BUILD_IMAGE_REF_FILE}" "build image reference"

aws_oidc_token_file="$(mktemp /tmp/${WORKFLOW_SLUG}-aws-oidc.XXXXXX.jwt)"
aws_auth_mode="static-key"
if aws_oidc_ready; then
  aws_auth_mode="oidc"
else
  export AWS_ACCESS_KEY_ID="${STAGING_AWS_ACCESS_KEY_ID}"
  export AWS_SECRET_ACCESS_KEY="${STAGING_AWS_SECRET_ACCESS_KEY}"
fi

IMAGE_REF="$(cat "${BUILD_IMAGE_REF_FILE}")"
IMAGE_REPOSITORY="${IMAGE_REF%:*}"
IMAGE_TAG="${IMAGE_REF##*:}"
ECR_REGISTRY="${IMAGE_REPOSITORY%%/*}"
OPERATOR_REPOSITORY_PATH="${IMAGE_REPOSITORY#${ECR_REGISTRY}/}"

resolve_ecr_region "${STAGING_AWS_DEFAULT_REGION:-}" "${ECR_REGISTRY}"
if [ -z "${RESOLVED_ECR_REGION}" ]; then
  echo "Unable to determine ECR region for integration test runtime" >&2
  exit 1
fi

enterprise_image="${RESOLVED_SPLUNK_ENTERPRISE_IMAGE_NO_DOCKER_IO}"
requested_profile="${STAGING_INT_TEST_PROFILE:-${JOB_INT_TEST_PROFILE:-managersecret}}"
resolve_integration_profile "${requested_profile}"
test_focus="${RESOLVED_INT_TEST_FOCUS}"
safe_test_focus="$(sanitize_slug "${test_focus}")"
cluster_name_prefix="${JOB_EKS_CLUSTER_NAME_PREFIX:-${STAGING_EKS_CLUSTER_NAME_PREFIX:-eks-integration-test-cluster}}"
cluster_nodes="${STAGING_INT_CLUSTER_NODES:-${JOB_INT_CLUSTER_NODES:-${RESOLVED_INT_CLUSTER_NODES_DEFAULT}}}"
cluster_workers="${STAGING_INT_CLUSTER_WORKERS:-${JOB_INT_CLUSTER_WORKERS:-${RESOLVED_INT_CLUSTER_WORKERS_DEFAULT}}}"
use_existing_cluster="false"
if bool_is_true "${STAGING_INT_USE_EXISTING_CLUSTER:-${JOB_USE_EXISTING_CLUSTER:-false}}"; then
  use_existing_cluster="true"
fi
existing_cluster_name="${STAGING_INT_EXISTING_CLUSTER_NAME:-${JOB_EXISTING_CLUSTER_NAME:-}}"

export AWS_DEFAULT_REGION="${RESOLVED_ECR_REGION}"
export AWS_REGION="${RESOLVED_ECR_REGION}"
export S3_REGION="${RESOLVED_ECR_REGION}"
export ECR_REGISTRY="${ECR_REGISTRY}"
export ECR_REPOSITORY="${ECR_REGISTRY}"
export PRIVATE_REGISTRY="${ECR_REGISTRY}"
export SPLUNK_OPERATOR_IMAGE="${OPERATOR_REPOSITORY_PATH}:${IMAGE_TAG}"
export SPLUNK_ENTERPRISE_IMAGE="${enterprise_image}"
normalize_testenv_commit_hash "${CI_COMMIT_SHORT_SHA:-${CI_COMMIT_SHA}}" 8
export COMMIT_HASH="${NORMALIZED_TESTENV_COMMIT_HASH}"
export TEST_FOCUS="${test_focus}"
export TEST_TO_SKIP="${STAGING_INT_TEST_TO_SKIP:-${JOB_INT_TEST_TO_SKIP:-${RESOLVED_INT_TEST_TO_SKIP_DEFAULT:-${integration_skip_regex}}}}"
export TEST_CLUSTER_PLATFORM="eks"
if [ "${use_existing_cluster}" = "true" ]; then
  if [ -z "${existing_cluster_name}" ]; then
    echo "Existing-cluster mode requires STAGING_INT_EXISTING_CLUSTER_NAME or JOB_EXISTING_CLUSTER_NAME" >&2
    exit 1
  fi
  export TEST_CLUSTER_NAME="${existing_cluster_name}"
else
  export TEST_CLUSTER_NAME="${cluster_name_prefix}-${safe_test_focus}-${CI_JOB_ID}"
fi
export CLUSTER_WIDE="${STAGING_INT_CLUSTER_WIDE:-${JOB_INT_CLUSTER_WIDE:-true}}"
export DEPLOYMENT_TYPE="${STAGING_INT_DEPLOYMENT_TYPE:-${JOB_INT_DEPLOYMENT_TYPE:-}}"
export CLUSTER_NODES="${cluster_nodes}"
export CLUSTER_WORKERS="${cluster_workers}"
export TEST_TIMEOUT="${STAGING_INT_TEST_TIMEOUT:-${JOB_INT_TEST_TIMEOUT:-7h}}"
export EKS_VPC_PUBLIC_SUBNET_STRING="${STAGING_EKS_VPC_PUBLIC_SUBNET_STRING}"
export EKS_VPC_PRIVATE_SUBNET_STRING="${STAGING_EKS_VPC_PRIVATE_SUBNET_STRING}"
export TEST_BUCKET="${STAGING_TEST_BUCKET}"
export TEST_INDEXES_S3_BUCKET="${STAGING_TEST_INDEXES_S3_BUCKET}"
export EKSCTL_VERSION="${STAGING_EKSCTL_VERSION:-${EKSCTL_VERSION}}"
export KUBECTL_VERSION="${STAGING_KUBECTL_VERSION:-${KUBECTL_VERSION}}"
export EKS_CLUSTER_K8_VERSION="${STAGING_EKS_CLUSTER_K8_VERSION:-${EKS_CLUSTER_K8_VERSION}}"

append_context "${context_file}" "input_artifact" "${BUILD_IMAGE_REF_FILE}"
append_context "${context_file}" "ecr_registry_present" "true"
append_context "${context_file}" "ecr_region_source" "${RESOLVED_ECR_REGION_SOURCE}"
append_context "${context_file}" "test_profile" "${RESOLVED_INT_TEST_PROFILE}"
append_context "${context_file}" "test_focus" "${TEST_FOCUS}"
append_context "${context_file}" "test_to_skip" "${TEST_TO_SKIP}"
append_context "${context_file}" "cluster_name" "${TEST_CLUSTER_NAME}"
append_context "${context_file}" "existing_cluster" "${use_existing_cluster}"
append_context "${context_file}" "cluster_workers" "${CLUSTER_WORKERS}"
append_context "${context_file}" "cluster_nodes" "${CLUSTER_NODES}"
append_context "${context_file}" "cluster_wide" "${CLUSTER_WIDE}"
append_context "${context_file}" "test_timeout" "${TEST_TIMEOUT}"
append_context "${context_file}" "operator_image" "${SPLUNK_OPERATOR_IMAGE}"
append_context "${context_file}" "enterprise_image" "${SPLUNK_ENTERPRISE_IMAGE}"
append_context "${context_file}" "source_mode" "${RESOLVED_SOK_SOURCE_MODE}"
append_context "${context_file}" "trigger_kind" "${RESOLVED_SOK_TRIGGER_KIND}"
append_context "${context_file}" "enterprise_image_source" "${RESOLVED_SPLUNK_ENTERPRISE_IMAGE_SOURCE}"
append_context "${context_file}" "aws_auth_mode" "${aws_auth_mode}"
append_context "${context_file}" "normalized_commit_hash" "${COMMIT_HASH}"
append_context "${context_file}" "kubectl_version" "${KUBECTL_VERSION}"
append_context "${context_file}" "eksctl_version" "${EKSCTL_VERSION}"
append_context "${context_file}" "ecr_registry" "${ECR_REGISTRY}"
append_context "${context_file}" "job_timeout" "${CI_JOB_TIMEOUT:-unknown}"

cleanup_and_exit() {
  rc="$1"
  cleanup_rc=0

  trap - EXIT INT TERM
  set +e

  log_step "cleanup:start" | tee -a "${cleanup_log}" >/dev/null

  copy_if_exists "${CI_PROJECT_DIR}/inttest-junit.xml" "${integration_junit}" >/dev/null 2>&1 || true

  log_step "cleanup:collect-test-logs" | tee -a "${cleanup_log}" >/dev/null
  find "${CI_PROJECT_DIR}/test" -name "*.log" -type f -exec cp {} "${pod_log_dir}/" \; >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  log_step "cleanup:make-cleanup" | tee -a "${cleanup_log}" >/dev/null
  make cleanup >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  log_step "cleanup:make-clean" | tee -a "${cleanup_log}" >/dev/null
  make clean >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  if [ "${use_existing_cluster}" = "true" ]; then
    log_step "cleanup:cluster-down:skipped-existing-cluster" | tee -a "${cleanup_log}" >/dev/null
  else
    log_step "cleanup:cluster-down" | tee -a "${cleanup_log}" >/dev/null
    make cluster-down >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  fi
  log_step "cleanup:complete cleanup_rc=${cleanup_rc}" | tee -a "${cleanup_log}" >/dev/null

  rm -f "${aws_oidc_token_file}"

  if [ "${rc}" -ne 0 ]; then
    exit "${rc}"
  fi

  if [ "${cleanup_rc}" -ne 0 ]; then
    exit "${cleanup_rc}"
  fi

  exit 0
}

trap 'cleanup_and_exit $?' EXIT INT TERM

log_step "tools:install kubectl=${KUBECTL_VERSION} eksctl=${EKSCTL_VERSION}"
install_kubectl_version "${KUBECTL_VERSION}" "${ci_bin_dir}"
install_eksctl_version "${EKSCTL_VERSION}" "${ci_bin_dir}"

log_step "build-helpers:setup-ginkgo:start"
make setup/ginkgo
log_step "build-helpers:setup-ginkgo:complete"

log_step "build-helpers:kustomize:start"
make kustomize
log_step "build-helpers:kustomize:complete"

log_step "versions:start"
kubectl version --client=true
eksctl version
docker version
aws --version
log_step "versions:complete"

log_step "registry:ecr-login ${ECR_REGISTRY}"
if [ "${aws_auth_mode}" = "oidc" ]; then
  aws_prepare_oidc_env "${aws_oidc_token_file}"
fi
aws ecr get-login-password --region "${AWS_DEFAULT_REGION}" | docker login --username AWS --password-stdin "${ECR_REGISTRY}"
log_step "registry:ecr-login:complete"

if [ "${use_existing_cluster}" = "true" ]; then
  log_step "cluster:use-existing ${TEST_CLUSTER_NAME}"
  aws eks update-kubeconfig --name "${TEST_CLUSTER_NAME}" --region "${AWS_DEFAULT_REGION}" 2>&1 | tee -a "${cluster_log}"
  log_step "cluster:use-existing:complete"
else
  log_step "cluster:up ${TEST_CLUSTER_NAME}"
  make cluster-up 2>&1 | tee -a "${cluster_log}"
  log_step "cluster:up:complete"
fi
log_step "cluster:snapshot:nodes"
kubectl get nodes -o wide 2>&1 | tee -a "${cluster_log}"
log_step "cluster:snapshot:pods"
kubectl get pods -A 2>&1 | tee -a "${cluster_log}"

if [ "${use_existing_cluster}" = "true" ]; then
  log_step "cluster:addons:skipped-existing-cluster"
else
  log_step "cluster:addons:metrics-server"
  kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml 2>&1 | tee -a "${cluster_log}"
  log_step "cluster:addons:metrics-server:complete"

  log_step "cluster:addons:dashboard"
  kubectl apply -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.0.5/aio/deploy/recommended.yaml 2>&1 | tee -a "${cluster_log}"
  log_step "cluster:addons:dashboard:complete"
fi

log_step "tests:int-test:start focus=${TEST_FOCUS}"
make int-test
log_step "tests:int-test:complete"

copy_if_exists "${CI_PROJECT_DIR}/inttest-junit.xml" "${integration_junit}" >/dev/null 2>&1 || true
