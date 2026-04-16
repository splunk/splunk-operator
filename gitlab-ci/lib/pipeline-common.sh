#!/bin/sh

# Shared GitLab validation helpers
# - Keep these functions POSIX-shell compatible because the workflow template invokes them with /bin/sh.
# - Centralize registry resolution, tool bootstrapping, artifact checks, naming normalization, and context capture.
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

load_optional_release_controller_env() {
  controller_env_path="$1"
  if [ -f "$controller_env_path" ]; then
    set -a
    . "$controller_env_path"
    set +a
  fi
}

strip_docker_io_prefix() {
  image_ref="$1"
  case "${image_ref}" in
    docker.io/*)
      printf '%s' "${image_ref#docker.io/}"
      ;;
    *)
      printf '%s' "${image_ref}"
      ;;
  esac
}

strip_oci_prefix() {
  oci_ref="$1"
  case "${oci_ref}" in
    oci://*)
      printf '%s' "${oci_ref#oci://}"
      ;;
    *)
      printf '%s' "${oci_ref}"
      ;;
  esac
}

oci_registry_host() {
  oci_ref="$1"
  stripped_ref="$(strip_oci_prefix "${oci_ref}")"
  printf '%s' "${stripped_ref}" | cut -d/ -f1
}

normalize_chart_repository_base() {
  repository_ref="$1"
  stripped_ref="$(strip_oci_prefix "${repository_ref}")"
  stripped_ref="${stripped_ref%/}"

  case "${stripped_ref}" in
    */splunk-operator|*/splunk-enterprise)
      stripped_ref="${stripped_ref%/*}"
      ;;
  esac

  printf 'oci://%s' "${stripped_ref}"
}

chart_repository_ref() {
  repository_base="$1"
  chart_name="$2"
  normalized_base="${repository_base%/}"
  printf '%s/%s' "${normalized_base}" "${chart_name}"
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

install_os_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@"
    return 0
  fi

  if command -v dnf >/dev/null 2>&1; then
    dnf install -y "$@"
    return 0
  fi

  if command -v yum >/dev/null 2>&1; then
    yum install -y "$@"
    return 0
  fi

  if command -v apk >/dev/null 2>&1; then
    apk add --no-cache "$@"
    return 0
  fi

  echo "Unable to install packages because no supported package manager was found" >&2
  return 1
}

helm_registry_login_with_password() {
  registry_ref="$1"
  username="$2"
  password="$3"
  registry_host="$(oci_registry_host "${registry_ref}")"

  if [ -z "${registry_host}" ]; then
    echo "Unable to determine OCI registry host from ${registry_ref}" >&2
    return 1
  fi

  printf '%s' "${password}" | helm registry login "${registry_host}" --username "${username}" --password-stdin
}

ensure_python3() {
  if command -v python3 >/dev/null 2>&1; then
    return 0
  fi

  if command -v apt-get >/dev/null 2>&1; then
    install_os_packages python3
  elif command -v dnf >/dev/null 2>&1; then
    install_os_packages python3
  elif command -v yum >/dev/null 2>&1; then
    install_os_packages python3
  elif command -v apk >/dev/null 2>&1; then
    install_os_packages python3
  else
    echo "No supported package manager found to install python3" >&2
    return 1
  fi
}

ensure_python_venv_tooling() {
  if command -v apt-get >/dev/null 2>&1; then
    install_os_packages python3 python3-pip python3-venv
    return 0
  fi

  if command -v dnf >/dev/null 2>&1; then
    install_os_packages python3 python3-pip
    return 0
  fi

  if command -v yum >/dev/null 2>&1; then
    install_os_packages python3 python3-pip
    return 0
  fi

  if command -v apk >/dev/null 2>&1; then
    install_os_packages python3 py3-pip py3-virtualenv
    return 0
  fi

  echo "No supported package manager found to install Python venv tooling" >&2
  return 1
}

ensure_git_ripgrep_python_venv_tooling() {
  if command -v apt-get >/dev/null 2>&1; then
    install_os_packages git ripgrep python3 python3-pip python3-venv
    return 0
  fi

  if command -v dnf >/dev/null 2>&1; then
    install_os_packages git ripgrep python3 python3-pip
    return 0
  fi

  if command -v yum >/dev/null 2>&1; then
    install_os_packages git ripgrep python3 python3-pip
    return 0
  fi

  if command -v apk >/dev/null 2>&1; then
    install_os_packages git ripgrep python3 py3-pip py3-virtualenv
    return 0
  fi

  echo "No supported package manager found to install bias-language prerequisites" >&2
  return 1
}

