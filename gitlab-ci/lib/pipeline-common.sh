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
    docker.io/*)
      printf '%s' "${image_ref#docker.io/}"
      ;;
    *)
      printf '%s' "${image_ref}"
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

install_os_packages() {
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@"
}

ensure_python_venv_tooling() {
  install_os_packages python3 python3-pip python3-venv
}

require_file() {
  path="$1"
  description="$2"
  if [ ! -f "$path" ]; then
    echo "Missing required file: ${description} (${path})" >&2
    return 1
  fi
}

resolve_staging_image_repository() {
  staging_target="$1"
  default_repo_path="$2"

  if [ -z "${staging_target}" ]; then
    echo "STAGING_ECR_REPOSITORY must be set" >&2
    return 1
  fi

  case "${staging_target}" in
    */*)
      RESOLVED_ECR_REGISTRY="${staging_target%%/*}"
      RESOLVED_IMAGE_REPOSITORY="${staging_target}"
      RESOLVED_IMAGE_REPOSITORY_MODE="explicit-repository"
      RESOLVED_REPOSITORY_NAME="${staging_target#${RESOLVED_ECR_REGISTRY}/}"
      ;;
    *)
      RESOLVED_ECR_REGISTRY="${staging_target}"
      RESOLVED_IMAGE_REPOSITORY="${staging_target}/${default_repo_path}"
      RESOLVED_IMAGE_REPOSITORY_MODE="registry-only"
      RESOLVED_REPOSITORY_NAME="${default_repo_path}"
      ;;
  esac
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
