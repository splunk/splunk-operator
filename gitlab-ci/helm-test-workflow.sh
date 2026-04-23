#!/bin/sh
set -eu

# Runtime contract
# - Purpose: run KUTTL-backed Helm validation on an ephemeral nonprod EKS cluster against either the staged branch image/chart or the latest official released SOK charts.
# - Inputs: build artifact or released-SOK contract, pipeline AWS/EKS/S3 variables, Helm/KUTTL selectors, and repo .env defaults.
# - Outputs: runtime context, cluster logs, cleanup log, KUTTL artifacts, and JUnit XML under ci-output/.
# - Guardrails: nonprod ECR only, repo-owned Make targets for setup/package/test, cleanup on success, failure, or signal.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
cleanup_log="ci-output/${WORKFLOW_SLUG}-cleanup.log"
cluster_log="ci-output/${WORKFLOW_SLUG}-cluster.log"
kuttl_log="ci-output/${WORKFLOW_SLUG}-kuttl.log"
kuttl_artifacts_dir="ci-output/${WORKFLOW_SLUG}-kuttl-artifacts"
helm_junit="ci-output/${WORKFLOW_SLUG}-kuttl-junit.xml"

log_step() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

copy_kuttl_junit() {
  if copy_if_exists "${CI_PROJECT_DIR}/kuttl-report.xml" "${helm_junit}" >/dev/null 2>&1; then
    return 0
  fi

  if copy_if_exists "${CI_PROJECT_DIR}/TEST-kuttl-report.xml" "${helm_junit}" >/dev/null 2>&1; then
    return 0
  fi

  first_xml="$(find "${CI_PROJECT_DIR}/kuttl-artifacts" -maxdepth 2 -type f -name '*.xml' 2>/dev/null | head -n 1 || true)"
  if [ -n "${first_xml}" ]; then
    copy_if_exists "${first_xml}" "${helm_junit}" >/dev/null 2>&1 || true
  fi
}

mkdir -p "ci-output" "${kuttl_artifacts_dir}"
: > "${context_file}"
: > "${cleanup_log}"
: > "${cluster_log}"
: > "${kuttl_log}"

load_repo_dotenv "${CI_PROJECT_DIR}/.env"
ensure_pipeline_aws_env

ci_bin_dir="${CI_PROJECT_DIR}/bin"
ensure_ci_bin_path "${ci_bin_dir}"

BUILD_IMAGE_REF_FILE="${BUILD_IMAGE_REF_FILE:-ci-output/build-test-push-workflow-image-ref.txt}"
RELEASED_SOK_CONTRACT_FILE="${RELEASED_SOK_CONTRACT_FILE:-}"
resolve_operator_runtime_source "${BUILD_IMAGE_REF_FILE}" "${RELEASED_SOK_CONTRACT_FILE}" "splunk/splunk-operator"
ECR_REGISTRY="${RUNTIME_ECR_REGISTRY}"
ECR_REGION="${RUNTIME_ECR_REGION}"
if [ "${RUNTIME_OPERATOR_SOURCE_KIND}" = "official-release" ]; then
  RELEASED_HELM_REPO_URL="$(first_nonempty "${SOK_RELEASED_HELM_REPO_URL:-}" "")"
  RELEASED_ENTERPRISE_CHART_VERSION="$(first_nonempty "${SOK_RELEASED_ENTERPRISE_CHART_VERSION:-}" "")"
  RELEASED_OPERATOR_CHART_VERSION="$(first_nonempty "${SOK_RELEASED_OPERATOR_CHART_VERSION:-}" "")"
  if [ -z "${RELEASED_HELM_REPO_URL}" ] || [ -z "${RELEASED_ENTERPRISE_CHART_VERSION}" ] || [ -z "${RELEASED_OPERATOR_CHART_VERSION}" ]; then
    echo "Released SOK contract is missing chart fields" >&2
    exit 1
  fi
fi

if [ -z "${ECR_REGION}" ]; then
  echo "Unable to determine ECR region — set AWS_REGION, AWS_DEFAULT_REGION, or PIPELINE_AWS_DEFAULT_REGION" >&2
  exit 1