ensure_jq() {
  if command -v jq >/dev/null 2>&1; then
    return 0
  fi

  install_os_packages jq
}

ensure_trivy_scan_tooling() {
  if command -v apt-get >/dev/null 2>&1; then
    install_os_packages bash curl jq python3 python3-pip python3-venv tar
    return 0
  fi

  if command -v dnf >/dev/null 2>&1; then
    install_os_packages bash curl jq python3 python3-pip tar
    return 0
  fi

  if command -v yum >/dev/null 2>&1; then
    install_os_packages bash curl jq python3 python3-pip tar
    return 0
  fi

  if command -v apk >/dev/null 2>&1; then
    install_os_packages bash curl jq python3 py3-pip py3-virtualenv tar
    return 0
  fi

  echo "No supported package manager found to install Trivy scan prerequisites" >&2
  return 1
}

helm_registry_login_with_dockerconfig() {
  registry_ref="$1"
  dockerconfig_path="$2"
  registry_host="$(oci_registry_host "${registry_ref}")"

  if [ -z "${registry_host}" ]; then
    echo "Unable to determine OCI registry host from ${registry_ref}" >&2
    return 1
  fi

  if [ ! -f "${dockerconfig_path}" ]; then
    echo "Missing docker config file: ${dockerconfig_path}" >&2
    return 1
  fi

  ensure_python3

  auth_payload="$(python3 - "${dockerconfig_path}" "${registry_host}" <<'PY'
import base64
import json
import sys
from pathlib import Path

dockerconfig_path = Path(sys.argv[1])
registry_host = sys.argv[2]
payload = json.loads(dockerconfig_path.read_text(encoding="utf-8"))
auths = payload.get("auths", {})
entry = auths.get(registry_host)
if entry is None and f"https://{registry_host}" in auths:
    entry = auths[f"https://{registry_host}"]
if entry is None and f"http://{registry_host}" in auths:
    entry = auths[f"http://{registry_host}"]
if not entry or "auth" not in entry:
    raise SystemExit(1)
decoded = base64.b64decode(entry["auth"]).decode("utf-8")
print(decoded)
PY
)" || {
    echo "Unable to extract OCI registry auth for ${registry_host} from ${dockerconfig_path}" >&2
    return 1
  }

  username="${auth_payload%%:*}"
  password="${auth_payload#*:}"
  if [ -z "${username}" ] || [ "${password}" = "${auth_payload}" ]; then
    echo "Invalid auth payload for ${registry_host} in ${dockerconfig_path}" >&2
    return 1
  fi

  helm_registry_login_with_password "${registry_ref}" "${username}" "${password}"
}

