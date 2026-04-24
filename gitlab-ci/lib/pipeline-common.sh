#!/bin/sh

# Shared GitLab validation helpers
# - Keep these functions POSIX-shell compatible because the workflow template invokes them with /bin/sh.
# - Centralize tool bootstrapping, artifact checks, naming normalization, and context capture.
# - Runtime scripts should prefer these helpers instead of duplicating parsing or bootstrap logic.

append_context() {
  context_file="$1"
  key="$2"
  value="$3"
  printf '%s=%s\n' "$key" "$value" >> "$context_file"
}

load_repo_dotenv() {
  dotenv_path="$1"
  if [ ! -f "$dotenv_path" ]; then
    echo "Missing dotenv file: ${dotenv_path}" >&2
    return 1
  fi

  set -a
  . "$dotenv_path"
  set +a
}

strip_docker_io_prefix() {
  image_ref="$1"
  case "${image_ref}" in
    registry-1.docker.io/*)
      printf '%s' "${image_ref#registry-1.docker.io/}"
      ;;
    index.docker.io/*)
      printf '%s' "${image_ref#index.docker.io/}"
      ;;
    docker.io/*)
      printf '%s' "${image_ref#docker.io/}"
      ;;
    *)
      printf '%s' "${image_ref}"
      ;;
  esac
}

registry_host_from_image_ref() {
  image_ref="$1"
  first_component="$(printf '%s' "${image_ref}" | cut -d/ -f1)"
  case "${first_component}" in
    *.*|*:*|localhost)
      printf '%s' "${first_component}"
      ;;
    *)
      printf '%s' "docker.io"
      ;;
  esac
}

bool_is_true() {
  case "${1:-}" in
    1|true|TRUE|True|yes|YES|Yes|on|ON|On)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

first_nonempty() {
  for value in "$@"; do
    if [ -n "${value}" ]; then
      printf '%s' "${value}"
      return 0
    fi
  done

  # Optional pipeline inputs often resolve through this helper inside command
  # substitutions under `set -e`. Return an empty string instead of failing so
  # callers can explicitly validate required values after fallback resolution.
  printf '%s' ""
  return 0
}

load_optional_env_file() {
  dotenv_path="$1"
  if [ -f "${dotenv_path}" ]; then
    set -a
    . "${dotenv_path}"
    set +a
  fi
}

load_optional_release_controller_env() {
  controller_env_path="$1"
  load_optional_env_file "${controller_env_path}"
}

require_nonempty() {
  value="$1"
  description="$2"
  if [ -z "${value}" ]; then
    echo "Missing required value: ${description}" >&2
    return 1
  fi
}

require_commands() {
  for command_name in "$@"; do
    if ! command -v "${command_name}" >/dev/null 2>&1; then
      echo "Missing required command: ${command_name}" >&2
      return 1
    fi
  done
}

require_envs() {
  for env_name in "$@"; do
    # POSIX sh does not support ${!name}; keep env_name sourced from fixed call
    # sites rather than dynamic user input when using this indirection.
    eval "env_value=\${${env_name}:-}"
    if [ -z "${env_value}" ]; then
      echo "Missing required environment variable: ${env_name}" >&2
      return 1
    fi
  done
}

materialize_file_secret() {
  secret_value="$1"
  output_path="$2"

  mkdir -p "$(dirname "${output_path}")"
  if [ -f "${secret_value}" ]; then
    cp "${secret_value}" "${output_path}"
  else
    printf '%s' "${secret_value}" | base64 -d > "${output_path}"
  fi
  chmod 0600 "${output_path}"
}

install_os_packages() {
  if ! command -v apt-get >/dev/null 2>&1; then
    echo "GitLab CI expects Debian-based runners with apt-get available" >&2
    return 1
  fi

  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@"
}

ensure_python_venv_tooling() {
  install_os_packages python3 python3-pip python3-venv
}

ensure_jq() {
  if command -v jq >/dev/null 2>&1; then
    return 0
  fi

  install_os_packages jq
}

require_file() {
  path="$1"
  description="$2"
  if [ ! -f "$path" ]; then
    echo "Missing required file: ${description} (${path})" >&2
    return 1
  fi
}

urlencode() {
  python3 - "$1" <<'PY'
import sys
import urllib.parse

print(urllib.parse.quote(sys.argv[1], safe=""))
PY
}

require_gitlab_job_token() {
  require_envs CI_API_V4_URL CI_PROJECT_ID CI_JOB_TOKEN
}

download_gitlab_job_artifacts_archive_by_ref() {
  ref_name="$1"
  job_name="$2"
  output_path="$3"

  require_gitlab_job_token
  encoded_ref="$(urlencode "${ref_name}")"
  curl --fail --location --silent --show-error \
    --header "JOB-TOKEN: ${CI_JOB_TOKEN}" \
    "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/jobs/artifacts/${encoded_ref}/download?job=${job_name}" \
    -o "${output_path}"
}

download_gitlab_job_artifacts_archive_by_pipeline() {
  pipeline_id="$1"
  job_name="$2"
  output_path="$3"

  require_gitlab_job_token
  ensure_jq

  jobs_json="$(curl --fail --location --silent --show-error \
    --header "JOB-TOKEN: ${CI_JOB_TOKEN}" \
    "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/pipelines/${pipeline_id}/jobs?scope[]=success&per_page=100")"
  job_id="$(printf '%s' "${jobs_json}" | jq -r --arg job_name "${job_name}" '.[] | select(.name == $job_name) | .id' | head -n 1)"
  require_nonempty "${job_id}" "successful ${job_name} job in pipeline ${pipeline_id}"

  curl --fail --location --silent --show-error \
    --header "JOB-TOKEN: ${CI_JOB_TOKEN}" \
    "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/jobs/${job_id}/artifacts" \
    -o "${output_path}"
}

resolve_pipeline_image_repository() {
  pipeline_target="$1"
  default_repo_path="$2"

  if [ -z "${pipeline_target}" ]; then
    echo "PIPELINE_ECR_REPOSITORY must be set" >&2
    return 1
  fi

  case "${pipeline_target}" in
    */*)
      RESOLVED_ECR_REGISTRY="${pipeline_target%%/*}"
      RESOLVED_IMAGE_REPOSITORY="${pipeline_target}"
      RESOLVED_IMAGE_REPOSITORY_MODE="explicit-repository"
      RESOLVED_REPOSITORY_NAME="${pipeline_target#${RESOLVED_ECR_REGISTRY}/}"
      ;;
    *)
      RESOLVED_ECR_REGISTRY="${pipeline_target}"
      RESOLVED_IMAGE_REPOSITORY="${pipeline_target}/${default_repo_path}"
      RESOLVED_IMAGE_REPOSITORY_MODE="registry-only"
      RESOLVED_REPOSITORY_NAME="${default_repo_path}"
      ;;
  esac
}