fi

resolve_enterprise_release_image
enterprise_image="${RESOLVED_ENTERPRISE_IMAGE}"

requested_profile="$(first_nonempty "${PIPELINE_HELM_TEST_PROFILE:-}" "${JOB_HELM_TEST_PROFILE:-}" "full")"
resolve_helm_test_profile "${requested_profile}"
generated_kuttl_config="ci-output/${WORKFLOW_SLUG}-kuttl-suite.yaml"

cluster_name_prefix="$(first_nonempty "${PIPELINE_HELM_CLUSTER_NAME_PREFIX:-}" "${JOB_HELM_CLUSTER_NAME_PREFIX:-}" "eks-helm-test-cluster")"
helm_version="$(first_nonempty "${PIPELINE_HELM_VERSION:-}" "${JOB_HELM_VERSION:-}" "v3.8.2")"
kuttl_version="$(first_nonempty "${PIPELINE_KUTTL_VERSION:-}" "${JOB_KUTTL_VERSION:-}" "0.22.0")"

export AWS_DEFAULT_REGION="${ECR_REGION}"
export S3_REGION="${ECR_REGION}"
export ECR_REGISTRY
export ECR_REPOSITORY="${ECR_REGISTRY}"
export PRIVATE_REGISTRY="${ECR_REGISTRY}"
export SPLUNK_OPERATOR_IMAGE="${RUNTIME_OPERATOR_REPO_IMAGE}"
export SPLUNK_ENTERPRISE_IMAGE="${enterprise_image}"
export TEST_CLUSTER_PLATFORM="eks"
export TEST_CLUSTER_NAME="${cluster_name_prefix}-${CI_JOB_ID}"
export CLUSTER_WIDE="$(first_nonempty "${PIPELINE_HELM_CLUSTER_WIDE:-}" "${JOB_HELM_CLUSTER_WIDE:-}" "true")"
export DEPLOYMENT_TYPE="helm"
export INSTALL_OPERATOR="true"
export TEST_BUCKET="$(first_nonempty "${PIPELINE_TEST_BUCKET:-}" "${TEST_BUCKET:-}" "")"
export TEST_S3_BUCKET="${TEST_BUCKET}"
export TEST_INDEXES_S3_BUCKET="$(first_nonempty "${PIPELINE_TEST_INDEXES_S3_BUCKET:-}" "${TEST_INDEXES_S3_BUCKET:-}" "")"
export EKS_VPC_PUBLIC_SUBNET_STRING="$(first_nonempty "${PIPELINE_EKS_VPC_PUBLIC_SUBNET_STRING:-}" "${EKS_VPC_PUBLIC_SUBNET_STRING:-}" "")"
export EKS_VPC_PRIVATE_SUBNET_STRING="$(first_nonempty "${PIPELINE_EKS_VPC_PRIVATE_SUBNET_STRING:-}" "${EKS_VPC_PRIVATE_SUBNET_STRING:-}" "")"
export TEST_VPC_ENDPOINT_URL="$(first_nonempty "${PIPELINE_TEST_VPC_ENDPOINT_URL:-}" "${TEST_VPC_ENDPOINT_URL:-}" "")"
export EKSCTL_VERSION="$(first_nonempty "${PIPELINE_EKSCTL_VERSION:-}" "${EKSCTL_VERSION:-}" "")"
export KUBECTL_VERSION="$(first_nonempty "${PIPELINE_KUBECTL_VERSION:-}" "${KUBECTL_VERSION:-}" "")"
export EKS_CLUSTER_K8_VERSION="$(first_nonempty "${PIPELINE_EKS_CLUSTER_K8_VERSION:-}" "${EKS_CLUSTER_K8_VERSION:-}" "")"