resolve_enterprise_source_image() {
  target_branch="${CI_MERGE_REQUEST_TARGET_BRANCH_NAME:-${CI_COMMIT_REF_NAME:-}}"
  source_mode="${SOK_SOURCE_MODE:-}"

  if [ -z "${source_mode}" ]; then
    case "${target_branch}" in
      main|release/*|patch/*)
        source_mode="release"
        ;;
      *)
        source_mode="develop"
        ;;
    esac
  fi

  trigger_kind="${SOK_TRIGGER_KIND:-}"
  if [ -z "${trigger_kind}" ]; then
    case "${source_mode}" in
      release)
        if [ -n "${SPLUNK_ENTERPRISE_RELEASE_IMAGE:-}" ] || [ -n "${SOK_ENTERPRISE_RELEASE_IMAGE:-}" ]; then
          trigger_kind="release-image-ready"
        else
          trigger_kind="qualification-cycle"
        fi
        ;;
      *)
        if [ -n "${SPLUNK_ENTERPRISE_DEVELOP_IMAGE:-}" ] || [ -n "${SOK_ENTERPRISE_DEVELOP_IMAGE:-}" ]; then
          trigger_kind="develop-image-ready"
        else
          trigger_kind="develop-checkin"
        fi
        ;;
    esac
  fi

  develop_image="${SPLUNK_ENTERPRISE_DEVELOP_IMAGE:-${SOK_ENTERPRISE_DEVELOP_IMAGE:-}}"
  release_image="${SPLUNK_ENTERPRISE_RELEASE_IMAGE:-${SOK_ENTERPRISE_RELEASE_IMAGE:-${SPLUNK_ENTERPRISE_IMAGE:-${SOK_ENTERPRISE_IMAGE:-}}}}"
  fallback_image="${STAGING_SPLUNK_ENTERPRISE_IMAGE:-${RELATED_IMAGE_SPLUNK_ENTERPRISE:-}}"

  selected_image=""
  selected_source=""
  case "${source_mode}" in
    release)
      if [ -n "${release_image}" ]; then
        selected_image="${release_image}"
        selected_source="release-image"
      elif [ -n "${fallback_image}" ]; then
        selected_image="${fallback_image}"
        selected_source="staging-fallback-image"
      elif [ -n "${develop_image}" ]; then
        selected_image="${develop_image}"
        selected_source="develop-image-fallback"
      fi
      ;;
    *)
      if [ -n "${develop_image}" ]; then
        selected_image="${develop_image}"
        selected_source="develop-image"
      elif [ -n "${fallback_image}" ]; then
        selected_image="${fallback_image}"
        selected_source="staging-fallback-image"
      elif [ -n "${release_image}" ]; then
        selected_image="${release_image}"
        selected_source="release-image-fallback"
      fi
      ;;
  esac

  if [ -z "${selected_image}" ]; then
    echo "Unable to resolve a Splunk Enterprise image for source mode ${source_mode}" >&2
    return 1
  fi

  RESOLVED_SOK_SOURCE_MODE="${source_mode}"
  RESOLVED_SOK_TRIGGER_KIND="${trigger_kind}"
  RESOLVED_SPLUNK_ENTERPRISE_IMAGE="${selected_image}"
  RESOLVED_SPLUNK_ENTERPRISE_IMAGE_SOURCE="${selected_source}"
  RESOLVED_SPLUNK_ENTERPRISE_IMAGE_NO_DOCKER_IO="$(strip_docker_io_prefix "${selected_image}")"

  export RESOLVED_SOK_SOURCE_MODE
  export RESOLVED_SOK_TRIGGER_KIND
  export RESOLVED_SPLUNK_ENTERPRISE_IMAGE
  export RESOLVED_SPLUNK_ENTERPRISE_IMAGE_SOURCE
  export RESOLVED_SPLUNK_ENTERPRISE_IMAGE_NO_DOCKER_IO
}

require_file() {
  path="$1"
  description="$2"
  if [ ! -f "$path" ]; then
    echo "Missing required file: ${description} (${path})" >&2
    return 1
  fi
}

env_present() {
  env_name="$1"
  env_value="$(printenv "${env_name}" 2>/dev/null || true)"
  [ -n "${env_value}" ]
}

aws_oidc_ready() {
  env_present GITLAB_OIDC_TOKEN && env_present AWS_ROLE_ARN
}

aws_prepare_oidc_env() {
  token_file="$1"

  if [ -z "${GITLAB_OIDC_TOKEN:-}" ] || [ -z "${AWS_ROLE_ARN:-}" ]; then
    echo "GitLab AWS OIDC requires GITLAB_OIDC_TOKEN and AWS_ROLE_ARN" >&2
    return 1
  fi

  printf '%s' "${GITLAB_OIDC_TOKEN}" > "${token_file}"
  export AWS_WEB_IDENTITY_TOKEN_FILE="${token_file}"
  export AWS_ROLE_ARN="${AWS_ROLE_ARN}"
  export AWS_ROLE_SESSION_NAME="${AWS_ROLE_SESSION_NAME:-gitlab-${CI_JOB_ID:-session}}"
  export AWS_STS_REGIONAL_ENDPOINTS=regional
}

resolve_staging_image_repository() {
  staging_target="$1"
  default_repo_path="$2"

  case "${staging_target}" in
    */*)
      RESOLVED_ECR_REGISTRY="${staging_target%%/*}"
      RESOLVED_IMAGE_REPOSITORY="${staging_target}"
      RESOLVED_IMAGE_REPOSITORY_MODE="explicit-repository"
      ;;
    *)
      RESOLVED_ECR_REGISTRY="${staging_target}"
      RESOLVED_IMAGE_REPOSITORY="${staging_target}/${default_repo_path}"
      RESOLVED_IMAGE_REPOSITORY_MODE="registry-only"
      ;;
  esac
}

