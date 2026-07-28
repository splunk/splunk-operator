#!/bin/sh
set -eu

test_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "${test_dir}/../.." && pwd)"
test_tmp="$(mktemp -d)"
mock_bin="${test_tmp}/bin"
mock_docker_config="${test_tmp}/docker-config"
creds_helper_calls="${test_tmp}/creds-helper.calls"
docker_login_calls="${test_tmp}/docker-login.calls"

# Routing-only fixtures: these tests mock authentication and never resolve or
# pull either image.
artifactory_enterprise_test_image="docker.repo.splunkdev.net/test-fixtures/splunk-enterprise:test-only"
non_artifactory_enterprise_test_image="private.example.com/test-fixtures/splunk-enterprise:test-only"
public_operator_test_image="docker.io/test-fixtures/splunk-operator:test-only"
artifactory_operator_test_image="docker-hub.repo.splunkdev.net/test-fixtures/splunk-operator:test-only"

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

  login_enterprise_source_registry_if_needed "${artifactory_enterprise_test_image}"

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

  login_enterprise_source_registry_if_needed "${artifactory_enterprise_test_image}"

  assert_file_line "docker --eval artifactory:v2/cloud/role/docker-prod-read-role" "${creds_helper_calls}" "protected reader role"
  assert_file_empty "${docker_login_calls}" "Artifactory must not perform a second Docker login"
)

test_non_artifactory_registry_does_not_use_generic_credentials() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  unset DOCKER_CONFIG
  PIPELINE_DOCKER_USERNAME="generic-user"
  PIPELINE_DOCKER_PASSWORD="generic-password"

  login_enterprise_source_registry_if_needed "${non_artifactory_enterprise_test_image}"

  assert_file_empty "${creds_helper_calls}" "non-Artifactory registry"
  assert_file_empty "${docker_login_calls}" "generic credentials for non-Artifactory registry"
)

test_public_docker_hub_ignores_stale_optional_credentials() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  unset DOCKER_CONFIG SOK_ARTIFACTORY_DOCKER_READER_CREDS_READY
  PIPELINE_RELEASED_OPERATOR_REGISTRY_USERNAME="stale-user"
  PIPELINE_RELEASED_OPERATOR_REGISTRY_PASSWORD="stale-password"

  login_source_registry_for_image "${public_operator_test_image}"

  assert_file_empty "${creds_helper_calls}" "direct public Docker Hub source"
  assert_file_empty "${docker_login_calls}" "public Docker Hub source"
)

test_unprotected_docker_hub_proxy_uses_creds_helper() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  rm -rf "${mock_docker_config}"
  unset DOCKER_CONFIG SOK_ARTIFACTORY_DOCKER_READER_CREDS_READY
  CI_COMMIT_REF_PROTECTED="false"

  login_source_registry_for_image "${artifactory_operator_test_image}"

  assert_file_line "init" "${creds_helper_calls}" "Docker Hub proxy creds-helper initialization"
  assert_file_line "docker --eval artifactory:v2/cloud/role/docker-nonprod-read-role" "${creds_helper_calls}" "Docker Hub proxy reader role"
  assert_equal "${mock_docker_config}" "${DOCKER_CONFIG}" "Docker Hub proxy Docker config"
  assert_file_empty "${docker_login_calls}" "Docker Hub proxy must use creds-helper's Docker config"
)

test_artifactory_reader_credentials_are_reused_for_both_sources() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  rm -rf "${mock_docker_config}"
  unset DOCKER_CONFIG SOK_ARTIFACTORY_DOCKER_READER_CREDS_READY
  CI_COMMIT_REF_PROTECTED="true"

  login_enterprise_source_registry_if_needed "${artifactory_enterprise_test_image}"
  login_source_registry_for_image "${artifactory_operator_test_image}"

  helper_count="$(grep -Fxc "docker --eval artifactory:v2/cloud/role/docker-prod-read-role" "${creds_helper_calls}")"
  assert_equal "1" "${helper_count}" "shared Artifactory reader initialization"
)

test_proxy_creds_helper_failure_is_propagated() (
  : > "${creds_helper_calls}"
  : > "${docker_login_calls}"
  rm -rf "${mock_docker_config}"
  unset DOCKER_CONFIG PIPELINE_DOCKER_USERNAME PIPELINE_DOCKER_PASSWORD SOK_ARTIFACTORY_DOCKER_READER_CREDS_READY
  CI_COMMIT_REF_PROTECTED="false"
  MOCK_CREDS_HELPER_DOCKER_FAIL="true"
  export MOCK_CREDS_HELPER_DOCKER_FAIL

  if login_source_registry_for_image "${artifactory_operator_test_image}" 2>/dev/null; then
    fail "Docker Hub proxy creds-helper failure should fail authentication"
  fi
  assert_file_empty "${docker_login_calls}" "failed creds-helper authentication"
)

test_unprotected_artifactory_uses_creds_helper_docker_config
test_protected_artifactory_uses_prod_role
test_non_artifactory_registry_does_not_use_generic_credentials
test_public_docker_hub_ignores_stale_optional_credentials
test_unprotected_docker_hub_proxy_uses_creds_helper
test_artifactory_reader_credentials_are_reused_for_both_sources
test_proxy_creds_helper_failure_is_propagated

printf '%s\n' "PASS: pipeline-common registry authentication tests"
