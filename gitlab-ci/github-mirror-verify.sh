#!/usr/bin/env bash
#
# Verify a mirrored GitHub branch: the excluded GitLab-only paths must be
# absent from EVERY commit reachable from the branch tip, not just HEAD.
#
# Clones the GitHub target branch and greps the full history tree.
#
# Required env (App auth, to read a private/forbidden target if needed):
#   GITHUB_APP_ID, GITHUB_APP_PRIVATE_KEY_FILE
# Optional env:
#   GITHUB_APP_INSTALLATION_ID
#   MIRROR_TARGET_REPO   target GitHub repo. CI sets this to splunk/splunk-operator
#                        (gitlab-ci/includes/admin.yml); the default below is the
#                        test repo, used only for manual/local runs.
#   MIRROR_BRANCHES      default "main develop"
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
config_file="${script_dir}/gitlab-only-paths.conf"
target_repo="${MIRROR_TARGET_REPO:-adghadmin/sok-mirror-test}"
branches="${MIRROR_BRANCHES:-main develop}"

log() { printf '%s\n' "$*" >&2; }

EXCLUDES=()
while IFS= read -r line || [ -n "${line}" ]; do
  line="${line%%#*}"; line="${line#"${line%%[![:space:]]*}"}"; line="${line%"${line##*[![:space:]]}"}"
  [ -z "${line}" ] && continue
  EXCLUDES+=("${line%/}")
done < "${config_file}"

TOKEN="$("${script_dir}/github-app-token.sh")"
scratch="$(mktemp -d)"
cleanup() { rm -rf "${scratch}"; unset TOKEN AUTH_HEADER GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0; }
trap cleanup EXIT

# Inject the Basic-auth header via git's environment-based config so the
# installation token never appears on the process argv (see github-mirror-push.sh).
AUTH_HEADER="AUTHORIZATION: basic $(printf 'x-access-token:%s' "${TOKEN}" | base64 | tr -d '\n')"
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0='http.https://github.com/.extraheader'
export GIT_CONFIG_VALUE_0="${AUTH_HEADER}"

rc=0
for ref in ${branches}; do
  log ""
  log "=== verify ${ref} (full history) ==="
  work="${scratch}/${ref//\//_}"
  git clone --quiet --single-branch --branch "${ref}" \
    "https://github.com/${target_repo}.git" "${work}"

  commits="$(git -C "${work}" rev-list --count HEAD)"
  log "  ${commits} commits reachable"
  for path in "${EXCLUDES[@]}"; do
    # list every commit whose tree contains this path
    hits="$(git -C "${work}" log --all --oneline --format='%H' -- "${path}" 2>/dev/null | head -1)"
    if [ -z "${hits}" ]; then
      log "  ✓ ${path}: absent from all ${commits} commits"
    else
      log "  ✗ ${path}: STILL PRESENT (e.g. commit ${hits:0:8})"
      rc=1
    fi
  done
done

[ "${rc}" = "0" ] && log "" && log "VERIFY OK: no excluded paths anywhere in mirrored history" \
  || log "VERIFY FAILED"
exit "${rc}"