resolve_ecr_region() {
  configured_region="$1"
  ecr_registry="$2"
  trimmed_region="$(printf '%s' "${configured_region}" | tr -d '[:space:]')"

  if [ -n "${trimmed_region}" ]; then
    RESOLVED_ECR_REGION="${trimmed_region}"
    RESOLVED_ECR_REGION_SOURCE="configured-variable"
    return 0
  fi

  RESOLVED_ECR_REGION="$(printf '%s' "${ecr_registry}" | cut -d. -f4)"
  RESOLVED_ECR_REGION_SOURCE="registry-hostname"
}

ensure_ci_bin_path() {
  ci_bin_dir="$1"
  mkdir -p "$ci_bin_dir"
  PATH="${ci_bin_dir}:${PATH}"
  export PATH
}

install_kubectl_version() {
  kubectl_version="$1"
  ci_bin_dir="$2"

  ensure_ci_bin_path "$ci_bin_dir"

  if [ ! -x "${ci_bin_dir}/kubectl" ]; then
    curl -fsSL -o "${ci_bin_dir}/kubectl" "https://dl.k8s.io/release/${kubectl_version}/bin/linux/amd64/kubectl"
    chmod +x "${ci_bin_dir}/kubectl"
  fi
}

install_eksctl_version() {
  eksctl_version="$1"
  ci_bin_dir="$2"
  temp_archive="/tmp/eksctl-${eksctl_version}-amd64.tar.gz"

  ensure_ci_bin_path "$ci_bin_dir"

  if [ ! -x "${ci_bin_dir}/eksctl" ]; then
    curl --silent --location -o "${temp_archive}" "https://github.com/weaveworks/eksctl/releases/download/${eksctl_version}/eksctl_$(uname -s)_amd64.tar.gz"
    tar -xzf "${temp_archive}" -C "${ci_bin_dir}" eksctl
    chmod +x "${ci_bin_dir}/eksctl"
    rm -f "${temp_archive}"
  fi
}

install_helm_version() {
  helm_version="$1"
  ci_bin_dir="$2"
  temp_archive="/tmp/helm-${helm_version}-linux-amd64.tar.gz"

  ensure_ci_bin_path "$ci_bin_dir"

  if [ ! -x "${ci_bin_dir}/helm" ]; then
    curl -fsSL -o "${temp_archive}" "https://get.helm.sh/helm-${helm_version}-linux-amd64.tar.gz"
    tar -xzf "${temp_archive}" -C /tmp linux-amd64/helm
    mv /tmp/linux-amd64/helm "${ci_bin_dir}/helm"
    chmod +x "${ci_bin_dir}/helm"
    rm -f "${temp_archive}"
    rm -rf /tmp/linux-amd64
  fi
}

