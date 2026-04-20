#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

DOCKERFILE_PATH="${REPO_ROOT}/Dockerfile"
MAKEFILE_PATH="${REPO_ROOT}/Makefile"
PR_TEMPLATE_PATH="${REPO_ROOT}/.github/pull_request_template.md"
PR_BODY_OUTPUT_PATH="${PR_BODY_OUTPUT_PATH:-${RUNNER_TEMP:-/tmp}/ubi8-minimal-pr-body.md}"
REDHAT_API_URL="${REDHAT_API_URL:-https://catalog.redhat.com/api/containers/v1/repositories/registry/registry.access.redhat.com/repository/ubi8/ubi-minimal/tag/latest?page_size=1&sort_by=creation_date%5Bdesc%5D}"
TARGET_REPOSITORY="${TARGET_REPOSITORY:-ubi8/ubi-minimal}"
DRY_RUN="${DRY_RUN:-false}"

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Required command not found: $1" >&2
        exit 1
    fi
}

get_current_version() {
    sed -n 's/^ARG BASE_IMAGE_VERSION=\(.*\)$/\1/p' "${DOCKERFILE_PATH}"
}

fetch_latest_version() {
    if [[ -n "${LATEST_VERSION_OVERRIDE:-}" ]]; then
        printf '%s\n' "${LATEST_VERSION_OVERRIDE}"
        return
    fi

    curl -fsSL "${REDHAT_API_URL}" | jq -r --arg repo "${TARGET_REPOSITORY}" '
        [
            .data[].repositories[]
            | select(.repository == $repo)
            | .tags[].name
            | select(test("^[0-9]+\\.[0-9]+-[0-9]+$"))
        ][0] // empty
    '
}

update_versioned_files() {
    local new_version="$1"

    perl -0pi -e "s/^ARG BASE_IMAGE_VERSION=.*/ARG BASE_IMAGE_VERSION=${new_version}/m" "${DOCKERFILE_PATH}"
    perl -0pi -e "s/^(#   Build Base OS Version: ).*/\${1}${new_version}/m" "${MAKEFILE_PATH}"
    perl -0pi -e "s/^(BASE_IMAGE_VERSION \\?= ).*/\${1}${new_version}/m" "${MAKEFILE_PATH}"
}

render_pr_body() {
    local old_version="$1"
    local new_version="$2"

    mkdir -p "$(dirname "${PR_BODY_OUTPUT_PATH}")"

    DESCRIPTION_TEXT=$'Automated UBI 8 minimal base image refresh from `'"${old_version}"$'` to `'"${new_version}"$'`.\n\nThis updates the pinned Red Hat base image tag anywhere it is intentionally maintained in the repo today.'
    KEY_CHANGES_TEXT=$'- `Dockerfile`: updates `ARG BASE_IMAGE_VERSION` to `'"${new_version}"$'`.\n- `Makefile`: updates the `BASE_IMAGE_VERSION` default and the related build comment to `'"${new_version}"$'`.'
    TESTING_TEXT=$'- All unit and smoke tests will be run with the PR creation.'
    RELATED_ISSUES_TEXT='N/A'

    export DESCRIPTION_TEXT KEY_CHANGES_TEXT TESTING_TEXT RELATED_ISSUES_TEXT

    perl -0pe '
        s{_What does this PR have in it\?_}{$ENV{DESCRIPTION_TEXT}}s;
        s{_Highlight the updates in specific files_}{$ENV{KEY_CHANGES_TEXT}}s;
        s{_How did you test these changes\? What automated tests are added\?_}{$ENV{TESTING_TEXT}}s;
        s{_Jira tickets, GitHub issues, Support tickets\.\.\._}{$ENV{RELATED_ISSUES_TEXT}}s;
        s/- \[ \] Code changes adhere to the project'\''s coding standards\./- [x] Code changes adhere to the project'\''s coding standards./s;
        s/- \[ \] Relevant unit and integration tests are included\./- [ ] Relevant unit and integration tests are included./s;
        s/- \[ \] Documentation has been updated accordingly\./- [ ] Documentation has been updated accordingly./s;
        s/- \[ \] If test framework files were changed \(`test\/testenv\/`, `test\/run-tests\.sh`, `test\/env\.sh`\), `docs\/IntegrationTesting\.md` has been updated/- [ ] If test framework files were changed (`test\/testenv\/`, `test\/run-tests.sh`, `test\/env.sh`), `docs\/IntegrationTesting.md` has been updated/s;
        s/- \[ \] All tests pass locally\./- [ ] All tests pass locally./s;
        s/- \[ \] The PR description follows the project'\''s guidelines\./- [x] The PR description follows the project'\''s guidelines./s;
    ' "${PR_TEMPLATE_PATH}" > "${PR_BODY_OUTPUT_PATH}"
}

write_github_outputs() {
    local current_version="$1"
    local latest_version="$2"
    local update_needed="$3"

    if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
        {
            echo "current_version=${current_version}"
            echo "latest_version=${latest_version}"
            echo "update_needed=${update_needed}"
            echo "pr_body_path=${PR_BODY_OUTPUT_PATH}"
        } >> "${GITHUB_OUTPUT}"
    fi
}

main() {
    require_command curl
    require_command jq
    require_command perl

    local current_version latest_version
    current_version="$(get_current_version)"
    latest_version="$(fetch_latest_version)"

    if [[ -z "${current_version}" ]]; then
        echo "Could not determine the current base image version from ${DOCKERFILE_PATH}" >&2
        exit 1
    fi

    if [[ -z "${latest_version}" ]]; then
        echo "Could not determine the latest base image version from Red Hat catalog output" >&2
        exit 1
    fi

    local update_needed="false"
    if [[ "${current_version}" != "${latest_version}" ]]; then
        update_needed="true"
    fi

    if [[ "${DRY_RUN}" != "true" ]]; then
        update_versioned_files "${latest_version}"
        render_pr_body "${current_version}" "${latest_version}"
    fi

    write_github_outputs "${current_version}" "${latest_version}" "${update_needed}"

    printf 'Current version: %s\n' "${current_version}"
    printf 'Latest version: %s\n' "${latest_version}"
    printf 'Update needed: %s\n' "${update_needed}"
    printf 'PR body path: %s\n' "${PR_BODY_OUTPUT_PATH}"
}

main "$@"