resolve_ecr_region() {
  registry="$1"
  RESOLVED_ECR_REGION="$(first_nonempty "${AWS_REGION:-}" "${AWS_DEFAULT_REGION:-}" "${PIPELINE_AWS_DEFAULT_REGION:-}" "$(printf '%s' "${registry}" | cut -d. -f4)")"
}

resolve_enterprise_release_image() {
  requested_enterprise_image="$(first_nonempty "${SPLUNK_ENTERPRISE_RELEASE_IMAGE:-}" "splunk/splunk:latest")"
  RESOLVED_ENTERPRISE_IMAGE="$(strip_docker_io_prefix "${requested_enterprise_image}")"
}

resolve_runtime_enterprise_image() {
  requested_enterprise_image="$(first_nonempty \
    "${PIPELINE_RUNTIME_ENTERPRISE_IMAGE:-}" \
    "${JOB_RUNTIME_ENTERPRISE_IMAGE:-}" \
    "${PIPELINE_INT_ENTERPRISE_IMAGE:-}" \
    "${JOB_INT_ENTERPRISE_IMAGE:-}" \
    "${SPLUNK_ENTERPRISE_RELEASE_IMAGE:-}" \
    "splunk/splunk:latest")"
  RESOLVED_ENTERPRISE_IMAGE="$(strip_docker_io_prefix "${requested_enterprise_image}")"
}

resolve_makefile_version() {
  makefile_path="$1"
  RESOLVED_MAKEFILE_VERSION="$(awk '/^VERSION[[:space:]]*\?/ {print $3; exit}' "${makefile_path}")"
  require_nonempty "${RESOLVED_MAKEFILE_VERSION}" "Makefile VERSION"
}

