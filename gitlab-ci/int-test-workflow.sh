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

log_step() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

copy_integration_junit() {
  if copy_if_exists "${CI_PROJECT_DIR}/inttest-junit.xml" "${integration_junit}" >/dev/null 2>&1; then
    return 0
  fi

  junit_report="$(find "${CI_PROJECT_DIR}" -maxdepth 1 -name 'report-junit-*.xml' -type f | sort | tail -n 1)"
  if [ -n "${junit_report}" ]; then
    copy_if_exists "${junit_report}" "${integration_junit}" >/dev/null 2>&1 || true
  fi
}

: > "${context_file}"
: > "${cleanup_log}"
: > "${cluster_log}"
mkdir -p "${pod_log_dir}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"

BUILD_IMAGE_REF_FILE="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-image-ref.txt}"
require_file "${BUILD_IMAGE_REF_FILE}" "build image reference"

IMAGE_REF="$(cat "${BUILD_IMAGE_REF_FILE}")"
IMAGE_REPOSITORY="${IMAGE_REF%:*}"
IMAGE_TAG="${IMAGE_REF##*:}"
ECR_REGISTRY="${IMAGE_REPOSITORY%%/*}"
OPERATOR_REPOSITORY_PATH="${IMAGE_REPOSITORY#${ECR_REGISTRY}/}"

ECR_REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-${STAGING_AWS_DEFAULT_REGION:-$(printf '%s' "${ECR_REGISTRY}" | cut -d. -f4)}}}"

if [ -z "${ECR_REGION}" ]; then
  echo "Unable to determine ECR region — set AWS_REGION or STAGING_AWS_DEFAULT_REGION" >&2
  exit 1
fi

# Enterprise image for the minimal develop lane — prefer an explicit staging
# override and otherwise reuse the existing repo .env image pin.
enterprise_image="${STAGING_SPLUNK_ENTERPRISE_IMAGE:-${SPLUNK_ENTERPRISE_RELEASE_IMAGE:-splunk/splunk:latest}}"
enterprise_image="$(strip_docker_io_prefix "${enterprise_image}")"

requested_profile="${STAGING_INT_TEST_PROFILE:-${JOB_INT_TEST_PROFILE:-smoke}}"
resolve_integration_profile "${requested_profile}"
test_focus="${RESOLVED_INT_TEST_FOCUS}"
safe_test_focus="$(sanitize_slug "${test_focus}")"

cluster_name_prefix="${STAGING_EKS_CLUSTER_NAME_PREFIX:-${JOB_EKS_CLUSTER_NAME_PREFIX:-eks-int-test-cluster}}"
cluster_nodes="${STAGING_INT_CLUSTER_NODES:-${JOB_INT_CLUSTER_NODES:-${RESOLVED_INT_CLUSTER_NODES_DEFAULT}}}"
cluster_workers="${STAGING_INT_CLUSTER_WORKERS:-${JOB_INT_CLUSTER_WORKERS:-${RESOLVED_INT_CLUSTER_WORKERS_DEFAULT}}}"

use_existing_cluster="false"
if bool_is_true "${STAGING_INT_USE_EXISTING_CLUSTER:-${JOB_USE_EXISTING_CLUSTER:-false}}"; then
  use_existing_cluster="true"
fi
existing_cluster_name="${STAGING_INT_EXISTING_CLUSTER_NAME:-${JOB_EXISTING_CLUSTER_NAME:-}}"

export AWS_DEFAULT_REGION="${ECR_REGION}"
export S3_REGION="${ECR_REGION}"
export ECR_REGISTRY
export ECR_REPOSITORY="${ECR_REGISTRY}"
export PRIVATE_REGISTRY="${ECR_REGISTRY}"
export SPLUNK_OPERATOR_IMAGE="${OPERATOR_REPOSITORY_PATH}:${IMAGE_TAG}"
export SPLUNK_ENTERPRISE_IMAGE="${enterprise_image}"
normalize_testenv_commit_hash "${CI_COMMIT_SHORT_SHA:-${CI_COMMIT_SHA}}" 8
export COMMIT_HASH="${NORMALIZED_TESTENV_COMMIT_HASH}"
export TEST_FOCUS="${test_focus}"
export TEST_TO_SKIP="${STAGING_INT_TEST_TO_SKIP:-${JOB_INT_TEST_TO_SKIP:-${RESOLVED_INT_TEST_TO_SKIP_DEFAULT}}}"
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
append_context "${context_file}" "ecr_registry" "${ECR_REGISTRY}"
append_context "${context_file}" "ecr_region" "${ECR_REGION}"
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
append_context "${context_file}" "normalized_commit_hash" "${COMMIT_HASH}"
append_context "${context_file}" "kubectl_version" "${KUBECTL_VERSION}"
append_context "${context_file}" "eksctl_version" "${EKSCTL_VERSION}"

cleanup_and_exit() {
  rc="$1"
  cleanup_rc=0

  trap - EXIT INT TERM
  set +e

  log_step "cleanup:start" | tee -a "${cleanup_log}" >/dev/null

  copy_integration_junit

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
make setup/kubectl KUBECTL_VERSION="${KUBECTL_VERSION}" CI_BIN_DIR="${ci_bin_dir}"
make setup/eksctl EKSCTL_VERSION="${EKSCTL_VERSION}" CI_BIN_DIR="${ci_bin_dir}"

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

copy_integration_junit
