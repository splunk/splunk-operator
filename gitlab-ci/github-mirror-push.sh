#!/usr/bin/env bash
#
# Production GitLab -> GitHub mirror push (one-way; GitLab is authoritative).
#
# For each mirrored branch, a fresh single-branch clone of the GitLab
# source is sanitized with git-filter-repo to strip the GitLab-only paths,
# then pushed to the GitHub target as the GitHub App. The push is a plain
# fast-forward in steady state; a non-fast-forward (first cutover or an
# exclusion-list change) is refused unless MIRROR_ALLOW_NON_FF=1.
#
# Scoped rewrite: the GitLab-only paths were introduced partway through
# develop's history, not at its root. filter-repo therefore rewrites only
# the range from the EARLIEST commit that introduces an excluded path
# through the tip (--partial --refs ^<boundary-parents> HEAD); every older
# commit never held an excluded path and is left untouched with its original
# upstream SHA. A whole-branch rewrite would re-mint every ancestor too —
# filter-repo drops GPG signatures, so even unchanged, path-free commits get
# new SHAs — cascading new SHAs through all of history; scoping confines that
# churn to the boundary-and-after range and preserves the bulk of the SHAs.
#
# The strip is by-range but the guarantee is whole-history: because the
# boundary is the earliest commit that ever held an excluded path, the
# excluded paths end up unreachable from EVERY published commit, which
# github-mirror-verify.sh asserts independently.
#
# Branches with none of the excluded paths anywhere (e.g. main) skip
# filter-repo entirely, so their commit SHAs are preserved and the push is a
# plain fast-forward.
#
# Required env (see github-app-token.sh):
#   GITHUB_APP_ID, GITHUB_APP_PRIVATE_KEY_FILE
# Optional env:
#   GITHUB_APP_INSTALLATION_ID   auto-discovered if unset
#   MIRROR_TARGET_REPO   target GitHub repo. CI sets this to splunk/splunk-operator
#                        (gitlab-ci/includes/admin.yml); the default below is the
#                        test repo, used only for manual/local runs.
#   MIRROR_BRANCHES      space-separated; default "main develop"
#   MIRROR_SOURCE_URL    default = origin remote URL of this repo (GitLab)
#   GITHUB_API           default https://api.github.com
#   MIRROR_ALLOW_NON_FF  set to 1 to permit a non-fast-forward (force) update
#                        that rewrites already-published GitHub history. Off by
#                        default: steady-state runs are fast-forwards and never
#                        need it. Required only for the first cutover or an
#                        exclusion-list change, which must be drained first
#                        (see the open-PR mirror runbook).
#
# Secret hygiene: both credentials — the GitHub App installation token AND any
# inline credential carried by the GitLab source URL (e.g. the CI clone token
# in CI_REPOSITORY_URL) — live only in variables and are injected into git as
# host-scoped http.extraHeader entries via environment-based config
# (GIT_CONFIG_*). Neither ever appears on the process argv (ps /
# /proc/<pid>/cmdline) or in .git/config on disk; the GitLab header is scoped
# to its own host so the source token is never sent to GitHub. All scratch
# state and the tokens are scrubbed on exit.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
config_file="${script_dir}/gitlab-only-paths.conf"

target_repo="${MIRROR_TARGET_REPO:-adghadmin/sok-mirror-test}"
branches="${MIRROR_BRANCHES:-main develop}"
source_url="${MIRROR_SOURCE_URL:-$(git -C "${repo_root}" remote get-url origin)}"
api="${GITHUB_API:-https://api.github.com}"