resolve_release_version() {
  makefile_path="$1"
  resolve_makefile_version "${makefile_path}"
  RESOLVED_RELEASE_VERSION="$(first_nonempty "${PIPELINE_RELEASE_VERSION:-}" "${RESOLVED_MAKEFILE_VERSION}")"
  RESOLVED_RELEASE_OPERATOR_TAG="$(first_nonempty "${PIPELINE_RELEASE_OPERATOR_TAG:-}" "${RESOLVED_RELEASE_VERSION}")"
  RESOLVED_RELEASE_CANDIDATE_NUMBER="$(first_nonempty "${PIPELINE_RELEASE_CANDIDATE_NUMBER:-}" "1")"
}

resolve_release_image_repository() {
  release_target="$(first_nonempty "${PIPELINE_RELEASE_IMAGE_REPOSITORY:-}" "docker.io/splunk/splunk-operator")"
  resolve_pipeline_image_repository "${release_target}" "splunk/splunk-operator"
  RESOLVED_RELEASE_IMAGE_REGISTRY="${RESOLVED_ECR_REGISTRY}"
  RESOLVED_RELEASE_IMAGE_REPOSITORY="${RESOLVED_IMAGE_REPOSITORY}"
}

# Resolve the operator under test for runtime jobs.
# - branch-build mode consumes the staged build artifact from this pipeline.
# - official-release mode consumes the released-SOK contract and mirrors the
#   released operator image into nonprod ECR before tests run.
resolve_operator_runtime_source() {
  build_image_ref_file="$1"
  released_sok_contract_file="$2"
  default_repo_path="$3"

  RUNTIME_INPUT_ARTIFACT=""
  RUNTIME_OPERATOR_IMAGE_VARIANT="$(first_nonempty "${PIPELINE_RUNTIME_IMAGE_VARIANT:-}" "${JOB_RUNTIME_IMAGE_VARIANT:-}" "standard")"
  RUNTIME_OPERATOR_SOURCE_KIND="branch-build"
  RUNTIME_OPERATOR_SOURCE_IMAGE=""
  RUNTIME_OPERATOR_MIRROR_PATH=""
  RUNTIME_OPERATOR_FULL_IMAGE_REF=""
  RUNTIME_OPERATOR_REPO_IMAGE=""
  RUNTIME_ECR_REGISTRY=""
  RUNTIME_ECR_REGION=""

  if [ -n "${released_sok_contract_file}" ]; then
    require_file "${released_sok_contract_file}" "released SOK contract"
    load_repo_dotenv "${released_sok_contract_file}"
    RUNTIME_OPERATOR_SOURCE_KIND="official-release"
    RUNTIME_INPUT_ARTIFACT="${released_sok_contract_file}"
    resolve_pipeline_image_repository "$(first_nonempty "${PIPELINE_ECR_REPOSITORY:-}" "")" "${default_repo_path}"
    RUNTIME_ECR_REGISTRY="${RESOLVED_ECR_REGISTRY}"
    case "${RUNTIME_OPERATOR_IMAGE_VARIANT}" in
      distroless)
        RUNTIME_OPERATOR_SOURCE_IMAGE="$(first_nonempty "${SOK_RELEASED_DISTROLESS_IMAGE_SOURCE:-}" "")"
        RUNTIME_OPERATOR_MIRROR_PATH="$(first_nonempty "${SOK_RELEASED_DISTROLESS_IMAGE_MIRROR_PATH:-}" "")"
        ;;
      *)
        RUNTIME_OPERATOR_SOURCE_IMAGE="$(first_nonempty "${SOK_RELEASED_OPERATOR_IMAGE_SOURCE:-}" "")"
        RUNTIME_OPERATOR_MIRROR_PATH="$(first_nonempty "${SOK_RELEASED_OPERATOR_IMAGE_MIRROR_PATH:-}" "")"
        ;;
    esac
    if [ -z "${RUNTIME_OPERATOR_SOURCE_IMAGE}" ] || [ -z "${RUNTIME_OPERATOR_MIRROR_PATH}" ]; then
      echo "Released SOK contract is missing ${RUNTIME_OPERATOR_IMAGE_VARIANT} operator image fields" >&2
      return 1
    fi
    RUNTIME_OPERATOR_REPO_IMAGE="${RUNTIME_OPERATOR_MIRROR_PATH}"
  else
    require_file "${build_image_ref_file}" "build image reference"
    runtime_build_image_ref="$(cat "${build_image_ref_file}")"
    runtime_image_repository="${runtime_build_image_ref%:*}"
    runtime_image_tag="${runtime_build_image_ref##*:}"
    RUNTIME_ECR_REGISTRY="${runtime_image_repository%%/*}"
    runtime_operator_repository_path="${runtime_image_repository#${RUNTIME_ECR_REGISTRY}/}"
    RUNTIME_INPUT_ARTIFACT="${build_image_ref_file}"
    RUNTIME_OPERATOR_FULL_IMAGE_REF="${runtime_build_image_ref}"
    RUNTIME_OPERATOR_REPO_IMAGE="${runtime_operator_repository_path}:${runtime_image_tag}"
  fi

  resolve_ecr_region "${RUNTIME_ECR_REGISTRY}"
  RUNTIME_ECR_REGION="${RESOLVED_ECR_REGION}"
}

