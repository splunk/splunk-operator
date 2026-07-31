#!/usr/bin/env bash

set -euo pipefail

namespace="${SHC84_NAMESPACE:-shc84-startup-term}"
cr_name="${SHC84_CR_NAME:-shc84-shc}"
target_pod="${SHC84_TARGET_POD:?set SHC84_TARGET_POD}"
scenario="${SHC84_SCENARIO:-direct TERM}"
samples="${SHC84_SAMPLES:-360}"
interval_seconds="${SHC84_INTERVAL_SECONDS:-2}"
stable_samples_required="${SHC84_STABLE_SAMPLES:-12}"
evidence_file="${SHC84_EVIDENCE_FILE:-build/_test/shc84/term-exit.tsv}"
service_name="splunk-${cr_name}-search-head-service"

mkdir -p "$(dirname "${evidence_file}")"

baseline_json="$(kubectl -n "${namespace}" get pod "${target_pod}" -o json)"
baseline_uid="$(jq -r '.metadata.uid' <<<"${baseline_json}")"
baseline_restarts="$(jq -r \
  '[.status.containerStatuses[]?.restartCount] | add // 0' \
  <<<"${baseline_json}")"

printf '%s\n' \
  $'timestamp\tphase\tformation_stage\tendpoints\ttarget\truntime_artifacts\tprobe_events\tprevious_shutdown_log' \
  >"${evidence_file}"

stable_samples=0

