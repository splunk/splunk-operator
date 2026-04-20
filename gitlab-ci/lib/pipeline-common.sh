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

ensure_trivy_scan_tooling() {
  install_os_packages bash curl jq python3 python3-pip python3-venv tar
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