append_operator_runtime_context() {
  context_file="$1"

  append_context "${context_file}" "input_artifact" "${RUNTIME_INPUT_ARTIFACT}"
  append_context "${context_file}" "operator_image_variant" "${RUNTIME_OPERATOR_IMAGE_VARIANT}"
  if [ "${RUNTIME_OPERATOR_SOURCE_KIND}" = "official-release" ]; then
    append_context "${context_file}" "released_operator_image_source" "${RUNTIME_OPERATOR_SOURCE_IMAGE}"
  else
    append_context "${context_file}" "operator_image" "${RUNTIME_OPERATOR_FULL_IMAGE_REF}"
  fi
  append_context "${context_file}" "operator_image_source" "${RUNTIME_OPERATOR_SOURCE_KIND}"
}

mirror_operator_image_to_ecr_if_needed() {
  if [ "${RUNTIME_OPERATOR_SOURCE_KIND}" = "official-release" ]; then
    RUNTIME_OPERATOR_FULL_IMAGE_REF="${RUNTIME_ECR_REGISTRY}/${RUNTIME_OPERATOR_MIRROR_PATH}"
    source_registry="$(registry_host_from_image_ref "${RUNTIME_OPERATOR_SOURCE_IMAGE}")"
    source_username="$(first_nonempty "${PIPELINE_RELEASED_OPERATOR_REGISTRY_USERNAME:-}" "${PIPELINE_RELEASE_REGISTRY_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
    source_password="$(first_nonempty "${PIPELINE_RELEASED_OPERATOR_REGISTRY_PASSWORD:-}" "${PIPELINE_RELEASE_REGISTRY_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"
    log_step "registry:mirror-operator:start ${RUNTIME_OPERATOR_SOURCE_IMAGE}"
    if [ -n "${source_username}" ] || [ -n "${source_password}" ] || printf '%s' "${source_registry}" | grep -Eq '\.dkr\.ecr\..*\.amazonaws\.com$'; then
      docker_login_registry "${source_registry}" "${source_username}" "${source_password}"
    fi
    docker pull "${RUNTIME_OPERATOR_SOURCE_IMAGE}"
    docker tag "${RUNTIME_OPERATOR_SOURCE_IMAGE}" "${RUNTIME_OPERATOR_FULL_IMAGE_REF}"
    docker push "${RUNTIME_OPERATOR_FULL_IMAGE_REF}"
    log_step "registry:mirror-operator:complete ${RUNTIME_OPERATOR_FULL_IMAGE_REF}"
  fi
}

login_source_registry_for_image() {
  source_image_ref="$1"
  source_registry="$(registry_host_from_image_ref "${source_image_ref}")"
  source_username="$(first_nonempty "${PIPELINE_DOCKER_USERNAME:-}" "")"
  source_password="$(first_nonempty "${PIPELINE_DOCKER_PASSWORD:-}" "")"

  if printf '%s' "${source_registry}" | grep -Eq '\.dkr\.ecr\..*\.amazonaws\.com$'; then
    docker_login_registry "${source_registry}" "" ""
    return 0
  fi

  if [ -n "${source_username}" ] || [ -n "${source_password}" ]; then
    docker_login_registry "${source_registry}" "${source_username}" "${source_password}"
  fi
}

promote_image_to_private_registry() {
  source_image_ref="$1"
  target_image_ref="$2"

  docker pull "${source_image_ref}"
  docker tag "${source_image_ref}" "${target_image_ref}"
  docker push "${target_image_ref}"
}

ensure_ci_bin_path() {
  ci_bin_dir="$1"
  mkdir -p "$ci_bin_dir"
  PATH="${ci_bin_dir}:${PATH}"
  export PATH
}

copy_if_exists() {
  src="$1"
  dest="$2"

  if [ -f "$src" ]; then
    mkdir -p "$(dirname "$dest")"
    cp "$src" "$dest"
    return 0
  fi

  return 1
}

sanitize_slug() {
  printf '%s' "$1" \
    | tr '[:upper:]' '[:lower:]' \
    | sed 's/[^a-z0-9]/-/g; s/--*/-/g; s/^-//; s/-$//'
}

normalize_testenv_commit_hash() {
  commit_hash="$1"
  max_length="${2:-8}"
  sanitized_hash="$(printf '%s' "${commit_hash}" | tr -cd '[:alnum:]')"

  if [ -z "${sanitized_hash}" ]; then
    NORMALIZED_TESTENV_COMMIT_HASH=""
    return 0
  fi

  NORMALIZED_TESTENV_COMMIT_HASH="$(printf '%s' "${sanitized_hash}" | cut -c1-"${max_length}")"
}

shorten_eks_test_name() {
  case "$1" in
    managerappframeworkc3)
      printf '%s' "mgr-appfw-c3"
      ;;
    managerappframeworkm4)
      printf '%s' "mgr-appfw-m4"
      ;;
    appframeworksS1)
      printf '%s' "appfw-s1"
      ;;
    managersecret)
      printf '%s' "mgr-secret"
      ;;
    managermc)
      printf '%s' "mgr-mc"
      ;;
    *)
      printf '%s' "$1"
      ;;
  esac
}

