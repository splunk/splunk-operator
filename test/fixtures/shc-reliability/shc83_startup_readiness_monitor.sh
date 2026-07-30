#!/usr/bin/env bash

set -euo pipefail

namespace="${SHC83_NAMESPACE:-shc83-startup-readiness}"
samples="${SHC83_SAMPLES:-720}"
interval_seconds="${SHC83_INTERVAL_SECONDS:-5}"
stable_samples_required="${SHC83_STABLE_SAMPLES:-12}"
evidence_file="${SHC83_EVIDENCE_FILE:-build/_test/shc83/startup-readiness.tsv}"
cr_name="${SHC83_CR_NAME:-shc83-shc}"
pod_prefix="splunk-${cr_name}-search-head-"
service_name="splunk-${cr_name}-search-head-service"
condition_type="enterprise.splunk.com/shc-serving"

mkdir -p "$(dirname "${evidence_file}")"
printf '%s\n' \
  $'timestamp\tphase\tdesired\tpods\tcontainers_ready\tpod_ready\tserving_true\tendpoints\trestarts\tinitialized\tmin_peers\tcaptain_ready\tlast_stable\tcaptain_api_observed\tcaptain_rolling_restart\trestart_required_members\tpod_detail\tendpoint_pods\tcontainer_states' \
  > "${evidence_file}"

stable_samples=0

