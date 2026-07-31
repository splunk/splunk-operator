#!/usr/bin/env bash

set -euo pipefail

namespace="${SHC84_NAMESPACE:-shc84-startup-term}"
cr_name="${SHC84_CR_NAME:-shc84-shc}"
samples="${SHC84_SAMPLES:-720}"
interval_seconds="${SHC84_INTERVAL_SECONDS:-5}"
stable_samples_required="${SHC84_STABLE_SAMPLES:-12}"
evidence_file="${SHC84_EVIDENCE_FILE:-build/_test/shc84/probe-budget.tsv}"
pod_prefix="splunk-${cr_name}-search-head-"
service_name="splunk-${cr_name}-search-head-service"

mkdir -p "$(dirname "${evidence_file}")"
printf '%s\n' \
  $'timestamp\tphase\tformation_stage\tdesired\tpods\tcontainers_ready\tpod_ready\tendpoints\trestarts\tpod_detail\tprobe_events\truntime_artifacts' \
  > "${evidence_file}"

stable_samples=0

for ((sample = 1; sample <= samples; sample++)); do
  timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  if ! cr_json="$(kubectl -n "${namespace}" get \
    searchheadcluster.enterprise.splunk.com "${cr_name}" -o json 2>/dev/null)"; then
    printf '%s\t%s\n' "${timestamp}" "SearchHeadClusterAbsent" \
      >> "${evidence_file}"
    sleep "${interval_seconds}"
    continue
  fi

  phase="$(jq -r '.status.phase // ""' <<<"${cr_json}")"
  formation_stage="$(jq -r '.status.initialFormationStage // ""' \
    <<<"${cr_json}")"
  desired="$(jq -r '.spec.replicas // 0' <<<"${cr_json}")"

  pods_json="$(kubectl -n "${namespace}" get pods -o json | \
    jq -c --arg prefix "${pod_prefix}" \
      '[.items[] | select(.metadata.name | startswith($prefix))]')"
  pod_count="$(jq 'length' <<<"${pods_json}")"
  containers_ready="$(jq \
    '[.[] | select(any(.status.conditions[]?;
      .type == "ContainersReady" and .status == "True"))] | length' \
    <<<"${pods_json}")"
  pod_ready="$(jq \
    '[.[] | select(any(.status.conditions[]?;
      .type == "Ready" and .status == "True"))] | length' \
    <<<"${pods_json}")"
  restarts="$(jq '[.[].status.containerStatuses[]?.restartCount] | add // 0' \
    <<<"${pods_json}")"
  pod_detail="$(jq -c \
    '[.[] | {
      name: .metadata.name,
      uid: .metadata.uid,
      createdAt: .metadata.creationTimestamp,
      podStartedAt: (.status.startTime // ""),
      deletingAt: (.metadata.deletionTimestamp // ""),
      containersReady: ([.status.conditions[]? |
        select(.type == "ContainersReady") | .status][0] // "Unknown"),
      ready: ([.status.conditions[]? |
        select(.type == "Ready") | .status][0] // "Unknown"),
      restartCount: ([.status.containerStatuses[]?.restartCount] | add // 0),
      currentState: ([.status.containerStatuses[]? |
        {name, state}][0] // {}),
      lastTermination: ([.status.containerStatuses[]? |
        {name, lastTerminationState}][0] // {}),
      startupProbe: ([.spec.containers[] |
        select(.name == "splunk") | .startupProbe][0] // {}),
      livenessProbe: ([.spec.containers[] |
        select(.name == "splunk") | .livenessProbe][0] // {}),
      podTerminationGrace: (.spec.terminationGracePeriodSeconds // null)
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

  probe_events="$(kubectl -n "${namespace}" get events -o json | \
    jq -c --arg prefix "${pod_prefix}" \
      '[.items[] |
        select(.involvedObject.kind == "Pod") |
        select(.involvedObject.name | startswith($prefix)) |
        select(.reason == "Unhealthy" or .reason == "Killing") |
        {
          pod: .involvedObject.name,
          reason,
          count: (.count // 1),
          first: (.firstTimestamp // .eventTime // ""),
          last: (.lastTimestamp // .eventTime // ""),
          message
        }] | sort_by(.pod, .reason, .last)')"

  runtime_artifacts="[]"
  while IFS= read -r pod_name; do
    # The command is single-quoted so all runtime paths are expanded in the Pod.
    # shellcheck disable=SC2016
    artifacts="$(kubectl -n "${namespace}" exec "${pod_name}" -c splunk -- \
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
    runtime_artifacts="$(jq -c \
      --arg pod "${pod_name}" \
      --arg state "${state}" \
      --arg owner "${owner}" \
      --arg result "${result}" \
      '. + [{pod: $pod, state: $state, shutdownOwner: $owner,
        shutdownResult: $result}]' <<<"${runtime_artifacts}")"
  done < <(jq -r '.[].metadata.name' <<<"${pods_json}")

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${timestamp}" "${phase}" "${formation_stage}" "${desired}" \
    "${pod_count}" "${containers_ready}" "${pod_ready}" \
    "${endpoint_count}" "${restarts}" "${pod_detail}" "${probe_events}" \
    "${runtime_artifacts}" >> "${evidence_file}"

  if ((restarts != 0)); then
    echo "FAIL: fresh formation incurred ${restarts} container restart(s)" >&2
    exit 1
  fi

  if [[ "${phase}" == "Ready" &&
        "${formation_stage}" == "Complete" &&
        "${desired}" -gt 0 &&
        "${pod_count}" -eq "${desired}" &&
        "${containers_ready}" -eq "${desired}" &&
        "${pod_ready}" -eq "${desired}" &&
        "${endpoint_count}" -eq "${desired}" ]]; then
    stable_samples=$((stable_samples + 1))
    if ((stable_samples >= stable_samples_required)); then
      echo "PASS: SHC reached ${stable_samples_required} stable samples; container restarts=${restarts}"
      exit 0
    fi
  else
    stable_samples=0
  fi

  sleep "${interval_seconds}"
done

echo "FAIL: SHC did not reach the stable probe-budget gate" >&2
exit 1
