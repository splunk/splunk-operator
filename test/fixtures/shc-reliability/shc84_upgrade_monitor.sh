#!/usr/bin/env bash

set -euo pipefail

namespace="${SHC84_NAMESPACE:-shc84-upgrade-candidate}"
cr_name="${SHC84_CR_NAME:-shc84-shc}"
target_digest="${SHC84_TARGET_IMAGE_DIGEST:?set SHC84_TARGET_IMAGE_DIGEST}"
samples="${SHC84_SAMPLES:-1800}"
interval_seconds="${SHC84_INTERVAL_SECONDS:-2}"
stable_samples_required="${SHC84_STABLE_SAMPLES:-12}"
evidence_file="${SHC84_EVIDENCE_FILE:-build/_test/shc84/supported-upgrade.tsv}"
pod_prefix="splunk-${cr_name}-search-head-"
statefulset_name="splunk-${cr_name}-search-head"
service_name="splunk-${cr_name}-search-head-service"
condition_type="enterprise.splunk.com/shc-serving"

if [[ ! "${target_digest}" =~ ^sha256:[[:xdigit:]]{64}$ ]]; then
  echo "FAIL: SHC84_TARGET_IMAGE_DIGEST must be an sha256 digest" >&2
  exit 1
fi

mkdir -p "$(dirname "${evidence_file}")"

baseline_cr_json="$(kubectl -n "${namespace}" get \
  searchheadcluster.enterprise.splunk.com "${cr_name}" -o json)"
desired="$(jq -r '.spec.replicas // 0' <<<"${baseline_cr_json}")"
baseline_pods_json="$(kubectl -n "${namespace}" get pods -o json | jq -c \
  --arg prefix "${pod_prefix}" \
  '[.items[] |
    select(.metadata.name | startswith($prefix)) |
    select(.metadata.deletionTimestamp == null) |
    {
      name: .metadata.name,
      uid: .metadata.uid,
      restarts: ([.status.containerStatuses[]? |
        select(.name == "splunk") | .restartCount][0] // 0),
      imageID: ([.status.containerStatuses[]? |
        select(.name == "splunk") | .imageID][0] // "")
    }] | sort_by(.name)')"
baseline_endpoint_pods="$(kubectl -n "${namespace}" get \
  endpointslices.discovery.k8s.io -o json | \
  jq -c --arg service "${service_name}" \
    '[.items[] |
      select(.metadata.labels["kubernetes.io/service-name"] == $service) |
      .endpoints[]? |
      select(.conditions.ready == true) |
      .targetRef.name] | unique | sort')"

if [[ "${desired}" -lt 3 ||
      "$(jq 'length' <<<"${baseline_pods_json}")" -ne "${desired}" ||
      "$(jq 'length' <<<"${baseline_endpoint_pods}")" -ne "${desired}" ]]; then
  echo "FAIL: supported upgrade requires an established, fully serving SHC" >&2
  exit 1
fi
if jq -e --arg digest "${target_digest}" \
  'any(.[]; .imageID | endswith($digest))' \
  >/dev/null <<<"${baseline_pods_json}"; then
  echo "FAIL: a baseline Search Head already uses the target digest" >&2
  exit 1
fi

printf '%s\n' \
  $'timestamp\tgeneration\tobserved_generation\tphase\tformation_stage\tupgrade_phase\tcaptain\tcaptain_ready\tlast_stable\tconditions\tmembers\tstrategy\tpartition\tcurrent_revision\tupdate_revision\tready_replicas\tupdated_replicas\tendpoints\tendpoint_pods\ttarget_image_pods\treplaced_pods\trestarts\tpod_detail\tprobe_events' \
  >"${evidence_file}"

stable_samples=0
replacement_observed=false