for ((sample = 1; sample <= samples; sample++)); do
  timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  if ! cr_json="$(kubectl -n "${namespace}" get \
    searchheadcluster.enterprise.splunk.com "${cr_name}" -o json 2>/dev/null)"; then
    printf '%s\t%s\n' "${timestamp}" "SearchHeadClusterAbsent" >> "${evidence_file}"
    sleep "${interval_seconds}"
    continue
  fi

  desired="$(jq -r '.spec.replicas // 0' <<<"${cr_json}")"
  phase="$(jq -r '.status.phase // ""' <<<"${cr_json}")"
  initialized="$(jq -r '.status.initialized // false' <<<"${cr_json}")"
  min_peers="$(jq -r '.status.minPeersJoined // false' <<<"${cr_json}")"
  captain_ready="$(jq -r '.status.captainReady // false' <<<"${cr_json}")"
  last_stable="$(jq -r '.status.lastStableReplicas // ""' <<<"${cr_json}")"

  all_pods_json="$(kubectl -n "${namespace}" get pods -o json)"
  pods_json="$(jq -c --arg prefix "${pod_prefix}" \
    '[.items[] | select(.metadata.name | startswith($prefix))]' \
    <<<"${all_pods_json}")"
  pod_count="$(jq 'length' <<<"${pods_json}")"
  containers_ready="$(jq \
    '[.[] | select(any(.status.conditions[]?; .type == "ContainersReady" and .status == "True"))] | length' \
    <<<"${pods_json}")"
  pod_ready="$(jq \
    '[.[] | select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] | length' \
    <<<"${pods_json}")"
  serving_true="$(jq --arg condition "${condition_type}" \
    '[.[] | select(any(.status.conditions[]?; .type == $condition and .status == "True"))] | length' \
    <<<"${pods_json}")"
  restarts="$(jq \
    '[.[].status.containerStatuses[]?.restartCount] | add // 0' \
    <<<"${pods_json}")"
  pod_detail="$(jq -c --arg condition "${condition_type}" \
    '[.[] | {
      name: .metadata.name,
      uid: .metadata.uid,
      containersReady: ([.status.conditions[]? |
        select(.type == "ContainersReady") | .status][0] // "Unknown"),
      ready: ([.status.conditions[]? |
        select(.type == "Ready") | .status][0] // "Unknown"),
      serving: ([.status.conditions[]? |
        select(.type == $condition) |
        {status, reason, message}][0] //
        {status: "Unknown", reason: "ConditionAbsent", message: ""}),
      restarts: ([.status.containerStatuses[]?.restartCount] | add // 0)
    }]' <<<"${pods_json}")"

  endpoint_pods="$(kubectl -n "${namespace}" get \
    endpointslices.discovery.k8s.io -o json | \
    jq -c --arg service "${service_name}" \
      '[.items[] |
        select(.metadata.labels["kubernetes.io/service-name"] == $service) |
        .endpoints[]? |
        select(.conditions.ready == true) |
        .targetRef.name] | unique')"
  endpoint_count="$(jq 'length' <<<"${endpoint_pods}")"

  container_states="[]"
  while IFS= read -r pod_name; do
    # The single-quoted command is intentionally expanded inside the Pod.
    # shellcheck disable=SC2016
    state="$(kubectl -n "${namespace}" exec "${pod_name}" -c splunk -- \
      /bin/sh -c \
      'state_file="${CONTAINER_ARTIFACT_DIR:-/opt/container_artifact}/splunk-container.state"; if [ -r "${state_file}" ]; then tr -d "\r\n" < "${state_file}"; else printf absent; fi' \
      2>/dev/null || printf exec-unavailable)"
    container_states="$(jq -c \
      --arg pod "${pod_name}" \
      --arg state "${state}" \
      '. + [{pod: $pod, state: $state}]' \
      <<<"${container_states}")"
  done < <(jq -r '.[].metadata.name' <<<"${pods_json}")

  captain_api_observed=false
  captain_rolling_restart=unknown
  restart_required_members="[]"
  while IFS= read -r pod_name; do
    # Authentication is evaluated inside the Pod; the password is never
    # returned by this monitor.
    # shellcheck disable=SC2016
    captain_info="$(kubectl -n "${namespace}" exec "${pod_name}" -c splunk -- \
      /bin/sh -c \
      'password=$(cat /mnt/splunk-secrets/password); curl -ksS -u "admin:${password}" "https://localhost:8089/services/shcluster/captain/info?count=0&output_mode=json"' \
      2>/dev/null || true)"
    # shellcheck disable=SC2016
    captain_members="$(kubectl -n "${namespace}" exec "${pod_name}" -c splunk -- \
      /bin/sh -c \
      'password=$(cat /mnt/splunk-secrets/password); curl -ksS -u "admin:${password}" "https://localhost:8089/services/shcluster/captain/members?count=0&output_mode=json"' \
      2>/dev/null || true)"
    if jq -e '.entry[0].content.rolling_restart_flag != null' \
      >/dev/null 2>&1 <<<"${captain_info}" &&
      jq -e '.entry != null' >/dev/null 2>&1 <<<"${captain_members}"; then
      captain_api_observed=true
      captain_rolling_restart="$(jq -r \
        '.entry[0].content.rolling_restart_flag' <<<"${captain_info}")"
      restart_required_members="$(jq -c \
        '[.entry[] |
          select(.content.advertise_restart_required == true) |
          .content.label] | sort' <<<"${captain_members}")"
      break
    fi
  done < <(jq -r '.[].metadata.name' <<<"${pods_json}")

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${timestamp}" "${phase}" "${desired}" "${pod_count}" \
    "${containers_ready}" "${pod_ready}" "${serving_true}" \
    "${endpoint_count}" "${restarts}" "${initialized}" "${min_peers}" \
    "${captain_ready}" "${last_stable}" "${captain_api_observed}" \
    "${captain_rolling_restart}" "${restart_required_members}" \
    "${pod_detail}" "${endpoint_pods}" "${container_states}" \
    >> "${evidence_file}"

  if [[ -z "${last_stable}" ]] &&
    ((desired > 0 && endpoint_count > 0 && containers_ready < desired)); then
    echo "FAIL: Service endpoints appeared before every desired container was ready" >&2
    exit 1
  fi
  if [[ -z "${last_stable}" ]] &&
    ((desired > 0 && serving_true > 0 && containers_ready < desired)); then
    echo "FAIL: an SHC serving gate became true before every desired container was ready" >&2
    exit 1
  fi
  if [[ "${last_stable}" == "${desired}" &&
        "${captain_api_observed}" == "true" ]] &&
    { [[ "${captain_rolling_restart}" == "true" ]] ||
      (( "$(jq 'length' <<<"${restart_required_members}")" > 0 )); }; then
    echo "FAIL: the Operator recorded stable formation while Splunk still required or coordinated a rolling restart" >&2
    exit 1
  fi
  if ((restarts > 0)); then
    echo "FAIL: Search Head container restart count became non-zero" >&2
    exit 1
  fi

  if [[ "${phase}" == "Ready" &&
        "${desired}" -gt 0 &&
        "${pod_count}" -eq "${desired}" &&
        "${containers_ready}" -eq "${desired}" &&
        "${pod_ready}" -eq "${desired}" &&
        "${serving_true}" -eq "${desired}" &&
        "${endpoint_count}" -eq "${desired}" &&
        "${initialized}" == "true" &&
        "${min_peers}" == "true" &&
        "${captain_ready}" == "true" &&
        "${last_stable}" == "${desired}" &&
        "${captain_api_observed}" == "true" &&
        "${captain_rolling_restart}" == "false" &&
        "$(jq 'length' <<<"${restart_required_members}")" -eq 0 ]]; then
    stable_samples=$((stable_samples + 1))
    if ((stable_samples >= stable_samples_required)); then
      echo "PASS: SHC startup readiness remained stable for ${stable_samples_required} samples"
      exit 0
    fi
  else
    stable_samples=0
  fi

  sleep "${interval_seconds}"
done

echo "FAIL: SHC startup readiness did not reach the stable qualification gate" >&2
exit 1
