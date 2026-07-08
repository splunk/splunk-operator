#!/bin/sh
set -eu

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

trim_csv_field() {
  printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

# GitLab-only path prefixes that the mirror push strips from every commit. The
# health check must subtract the SAME set from the GitLab side before comparing,
# because comparing raw commit SHAs is wrong here: filter-repo rewrites every
# commit on any branch that carries an excluded path (develop), so its GitHub
# SHA never equals GitLab's even when the mirror is healthy.
EXCLUDE_PREFIXES=""
EXCLUDE_RE=""
load_exclude_prefixes() {
  _conf="${CI_PROJECT_DIR}/gitlab-ci/gitlab-only-paths.conf"
  if [ ! -f "${_conf}" ]; then
    printf 'ERROR: missing config file: %s\n' "${_conf}" >&2
    exit 1
  fi
  _raw="$(sed 's/#.*$//' "${_conf}")"
  if [ -z "$(printf '%s' "${_raw}" | tr -d '[:space:]')" ]; then
    printf 'ERROR: no paths parsed from %s\n' "${_conf}" >&2
    exit 1
  fi
  _alt=""
  for _p in ${_raw}; do
    _p="${_p%/}"
    [ -z "${_p}" ] && continue
    EXCLUDE_PREFIXES="${EXCLUDE_PREFIXES}${_p} "
    _esc="$(printf '%s' "${_p}" | sed 's/[.[\\*^$]/\\&/g')"
    if [ -z "${_alt}" ]; then _alt="${_esc}"; else _alt="${_alt}|${_esc}"; fi
  done
  # normalized_tree emits "path<TAB>blobsha", so a matched prefix is followed by
  # '/' (a child under an excluded dir), a TAB (the prefix IS the whole path —
  # e.g. a file entry like .gitlab-ci.yml), or end-of-line. Without the TAB
  # terminator a bare file entry would never match and never be stripped.
  _tab="$(printf '\t')"
  EXCLUDE_RE="^(${_alt})(/|${_tab}|$)"
}

# Emit a normalized, C-sorted "path<TAB>blobsha" listing for a local commit-ish.
# With $2 = "strip", drop every path at or under an excluded prefix. The strip is
# applied to the GitLab side ONLY, so a private dir that ever leaks onto GitHub
# shows up as a divergence instead of being masked. Needs only trees + blob
# names, so a blobless fetch (--filter=blob:none) suffices; blob contents are
# never read. Blob SHAs are content-addressed and so are directly comparable
# across the two hosts even though the enclosing commit SHAs differ.
normalized_tree() {
  _sha="$1"; _mode="${2:-keep}"
  git ls-tree -r "${_sha}" \
    | awk -F'\t' '{ split($1, a, " "); print $2 "\t" a[3] }' \
    | if [ "${_mode}" = "strip" ]; then grep -vE "${EXCLUDE_RE}"; else cat; fi \
    | LC_ALL=C sort
}

mkdir -p ci-output

context_file="ci-output/${WORKFLOW_SLUG}-runtime-context.txt"
summary_file="ci-output/${WORKFLOW_SLUG}-summary.txt"
report_file="ci-output/${WORKFLOW_SLUG}-health.md"
refs_file="ci-output/${WORKFLOW_SLUG}-ref-compare.tsv"

mirror_repo="${PIPELINE_GITHUB_MIRROR_REPO:-splunk/splunk-operator}"
mirror_compare_refs="${PIPELINE_GITHUB_MIRROR_COMPARE_REFS:-main,develop}"
mirror_auth_mode="anonymous"
mirror_url="https://github.com/${mirror_repo}.git"
source_remote="origin"

# Authenticate the GitHub read with the mirror App when its credentials are
# available, mirroring the token-handling of github-mirror-push.sh.
#
# Auth is OPTIONAL and currently DORMANT. splunk/splunk-operator is public, so
# anonymous git ls-remote/fetch reads its branch tips fine.
#
# When the App auth becomes worth activating:
#   - Heavy polling. Anonymous reads are rate-limited by source IP, a high frequency
#     or many compare-refs can hit GitHub's per-IP limit. The App attributes reads
#     to the installation's own, far larger quota instead.
#   - Private mirror. If splunk/splunk-operator ever becomes private, anonymous
#     reads fail outright and auth is mandatory.
#
# How to activate -- Make the health-check jobs supply the App env the way
# .github-mirror-push-base does in gitlab-ci/includes/admin.yml.
token_helper="${CI_PROJECT_DIR}/gitlab-ci/github-app-token.sh"
if [ -f "${token_helper}" ] && [ -n "${GITHUB_APP_ID:-}" ] && [ -n "${GITHUB_APP_PRIVATE_KEY_FILE:-}" ]; then
  MIRROR_TOKEN="$("${token_helper}")"
  AUTH_HEADER="AUTHORIZATION: basic $(printf 'x-access-token:%s' "${MIRROR_TOKEN}" | base64 | tr -d '\n')"
  export GIT_CONFIG_COUNT=1
  export GIT_CONFIG_KEY_0='http.https://github.com/.extraheader'
  export GIT_CONFIG_VALUE_0="${AUTH_HEADER}"
  cleanup() { unset MIRROR_TOKEN AUTH_HEADER GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0; }
  trap cleanup EXIT
  mirror_auth_mode="github-app"
fi

: > "${context_file}"
: > "${summary_file}"
: > "${report_file}"
: > "${refs_file}"

printf '%s\t%s\t%s\t%s\t%s\n' "ref" "status" "gitlab_tip" "github_tip" "differing_paths" >> "${refs_file}"

append_context "${context_file}" "observed_at_utc" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
append_context "${context_file}" "mirror_repo" "${mirror_repo}"
append_context "${context_file}" "mirror_compare_refs" "${mirror_compare_refs}"
append_context "${context_file}" "mirror_auth_mode" "${mirror_auth_mode}"
append_context "${context_file}" "mutation_policy" "read-only"
append_context "${context_file}" "source_remote" "${source_remote}"

load_exclude_prefixes
append_context "${context_file}" "compare_mode" "tree-parity"
append_context "${context_file}" "stripped_prefixes" "$(trim_csv_field "${EXCLUDE_PREFIXES}")"

remote_refs="$(git ls-remote "${mirror_url}")"
source_refs="$(git ls-remote "${source_remote}")"

old_ifs="${IFS}"
IFS=','
for raw_ref in ${mirror_compare_refs}; do
  ref_name="$(trim_csv_field "${raw_ref}")"
  [ -z "${ref_name}" ] && continue

  source_sha=""
  remote_sha=""
  status="missing-source"

  detail="-"

  source_sha="$(printf '%s\n' "${source_refs}" | awk '$2=="refs/heads/'"${ref_name}"'" {print $1; exit}')"
  if [ -n "${source_sha}" ]; then
    status="missing-remote"
  fi

  remote_sha="$(printf '%s\n' "${remote_refs}" | awk '$2=="refs/heads/'"${ref_name}"'" {print $1; exit}')"

  # Both tips exist: compare CONTENT, not commit SHAs. Fetch both tips blobless
  # and shallow (trees only, no blob contents) so the comparison stays cheap.
  if [ -n "${source_sha}" ] && [ -n "${remote_sha}" ]; then
    gl_ok=0
    gh_ok=0
    if git fetch --quiet --no-tags --depth=1 --filter=blob:none \
         "${source_remote}" "refs/heads/${ref_name}" 2>/dev/null; then
      gl_commit="$(git rev-parse FETCH_HEAD)"
      gl_ok=1
    fi
    if git fetch --quiet --no-tags --depth=1 --filter=blob:none \
         "${mirror_url}" "refs/heads/${ref_name}" 2>/dev/null; then
      gh_commit="$(git rev-parse FETCH_HEAD)"
      gh_ok=1
    fi

    if [ "${gl_ok}" = 1 ] && [ "${gh_ok}" = 1 ]; then
      gl_tree="$(normalized_tree "${gl_commit}" strip)"
      gh_tree="$(normalized_tree "${gh_commit}" keep)"
      if [ "${gl_tree}" = "${gh_tree}" ]; then
        status="match"
      else
        status="mismatch"
        diff_paths="$({ printf '%s\n' "${gl_tree}"; printf '%s\n' "${gh_tree}"; } \
          | LC_ALL=C sort | uniq -u | awk -F'\t' '{print $1}' | LC_ALL=C sort -u | sed '/^$/d')"
        diff_count="$(printf '%s\n' "${diff_paths}" | sed '/^$/d' | wc -l | tr -d ' ')"
        detail="$(printf '%s\n' "${diff_paths}" | head -n 5 | paste -sd, - | sed 's/,/, /g')"
        [ "${diff_count}" -gt 5 ] && detail="${detail}, (+$((diff_count - 5)) more)"
        [ -z "${detail}" ] && detail="${diff_count} differing path(s)"
      fi
    else
      status="fetch-error"
    fi
  fi

  printf '%s\t%s\t%s\t%s\t%s\n' "${ref_name}" "${status}" "${source_sha:-missing}" "${remote_sha:-missing}" "${detail}" >> "${refs_file}"
done
IFS="${old_ifs}"

cat > "${report_file}" <<EOF
# GitHub Mirror Health Check

- Mirror repository: \`${mirror_repo}\`
- Compared refs: \`${mirror_compare_refs}\`
- Auth mode: \`${mirror_auth_mode}\`
- Compare mode: \`tree-parity\`
- Stripped prefixes (GitLab side): \`$(trim_csv_field "${EXCLUDE_PREFIXES}")\`
- Mutation policy: read-only
- Source remote: \`${source_remote}\`

This validation checks read-only branch parity against the configured GitHub
repository. Because the mirror push strips the GitLab-only prefixes from every
commit (which rewrites commit SHAs), parity is verified by content: a ref is
\`match\` when the GitHub tree equals the GitLab tree with those prefixes
removed. It does not push, disable, or mutate any GitHub mirror settings.

| Ref | Status | GitLab tip | GitHub tip | Differing paths |
| --- | --- | --- | --- | --- |
EOF

while IFS="$(printf '\t')" read -r r_ref r_status r_src r_remote r_detail; do
  [ -z "${r_ref}" ] && continue
  [ "${r_ref}" = "ref" ] && continue
  printf '| `%s` | %s | `%.12s` | `%.12s` | %s |\n' \
    "${r_ref}" "${r_status}" "${r_src}" "${r_remote}" "${r_detail}" >> "${report_file}"
done < "${refs_file}"

cat > "${summary_file}" <<EOF
mirror_repo=${mirror_repo}
mirror_compare_refs=${mirror_compare_refs}
mirror_auth_mode=${mirror_auth_mode}
compare_mode=tree-parity
stripped_prefixes=$(trim_csv_field "${EXCLUDE_PREFIXES}")
mutation_policy=read-only
source_remote=${source_remote}
EOF