for ((sample = 1; sample <= samples; sample++)); do
  timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  cr_json="$(kubectl -n "${namespace}" get \
    searchheadcluster.enterprise.splunk.com "${cr_name}" -o json)"
  statefulset_json="$(kubectl -n "${namespace}" get statefulset \
    "${statefulset_name}" -o json)"
  pods_json="$(kubectl -n "${namespace}" get pods -o json | jq -c \
    --arg prefix "${pod_prefix}" \
    '[.items[] |
      select(.metadata.name | startswith($prefix))] |
      sort_by(.metadata.name)')"

  generation="$(jq -r '.metadata.generation // 0' <<<"${cr_json}")"
  observed_generation="$(jq -r '.status.observedGeneration // 0' \
    <<<"${cr_json}")"
  phase="$(jq -r '.status.phase // ""' <<<"${cr_json}")"
  formation_stage="$(jq -r '.status.initialFormationStage // ""' \
    <<<"${cr_json}")"
  upgrade_phase="$(jq -r '.status.upgradePhase // ""' <<<"${cr_json}")"
  captain="$(jq -r '.status.captain // ""' <<<"${cr_json}")"
  captain_ready="$(jq -r '.status.captainReady // false' <<<"${cr_json}")"
  last_stable="$(jq -r '.status.lastStableReplicas // 0' <<<"${cr_json}")"
  conditions="$(jq -c '.status.conditions // []' <<<"${cr_json}")"
  members="$(jq -c '.status.members // []' <<<"${cr_json}")"

  strategy="$(jq -r '.spec.updateStrategy.type // ""' \
    <<<"${statefulset_json}")"
  partition="$(jq -r \
    '.spec.updateStrategy.rollingUpdate.partition // ""' \
    <<<"${statefulset_json}")"
  current_revision="$(jq -r '.status.currentRevision // ""' \
    <<<"${statefulset_json}")"
  update_revision="$(jq -r '.status.updateRevision // ""' \
    <<<"${statefulset_json}")"
  ready_replicas="$(jq -r '.status.readyReplicas // 0' \
    <<<"${statefulset_json}")"
  updated_replicas="$(jq -r '.status.updatedReplicas // 0' \
    <<<"${statefulset_json}")"

  endpoint_pods="$(kubectl -n "${namespace}" get \
    endpointslices.discovery.k8s.io -o json | \
    jq -c --arg service "${service_name}" \
      '[.items[] |
        select(.metadata.labels["kubernetes.io/service-name"] == $service) |
        .endpoints[]? |
        select(.conditions.ready == true) |
        .targetRef.name] | unique | sort')"
  endpoint_count="$(jq 'length' <<<"${endpoint_pods}")"

  pod_detail="$(jq -c --arg condition "${condition_type}" \
    '[.[] | {
      name: .metadata.name,
      uid: .metadata.uid,
      createdAt: .metadata.creationTimestamp,
      deletingAt: (.metadata.deletionTimestamp // ""),
      podReady: ([.status.conditions[]? |
        select(.type == "Ready") | .status][0] // "Unknown"),
      serving: ([.status.conditions[]? |
        select(.type == $condition) | .status][0] // "Unknown"),
      restartCount: ([.status.containerStatuses[]? |
        select(.name == "splunk") | .restartCount][0] // 0),
      image: ([.status.containerStatuses[]? |
        select(.name == "splunk") | .image][0] // ""),
      imageID: ([.status.containerStatuses[]? |
        select(.name == "splunk") | .imageID][0] // ""),
      currentState: ([.status.containerStatuses[]? |
        select(.name == "splunk") | .state][0] // {}),
      lastTermination: ([.status.containerStatuses[]? |
        select(.name == "splunk") | .lastState.terminated][0] // {}),
      startupProbe: ([.spec.containers[] |
        select(.name == "splunk") | .startupProbe][0] // {}),
      livenessProbe: ([.spec.containers[] |
        select(.name == "splunk") | .livenessProbe][0] // {}),
      readinessProbe: ([.spec.containers[] |
        select(.name == "splunk") | .readinessProbe][0] // {}),
      podTerminationGrace: (.spec.terminationGracePeriodSeconds // null)
    }]' <<<"${pods_json}")"
  target_image_pods="$(jq --arg digest "${target_digest}" \
    '[.[] | select(.imageID | endswith($digest))] | length' \
    <<<"${pod_detail}")"
  replaced_pods="$(jq -cn \
    --argjson baseline "${baseline_pods_json}" \
    --argjson current "${pod_detail}" \
    '[
      $baseline[] as $before |
      $current[] |
      select(.name == $before.name and .uid != $before.uid)
    ] | length')"
  restart_total="$(jq '[.[].restartCount] | add // 0' <<<"${pod_detail}")"

  probe_events="$(kubectl -n "${namespace}" get events -o json | \
    jq -c --arg prefix "${pod_prefix}" \
      '[.items[] |
        select(.involvedObject.kind == "Pod") |
        select(.involvedObject.name | startswith($prefix)) |
        select(.reason == "Unhealthy" or .reason == "Killing" or
          .reason == "FailedMount" or .reason == "FailedAttachVolume") |
        {
          pod: .involvedObject.name,
          reason,
          count: (.count // 1),
          first: (.firstTimestamp // .eventTime // ""),
          last: (.lastTimestamp // .eventTime // ""),
          message
        }] | sort_by(.pod, .reason, .last)')"

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${timestamp}" "${generation}" "${observed_generation}" "${phase}" \
    "${formation_stage}" "${upgrade_phase}" "${captain}" \
    "${captain_ready}" "${last_stable}" "${conditions}" "${members}" \
    "${strategy}" "${partition}" "${current_revision}" \
    "${update_revision}" "${ready_replicas}" "${updated_replicas}" \
    "${endpoint_count}" "${endpoint_pods}" "${target_image_pods}" \
    "${replaced_pods}" "${restart_total}" "${pod_detail}" \
    "${probe_events}" >>"${evidence_file}"

  if ((endpoint_count < desired - 1)); then
    echo "FAIL: supported upgrade left fewer than $((desired - 1)) endpoints" >&2
    exit 1
  fi
  if [[ "$(jq 'length' <<<"${pods_json}")" -gt "${desired}" ]]; then
    echo "FAIL: supported upgrade created more than ${desired} Search Head Pods" >&2
    exit 1
  fi

  drift="$(jq -cn \
    --argjson baseline "${baseline_pods_json}" \
    --argjson current "${pod_detail}" \
    '[
      $current[] as $after |
      $baseline[] |
      select(.name == $after.name) |
      if .uid == $after.uid then
        select(.restarts != $after.restartCount) |
        {
          name: $after.name,
          problem: "baseline Pod restart count changed",
          baselineUID: .uid,
          currentUID: $after.uid,
          baselineRestarts: .restarts,
          currentRestarts: $after.restartCount
        }
      else
        select($after.restartCount != 0) |
        {
          name: $after.name,
          problem: "replacement Pod restarted",
          baselineUID: .uid,
          currentUID: $after.uid,
          currentRestarts: $after.restartCount
        }
      end
    ]')"
  if (( "$(jq 'length' <<<"${drift}")" > 0 )); then
    echo "FAIL: Search Head container restart invariant failed: ${drift}" >&2
    exit 1
  fi

  invalid_probe_contract="$(jq -c \
    '[.[] |
      select(
        .startupProbe.failureThreshold != 60 or
        .startupProbe.terminationGracePeriodSeconds != 660 or
        .livenessProbe.terminationGracePeriodSeconds != 660 or
        (.readinessProbe | has("terminationGracePeriodSeconds")) or
        .podTerminationGrace != 1200
      ) |
      {
        name,
        startupProbe,
        livenessProbe,
        readinessProbe,
        podTerminationGrace
      }]' <<<"${pod_detail}")"
  if (( "$(jq 'length' <<<"${invalid_probe_contract}")" > 0 )); then
    echo "FAIL: rendered probe or Pod grace contract changed: ${invalid_probe_contract}" >&2
    exit 1
  fi

  if ((replaced_pods > 0)); then
    replacement_observed=true
  fi

  registered_up="$(jq \
    '[.[] | select(.status == "Up" and .is_registered == true)] | length' \
    <<<"${members}")"
  if [[ "${replacement_observed}" == "true" &&
        "${phase}" == "Ready" &&
        "${formation_stage}" == "Complete" &&
        "${captain_ready}" == "true" &&
        "${last_stable}" -eq "${desired}" &&
        "${observed_generation}" -eq "${generation}" &&
        "${replaced_pods}" -eq "${desired}" &&
        "${target_image_pods}" -eq "${desired}" &&
        "${endpoint_count}" -eq "${desired}" &&
        "${registered_up}" -eq "${desired}" ]]; then
    stable_samples=$((stable_samples + 1))
    if ((stable_samples >= stable_samples_required)); then
      echo "PASS: supported upgrade reached target digest with ${desired} serving members and zero container restarts"
      exit 0
    fi
  else
    stable_samples=0
  fi

  sleep "${interval_seconds}"
done

echo "FAIL: supported upgrade did not reach the stable target-image gate" >&2
exit 1