build_eks_test_cluster_name() {
  test_type="$1"
  platform_suffix="$2"
  test_name="$3"
  run_id="$4"

  shortened_test_name="$(shorten_eks_test_name "${test_name}")"
  safe_test_type="$(sanitize_slug "${test_type}")"
  safe_platform_suffix="$(sanitize_slug "${platform_suffix}")"
  safe_test_name="$(sanitize_slug "${shortened_test_name}")"
  safe_run_id="$(printf '%s' "${run_id}" | tr -cd '[:alnum:]')"

  if [ -z "${safe_test_type}" ] || [ -z "${safe_test_name}" ] || [ -z "${safe_run_id}" ]; then
    echo "EKS cluster naming requires non-empty test_type, test_name, and run_id" >&2
    return 1
  fi

  if [ -n "${safe_platform_suffix}" ]; then
    GENERATED_EKS_TEST_CLUSTER_NAME="eks-test-${safe_test_type}-${safe_platform_suffix}-${safe_test_name}-${safe_run_id}"
  else
    GENERATED_EKS_TEST_CLUSTER_NAME="eks-test-${safe_test_type}-${safe_test_name}-${safe_run_id}"
  fi
}

resolve_integration_profile() {
  requested_profile="$1"

  case "${requested_profile}" in
    ""|managersecret)
      RESOLVED_INT_TEST_PROFILE="managersecret"
      RESOLVED_INT_TEST_FOCUS="$(first_nonempty "${PIPELINE_INT_TEST_FOCUS:-}" "${JOB_INT_TEST_FOCUS:-}" "managersecret")"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT='^(?:[^i]+|i(?:$|[^n]|n(?:$|[^t]|t(?:$|[^e]|e(?:$|[^g]|g(?:$|[^r]|r(?:$|[^a]|a(?:$|[^t]|t(?:$|[^i]|i(?:$|[^o]|o(?:$|[^n])))))))))))*$'
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="$(first_nonempty "${PIPELINE_INT_MANAGERSECRET_CLUSTER_NODES:-}" "1")"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="$(first_nonempty "${PIPELINE_INT_MANAGERSECRET_CLUSTER_WORKERS:-}" "3")"
      ;;
    managersecret-smoke-s1)
      RESOLVED_INT_TEST_PROFILE="managersecret-smoke-s1"
      RESOLVED_INT_TEST_FOCUS="$(first_nonempty "${PIPELINE_INT_TEST_FOCUS:-}" "${JOB_INT_TEST_FOCUS:-}" "managersecret, smoke, s1")"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="$(first_nonempty "${PIPELINE_INT_MANAGERSECRET_SMOKE_S1_TEST_TO_SKIP:-}" "^$")"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="$(first_nonempty "${PIPELINE_INT_MANAGERSECRET_SMOKE_S1_CLUSTER_NODES:-}" "1")"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="$(first_nonempty "${PIPELINE_INT_MANAGERSECRET_SMOKE_S1_CLUSTER_WORKERS:-}" "2")"
      ;;
    managersecret-smoke-c3)
      RESOLVED_INT_TEST_PROFILE="managersecret-smoke-c3"
      RESOLVED_INT_TEST_FOCUS="$(first_nonempty "${PIPELINE_INT_TEST_FOCUS:-}" "${JOB_INT_TEST_FOCUS:-}" "managersecret, smoke, c3")"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="$(first_nonempty "${PIPELINE_INT_MANAGERSECRET_SMOKE_C3_TEST_TO_SKIP:-}" "^$")"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="$(first_nonempty "${PIPELINE_INT_MANAGERSECRET_SMOKE_C3_CLUSTER_NODES:-}" "1")"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="$(first_nonempty "${PIPELINE_INT_MANAGERSECRET_SMOKE_C3_CLUSTER_WORKERS:-}" "3")"
      ;;
    licensemanager-smoke-s1)
      RESOLVED_INT_TEST_PROFILE="licensemanager-smoke-s1"
      RESOLVED_INT_TEST_FOCUS="$(first_nonempty "${PIPELINE_INT_TEST_FOCUS:-}" "${JOB_INT_TEST_FOCUS:-}" "licensemanager, smoke, s1")"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="$(first_nonempty "${PIPELINE_INT_LICENSEMANAGER_SMOKE_S1_TEST_TO_SKIP:-}" "^$")"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="$(first_nonempty "${PIPELINE_INT_LICENSEMANAGER_SMOKE_S1_CLUSTER_NODES:-}" "1")"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="$(first_nonempty "${PIPELINE_INT_LICENSEMANAGER_SMOKE_S1_CLUSTER_WORKERS:-}" "2")"
      ;;
    smoke)
      RESOLVED_INT_TEST_PROFILE="smoke"
      RESOLVED_INT_TEST_FOCUS="$(first_nonempty "${PIPELINE_INT_TEST_FOCUS:-}" "${JOB_INT_TEST_FOCUS:-}" "smoke")"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="$(first_nonempty "${PIPELINE_INT_SMOKE_TEST_TO_SKIP:-}" "^$")"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="$(first_nonempty "${PIPELINE_INT_SMOKE_CLUSTER_NODES:-}" "1")"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="$(first_nonempty "${PIPELINE_INT_SMOKE_CLUSTER_WORKERS:-}" "2")"
      ;;
    appframework)
      RESOLVED_INT_TEST_PROFILE="appframework"
      RESOLVED_INT_TEST_FOCUS="$(first_nonempty "${PIPELINE_INT_TEST_FOCUS:-}" "${JOB_INT_TEST_FOCUS:-}" "appframework")"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="$(first_nonempty "${PIPELINE_INT_APPFRAMEWORK_TEST_TO_SKIP:-}" "^(?:[^i]+|i(?:$|[^n]|n(?:$|[^t]|t(?:$|[^e]|e(?:$|[^g]|g(?:$|[^r]|r(?:$|[^a]|a(?:$|[^t]|t(?:$|[^i]|i(?:$|[^o]|o(?:$|[^n])))))))))))*$")"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="$(first_nonempty "${PIPELINE_INT_APPFRAMEWORK_CLUSTER_NODES:-}" "2")"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="$(first_nonempty "${PIPELINE_INT_APPFRAMEWORK_CLUSTER_WORKERS:-}" "5")"
      ;;
    full)
      RESOLVED_INT_TEST_PROFILE="full"
      RESOLVED_INT_TEST_FOCUS="$(first_nonempty "${PIPELINE_INT_TEST_FOCUS:-}" "${JOB_INT_TEST_FOCUS:-}" "integration")"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="$(first_nonempty "${PIPELINE_INT_FULL_TEST_TO_SKIP:-}" "^$")"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="$(first_nonempty "${PIPELINE_INT_FULL_CLUSTER_NODES:-}" "2")"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="$(first_nonempty "${PIPELINE_INT_FULL_CLUSTER_WORKERS:-}" "5")"
      ;;
    *)
      RESOLVED_INT_TEST_PROFILE="${requested_profile}"
      RESOLVED_INT_TEST_FOCUS="$(first_nonempty "${PIPELINE_INT_TEST_FOCUS:-}" "${JOB_INT_TEST_FOCUS:-}" "${requested_profile}")"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="$(first_nonempty "${PIPELINE_INT_TEST_TO_SKIP_DEFAULT:-}" "^$")"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="$(first_nonempty "${PIPELINE_INT_CLUSTER_NODES:-}" "1")"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="$(first_nonempty "${PIPELINE_INT_CLUSTER_WORKERS:-}" "3")"
      ;;
  esac
}

