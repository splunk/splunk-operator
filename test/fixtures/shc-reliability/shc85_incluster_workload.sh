#!/usr/bin/env bash

set -euo pipefail

run_id="${SHC85_RUN_ID:-${HOSTNAME:-shc85-incluster}}"
samples="${SHC85_WORKLOAD_SAMPLES:-1800}"
interval_seconds="${SHC85_WORKLOAD_INTERVAL_SECONDS:-1}"
settle_attempts="${SHC85_WORKLOAD_SETTLE_ATTEMPTS:-60}"
hec_service="${SHC85_HEC_SERVICE:-splunk-shc85-idxc-indexer-service}"
search_service="${SHC85_SEARCH_SERVICE:-splunk-shc85-shc-search-head-service}"

for name in HEC_TOKEN ADMIN_PASSWORD; do
  if [[ -z "${!name:-}" ]]; then
    printf 'required credential environment variable is empty: %s\n' \
      "${name}" >&2
    exit 2
  fi
done

for value in "${samples}" "${interval_seconds}" "${settle_attempts}"; do
  if ! [[ "${value}" =~ ^[1-9][0-9]*$ ]]; then
    printf 'sample, interval, and settle values must be positive integers\n' \
      >&2
    exit 2
  fi
done

extract_field() {
  local field="$1"
  local response="$2"
  printf '%s' "${response}" |
    sed -n "s/.*\"${field}\":\"\\([0-9][0-9]*\\)\".*/\\1/p" |
    tail -1
}

submit_event() {
  local sequence="$1"
  local payload response
  payload="$(
    printf '{"event":{"shc85_run":"%s","seq":%d},"sourcetype":"_json","index":"main"}' \
      "${run_id}" "${sequence}"
  )"
  response="$(
    curl -skS --connect-timeout 3 --max-time 15 \
      -H "Authorization: Splunk ${HEC_TOKEN}" \
      -H 'Content-Type: application/json' \
      --data-binary "${payload}" \
      "https://${hec_service}:8088/services/collector/event" 2>&1
  )" || return 1
  grep -q '"code":0' <<<"${response}"
}

search_sequences() {
  curl -skS --connect-timeout 3 --max-time 20 \
    -u "admin:${ADMIN_PASSWORD}" \
    -X POST \
    "https://${search_service}:8089/services/search/jobs/export" \
    --data-urlencode \
    "search=search index=main earliest=-24h shc85_run=\"${run_id}\" | stats count min(seq) as min max(seq) as max dc(seq) as distinct" \
    --data 'output_mode=json' 2>&1
}

printf 'run=%s start=%s samples=%s intervalSeconds=%s\n' \
  "${run_id}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "${samples}" "${interval_seconds}"

hec_failures=0
search_failures=0
last_count=0
last_distinct=0
last_max=0
previous_count=0
count_regressions=0
max_pending=0
max_pending_sequence=0
max_pending_timestamp=""

for sequence in $(seq 1 "${samples}"); do
  if submit_event "${sequence}"; then
    hec_state=ok
  else
    hec_state=fail
    hec_failures=$((hec_failures + 1))
  fi

  search_response="$(search_sequences || true)"
  count="$(extract_field count "${search_response}")"
  minimum="$(extract_field min "${search_response}")"
  maximum="$(extract_field max "${search_response}")"
  distinct="$(extract_field distinct "${search_response}")"
  if [[ -n "${count}" && -n "${distinct}" ]] &&
    { [[ "${count}" -eq 0 ]] || [[ -n "${maximum}" ]]; }; then
    search_state=ok
    minimum="${minimum:-0}"
    maximum="${maximum:-0}"
    last_count="${count}"
    last_distinct="${distinct}"
    last_max="${maximum}"
    if ((count < previous_count)); then
      count_regressions=$((count_regressions + 1))
    fi
    previous_count="${count}"
    pending=$((sequence - count))
    if ((pending < 0)); then
      pending=0
    fi
    if ((pending > max_pending)); then
      max_pending="${pending}"
      max_pending_sequence="${sequence}"
      max_pending_timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    fi
  else
    search_state=fail
    search_failures=$((search_failures + 1))
    count="${last_count}"
    minimum=unknown
    maximum="${last_max}"
    distinct="${last_distinct}"
    pending=unknown
  fi

  printf '%s seq=%s hec=%s search=%s count=%s min=%s max=%s distinct=%s pending=%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${sequence}" "${hec_state}" \
    "${search_state}" "${count}" "${minimum}" "${maximum}" "${distinct}" \
    "${pending}"
  sleep "${interval_seconds}"
done

final_complete=false
for _ in $(seq 1 "${settle_attempts}"); do
  search_response="$(search_sequences || true)"
  count="$(extract_field count "${search_response}")"
  minimum="$(extract_field min "${search_response}")"
  maximum="$(extract_field max "${search_response}")"
  distinct="$(extract_field distinct "${search_response}")"
  if [[ "${count:-0}" -eq "${samples}" &&
    "${minimum:-0}" -eq 1 && "${maximum:-0}" -eq "${samples}" &&
    "${distinct:-0}" -eq "${samples}" ]]; then
    final_complete=true
    break
  fi
  sleep 5
done

printf 'run=%s end=%s submitted=%s hecFailures=%s searchFailures=%s countRegressions=%s maxPending=%s maxPendingSequence=%s maxPendingTimestamp=%s finalCount=%s finalMin=%s finalMax=%s finalDistinct=%s complete=%s\n' \
  "${run_id}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${samples}" \
  "${hec_failures}" "${search_failures}" "${count_regressions}" \
  "${max_pending}" "${max_pending_sequence}" \
  "${max_pending_timestamp:-none}" "${count:-0}" "${minimum:-0}" \
  "${maximum:-0}" "${distinct:-0}" "${final_complete}"

if [[ "${hec_failures}" -ne 0 || "${search_failures}" -ne 0 ||
  "${final_complete}" != true ]]; then
  exit 1
fi