for ((sample = 1; sample <= samples; sample++)); do
  timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  cr_json="$(kubectl -n "${namespace}" get \
    searchheadcluster.enterprise.splunk.com "${cr_name}" -o json)"
  pod_json="$(kubectl -n "${namespace}" get pod "${target_pod}" -o json)"

  phase="$(jq -r '.status.phase // ""' <<<"${cr_json}")"
  formation_stage="$(jq -r '.status.initialFormationStage // ""' \
    <<<"${cr_json}")"
  endpoint_pods="$(kubectl -n "${namespace}" get \
    endpointslices.discovery.k8s.io -o json | \
    jq -c --arg service "${service_name}" \
      '[.items[] |
        select(.metadata.labels["kubernetes.io/service-name"] == $service) |
        .endpoints[]? |
        select(.conditions.ready == true) |
        .targetRef.name] | unique')"
  endpoint_count="$(jq 'length' <<<"${endpoint_pods}")"

  target_detail="$(jq -c \
    --arg baselineUID "${baseline_uid}" \
    --argjson baselineRestarts "${baseline_restarts}" \
    '{
      name: .metadata.name,
      uid: .metadata.uid,
      baselineUID: $baselineUID,
      samePod: (.metadata.uid == $baselineUID),
      podReady: ([.status.conditions[]? |
        select(.type == "Ready") | .status][0] // "Unknown"),
      containersReady: ([.status.conditions[]? |
        select(.type == "ContainersReady") | .status][0] // "Unknown"),
      restartCount: ([.status.containerStatuses[]?.restartCount] | add // 0),
      baselineRestarts: $baselineRestarts,
      currentState: ([.status.containerStatuses[]? |
        {name, state}][0] // {}),
      lastTermination: ([.status.containerStatuses[]? |
        {name, lastTerminationState}][0] // {}),
      startupProbe: ([.spec.containers[] |
        select(.name == "splunk") | .startupProbe][0] // {}),
      livenessProbe: ([.spec.containers[] |
        select(.name == "splunk") | .livenessProbe][0] // {}),
      podTerminationGrace: (.spec.terminationGracePeriodSeconds // null)
    }' <<<"${pod_json}")"

  # The command is single-quoted so runtime paths are expanded in the Pod.
  # shellcheck disable=SC2016
  artifacts="$(kubectl -n "${namespace}" exec "${target_pod}" -c splunk -- \
    /bin/sh -c '
      artifact_dir="${CONTAINER_ARTIFACT_DIR:-/opt/container_artifact}"
      state=absent
      owner=absent
      result=absent
      [ ! -r "${artifact_dir}/splunk-container.state" ] ||
        state=$(tr -d "\r\n" < "${artifact_dir}/splunk-container.state")
      [ ! -r "${artifact_dir}/splunk-shutdown.lock/owner" ] ||
        owner=$(tr -d "\r\n" < "${artifact_dir}/splunk-shutdown.lock/owner")
      [ ! -r "${artifact_dir}/splunk-shutdown.lock/result" ] ||
        result=$(tr -d "\r\n" < "${artifact_dir}/splunk-shutdown.lock/result")
      printf "%s\t%s\t%s" "${state}" "${owner}" "${result}"
    ' 2>/dev/null || printf 'exec-unavailable\tabsent\tabsent')"
  state="${artifacts%%$'\t'*}"
  remainder="${artifacts#*$'\t'}"
  owner="${remainder%%$'\t'*}"
  result="${remainder#*$'\t'}"
  runtime_artifacts="$(jq -cn \
    --arg state "${state}" \
    --arg owner "${owner}" \
    --arg result "${result}" \
    '{state: $state, shutdownOwner: $owner, shutdownResult: $result}')"

  probe_events="$(kubectl -n "${namespace}" get events -o json | \
    jq -c --arg pod "${target_pod}" \
      '[.items[] |
        select(.involvedObject.kind == "Pod") |
        select(.involvedObject.name == $pod) |
        select(.reason == "Unhealthy" or .reason == "Killing") |
        {
          reason,
          count: (.count // 1),
          first: (.firstTimestamp // .eventTime // ""),
          last: (.lastTimestamp // .eventTime // ""),
          message
        }] | sort_by(.reason, .last)')"

  previous_log_text="$(kubectl -n "${namespace}" logs "${target_pod}" \
    -c splunk --previous 2>/dev/null || true)"
  previous_shutdown_text="$(printf '%s\n' "${previous_log_text}" | \
    /usr/bin/grep 'splunk-shutdown:' | tail -n 20 || true)"
  previous_shutdown_log="$(jq -cn \
    --arg log "${previous_shutdown_text}" '$log')"

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${timestamp}" "${phase}" "${formation_stage}" "${endpoint_count}" \
    "${target_detail}" "${runtime_artifacts}" "${probe_events}" \
    "${previous_shutdown_log}" >>"${evidence_file}"

  current_uid="$(jq -r '.metadata.uid' <<<"${pod_json}")"
  current_restarts="$(jq -r \
    '[.status.containerStatuses[]?.restartCount] | add // 0' \
    <<<"${pod_json}")"
  pod_ready="$(jq -r \
    '[.status.conditions[]? |
      select(.type == "Ready") | .status][0] // "Unknown"' \
    <<<"${pod_json}")"
  containers_ready="$(jq -r \
    '[.status.conditions[]? |
      select(.type == "ContainersReady") | .status][0] // "Unknown"' \
    <<<"${pod_json}")"

  if [[ "${current_uid}" != "${baseline_uid}" ]]; then
    echo "FAIL: ${scenario} replaced the Pod instead of its container" >&2
    exit 1
  fi

  if ((current_restarts == baseline_restarts + 1)) &&
    [[ "${phase}" == "Ready" &&
      "${formation_stage}" == "Complete" &&
      "${pod_ready}" == "True" &&
      "${containers_ready}" == "True" ]] &&
    ((endpoint_count == 3)); then
    stable_samples=$((stable_samples + 1))
    if ((stable_samples >= stable_samples_required)); then
      echo "PASS: ${scenario} restarted the container exactly once and SHC recovered"
      exit 0
    fi
  else
    stable_samples=0
  fi

  sleep "${interval_seconds}"
done

echo "FAIL: ${scenario} did not reach the stable recovery gate" >&2
exit 1