log() { printf '%s\n' "$*" >&2; }
# Strip any "user[:password]@" userinfo so an embedded credential (e.g. the
# CI clone token in CI_REPOSITORY_URL) is never written to the job log.
redact_url() { printf '%s' "$1" | sed -E 's|(://)[^/@]*@|\1<redacted>@|'; }
# Remove the "user[:password]@" userinfo entirely, yielding a credential-free
# URL that is safe to place on the `git clone` argv.
strip_userinfo() { printf '%s' "$1" | sed -E 's|(://)[^/@]*@|\1|'; }
# Print just the "user[:password]" userinfo of a URL (empty when there is none).
url_userinfo() { printf '%s' "$1" | sed -nE 's|^[a-zA-Z][a-zA-Z0-9+.-]*://([^/@]+)@.*$|\1|p'; }
# Print the "scheme://host[:port]" prefix of a URL (no path, no userinfo) — the
# form git matches http.<url>.extraheader against.
url_prefix() { printf '%s' "$1" | sed -nE 's#^([a-zA-Z][a-zA-Z0-9+.-]*://)([^/@]+@)?([^/]+).*#\1\3#p'; }
command -v git-filter-repo >/dev/null 2>&1 || { log "ERROR: git-filter-repo not installed"; exit 1; }

# --- parse exclusion config (shared with validate-no-private-refs.sh) ---
[ -f "${config_file}" ] || { log "ERROR: missing ${config_file}"; exit 1; }
EXCLUDES=()
while IFS= read -r line || [ -n "${line}" ]; do
  line="${line%%#*}"
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  [ -z "${line}" ] && continue
  EXCLUDES+=("${line%/}")   # filter-repo path form: no trailing slash
done < "${config_file}"
[ "${#EXCLUDES[@]}" -gt 0 ] || { log "ERROR: no excludes parsed"; exit 1; }
log "Stripping from every commit: ${EXCLUDES[*]}"
log "Source: $(redact_url "${source_url}")"
log "Target: ${target_repo}  branches: ${branches}"

# --- mint installation token once (never printed) ---
log "Minting App installation token..."
TOKEN="$("${script_dir}/github-app-token.sh")"
log "Token minted (len ${#TOKEN}, withheld)."

scratch="$(mktemp -d)"
cleanup() {
  rm -rf "${scratch}"
  unset TOKEN AUTH_HEADER SRC_AUTH_HEADER \
        GIT_CONFIG_COUNT \
        GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0 \
        GIT_CONFIG_KEY_1 GIT_CONFIG_VALUE_1
}
trap cleanup EXIT

# Inject the GitHub App Basic-auth header through git's environment-based config
# rather than `-c` on the command line, so the installation token never lands on
# the process argv (readable via ps / /proc/<pid>/cmdline by same-user processes).
AUTH_HEADER="AUTHORIZATION: basic $(printf 'x-access-token:%s' "${TOKEN}" | base64 | tr -d '\n')"
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0='http.https://github.com/.extraheader'
export GIT_CONFIG_VALUE_0="${AUTH_HEADER}"

# If the GitLab source URL carries an inline credential (the usual CI case,
# CI_REPOSITORY_URL = https://gitlab-ci-token:<token>@host/...), keep it off the
# clone argv too: strip the userinfo from the URL handed to `git clone`, and
# re-inject the credential as a SECOND extraheader scoped to the GitLab host, so
# it is sent only there and never leaks to the GitHub target.
src_userinfo="$(url_userinfo "${source_url}")"
if [ -n "${src_userinfo}" ]; then
  src_prefix="$(url_prefix "${source_url}")"
  SRC_AUTH_HEADER="AUTHORIZATION: basic $(printf '%s' "${src_userinfo}" | base64 | tr -d '\n')"
  export GIT_CONFIG_KEY_1="http.${src_prefix}/.extraheader"
  export GIT_CONFIG_VALUE_1="${SRC_AUTH_HEADER}"
  export GIT_CONFIG_COUNT=2
  source_url="$(strip_userinfo "${source_url}")"
fi

fr_path_args=()
for p in "${EXCLUDES[@]}"; do fr_path_args+=(--path "${p}"); done

