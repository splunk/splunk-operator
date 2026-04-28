#!/bin/sh

# Shared cloud-runtime helpers
# - Keep Azure and GCP runtime wrappers thin and auditable.
# - Reuse the common GitLab pipeline helpers for registry and artifact logic.

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

log_step() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

env_present() {
  env_name="$1"
  env_value="$(printenv "${env_name}" 2>/dev/null || true)"
  [ -n "${env_value}" ]
}

prepare_runtime_artifacts() {
  context_file="$1"
  cleanup_log="$2"
  cluster_log="$3"
  build_log="$4"
  run_log="$5"
  pod_log_dir="$6"
  mkdir -p "$(dirname "${context_file}")" "$(dirname "${cleanup_log}")" "$(dirname "${cluster_log}")" "$(dirname "${build_log}")" "$(dirname "${run_log}")" "${pod_log_dir}"
  : > "${context_file}"
  : > "${cleanup_log}"
  : > "${cluster_log}"
  : > "${build_log}"
  : > "${run_log}"
}

capture_test_logs() {
  source_root="$1"
  dest_dir="$2"

  if [ -d "${source_root}" ]; then
    find "${source_root}" -name "*.log" -type f -exec cp {} "${dest_dir}/" \; >/dev/null 2>&1 || true
  fi
}

capture_junit_artifact() {
  src="$1"
  dest="$2"
  ensure_junit_artifact "${dest}" "${src}"
}

materialize_json_secret() {
  secret_value="$1"
  dest_path="$2"

  if printf '%s' "${secret_value}" | jq -e . >/dev/null 2>&1; then
    printf '%s\n' "${secret_value}" > "${dest_path}"
    return 0
  fi

  if printf '%s' "${secret_value}" | base64 -d >/dev/null 2>&1; then
    printf '%s' "${secret_value}" | base64 -d > "${dest_path}"
    return 0
  fi

  echo "Unable to interpret secret payload as JSON or base64-encoded JSON" >&2
  return 1
}

ensure_azure_cli() {
  if command -v az >/dev/null 2>&1; then
    return 0
  fi

  install_os_packages ca-certificates curl gnupg lsb-release apt-transport-https
  azure_apt_release="$(lsb_release -cs)"
  case "${azure_apt_release}" in
    bullseye|bookworm)
      ;;
    *)
      azure_apt_release="bookworm"
      ;;
  esac
  install -d -m 0755 /etc/apt/keyrings
  curl -fsSL https://packages.microsoft.com/keys/microsoft.asc | gpg --dearmor -o /etc/apt/keyrings/microsoft.gpg
  echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/microsoft.gpg] https://packages.microsoft.com/repos/azure-cli/ ${azure_apt_release} main" >/etc/apt/sources.list.d/azure-cli.list
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends azure-cli
}

ensure_gcloud_cli() {
  if command -v gcloud >/dev/null 2>&1; then
    return 0
  fi

  install_os_packages ca-certificates curl gnupg apt-transport-https
  install -d -m 0755 /etc/apt/keyrings
  curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | gpg --dearmor -o /etc/apt/keyrings/google-cloud-cli.gpg
  echo "deb [signed-by=/etc/apt/keyrings/google-cloud-cli.gpg] https://packages.cloud.google.com/apt cloud-sdk main" >/etc/apt/sources.list.d/google-cloud-sdk.list
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends google-cloud-cli google-cloud-cli-gke-gcloud-auth-plugin
}

azure_oidc_ready() {
  env_present GITLAB_OIDC_TOKEN &&
    env_present AZURE_CLIENT_ID &&
    env_present AZURE_TENANT_ID &&
    env_present AZURE_SUBSCRIPTION_ID
}

azure_login_oidc() {
  require_envs GITLAB_OIDC_TOKEN AZURE_CLIENT_ID AZURE_TENANT_ID AZURE_SUBSCRIPTION_ID
  az login --service-principal \
    --username "${AZURE_CLIENT_ID}" \
    --tenant "${AZURE_TENANT_ID}" \
    --federated-token "${GITLAB_OIDC_TOKEN}" >/dev/null
  az account set --subscription "${AZURE_SUBSCRIPTION_ID}" >/dev/null
}

gcp_oidc_ready() {
  env_present GITLAB_OIDC_TOKEN &&
    env_present GCP_WORKLOAD_IDENTITY_PROVIDER &&
    env_present GCP_SERVICE_ACCOUNT_EMAIL
}

gcp_login_oidc() {
  token_file="$1"
  cred_file="$2"

  require_envs GITLAB_OIDC_TOKEN GCP_WORKLOAD_IDENTITY_PROVIDER GCP_SERVICE_ACCOUNT_EMAIL
  printf '%s' "${GITLAB_OIDC_TOKEN}" > "${token_file}"
  gcloud iam workload-identity-pools create-cred-config \
    "${GCP_WORKLOAD_IDENTITY_PROVIDER}" \
    --service-account="${GCP_SERVICE_ACCOUNT_EMAIL}" \
    --credential-source-file="${token_file}" \
    --output-file="${cred_file}" >/dev/null
  gcloud auth login --cred-file="${cred_file}" --quiet >/dev/null
}
