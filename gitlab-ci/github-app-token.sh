#!/usr/bin/env bash
#
# Mint a short-lived (1h) GitHub App installation access token.
#
# Signs a JWT locally with the App private key (RS256), then exchanges
# it for an installation token via the GitHub API. The token is written
# to stdout and NOTHING else is, so callers can capture it with:
#
#   TOKEN="$(GITHUB_APP_ID=... GITHUB_APP_INSTALLATION_ID=... \
#            GITHUB_APP_PRIVATE_KEY_FILE=... ./github-app-token.sh)"
#
# Required env:
#   GITHUB_APP_ID                 numeric App ID
#   GITHUB_APP_PRIVATE_KEY_FILE   path to the App's .pem private key
# Optional env:
#   GITHUB_APP_INSTALLATION_ID    install id; auto-discovered if unset
#   GITHUB_API                    default https://api.github.com
#
# Secret hygiene: the private key is read by openssl from its file path
# (never echoed); the JWT and token are kept in variables and never
# printed to stderr or logs. The bearer JWT is handed to curl through a
# 0600 config file (curl -K), never via -H on the argv, so it cannot be
# read from ps / /proc/<pid>/cmdline by other same-user processes.
set -euo pipefail

: "${GITHUB_APP_ID:?set GITHUB_APP_ID}"
: "${GITHUB_APP_PRIVATE_KEY_FILE:?set GITHUB_APP_PRIVATE_KEY_FILE}"
GITHUB_API="${GITHUB_API:-https://api.github.com}"

if [ ! -r "${GITHUB_APP_PRIVATE_KEY_FILE}" ]; then
  printf 'ERROR: cannot read private key file: %s\n' "${GITHUB_APP_PRIVATE_KEY_FILE}" >&2
  exit 1
fi

b64url() {
  # base64url without padding, no newlines
  openssl base64 -A | tr '+/' '-_' | tr -d '='
}

now="$(date +%s)"
# iat backdated 60s for clock skew; iat->exp span = 600s (GitHub's 10min max).
iat="$((now - 60))"
exp="$((now + 540))"

header='{"alg":"RS256","typ":"JWT"}'
payload="$(printf '{"iat":%d,"exp":%d,"iss":"%s"}' "${iat}" "${exp}" "${GITHUB_APP_ID}")"

header_b64="$(printf '%s' "${header}"  | b64url)"
payload_b64="$(printf '%s' "${payload}" | b64url)"
signing_input="${header_b64}.${payload_b64}"

signature="$(printf '%s' "${signing_input}" \
  | openssl dgst -sha256 -sign "${GITHUB_APP_PRIVATE_KEY_FILE}" \
  | b64url)"

jwt="${signing_input}.${signature}"

# Hand the bearer JWT to curl via a 0600 config file (curl -K) instead of -H
# on the command line, so the signed JWT never lands on the process argv
# (readable via ps / /proc/<pid>/cmdline by same-user processes).
jwt_cfg="$(mktemp)"
chmod 600 "${jwt_cfg}"
cleanup() { rm -f "${jwt_cfg}"; unset jwt; }
trap cleanup EXIT
printf 'header = "Authorization: Bearer %s"\n' "${jwt}" > "${jwt_cfg}"

# Discover installation id if not provided.
install_id="${GITHUB_APP_INSTALLATION_ID:-}"
if [ -z "${install_id}" ]; then
  install_id="$(curl -fsS \
    -K "${jwt_cfg}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "${GITHUB_API}/app/installations" \
    | jq -r '.[0].id')"
  if [ -z "${install_id}" ] || [ "${install_id}" = "null" ]; then
    printf 'ERROR: could not auto-discover installation id\n' >&2
    exit 1
  fi
fi

# Exchange JWT for an installation access token.
response="$(curl -fsS -X POST \
  -K "${jwt_cfg}" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "${GITHUB_API}/app/installations/${install_id}/access_tokens")"

token="$(printf '%s' "${response}" | jq -r '.token')"
if [ -z "${token}" ] || [ "${token}" = "null" ]; then
  printf 'ERROR: token mint failed; response had no .token\n' >&2
  exit 1
fi

# Only the token goes to stdout.
printf '%s\n' "${token}"