resolve_helm_test_profile() {
  requested_profile="$1"

  case "${requested_profile}" in
    ""|smoke)
      RESOLVED_HELM_TEST_PROFILE="smoke"
      RESOLVED_HELM_TEST_DIRS="./kuttl/tests/helm/s1,./kuttl/tests/helm/s1-with-operator,./kuttl/tests/helm/operator-with-ephemeral-volume"
      RESOLVED_HELM_TEST_TIMEOUT="$(first_nonempty "${PIPELINE_HELM_TEST_TIMEOUT:-}" "${JOB_HELM_TEST_TIMEOUT:-}" "4000")"
      RESOLVED_HELM_TEST_PARALLEL="$(first_nonempty "${PIPELINE_HELM_TEST_PARALLEL:-}" "${JOB_HELM_TEST_PARALLEL:-}" "1")"
      ;;
    qualification|full)
      # `qualification` is a legacy alias; qualification and release both run
      # the full Helm suite now, so normalize the effective profile to `full`.
      RESOLVED_HELM_TEST_PROFILE="full"
      RESOLVED_HELM_TEST_DIRS="$(first_nonempty "${PIPELINE_HELM_TEST_DIRS:-}" "${JOB_HELM_TEST_DIRS:-}" "./kuttl/tests/helm")"
      RESOLVED_HELM_TEST_TIMEOUT="$(first_nonempty "${PIPELINE_HELM_TEST_TIMEOUT:-}" "${JOB_HELM_TEST_TIMEOUT:-}" "7000")"
      RESOLVED_HELM_TEST_PARALLEL="$(first_nonempty "${PIPELINE_HELM_TEST_PARALLEL:-}" "${JOB_HELM_TEST_PARALLEL:-}" "1")"
      ;;
    *)
      RESOLVED_HELM_TEST_PROFILE="${requested_profile}"
      RESOLVED_HELM_TEST_DIRS="$(first_nonempty "${PIPELINE_HELM_TEST_DIRS:-}" "${JOB_HELM_TEST_DIRS:-}" "./kuttl/tests/helm")"
      RESOLVED_HELM_TEST_TIMEOUT="$(first_nonempty "${PIPELINE_HELM_TEST_TIMEOUT:-}" "${JOB_HELM_TEST_TIMEOUT:-}" "7000")"
      RESOLVED_HELM_TEST_PARALLEL="$(first_nonempty "${PIPELINE_HELM_TEST_PARALLEL:-}" "${JOB_HELM_TEST_PARALLEL:-}" "1")"
      ;;
  esac
}