rc=0
for ref in ${branches}; do
  log ""
  log "=== ${ref} ==="
  work="${scratch}/${ref//\//_}"
  git clone --quiet --single-branch --branch "${ref}" "${source_url}" "${work}"

  before="$(git -C "${work}" rev-parse HEAD)"

  # Scoped rewrite: rewrite only from the EARLIEST commit that introduces an
  # excluded path through the tip.
  matching="$(git -C "${work}" log HEAD --full-history --topo-order --reverse \
                --format='%H' -- "${EXCLUDES[@]}")"
  earliest="${matching%%$'\n'*}"

  if [ -z "${earliest}" ]; then
    after="${before}"
    log "  no excluded paths in history (${before:0:8}) — original commits (SHAs preserved)"
  else
    # Rewrite exactly `earliest` and its descendants: exclude EVERY parent of
    # `earliest`.
    read -r _ parents <<<"$(git -C "${work}" rev-list --parents -n1 "${earliest}")"
    refs_args=()
    nparents=0
    for p in ${parents}; do refs_args+=("^${p}"); nparents=$((nparents + 1)); done
    refs_args+=(HEAD)
    if [ "${nparents}" -gt 0 ]; then
      log "  earliest private-path commit ${earliest:0:8} (${nparents}-parent); preserving all ancestors"
    else
      log "  earliest private-path commit ${earliest:0:8} is the root — rewriting full history"
    fi
    git -C "${work}" filter-repo --force --partial --refs "${refs_args[@]}" \
      --invert-paths "${fr_path_args[@]}" >/dev/null
    after="$(git -C "${work}" rev-parse HEAD)"
    log "  history rewritten ${before:0:8} → ${after:0:8} (private paths stripped; ancestors preserved)"
  fi

  # --- forward-moving guard -------------------------------------------------
  # Compare our HEAD against the current GitHub tip and pick a push mode:
  #   first  : ref absent on GitHub                       -> plain push
  #   ff     : GitHub tip is an ANCESTOR of our HEAD       -> plain push (no --force)
  #   nonff  : histories diverge (published history rebuilt)
  #            -> REFUSED unless MIRROR_ALLOW_NON_FF=1, then --force
  #
  # In steady state filter-repo is deterministic, so even a rewritten develop
  # stays a fast-forward over the previously-mirrored tip (push_mode=ff) and
  # needs no force. A non-fast-forward only arises on first cutover or an
  # exclusion-list change, which must be an explicit, drained operation.
  if git -C "${work}" \
       fetch --quiet "https://github.com/${target_repo}.git" \
       "refs/heads/${ref}:refs/remotes/ghtarget/${ref}" 2>/dev/null; then
    gh_tip="$(git -C "${work}" rev-parse "refs/remotes/ghtarget/${ref}")"
    if git -C "${work}" merge-base --is-ancestor "${gh_tip}" HEAD; then
      push_mode="ff"
      log "  GitHub ${ref}@${gh_tip:0:8} is an ancestor of ${after:0:8} — fast-forward"
    else
      push_mode="nonff"
      log "  GitHub ${ref}@${gh_tip:0:8} is NOT an ancestor of ${after:0:8} — non-fast-forward (published history would be rebuilt)"
    fi
  else
    push_mode="first"
    log "  GitHub ${ref} does not exist yet — first push"
  fi

  if [ "${push_mode}" = "nonff" ] && [ "${MIRROR_ALLOW_NON_FF:-0}" != "1" ]; then
    log "  REFUSED: non-fast-forward update to '${ref}' would rewrite already-published history."
    log "           Expected only on first cutover or an exclusion-list change."
    log "           Drain/notify open PRs (see the mirror runbook), then re-run with MIRROR_ALLOW_NON_FF=1."
    rc=1
    continue
  fi

  push_flags=()
  [ "${push_mode}" = "nonff" ] && push_flags=(--force)

  if git -C "${work}" \
       push ${push_flags[@]+"${push_flags[@]}"} "https://github.com/${target_repo}.git" "HEAD:refs/heads/${ref}"; then
    log "  PUSH OK (${push_mode})"
  else
    log "  PUSH FAILED"; rc=1
  fi
done

exit "${rc}"
