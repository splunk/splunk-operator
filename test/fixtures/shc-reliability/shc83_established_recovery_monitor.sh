#!/usr/bin/env bash

set -euo pipefail

namespace="${SHC83_NAMESPACE:-shc83-startup-readiness}"
cr_name="${SHC83_CR_NAME:-shc83-shc}"
target_pod="${SHC83_TARGET_POD:?SHC83_TARGET_POD is required}"
samples="${SHC83_SAMPLES:-360}"
interval_seconds="${SHC83_INTERVAL_SECONDS:-5}"
stable_samples_required="${SHC83_STABLE_SAMPLES:-12}"
evidence_file="${SHC83_EVIDENCE_FILE:-build/_test/shc83/established-recovery.tsv}"
pod_prefix="splunk-${cr_name}-search-head-"
service_name="splunk-${cr_name}-search-head-service"
condition_type="enterprise.splunk.com/shc-serving"

mkdir -p "$(dirname "${evidence_file}")"

initial_pods_json="$(kubectl -n "${namespace}" get pods -o json | jq -c \
  --arg prefix "${pod_prefix}" \
  '[.items[] |
    select(.metadata.name | startswith($prefix)) |
    select(.metadata.deletionTimestamp == null)]')"
initial_target_uid="$(jq -r --arg pod "${target_pod}" \
  '[.[] | select(.metadata.name == $pod) | .metadata.uid][0] // ""' \
  <<<"${initial_pods_json}")"
if [[ -z "${initial_target_uid}" ]]; then
  echo "FAIL: target Pod ${target_pod} is not present" >&2
  exit 1
fi

expected_peer_pods="$(jq -c --arg target "${target_pod}" \
  '[.[].metadata.name | select(. != $target)] | sort' \
  <<<"${initial_pods_json}")"
desired="$(kubectl -n "${namespace}" get \
  searchheadcluster.enterprise.splunk.com "${cr_name}" \
  -o jsonpath='{.spec.replicas}')"
if [[ "$(jq 'length' <<<"${initial_pods_json}")" -ne "${desired}" ]]; then
  echo "FAIL: established baseline does not contain every desired Pod" >&2
  exit 1
fi

initial_endpoint_pods="$(kubectl -n "${namespace}" get \
  endpointslices.discovery.k8s.io -o json | \
  jq -c --arg service "${service_name}" \
    '[.items[] |
      select(.metadata.labels["kubernetes.io/service-name"] == $service) |
      .endpoints[]? |
      select(.conditions.ready == true) |
      .targetRef.name] | unique | sort')"
if [[ "$(jq 'length' <<<"${initial_endpoint_pods}")" -ne "${desired}" ]]; then
  echo "FAIL: established baseline does not contain every desired endpoint" >&2
  exit 1
fi

printf '%s\n' \
  $'timestamp\tphase\tformation_stage\tlast_stable\tcaptain\tcaptain_rolling_restart\ttarget_uid\ttarget_deleting\ttarget_containers_ready\ttarget_ready\ttarget_serving\ttarget_member_status\ttarget_registered\ttarget_restart_state\tendpoints\tendpoint_pods\treplacement_observed\ttarget_removal_observed\tpod_detail' \
  > "${evidence_file}"

replacement_observed=false
target_removal_observed=false
stable_samples=0