install_kuttl_version() {
  kuttl_version="$1"
  ci_bin_dir="$2"
  temp_archive="/tmp/kuttl_${kuttl_version#v}_linux_x86_64.tar.gz"

  ensure_ci_bin_path "$ci_bin_dir"

  if [ ! -x "${ci_bin_dir}/kubectl-kuttl" ]; then
    curl -fsSL -o "${temp_archive}" "https://github.com/kudobuilder/kuttl/releases/download/${kuttl_version}/kuttl_${kuttl_version#v}_linux_x86_64.tar.gz"
    tar -xzf "${temp_archive}" -C /tmp kubectl-kuttl
    mv /tmp/kubectl-kuttl "${ci_bin_dir}/kubectl-kuttl"
    chmod +x "${ci_bin_dir}/kubectl-kuttl"
    rm -f "${temp_archive}"
  fi
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

stage_enterprise_image_in_private_registry() {
  bash "${CI_PROJECT_DIR}/test/get-private-registry-enterprise.sh" | tail -1
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

trim_csv_field() {
  printf '%s' "$1" | sed 's/^ *//; s/ *$//'
}

resolve_integration_profile() {
  requested_profile="$1"

  case "${requested_profile}" in
    ""|managersecret)
      RESOLVED_INT_TEST_PROFILE="managersecret"
      RESOLVED_INT_TEST_FOCUS="${STAGING_INT_TEST_FOCUS:-managersecret}"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT='^(?:[^i]+|i(?:$|[^n]|n(?:$|[^t]|t(?:$|[^e]|e(?:$|[^g]|g(?:$|[^r]|r(?:$|[^a]|a(?:$|[^t]|t(?:$|[^i]|i(?:$|[^o]|o(?:$|[^n])))))))))))*$'
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="${STAGING_INT_MANAGERSECRET_CLUSTER_NODES:-1}"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="${STAGING_INT_MANAGERSECRET_CLUSTER_WORKERS:-3}"
      ;;
    managersecret-smoke-s1)
      RESOLVED_INT_TEST_PROFILE="managersecret-smoke-s1"
      RESOLVED_INT_TEST_FOCUS="${STAGING_INT_TEST_FOCUS:-managersecret, smoke, s1}"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="${STAGING_INT_MANAGERSECRET_SMOKE_S1_TEST_TO_SKIP:-^$}"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="${STAGING_INT_MANAGERSECRET_SMOKE_S1_CLUSTER_NODES:-1}"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="${STAGING_INT_MANAGERSECRET_SMOKE_S1_CLUSTER_WORKERS:-2}"
      ;;
    managersecret-smoke-c3)
      RESOLVED_INT_TEST_PROFILE="managersecret-smoke-c3"
      RESOLVED_INT_TEST_FOCUS="${STAGING_INT_TEST_FOCUS:-managersecret, smoke, c3}"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="${STAGING_INT_MANAGERSECRET_SMOKE_C3_TEST_TO_SKIP:-^$}"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="${STAGING_INT_MANAGERSECRET_SMOKE_C3_CLUSTER_NODES:-1}"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="${STAGING_INT_MANAGERSECRET_SMOKE_C3_CLUSTER_WORKERS:-3}"
      ;;
    licensemanager-smoke-s1)
      RESOLVED_INT_TEST_PROFILE="licensemanager-smoke-s1"
      RESOLVED_INT_TEST_FOCUS="${STAGING_INT_TEST_FOCUS:-licensemanager, smoke, s1}"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="${STAGING_INT_LICENSEMANAGER_SMOKE_S1_TEST_TO_SKIP:-^$}"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="${STAGING_INT_LICENSEMANAGER_SMOKE_S1_CLUSTER_NODES:-1}"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="${STAGING_INT_LICENSEMANAGER_SMOKE_S1_CLUSTER_WORKERS:-2}"
      ;;
    smoke)
      RESOLVED_INT_TEST_PROFILE="smoke"
      RESOLVED_INT_TEST_FOCUS="${STAGING_INT_TEST_FOCUS:-smoke}"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="${STAGING_INT_SMOKE_TEST_TO_SKIP:-^$}"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="${STAGING_INT_SMOKE_CLUSTER_NODES:-1}"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="${STAGING_INT_SMOKE_CLUSTER_WORKERS:-2}"
      ;;
    appframework)
      RESOLVED_INT_TEST_PROFILE="appframework"
      RESOLVED_INT_TEST_FOCUS="${STAGING_INT_TEST_FOCUS:-appframework}"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="${STAGING_INT_APPFRAMEWORK_TEST_TO_SKIP:-^(?:[^i]+|i(?:$|[^n]|n(?:$|[^t]|t(?:$|[^e]|e(?:$|[^g]|g(?:$|[^r]|r(?:$|[^a]|a(?:$|[^t]|t(?:$|[^i]|i(?:$|[^o]|o(?:$|[^n])))))))))))*$}"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="${STAGING_INT_APPFRAMEWORK_CLUSTER_NODES:-2}"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="${STAGING_INT_APPFRAMEWORK_CLUSTER_WORKERS:-5}"
      ;;
    full)
      RESOLVED_INT_TEST_PROFILE="full"
      RESOLVED_INT_TEST_FOCUS="${STAGING_INT_TEST_FOCUS:-integration}"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="${STAGING_INT_FULL_TEST_TO_SKIP:-^$}"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="${STAGING_INT_FULL_CLUSTER_NODES:-2}"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="${STAGING_INT_FULL_CLUSTER_WORKERS:-5}"
      ;;
    *)
      RESOLVED_INT_TEST_PROFILE="${requested_profile}"
      RESOLVED_INT_TEST_FOCUS="${STAGING_INT_TEST_FOCUS:-${requested_profile}}"
      RESOLVED_INT_TEST_TO_SKIP_DEFAULT="${STAGING_INT_TEST_TO_SKIP_DEFAULT:-^$}"
      RESOLVED_INT_CLUSTER_NODES_DEFAULT="${STAGING_INT_CLUSTER_NODES:-1}"
      RESOLVED_INT_CLUSTER_WORKERS_DEFAULT="${STAGING_INT_CLUSTER_WORKERS:-3}"
      ;;
  esac
}

