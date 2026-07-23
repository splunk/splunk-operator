#!/bin/sh
set -eu

test_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "${test_dir}/../.." && pwd)"
test_tmp="$(mktemp -d)"
mock_bin="${test_tmp}/bin"
mock_docker_config="${test_tmp}/docker-config"
creds_helper_calls="${test_tmp}/creds-helper.calls"
docker_login_calls="${test_tmp}/docker-login.calls"

cleanup() {
  rm -rf "${test_tmp}"
}
trap cleanup EXIT INT TERM

mkdir -p "${mock_bin}"
: > "${creds_helper_calls}"
: > "${docker_login_calls}"

cat > "${mock_bin}/creds-helper" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "$*" >> "${MOCK_CREDS_HELPER_CALLS}"
case "${1:-}" in
  init)
    ;;
  docker)
    if [ "${MOCK_CREDS_HELPER_DOCKER_FAIL:-false}" = "true" ]; then
      exit 23
    fi
    mkdir -p "${MOCK_DOCKER_CONFIG}"
    printf 'DOCKER_CONFIG=$MOCK_DOCKER_CONFIG; export DOCKER_CONFIG\n'
    ;;
  *)
    echo "Unexpected creds-helper command: $*" >&2
    exit 1
    ;;
esac
EOF
chmod +x "${mock_bin}/creds-helper"

PATH="${mock_bin}:${PATH}"
MOCK_CREDS_HELPER_CALLS="${creds_helper_calls}"
MOCK_DOCKER_CONFIG="${mock_docker_config}"
MOCK_DOCKER_LOGIN_CALLS="${docker_login_calls}"
export PATH MOCK_CREDS_HELPER_CALLS MOCK_DOCKER_CONFIG MOCK_DOCKER_LOGIN_CALLS

. "${repo_root}/gitlab-ci/lib/pipeline-common.sh"

log_step() {
  :
}

docker_login_registry() {
  printf '%s|%s|%s\n' "$1" "$2" "$3" >> "${MOCK_DOCKER_LOGIN_CALLS}"
}

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_equal() {
  expected="$1"
  actual="$2"
  description="$3"
  if [ "${actual}" != "${expected}" ]; then
    fail "${description}: expected '${expected}', got '${actual}'"
  fi
}

assert_file_line() {
  expected="$1"
  file="$2"
  description="$3"
  if ! grep -Fqx -- "${expected}" "${file}"; then
    fail "${description}: missing '${expected}' in ${file}"
  fi
}

assert_file_empty() {
  file="$1"
  description="$2"
  if [ -s "${file}" ]; then
    fail "${description}: unexpected contents in ${file}"
  fi
}

test_unprotected_artifactory_uses_creds_helper_docker_config() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  rm -rf "${mock_docker_config}"
  unset DOCKER_CONFIG
  CI_COMMIT_REF_PROTECTED="false"
  PIPELINE_DOCKER_USERNAME="stale-generic-user"
  PIPELINE_DOCKER_PASSWORD="stale-generic-password"

  login_enterprise_source_registry_if_needed "docker.repo.splunkdev.net/splunk/splunk:10.0.0"

  assert_file_line "init" "${creds_helper_calls}" "creds-helper initialization"
  assert_file_line "docker --eval artifactory:v2/cloud/role/docker-nonprod-read-role" "${creds_helper_calls}" "unprotected reader role"
  assert_equal "${mock_docker_config}" "${DOCKER_CONFIG}" "Docker config exported by creds-helper"
  assert_file_empty "${docker_login_calls}" "Artifactory must use creds-helper's Docker config"
  assert_equal "stale-generic-user" "${PIPELINE_DOCKER_USERNAME}" "generic credentials remain untouched"
)

test_protected_artifactory_uses_prod_role() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  rm -rf "${mock_docker_config}"
  unset DOCKER_CONFIG PIPELINE_DOCKER_USERNAME PIPELINE_DOCKER_PASSWORD
  CI_COMMIT_REF_PROTECTED="true"

  login_enterprise_source_registry_if_needed "docker.repo.splunkdev.net/splunk/splunk:10.0.0"

  assert_file_line "docker --eval artifactory:v2/cloud/role/docker-prod-read-role" "${creds_helper_calls}" "protected reader role"
  assert_file_empty "${docker_login_calls}" "Artifactory must not perform a second Docker login"
)

test_non_artifactory_registry_does_not_use_generic_credentials() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  unset DOCKER_CONFIG
  PIPELINE_DOCKER_USERNAME="generic-user"
  PIPELINE_DOCKER_PASSWORD="generic-password"

  login_enterprise_source_registry_if_needed "private.example.com/splunk/splunk:10.0.0"

  assert_file_empty "${creds_helper_calls}" "non-Artifactory registry"
  assert_file_empty "${docker_login_calls}" "generic credentials for non-Artifactory registry"
)

test_public_docker_hub_ignores_stale_optional_credentials() (
  : > "${docker_login_calls}"
  PIPELINE_RELEASED_OPERATOR_REGISTRY_USERNAME="stale-user"
  PIPELINE_RELEASED_OPERATOR_REGISTRY_PASSWORD="stale-password"

  login_source_registry_for_image "docker.io/splunk/splunk-operator:3.1.0"

  assert_file_empty "${docker_login_calls}" "public Docker Hub source"
)

test_creds_helper_failure_is_propagated() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  rm -rf "${mock_docker_config}"
  unset DOCKER_CONFIG PIPELINE_DOCKER_USERNAME PIPELINE_DOCKER_PASSWORD
  CI_COMMIT_REF_PROTECTED="false"
  MOCK_CREDS_HELPER_DOCKER_FAIL="true"
  export MOCK_CREDS_HELPER_DOCKER_FAIL

  if login_enterprise_source_registry_if_needed "docker.repo.splunkdev.net/splunk/splunk:10.0.0" 2>/dev/null; then
    fail "creds-helper Docker failure should fail authentication"
  fi
  assert_file_empty "${docker_login_calls}" "failed creds-helper authentication"
)

test_unprotected_artifactory_uses_creds_helper_docker_config
test_protected_artifactory_uses_prod_role
test_non_artifactory_registry_does_not_use_generic_credentials
test_public_docker_hub_ignores_stale_optional_credentials
test_creds_helper_failure_is_propagated

printf '%s\n' "PASS: pipeline-common registry authentication tests"