for ((sample = 1; sample <= samples; sample++)); do
  timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  cr_json="$(kubectl -n "${namespace}" get \
    searchheadcluster.enterprise.splunk.com "${cr_name}" -o json)"
  phase="$(jq -r '.status.phase // ""' <<<"${cr_json}")"
  formation_stage="$(
    jq -r '.status.initialFormationStage // ""' <<<"${cr_json}"
  )"
  last_stable="$(jq -r '.status.lastStableReplicas // ""' <<<"${cr_json}")"
  captain="$(jq -r '.status.captain // ""' <<<"${cr_json}")"
  captain_rolling_restart="$(
    jq -r '.status.captainRollingRestart // false' <<<"${cr_json}"
  )"
  target_member="$(jq -c --arg target "${target_pod}" \
    '[.status.members[]? | select(.name == $target)][0] // {}' \
    <<<"${cr_json}")"
  target_member_status="$(jq -r '.status // ""' <<<"${target_member}")"
  target_registered="$(jq -r '.is_registered // false' <<<"${target_member}")"
  target_restart_state="$(
    jq -r '.restart_state // ""' <<<"${target_member}"
  )"

  pods_json="$(kubectl -n "${namespace}" get pods -o json | jq -c \
    --arg prefix "${pod_prefix}" \
    '[.items[] | select(.metadata.name | startswith($prefix))]')"
  target_json="$(jq -c --arg target "${target_pod}" \
    '[.[] | select(.metadata.name == $target)][0] // {}' <<<"${pods_json}")"
  target_uid="$(jq -r '.metadata.uid // ""' <<<"${target_json}")"
  target_deleting="$(
    jq -r '.metadata.deletionTimestamp != null' <<<"${target_json}"
  )"
  target_containers_ready="$(jq -r \
    '[.status.conditions[]? |
      select(.type == "ContainersReady") | .status][0] // "Unknown"' \
    <<<"${target_json}")"
  target_ready="$(jq -r \
    '[.status.conditions[]? |
      select(.type == "Ready") | .status][0] // "Unknown"' \
    <<<"${target_json}")"
  target_serving="$(jq -r --arg condition "${condition_type}" \
    '[.status.conditions[]? |
      select(.type == $condition) | .status][0] // "Unknown"' \
    <<<"${target_json}")"
  pod_detail="$(jq -c --arg condition "${condition_type}" \
    '[.[] | {
      name: .metadata.name,
      uid: .metadata.uid,
      deleting: (.metadata.deletionTimestamp != null),
      ready: ([.status.conditions[]? |
        select(.type == "Ready") | .status][0] // "Unknown"),
      serving: ([.status.conditions[]? |
        select(.type == $condition) | .status][0] // "Unknown"),
      restarts: ([.status.containerStatuses[]?.restartCount] | add // 0)
    }]' <<<"${pods_json}")"

  endpoint_pods="$(kubectl -n "${namespace}" get \
    endpointslices.discovery.k8s.io -o json | \
    jq -c --arg service "${service_name}" \
      '[.items[] |
        select(.metadata.labels["kubernetes.io/service-name"] == $service) |
        .endpoints[]? |
        select(.conditions.ready == true) |
        .targetRef.name] | unique | sort')"
  endpoint_count="$(jq 'length' <<<"${endpoint_pods}")"

  if [[ "${target_uid}" != "" &&
        "${target_uid}" != "${initial_target_uid}" ]]; then
    replacement_observed=true
  fi
  if ! jq -e --arg target "${target_pod}" \
    'index($target) != null' >/dev/null <<<"${endpoint_pods}"; then
    target_removal_observed=true
  fi

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${timestamp}" "${phase}" "${formation_stage}" "${last_stable}" \
    "${captain}" "${captain_rolling_restart}" "${target_uid}" \
    "${target_deleting}" "${target_containers_ready}" "${target_ready}" \
    "${target_serving}" "${target_member_status}" "${target_registered}" \
    "${target_restart_state}" "${endpoint_count}" "${endpoint_pods}" \
    "${replacement_observed}" "${target_removal_observed}" "${pod_detail}" \
    >> "${evidence_file}"

  if ((endpoint_count < desired - 1)); then
    echo "FAIL: fewer than $((desired - 1)) established endpoints remained" >&2
    exit 1
  fi
  while IFS= read -r peer_pod; do
    if ! jq -e --arg pod "${peer_pod}" \
      'index($pod) != null' >/dev/null <<<"${endpoint_pods}"; then
      echo "FAIL: unaffected peer ${peer_pod} left the client endpoints" >&2
      exit 1
    fi
  done < <(jq -r '.[]' <<<"${expected_peer_pods}")
  if (( "$(jq \
    '[.[].status.containerStatuses[]?.restartCount] | add // 0' \
    <<<"${pods_json}")" > 0 )); then
    echo "FAIL: a Search Head container restart count became non-zero" >&2
    exit 1
  fi

  if [[ "${replacement_observed}" == "true" &&
        "${target_removal_observed}" == "true" &&
        "${phase}" == "Ready" &&
        "${formation_stage}" == "Complete" &&
        "${last_stable}" == "${desired}" &&
        "${captain_rolling_restart}" == "false" &&
        "${target_containers_ready}" == "True" &&
        "${target_ready}" == "True" &&
        "${target_serving}" == "True" &&
        "${target_member_status}" == "Up" &&
        "${target_registered}" == "true" &&
        "${target_restart_state}" == "NoRestart" &&
        "${endpoint_count}" -eq "${desired}" ]]; then
    stable_samples=$((stable_samples + 1))
    if ((stable_samples >= stable_samples_required)); then
      echo "PASS: established SHC member recovery remained stable for ${stable_samples_required} samples"
      exit 0
    fi
  else
    stable_samples=0
  fi

  sleep "${interval_seconds}"
done

echo "FAIL: established SHC member recovery did not reach the stable gate" >&2
exit 1