resolve_helm_test_profile() {
  requested_profile="$1"

  if [ -n "${STAGING_HELM_TEST_DIRS:-}" ]; then
    RESOLVED_HELM_TEST_PROFILE="${requested_profile:-custom}"
    RESOLVED_HELM_TEST_DIRS="${STAGING_HELM_TEST_DIRS}"
    RESOLVED_HELM_TEST_TIMEOUT="${STAGING_HELM_TEST_TIMEOUT:-7000}"
    RESOLVED_HELM_TEST_PARALLEL="${STAGING_HELM_TEST_PARALLEL:-1}"
    return 0
  fi

  case "${requested_profile}" in
    ""|smoke)
      RESOLVED_HELM_TEST_PROFILE="smoke"
      RESOLVED_HELM_TEST_DIRS="./kuttl/tests/helm/s1,./kuttl/tests/helm/s1-with-operator,./kuttl/tests/helm/operator-with-ephemeral-volume"
      RESOLVED_HELM_TEST_TIMEOUT="${STAGING_HELM_SMOKE_TIMEOUT:-4000}"
      RESOLVED_HELM_TEST_PARALLEL="${STAGING_HELM_SMOKE_PARALLEL:-1}"
      ;;
    clustered)
      RESOLVED_HELM_TEST_PROFILE="clustered"
      RESOLVED_HELM_TEST_DIRS="./kuttl/tests/helm/c3,./kuttl/tests/helm/c3-with-operator,./kuttl/tests/helm/m4,./kuttl/tests/helm/m4-with-operator"
      RESOLVED_HELM_TEST_TIMEOUT="${STAGING_HELM_CLUSTERED_TIMEOUT:-7000}"
      RESOLVED_HELM_TEST_PARALLEL="${STAGING_HELM_CLUSTERED_PARALLEL:-1}"
      ;;
    apps)
      RESOLVED_HELM_TEST_PROFILE="apps"
      RESOLVED_HELM_TEST_DIRS="./kuttl/tests/helm/c3-with-apps,./kuttl/tests/helm/c3-with-apps-private-link"
      RESOLVED_HELM_TEST_TIMEOUT="${STAGING_HELM_APPS_TIMEOUT:-7000}"
      RESOLVED_HELM_TEST_PARALLEL="${STAGING_HELM_APPS_PARALLEL:-1}"
      ;;
    full)
      RESOLVED_HELM_TEST_PROFILE="full"
      RESOLVED_HELM_TEST_DIRS="./kuttl/tests/helm"
      RESOLVED_HELM_TEST_TIMEOUT="${STAGING_HELM_FULL_TIMEOUT:-7000}"
      RESOLVED_HELM_TEST_PARALLEL="${STAGING_HELM_FULL_PARALLEL:-1}"
      ;;
    *)
      RESOLVED_HELM_TEST_PROFILE="${requested_profile}"
      RESOLVED_HELM_TEST_DIRS="./kuttl/tests/helm"
      RESOLVED_HELM_TEST_TIMEOUT="${STAGING_HELM_TEST_TIMEOUT:-7000}"
      RESOLVED_HELM_TEST_PARALLEL="${STAGING_HELM_TEST_PARALLEL:-1}"
      ;;
  esac
}

write_kuttl_testsuite_config() {
  output_path="$1"
  test_dirs_csv="$2"
  parallel_value="$3"
  timeout_value="$4"
  artifacts_dir="$5"

  {
    echo "# Generated by gitlab-ci/lib/pipeline-common.sh"
    echo "apiVersion: kuttl.dev/v1beta1"
    echo "kind: TestSuite"
    echo "testDirs:"
    old_ifs="${IFS}"
    IFS=','
    for raw_dir in ${test_dirs_csv}; do
      test_dir="$(trim_csv_field "${raw_dir}")"
      [ -z "${test_dir}" ] && continue
      echo "- ${test_dir}"
    done
    IFS="${old_ifs}"
    echo "parallel: ${parallel_value}"
    echo "timeout: ${timeout_value}"
    echo "startKIND: false"
    echo "artifactsDir: ${artifacts_dir}"
    echo "kindNodeCache: false"
  } > "${output_path}"
}