append_operator_runtime_context "${context_file}"
append_context "${context_file}" "ecr_region" "${ECR_REGION}"
append_context "${context_file}" "cluster_name" "${TEST_CLUSTER_NAME}"
append_context "${context_file}" "helm_test_profile" "${RESOLVED_HELM_TEST_PROFILE}"
append_context "${context_file}" "helm_test_dirs" "${RESOLVED_HELM_TEST_DIRS}"
append_context "${context_file}" "helm_test_timeout" "${RESOLVED_HELM_TEST_TIMEOUT}"
append_context "${context_file}" "helm_test_parallel" "${RESOLVED_HELM_TEST_PARALLEL}"
append_context "${context_file}" "helm_version" "${helm_version}"
append_context "${context_file}" "kuttl_version" "${kuttl_version}"
append_context "${context_file}" "enterprise_image" "${SPLUNK_ENTERPRISE_IMAGE}"

cleanup_and_exit() {
  rc="$1"
  cleanup_rc=0

  trap - EXIT INT TERM
  set +e

  log_step "cleanup:start" | tee -a "${cleanup_log}" >/dev/null
  copy_kuttl_junit

  if [ -d "${CI_PROJECT_DIR}/kuttl-artifacts" ]; then
    cp -R "${CI_PROJECT_DIR}/kuttl-artifacts/." "${kuttl_artifacts_dir}/" >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  fi

  log_step "cleanup:make-cleanup" | tee -a "${cleanup_log}" >/dev/null
  make cleanup >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  log_step "cleanup:make-clean" | tee -a "${cleanup_log}" >/dev/null
  make clean >> "${cleanup_log}" 2>&1 || cleanup_rc=1
  log_step "cleanup:cluster-down" | tee -a "${cleanup_log}" >/dev/null
  make cluster-down >> "${cleanup_log}" 2>&1 || cleanup_rc=1
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

log_step "tools:install kubectl=${KUBECTL_VERSION} eksctl=${EKSCTL_VERSION} helm=${helm_version} kuttl=${kuttl_version}"
make setup/kubectl KUBECTL_VERSION="${KUBECTL_VERSION}" CI_BIN_DIR="${ci_bin_dir}"
make setup/eksctl EKSCTL_VERSION="${EKSCTL_VERSION}" CI_BIN_DIR="${ci_bin_dir}"
make setup/helm HELM_VERSION="${helm_version}" CI_BIN_DIR="${ci_bin_dir}"
make setup/kuttl KUTTL_VERSION="${kuttl_version}" CI_BIN_DIR="${ci_bin_dir}"

log_step "build-helpers:kustomize:start"
make kustomize
log_step "build-helpers:kustomize:complete"

log_step "versions:start"
kubectl version --client=true
eksctl version
helm version
kubectl kuttl version
docker version
aws --version
log_step "versions:complete"

log_step "registry:ecr-login ${ECR_REGISTRY}"
aws ecr get-login-password --region "${AWS_DEFAULT_REGION}" | docker login --username AWS --password-stdin "${ECR_REGISTRY}"
log_step "registry:ecr-login:complete"

mirror_operator_image_to_ecr_if_needed

log_step "registry:enterprise-image:start"
# get-private-registry-enterprise.sh is a bash script and uses source/bash-only semantics.
PRIVATE_SPLUNK_ENTERPRISE_IMAGE="$(bash "${CI_PROJECT_DIR}/test/get-private-registry-enterprise.sh" | tail -n 1)"
log_step "registry:enterprise-image:complete ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"

log_step "cluster:up ${TEST_CLUSTER_NAME}"
make cluster-up 2>&1 | tee -a "${cluster_log}"
log_step "cluster:up:complete"
log_step "cluster:snapshot:nodes"
kubectl get nodes -o wide 2>&1 | tee -a "${cluster_log}"
log_step "cluster:snapshot:pods"
kubectl get pods -A 2>&1 | tee -a "${cluster_log}"

# TODO CSPL-4731: replace public GitHub URLs with internal mirror once
# artifact mirroring is set up for the SOK staging environment.
log_step "cluster:addons:metrics-server"
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml 2>&1 | tee -a "${cluster_log}"
log_step "cluster:addons:metrics-server:complete"

log_step "cluster:addons:dashboard"
kubectl apply -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.0.5/aio/deploy/recommended.yaml 2>&1 | tee -a "${cluster_log}"
log_step "cluster:addons:dashboard:complete"

log_step "cluster:crds-install:start"
make install 2>&1 | tee -a "${cluster_log}"
log_step "cluster:crds-install:complete"

if [ "${RUNTIME_OPERATOR_SOURCE_KIND}" = "official-release" ]; then
  released_helm_root="${CI_PROJECT_DIR}/ci-output/released-helm"
  rm -rf "${released_helm_root}"
  mkdir -p "${released_helm_root}"
  log_step "helm:pull-released:start version=${RELEASED_ENTERPRISE_CHART_VERSION}"
  case "${RELEASED_HELM_REPO_URL}" in
    oci://*)
      released_chart_path="${RELEASED_HELM_REPO_URL#oci://}"
      released_chart_registry="${released_chart_path%%/*}"
      chart_username="$(first_nonempty "${PIPELINE_CHART_RELEASE_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
      chart_password="$(first_nonempty "${PIPELINE_CHART_RELEASE_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"
      if [ -n "${chart_username}" ] || [ -n "${chart_password}" ] || printf '%s' "${released_chart_registry}" | grep -Eq '\.dkr\.ecr\..*\.amazonaws\.com$'; then
        helm_login_registry "${released_chart_registry}" "${chart_username}" "${chart_password}" >> "${kuttl_log}" 2>&1
      else
        log_step "helm:pull-released:registry-login:skipped"
      fi
      helm pull "${RELEASED_HELM_REPO_URL}/splunk-enterprise" --version "${RELEASED_ENTERPRISE_CHART_VERSION}" --untar --untardir "${released_helm_root}" >> "${kuttl_log}" 2>&1
      helm pull "${RELEASED_HELM_REPO_URL}/splunk-operator" --version "${RELEASED_OPERATOR_CHART_VERSION}" --untar --untardir "${released_helm_root}" >> "${kuttl_log}" 2>&1
      ;;
    *)
      helm repo add splunk "${RELEASED_HELM_REPO_URL}" >> "${kuttl_log}" 2>&1
      helm repo update >> "${kuttl_log}" 2>&1
      helm pull splunk/splunk-enterprise --version "${RELEASED_ENTERPRISE_CHART_VERSION}" --untar --untardir "${released_helm_root}" >> "${kuttl_log}" 2>&1
      helm pull splunk/splunk-operator --version "${RELEASED_OPERATOR_CHART_VERSION}" --untar --untardir "${released_helm_root}" >> "${kuttl_log}" 2>&1
      ;;
  esac
  export HELM_REPO_PATH="${released_helm_root}"
  log_step "helm:pull-released:complete"
else
  export HELM_REPO_PATH="${CI_PROJECT_DIR}/helm-chart"
  log_step "helm:package:start"
  make helm-package
  log_step "helm:package:complete"
fi

export KUTTL_SPLUNK_ENTERPRISE_IMAGE="${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
export KUTTL_SPLUNK_OPERATOR_IMAGE="${RUNTIME_OPERATOR_FULL_IMAGE_REF}"

write_kuttl_testsuite_config "${generated_kuttl_config}" "${RESOLVED_HELM_TEST_DIRS}" "${RESOLVED_HELM_TEST_PARALLEL}" "${RESOLVED_HELM_TEST_TIMEOUT}" "kuttl-artifacts"

append_context "${context_file}" "private_splunk_enterprise_image" "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
append_context "${context_file}" "kuttl_operator_image" "${KUTTL_SPLUNK_OPERATOR_IMAGE}"
append_context "${context_file}" "kuttl_enterprise_image" "${KUTTL_SPLUNK_ENTERPRISE_IMAGE}"
append_context "${context_file}" "generated_kuttl_config" "${generated_kuttl_config}"
append_context "${context_file}" "helm_repo_path" "${HELM_REPO_PATH}"

log_step "tests:helm-kuttl:start"
make helm-kuttl-test KUTTL_CONFIG="${generated_kuttl_config}" 2>&1 | tee -a "${kuttl_log}"
log_step "tests:helm-kuttl:complete"

copy_kuttl_junit