write_kuttl_testsuite_config() {
  output_path="$1"
  test_dirs="$2"
  parallel="$3"
  timeout="$4"
  artifacts_dir="$5"

  {
    echo "# Generated by gitlab-ci/lib/pipeline-common.sh"
    echo "apiVersion: kuttl.dev/v1beta1"
    echo "kind: TestSuite"
    echo "testDirs:"
    OLD_IFS="${IFS}"
    IFS=','
    set -- ${test_dirs}
    IFS="${OLD_IFS}"
    for test_dir in "$@"; do
      echo "- ${test_dir}"
    done
    echo "parallel: ${parallel}"
    echo "timeout: ${timeout}"
    echo "startKIND: false"
    echo "artifactsDir: ${artifacts_dir}"
    echo "kindNodeCache: false"
  } > "${output_path}"
}

ensure_pipeline_aws_env() {
  if [ -z "${AWS_ACCESS_KEY_ID:-}" ]; then
    AWS_ACCESS_KEY_ID="$(first_nonempty "${PIPELINE_AWS_ACCESS_KEY_ID:-}" "")"
    export AWS_ACCESS_KEY_ID
  fi

  if [ -z "${AWS_SECRET_ACCESS_KEY:-}" ]; then
    AWS_SECRET_ACCESS_KEY="$(first_nonempty "${PIPELINE_AWS_SECRET_ACCESS_KEY:-}" "")"
    export AWS_SECRET_ACCESS_KEY
  fi

  if [ -z "${AWS_DEFAULT_REGION:-}" ]; then
    AWS_DEFAULT_REGION="$(first_nonempty "${PIPELINE_AWS_DEFAULT_REGION:-}" "")"
    export AWS_DEFAULT_REGION
  fi
}

