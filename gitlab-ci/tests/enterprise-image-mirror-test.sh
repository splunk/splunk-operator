#!/bin/bash
set -euo pipefail

test_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "${test_dir}/../.." && pwd)"
test_tmp="$(mktemp -d)"
mock_bin="${test_tmp}/bin"
docker_calls="${test_tmp}/docker.calls"
helper_stderr="${test_tmp}/helper.stderr"

cleanup() {
  rm -rf "${test_tmp}"
}
trap cleanup EXIT INT TERM

mkdir -p "${mock_bin}"
: > "${docker_calls}"

cat > "${mock_bin}/docker" <<'EOF'
#!/bin/bash
set -euo pipefail

printf '%s\n' "$*" >> "${MOCK_DOCKER_CALLS}"

if [[ "${1:-}" == "manifest" && "${2:-}" == "inspect" ]]; then
  if [[ "${MOCK_MANIFEST_EXISTS:-false}" == "true" ]]; then
    printf '%s\n' "mock manifest"
    exit 0
  fi
  exit 1
fi

if [[ "${MOCK_DOCKER_FAIL_COMMAND:-}" == "${1:-}" ]]; then
  echo "mock ${1} failure" >&2
  exit 42
fi

# Deliberately write to stdout; the helper must keep this out of its result.
printf 'mock docker output: %s\n' "$*"
EOF
chmod +x "${mock_bin}/docker"

PATH="${mock_bin}:${PATH}"
MOCK_DOCKER_CALLS="${docker_calls}"
export PATH MOCK_DOCKER_CALLS

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_equal() {
  local expected="$1"
  local actual="$2"
  local description="$3"
  if [[ "${actual}" != "${expected}" ]]; then
    fail "${description}: expected '${expected}', got '${actual}'"
  fi
}

assert_file_line() {
  local expected="$1"
  local file="$2"
  local description="$3"
  if ! grep -Fqx -- "${expected}" "${file}"; then
    fail "${description}: missing '${expected}' in ${file}"
  fi
}

assert_file_has_no_line() {
  local unexpected="$1"
  local file="$2"
  local description="$3"
  if grep -Fqx -- "${unexpected}" "${file}"; then
    fail "${description}: found unexpected '${unexpected}' in ${file}"
  fi
}

run_helper() {
  bash "${repo_root}/test/get-private-registry-enterprise.sh"
}

test_internal_source_uses_stable_private_repository() (
  : > "${docker_calls}"
  export SPLUNK_ENTERPRISE_IMAGE="docker.repo.splunkdev.net/eng-effectiveness/docker-splunk/dev/sustain/splunk-10.4/splunk-redhat-8-amd64-10.4.0:build-123"
  export PRIVATE_REGISTRY="123456789.dkr.ecr.us-west-2.amazonaws.com"
  export ARM64="false"
  export MOCK_MANIFEST_EXISTS="false"
  unset MOCK_DOCKER_FAIL_COMMAND

  actual="$(run_helper 2> "${helper_stderr}")"
  expected="${PRIVATE_REGISTRY}/splunk/splunk:build-123"

  assert_equal "${expected}" "${actual}" "resolved private Enterprise image"
  assert_file_line "manifest inspect ${expected}" "${docker_calls}" "destination manifest check"
  assert_file_line "pull ${SPLUNK_ENTERPRISE_IMAGE}" "${docker_calls}" "source pull"
  assert_file_line "tag ${SPLUNK_ENTERPRISE_IMAGE} ${expected}" "${docker_calls}" "stable destination tag"
  assert_file_line "push ${expected}" "${docker_calls}" "stable destination push"
)

test_public_source_keeps_existing_destination_contract() (
  : > "${docker_calls}"
  export SPLUNK_ENTERPRISE_IMAGE="splunk/splunk:10.4.0"
  export PRIVATE_REGISTRY="registry.example.com"
  export ARM64="false"
  export MOCK_MANIFEST_EXISTS="true"
  unset MOCK_DOCKER_FAIL_COMMAND

  actual="$(run_helper 2> "${helper_stderr}")"
  expected="registry.example.com/splunk/splunk:10.4.0"

  assert_equal "${expected}" "${actual}" "public source destination"
  assert_file_line "pull ${expected}" "${docker_calls}" "existing destination pull"
  assert_file_has_no_line "pull ${SPLUNK_ENTERPRISE_IMAGE}" "${docker_calls}" "source should not be pulled twice"
)

test_arm64_source_is_not_remapped() (
  : > "${docker_calls}"
  export SPLUNK_ENTERPRISE_IMAGE="registry.example.com/splunk/splunk:arm64"
  export PRIVATE_REGISTRY="another-registry.example.com"
  export ARM64="true"
  export MOCK_MANIFEST_EXISTS="true"
  unset MOCK_DOCKER_FAIL_COMMAND

  actual="$(run_helper 2> "${helper_stderr}")"

  assert_equal "${SPLUNK_ENTERPRISE_IMAGE}" "${actual}" "ARM64 image"
  assert_file_line "manifest inspect ${SPLUNK_ENTERPRISE_IMAGE}" "${docker_calls}" "ARM64 source manifest check"
)

test_push_failure_is_propagated_without_stdout_pollution() (
  : > "${docker_calls}"
  export SPLUNK_ENTERPRISE_IMAGE="docker.repo.splunkdev.net/team/splunk:failed-build"
  export PRIVATE_REGISTRY="registry.example.com"
  export ARM64="false"
  export MOCK_MANIFEST_EXISTS="false"
  export MOCK_DOCKER_FAIL_COMMAND="push"

  if actual="$(run_helper 2> "${helper_stderr}")"; then
    fail "failed Docker push should fail the helper"
  fi

  assert_equal "" "${actual}" "failed helper stdout"
)

test_internal_source_uses_stable_private_repository
test_public_source_keeps_existing_destination_contract
test_arm64_source_is_not_remapped
test_push_failure_is_propagated_without_stdout_pollution

printf '%s\n' "PASS: Enterprise image mirror tests"