registry_login_password_for_host() {
  registry_host="$1"
  username="$2"
  password="$3"

  case "${registry_host}" in
    *.dkr.ecr.*.amazonaws.com)
      ensure_pipeline_aws_env
      resolve_ecr_region "${registry_host}"
      require_nonempty "${RESOLVED_ECR_REGION}" "ECR region for ${registry_host}"
      if REGISTRY_LOGIN_PASSWORD="$(aws ecr get-login-password --region "${RESOLVED_ECR_REGION}" 2>/dev/null)"; then
        REGISTRY_LOGIN_USERNAME="AWS"
        return 0
      fi
      if [ -n "${username}" ] || [ -n "${password}" ]; then
        require_nonempty "${username}" "registry username for ${registry_host}"
        require_nonempty "${password}" "registry password for ${registry_host}"
        REGISTRY_LOGIN_USERNAME="${username}"
        REGISTRY_LOGIN_PASSWORD="${password}"
        return 0
      fi
      echo "Registry ${registry_host} requires AWS credentials or explicit PIPELINE_* credentials" >&2
      return 1
      ;;
    *)
      if [ -n "${username}" ] || [ -n "${password}" ]; then
        require_nonempty "${username}" "registry username for ${registry_host}"
        require_nonempty "${password}" "registry password for ${registry_host}"
        REGISTRY_LOGIN_USERNAME="${username}"
        REGISTRY_LOGIN_PASSWORD="${password}"
        return 0
      fi
      echo "Registry ${registry_host} requires explicit PIPELINE_* credentials" >&2
      return 1
      ;;
  esac
}

docker_login_registry() {
  registry_host="$1"
  username="$2"
  password="$3"

  registry_login_password_for_host "${registry_host}" "${username}" "${password}"
  printf '%s' "${REGISTRY_LOGIN_PASSWORD}" | docker login --username "${REGISTRY_LOGIN_USERNAME}" --password-stdin "${registry_host}"
}

helm_login_registry() {
  registry_host="$1"
  username="$2"
  password="$3"

  registry_login_password_for_host "${registry_host}" "${username}" "${password}"
  printf '%s' "${REGISTRY_LOGIN_PASSWORD}" | helm registry login "${registry_host}" --username "${REGISTRY_LOGIN_USERNAME}" --password-stdin
}

ensure_operator_sdk() {
  ci_bin_dir="$1"
  operator_sdk_version="$2"

  require_nonempty "${operator_sdk_version}" "OPERATOR_SDK_VERSION"

  mkdir -p "${ci_bin_dir}"
  PATH="${ci_bin_dir}:${PATH}"
  export PATH

  if [ -x "${ci_bin_dir}/operator-sdk" ]; then
    return 0
  fi

  os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch_name="$(uname -m)"
  case "${arch_name}" in
    x86_64|amd64)
      arch_name="amd64"
      ;;
    aarch64|arm64)
      arch_name="arm64"
      ;;
  esac

  # TODO CSPL-4731: replace public GitHub URLs with internal mirror once
  # artifact mirroring is set up for the SOK staging environment.
  curl -fsSL -o "${ci_bin_dir}/operator-sdk" \
    "https://github.com/operator-framework/operator-sdk/releases/download/${operator_sdk_version}/operator-sdk_${os_name}_${arch_name}"
  chmod 0755 "${ci_bin_dir}/operator-sdk"
}
