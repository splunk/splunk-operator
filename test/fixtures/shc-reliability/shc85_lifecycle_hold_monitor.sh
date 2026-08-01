#!/usr/bin/env bash

set -euo pipefail

namespace="${SHC85_NAMESPACE:-shc85-lifecycle-hold}"
cr_name="${SHC85_CR_NAME:-shc85-idxc}"
operator_namespace="${SHC85_OPERATOR_NAMESPACE:-splunk-operator}"
operator_deployment="${SHC85_OPERATOR_DEPLOYMENT:-splunk-operator-controller-manager}"
hold_seconds="${SHC85_HOLD_SECONDS:-300}"
hold_stage="${SHC85_HOLD_STAGE:-ReadyForReplacement}"
controller_fault="${SHC85_CONTROLLER_FAULT:-ControllerAbsent}"
sample_seconds="${SHC85_SAMPLE_SECONDS:-2}"
stage_timeout_seconds="${SHC85_STAGE_TIMEOUT_SECONDS:-1800}"
roll_timeout_seconds="${SHC85_ROLL_TIMEOUT_SECONDS:-7200}"
stable_samples_required="${SHC85_STABLE_SAMPLES:-10}"
run_id="${SHC85_RUN_ID:-shc85-hold-$(date -u +%Y%m%dT%H%M%SZ)}"
evidence_file="${SHC85_EVIDENCE_FILE:-build/_test/shc85/${run_id}.tsv}"
api_fault_image="${SHC85_API_FAULT_IMAGE:-nicolaka/netshoot@sha256:a20c2531bf35436ed3766cd6cfe89d352b050ccc4d7005ce6400adf97503da1b}"
api_fault_container="${SHC85_API_FAULT_CONTAINER:-shc85-api-disconnect-$(date -u +%s)}"
api_fault_log="${SHC85_API_FAULT_LOG:-${evidence_file%.tsv}.api-fault.log}"
api_fault_profile="${SHC85_API_FAULT_PROFILE:-test/fixtures/shc-reliability/shc85-api-fault-profile.json}"
leader_election_lease="${SHC85_LEADER_ELECTION_LEASE:-270bec8c.splunk.com}"
leader_failover_timeout_seconds="${SHC85_LEADER_FAILOVER_TIMEOUT_SECONDS:-180}"
leader_stability_seconds="${SHC85_LEADER_STABILITY_SECONDS:-15}"
leader_failover_log="${SHC85_LEADER_FAILOVER_LOG:-${evidence_file%.tsv}.leader-failover.log}"

statefulset="splunk-${cr_name}-indexer"
service_name="${statefulset}-service"
pod_prefix="${statefulset}-"
driver_file="/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"
pause_annotation="indexercluster.enterprise.splunk.com/paused"

for command in kubectl jq pkill; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    printf 'required command is unavailable: %s\n' "${command}" >&2
    exit 2
  fi
done

for value in "${hold_seconds}" "${sample_seconds}" \
  "${stage_timeout_seconds}" "${roll_timeout_seconds}" \
  "${stable_samples_required}" "${leader_failover_timeout_seconds}" \
  "${leader_stability_seconds}"; do
  if ! printf '%s\n' "${value}" | grep -Eq '^[1-9][0-9]*$'; then
    printf 'SHC-85 timing values must be positive integers\n' >&2
    exit 2
  fi
done

case "${hold_stage}" in
TargetSelected | WithdrawingReadiness | Decommissioning | ReadyForReplacement) ;;
*)
  printf '%s\n' \
    'SHC85_HOLD_STAGE must be TargetSelected, WithdrawingReadiness, Decommissioning, or ReadyForReplacement' \
    >&2
  exit 2
  ;;
esac

case "${controller_fault}" in
ControllerAbsent | APIDisconnected | LeaderFailover) ;;
*)
  printf '%s\n' \
    'SHC85_CONTROLLER_FAULT must be ControllerAbsent, APIDisconnected, or LeaderFailover' \
    >&2
  exit 2
  ;;
esac
if [[ ("${controller_fault}" == APIDisconnected ||
  "${controller_fault}" == LeaderFailover) &&
  "${hold_stage}" != Decommissioning ]]; then
  printf '%s\n' \
    "${controller_fault} qualification currently requires SHC85_HOLD_STAGE=Decommissioning" \
    >&2
  exit 2
fi
if [[ "${controller_fault}" == APIDisconnected &&
  ! -r "${api_fault_profile}" ]]; then
  printf 'SHC85_API_FAULT_PROFILE is not readable: %s\n' \
    "${api_fault_profile}" >&2
  exit 2
fi

mkdir -p "$(dirname "${evidence_file}")"
printf '%s\n' \
  $'timestamp\tphase\toperator_replicas\tpod_update\tpods\tendpoints\tliveness_failures\toperator_runtime\tleader_election' \
  >"${evidence_file}"

operator_scaled=false
target_pause_applied=false
controller_stop_started=false
stage_watch_open=false
stage_watch_pid=""
api_fault_applied=false
api_service_ip=""
operator_fault_pod=""
operator_fault_pod_uid=""
leader_before_pod=""
leader_before_uid=""
leader_before_transitions=""
leader_after_pod=""
leader_after_uid=""
leader_after_transitions=""
leader_target_event_count_before=""
leader_failover_seconds=0
operator_original_replicas="$({
  kubectl -n "${operator_namespace}" get deployment \
    "${operator_deployment}" -o json | jq -r '.spec.replicas // 1'
})"
operator_selector="$({
  kubectl -n "${operator_namespace}" get deployment \
    "${operator_deployment}" -o json | jq -r '
      .spec.selector.matchLabels |
      to_entries | map(.key + "=" + .value) | join(",")'
})"

restore_operator() {
  if [[ "${operator_scaled}" == true ]]; then
    kubectl -n "${operator_namespace}" scale deployment \
      "${operator_deployment}" \
      --replicas="${operator_original_replicas}" >/dev/null || true
    operator_scaled=false
  fi
}

operator_runtime_json() {
  kubectl -n "${operator_namespace}" get pods -l "${operator_selector}" \
    -o json | jq -c '[.items[] | {
      name: .metadata.name,
      uid: .metadata.uid,
      node: .spec.nodeName,
      ready: (any(.status.conditions[]?;
        .type == "Ready" and .status == "True")),
      containerID: (.status.containerStatuses[0].containerID // ""),
      restartCount: (.status.containerStatuses[0].restartCount // 0),
      state: .status.containerStatuses[0].state
    }]'
}

leader_election_json() {
  kubectl -n "${operator_namespace}" get lease \
    "${leader_election_lease}" -o json | jq -c '{
      name: .metadata.name,
      holderIdentity: (.spec.holderIdentity // ""),
      holderPod: ((.spec.holderIdentity // "") | split("_")[0]),
      acquireTime: (.spec.acquireTime // ""),
      renewTime: (.spec.renewTime // ""),
      durationSeconds: (.spec.leaseDurationSeconds // 0),
      transitions: (.spec.leaseTransitions // 0)
    }'
}

operator_ready_count() {
  operator_runtime_json | jq '[.[] |
    select(.ready == true and .restartCount == 0)] | length'
}

wait_operator_ready_count() {
  local expected="$1"
  local timeout_seconds="$2"
  local deadline=$((SECONDS + timeout_seconds))

  while ((SECONDS < deadline)); do
    if [[ "$(operator_ready_count)" -eq "${expected}" ]] &&
      [[ "$(operator_runtime_json | jq 'length')" -eq "${expected}" ]]; then
      return 0
    fi
    sleep 1
  done
  return 1
}

decommission_event_count() {
  local target="$1"
  kubectl -n "${namespace}" get events -o json | jq \
    --arg target "${target}" '[.items[] |
      select(.reason == "IndexerDecommissionRequested") |
      select((.message // "") | contains($target)) |
      (.series.count // .count // 1)] | add // 0'
}

force_remove_api_disconnect() {
  local cleanup_container current_uid

  if [[ -z "${operator_fault_pod}" || -z "${operator_fault_pod_uid}" ||
    -z "${api_service_ip}" ]]; then
    return
  fi
  current_uid="$({
    kubectl -n "${operator_namespace}" get pod \
      "${operator_fault_pod}" -o jsonpath='{.metadata.uid}'
  } 2>/dev/null)" || return
  if [[ "${current_uid}" != "${operator_fault_pod_uid}" ]]; then
    return
  fi

  cleanup_container="shc85-api-cleanup-$(date -u +%s)"
  # The quoted program is evaluated inside the diagnostic container.
  # shellcheck disable=SC2016
  kubectl -n "${operator_namespace}" debug \
    "pod/${operator_fault_pod}" --attach=true --quiet \
    --container="${cleanup_container}" --image="${api_fault_image}" \
    --profile=sysadmin --custom="${api_fault_profile}" -- \
    env API_SERVICE_IP="${api_service_ip}" /bin/bash -lc '
      set -euo pipefail
      rule=(-p tcp -d "${API_SERVICE_IP}" --dport 443 -m comment
        --comment shc85-api-disconnect -j REJECT)
      removed=0
      while iptables -C OUTPUT "${rule[@]}" >/dev/null 2>&1; do
        iptables -D OUTPUT "${rule[@]}"
        removed=$((removed + 1))
      done
      after="$(curl -skS --connect-timeout 3 -o /dev/null \
        -w "%{http_code}" "https://${API_SERVICE_IP}/version")"
      echo "API_FAULT_FORCE_REMOVED count=${removed} after=${after}"
      test "${after}" = 200
    ' >>"${api_fault_log}" 2>&1 || true
}

release_api_disconnect() {
  local fault_state

  if [[ "${api_fault_applied}" == true && -n "${operator_fault_pod}" ]]; then
    kubectl -n "${operator_namespace}" exec "${operator_fault_pod}" \
      -c "${api_fault_container}" -- \
      /bin/sh -ec 'touch /tmp/shc85-release' >/dev/null 2>&1 || true
    for _ in $(seq 1 30); do
      fault_state="$({
        kubectl -n "${operator_namespace}" get pod \
          "${operator_fault_pod}" -o json 2>/dev/null | jq -r \
          --arg container "${api_fault_container}" '
            [.status.ephemeralContainerStatuses[]? |
              select(.name == $container) |
              if .state.terminated != null then "terminated"
              elif .state.running != null then "running"
              else "waiting" end][0] // "missing"'
      })" || fault_state=missing
      if [[ "${fault_state}" == terminated ||
        "${fault_state}" == missing ]]; then
        break
      fi
      sleep 1
    done
    kubectl -n "${operator_namespace}" logs "${operator_fault_pod}" \
      -c "${api_fault_container}" >"${api_fault_log}" 2>&1 || true
  fi
  if [[ -r "${api_fault_log}" ]] &&
    ! grep -q '^API_FAULT_REMOVED after=200$' "${api_fault_log}"; then
    force_remove_api_disconnect
  fi
  api_fault_applied=false
}

stop_stage_watch() {
  if [[ "${stage_watch_open}" == true ]]; then
    exec 3<&-
    stage_watch_open=false
  fi
  if [[ -n "${stage_watch_pid}" ]]; then
    pkill -TERM -P "${stage_watch_pid}" >/dev/null 2>&1 || true
    kill "${stage_watch_pid}" >/dev/null 2>&1 || true
    wait "${stage_watch_pid}" >/dev/null 2>&1 || true
    stage_watch_pid=""
  fi
}

remove_target_pause() {
  if [[ "${target_pause_applied}" == true ]]; then
    kubectl -n "${namespace}" annotate \
      indexercluster.enterprise.splunk.com "${cr_name}" \
      "${pause_annotation}-" >/dev/null 2>&1 || true
    target_pause_applied=false
  fi
}

cleanup() {
  stop_stage_watch
  release_api_disconnect
  restore_operator
  remove_target_pause
}
trap cleanup EXIT

pods_json() {
  kubectl -n "${namespace}" get pods -o json | jq -c \
    --arg prefix "${pod_prefix}" \
    '[.items[] |
      select(.metadata.name | startswith($prefix)) |
      {
        metadata: {
          name: .metadata.name,
          uid: .metadata.uid,
          labels: {
            "controller-revision-hash":
              .metadata.labels["controller-revision-hash"]
          }
        },
        status: {
          conditions: [.status.conditions[]? |
            select(.type == "Ready") | {type, status, reason}],
          containerStatuses: [.status.containerStatuses[]? | {
            name,
            restartCount,
            state,
            lastTerminationState
          }]
        }
      }]'
}

endpoint_pods_json() {
  kubectl -n "${namespace}" get endpointslices.discovery.k8s.io -o json |
    jq -c --arg service "${service_name}" '
      [.items[] |
        select(.metadata.labels["kubernetes.io/service-name"] == $service) |
        .endpoints[]? |
        select(.conditions.ready == true) |
        .targetRef.name] | unique | sort'
}

pod_update_json() {
  kubectl -n "${namespace}" get indexercluster.enterprise.splunk.com \
    "${cr_name}" -o json | jq -c '.status.podUpdate // {}'
}

liveness_failure_count() {
  kubectl -n "${namespace}" get events -o json | jq \
    --arg prefix "${pod_prefix}" '
      [.items[] |
        select(.involvedObject.kind == "Pod") |
        select(.involvedObject.name | startswith($prefix)) |
        select(.reason == "Unhealthy") |
        select(.message | startswith("Liveness probe failed")) |
        (.count // 1)] | add // 0'
}

record_sample() {
  local phase="$1"
  local current_pods current_endpoints current_update operator_replicas
  local current_operator_runtime current_leader_election
  current_pods="$(pods_json)"
  current_endpoints="$(endpoint_pods_json)"
  current_update="$(pod_update_json)"
  operator_replicas="$({
    kubectl -n "${operator_namespace}" get deployment \
      "${operator_deployment}" -o json | jq -r '.status.replicas // 0'
  })"
  current_operator_runtime="$(operator_runtime_json)"
  current_leader_election="$(leader_election_json)"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${phase}" \
    "${operator_replicas}" "${current_update}" "${current_pods}" \
    "${current_endpoints}" "$(liveness_failure_count)" \
    "${current_operator_runtime}" "${current_leader_election}" \
    >>"${evidence_file}"
}

start_api_disconnect() {
  local operator_pod_json operator_runtime fault_deadline fault_observed
  local api_fault_max_seconds

  operator_pod_json="$({
    kubectl -n "${operator_namespace}" get pods \
      -l "${operator_selector}" -o json
  })"
  if [[ "$(jq '.items | length' <<<"${operator_pod_json}")" -ne 1 ]]; then
    fail "api-disconnect-requires-one-operator-pod"
  fi
  operator_fault_pod="$(jq -r '.items[0].metadata.name' \
    <<<"${operator_pod_json}")"
  operator_fault_pod_uid="$(jq -r '.items[0].metadata.uid' \
    <<<"${operator_pod_json}")"
  if ! jq -e 'any(.items[0].status.conditions[]?;
      .type == "Ready" and .status == "True")' \
    <<<"${operator_pod_json}" >/dev/null; then
    fail "operator-not-ready-before-api-disconnect"
  fi

  api_service_ip="$({
    kubectl -n default get service kubernetes -o json |
      jq -r '.spec.clusterIP // ""'
  })"
  if ! printf '%s\n' "${api_service_ip}" | grep -Eq \
    '^([0-9]{1,3}\.){3}[0-9]{1,3}$'; then
    fail "api-service-ip-is-not-ipv4"
  fi

  api_fault_max_seconds=$((hold_seconds + 120))
  : >"${api_fault_log}"
  # The quoted program is evaluated inside the diagnostic container.
  # shellcheck disable=SC2016
  kubectl -n "${operator_namespace}" debug \
    "pod/${operator_fault_pod}" --attach=false --quiet \
    --container="${api_fault_container}" --image="${api_fault_image}" \
    --profile=sysadmin --custom="${api_fault_profile}" \
    -- env API_SERVICE_IP="${api_service_ip}" \
    MAX_HOLD_SECONDS="${api_fault_max_seconds}" /bin/bash -lc '
      set -euo pipefail
      rule=(-p tcp -d "${API_SERVICE_IP}" --dport 443 -m comment
        --comment shc85-api-disconnect -j REJECT)
      cleanup() {
        iptables -D OUTPUT "${rule[@]}" >/dev/null 2>&1 || true
      }
      trap cleanup EXIT TERM INT
      if iptables -C OUTPUT "${rule[@]}" >/dev/null 2>&1; then
        echo API_FAULT_FAILED reason=preexisting-rule
        exit 3
      fi
      before="$(curl -skS --connect-timeout 3 -o /dev/null \
        -w "%{http_code}" "https://${API_SERVICE_IP}/version")"
      iptables -I OUTPUT 1 "${rule[@]}"
      if curl -skS --connect-timeout 3 -o /dev/null \
        "https://${API_SERVICE_IP}/version"; then
        echo API_FAULT_FAILED reason=api-still-reachable
        exit 4
      fi
      echo "API_FAULT_APPLIED before=${before} blocked=true"
      deadline=$(($(date +%s) + MAX_HOLD_SECONDS))
      while [[ ! -e /tmp/shc85-release ]] &&
        [[ "$(date +%s)" -lt "${deadline}" ]]; do
        sleep 1
      done
      cleanup
      trap - EXIT TERM INT
      after="$(curl -skS --connect-timeout 3 -o /dev/null \
        -w "%{http_code}" "https://${API_SERVICE_IP}/version")"
      echo "API_FAULT_REMOVED after=${after}"
    ' >"${api_fault_log}" 2>&1
  api_fault_applied=true

  fault_deadline=$((SECONDS + 180))
  fault_observed=false
  while ((SECONDS < fault_deadline)); do
    kubectl -n "${operator_namespace}" logs "${operator_fault_pod}" \
      -c "${api_fault_container}" >"${api_fault_log}" 2>&1 || true
    if grep -q '^API_FAULT_APPLIED .*blocked=true$' "${api_fault_log}"; then
      fault_observed=true
      break
    fi
    if kubectl -n "${operator_namespace}" get pod \
      "${operator_fault_pod}" -o json | jq -e \
      --arg container "${api_fault_container}" '
        any(.status.ephemeralContainerStatuses[]?;
          .name == $container and .state.terminated != null)' \
      >/dev/null; then
      break
    fi
    sleep 1
  done
  if [[ "${fault_observed}" != true ]]; then
    fail "api-disconnect-was-not-applied"
  fi

  operator_runtime="$(operator_runtime_json)"
  if ! jq -e --arg pod "${operator_fault_pod}" \
    --arg uid "${operator_fault_pod_uid}" '
      length == 1 and .[0].name == $pod and .[0].uid == $uid' \
    <<<"${operator_runtime}" >/dev/null; then
    fail "operator-pod-changed-during-api-disconnect-setup"
  fi
}

prepare_leader_failover() {
  local baseline_lease baseline_holder baseline_transitions
  local current_lease current_runtime stable_deadline initial_renew
  local renew_progressed=false

  if [[ "${operator_original_replicas}" -ne 1 ]]; then
    fail "leader-failover-requires-one-original-operator-replica"
  fi
  baseline_lease="$(leader_election_json)"
  baseline_holder="$(jq -r '.holderPod' <<<"${baseline_lease}")"
  baseline_transitions="$(jq -r '.transitions' <<<"${baseline_lease}")"
  current_runtime="$(operator_runtime_json)"
  if ! jq -e --arg holder "${baseline_holder}" '
      length == 1 and
      .[0].name == $holder and
      .[0].ready == true and
      .[0].restartCount == 0' <<<"${current_runtime}" >/dev/null; then
    fail "single-operator-leader-baseline-not-stable"
  fi

  : >"${leader_failover_log}"
  printf 'LEADER_CONTENDER_BASELINE holder=%s transitions=%s runtime=%s\n' \
    "${baseline_holder}" "${baseline_transitions}" "${current_runtime}" \
    >>"${leader_failover_log}"
  kubectl -n "${operator_namespace}" scale deployment \
    "${operator_deployment}" --replicas=2 >/dev/null
  operator_scaled=true
  if ! wait_operator_ready_count 2 "${leader_failover_timeout_seconds}"; then
    fail "two-operator-contenders-did-not-become-ready"
  fi

  current_lease="$(leader_election_json)"
  baseline_holder="$(jq -r '.holderPod' <<<"${current_lease}")"
  baseline_transitions="$(jq -r '.transitions' <<<"${current_lease}")"
  initial_renew="$(jq -r '.renewTime' <<<"${current_lease}")"
  stable_deadline=$((SECONDS + leader_stability_seconds))
  while ((SECONDS < stable_deadline)); do
    current_lease="$(leader_election_json)"
    current_runtime="$(operator_runtime_json)"
    if [[ "$(jq -r '.holderPod' <<<"${current_lease}")" != \
      "${baseline_holder}" ]] ||
      [[ "$(jq -r '.transitions' <<<"${current_lease}")" -ne \
        "${baseline_transitions}" ]] ||
      ! jq -e --arg holder "${baseline_holder}" '
        length == 2 and
        all(.[]; .ready == true and .restartCount == 0) and
        any(.[]; .name == $holder)' <<<"${current_runtime}" >/dev/null; then
      fail "two-operator-contender-baseline-not-stable"
    fi
    if [[ "$(jq -r '.renewTime' <<<"${current_lease}")" != \
      "${initial_renew}" ]]; then
      renew_progressed=true
    fi
    sleep 1
  done
  if [[ "${renew_progressed}" != true ]]; then
    fail "leader-lease-did-not-renew-with-two-contenders"
  fi
  printf 'LEADER_CONTENDERS_READY holder=%s transitions=%s runtime=%s\n' \
    "${baseline_holder}" "${baseline_transitions}" "${current_runtime}" \
    >>"${leader_failover_log}"
  record_sample "leader-contenders-ready"
}

perform_leader_failover() {
  local lease_before runtime_before delete_started failover_deadline
  local current_lease current_runtime successor_found=false
  local stability_deadline initial_renew renew_progressed=false
  local current_update expected_decommission_requested successor_log

  if ! wait_operator_ready_count 2 "${leader_failover_timeout_seconds}"; then
    fail "operator-contenders-not-ready-at-failover-boundary"
  fi
  lease_before="$(leader_election_json)"
  runtime_before="$(operator_runtime_json)"
  leader_before_pod="$(jq -r '.holderPod' <<<"${lease_before}")"
  leader_before_transitions="$(jq -r '.transitions' <<<"${lease_before}")"
  leader_before_uid="$(jq -r --arg holder "${leader_before_pod}" '
    [.[] | select(.name == $holder) | .uid][0] // ""' \
    <<<"${runtime_before}")"
  if [[ -z "${leader_before_pod}" || -z "${leader_before_uid}" ]] ||
    ! jq -e --arg holder "${leader_before_pod}" '
      length == 2 and
      all(.[]; .ready == true and .restartCount == 0) and
      any(.[]; .name == $holder)' <<<"${runtime_before}" >/dev/null; then
    fail "active-leader-not-among-ready-contenders"
  fi

  expected_decommission_requested="$(jq -r '.decommissionRequestedAt // ""' \
    <<<"${target_operation}")"
  leader_target_event_count_before="$(decommission_event_count "${target_pod}")"
  printf 'LEADER_FAILOVER_BEFORE holder=%s uid=%s transitions=%s operation=%s target=%s targetUID=%s decommissionRequestedAt=%s eventCount=%s runtime=%s\n' \
    "${leader_before_pod}" "${leader_before_uid}" \
    "${leader_before_transitions}" "${operation_id}" "${target_pod}" \
    "${target_uid}" "${expected_decommission_requested}" \
    "${leader_target_event_count_before}" "${runtime_before}" \
    >>"${leader_failover_log}"

  delete_started="$(date +%s)"
  kubectl -n "${operator_namespace}" delete pod "${leader_before_pod}" \
    --grace-period=0 --force --wait=false >/dev/null
  record_sample "leader-delete-requested"

  failover_deadline=$((SECONDS + leader_failover_timeout_seconds))
  while ((SECONDS < failover_deadline)); do
    current_lease="$(leader_election_json)"
    current_runtime="$(operator_runtime_json)"
    leader_after_pod="$(jq -r '.holderPod' <<<"${current_lease}")"
    leader_after_transitions="$(jq -r '.transitions' <<<"${current_lease}")"
    if [[ -n "${leader_after_pod}" &&
      "${leader_after_pod}" != "${leader_before_pod}" &&
      "${leader_after_transitions}" -gt "${leader_before_transitions}" ]] &&
      jq -e --arg holder "${leader_after_pod}" '
        any(.[];
          .name == $holder and .ready == true and .restartCount == 0)' \
        <<<"${current_runtime}" >/dev/null; then
      successor_found=true
      break
    fi
    sleep 1
  done
  if [[ "${successor_found}" != true ]]; then
    fail "different-live-leader-was-not-elected"
  fi
  if ! wait_operator_ready_count 2 "${leader_failover_timeout_seconds}"; then
    fail "two-operator-contenders-did-not-recover-after-leader-loss"
  fi

  current_runtime="$(operator_runtime_json)"
  if jq -e --arg uid "${leader_before_uid}" \
    'any(.[]; .uid == $uid)' <<<"${current_runtime}" >/dev/null; then
    fail "deleted-leader-uid-survived-replacement"
  fi
  leader_after_uid="$(jq -r --arg holder "${leader_after_pod}" '
    [.[] | select(.name == $holder) | .uid][0] // ""' \
    <<<"${current_runtime}")"
  if [[ -z "${leader_after_uid}" ]] ||
    ! jq -e 'length == 2 and
      all(.[]; .ready == true and .restartCount == 0)' \
      <<<"${current_runtime}" >/dev/null; then
    fail "operator-contenders-not-stable-after-leader-loss"
  fi
  successor_log="$(kubectl -n "${operator_namespace}" logs \
    "${leader_after_pod}")"
  if ! grep -Fq 'Successfully acquired lease' <<<"${successor_log}"; then
    fail "successor-leader-acquisition-not-logged"
  fi

  current_lease="$(leader_election_json)"
  initial_renew="$(jq -r '.renewTime' <<<"${current_lease}")"
  stability_deadline=$((SECONDS + leader_stability_seconds))
  while ((SECONDS < stability_deadline)); do
    current_lease="$(leader_election_json)"
    current_runtime="$(operator_runtime_json)"
    if [[ "$(jq -r '.holderPod' <<<"${current_lease}")" != \
      "${leader_after_pod}" ]] ||
      [[ "$(jq -r '.transitions' <<<"${current_lease}")" -ne \
        "${leader_after_transitions}" ]] ||
      ! jq -e --arg holder "${leader_after_pod}" '
        length == 2 and
        all(.[]; .ready == true and .restartCount == 0) and
        any(.[]; .name == $holder)' <<<"${current_runtime}" >/dev/null; then
      fail "successor-leader-did-not-remain-stable"
    fi
    if [[ "$(jq -r '.renewTime' <<<"${current_lease}")" != \
      "${initial_renew}" ]]; then
      renew_progressed=true
    fi
    sleep 1
  done
  if [[ "${renew_progressed}" != true ]]; then
    fail "successor-leader-lease-did-not-renew"
  fi

  current_update="$(pod_update_json)"
  if ! jq -e --arg operation "${operation_id}" \
    --arg pod "${target_pod}" --arg uid "${target_uid}" \
    --arg source "${source_revision}" --arg desired "${desired_revision}" \
    --arg requested "${expected_decommission_requested}" \
    --argjson ordinal "${target_ordinal}" '
      .stage != "Cancelled" and
      .desiredRevision == $desired and
      ((.operationID == $operation and
        .sourceRevision == $source and
        ((.targetOrdinal // -1) < $ordinal or
         ((.targetOrdinal // -1) == $ordinal and
          .targetPod == $pod and
          .targetPodUID == $uid and
          .decommissionRequestedAt == $requested))) or
       (.operationID != $operation and
        (.targetOrdinal // -1) < $ordinal))' \
      <<<"${current_update}" >/dev/null; then
    fail "durable-operation-was-not-resumed-after-leader-loss"
  fi

  leader_failover_seconds=$(($(date +%s) - delete_started))
  printf 'LEADER_FAILOVER_AFTER holder=%s uid=%s transitions=%s durationSeconds=%s operation=%s podUpdate=%s runtime=%s\n' \
    "${leader_after_pod}" "${leader_after_uid}" \
    "${leader_after_transitions}" "${leader_failover_seconds}" \
    "${operation_id}" "${current_update}" "${current_runtime}" \
    >>"${leader_failover_log}"
  record_sample "leader-failover-complete"
}

assert_leader_failover_stable() {
  local current_lease current_runtime

  current_lease="$(leader_election_json)"
  current_runtime="$(operator_runtime_json)"
  if [[ "$(jq -r '.holderPod' <<<"${current_lease}")" != \
    "${leader_after_pod}" ]] ||
    [[ "$(jq -r '.transitions' <<<"${current_lease}")" -ne \
      "${leader_after_transitions}" ]] ||
    ! jq -e --arg holder "${leader_after_pod}" '
      length == 2 and
      all(.[]; .ready == true and .restartCount == 0) and
      any(.[]; .name == $holder)' <<<"${current_runtime}" >/dev/null; then
    fail "leader-or-contender-changed-during-resumed-roll"
  fi
}

restore_single_operator_leader() {
  local deadline current_runtime current_lease only_pod candidate_holder=""
  local candidate_renew="" stable_started=0 renew_progressed=false

  kubectl -n "${operator_namespace}" scale deployment \
    "${operator_deployment}" --replicas="${operator_original_replicas}" \
    >/dev/null
  if ! wait_operator_ready_count 1 "${leader_failover_timeout_seconds}"; then
    fail "single-operator-replica-did-not-restore"
  fi

  deadline=$((SECONDS + leader_failover_timeout_seconds))
  while ((SECONDS < deadline)); do
    current_runtime="$(operator_runtime_json)"
    current_lease="$(leader_election_json)"
    only_pod="$(jq -r '.[0].name // ""' <<<"${current_runtime}")"
    if [[ "$(jq -r '.holderPod' <<<"${current_lease}")" == \
      "${only_pod}" ]] &&
      jq -e 'length == 1 and .[0].ready == true and
        .[0].restartCount == 0' <<<"${current_runtime}" >/dev/null; then
      if [[ "${candidate_holder}" != "${only_pod}" ]]; then
        candidate_holder="${only_pod}"
        candidate_renew="$(jq -r '.renewTime' <<<"${current_lease}")"
        stable_started="${SECONDS}"
        renew_progressed=false
      elif [[ "$(jq -r '.renewTime' <<<"${current_lease}")" != \
        "${candidate_renew}" ]]; then
        renew_progressed=true
      fi
      if ((SECONDS - stable_started >= leader_stability_seconds)) &&
        [[ "${renew_progressed}" == true ]]; then
        operator_scaled=false
        record_sample "single-operator-leader-restored"
        return 0
      fi
    else
      candidate_holder=""
      stable_started=0
      renew_progressed=false
    fi
    sleep 1
  done
  fail "single-operator-leader-did-not-stabilize"
}

fail() {
  record_sample "FAIL-$1" || true
  printf 'FAIL: %s; evidence=%s\n' "$1" "${evidence_file}" >&2
  exit 1
}

cr_json="$({
  kubectl -n "${namespace}" get indexercluster.enterprise.splunk.com \
    "${cr_name}" -o json
})"
desired_replicas="$(jq -r '.spec.replicas' <<<"${cr_json}")"
if [[ "$(jq -r '.status.phase' <<<"${cr_json}")" != Ready ]] ||
  [[ "$(jq -r '.status.readyReplicas' <<<"${cr_json}")" \
    -ne "${desired_replicas}" ]]; then
  fail "baseline-indexercluster-not-ready"
fi
baseline_operation_stage="$(jq -r '.status.podUpdate.stage // ""' \
  <<<"${cr_json}")"
if [[ -n "${baseline_operation_stage}" &&
  "${baseline_operation_stage}" != Completed &&
  "${baseline_operation_stage}" != Cancelled ]]; then
  fail "baseline-pod-update-still-active"
fi
if [[ "${hold_stage}" == TargetSelected ]] &&
  jq -e --arg annotation "${pause_annotation}" \
    '.metadata.annotations[$annotation] == "true"' \
    <<<"${cr_json}" >/dev/null; then
  fail "baseline-indexercluster-paused"
fi

baseline_pods="$(pods_json)"
baseline_endpoints="$(endpoint_pods_json)"
baseline_revision="$({
  kubectl -n "${namespace}" get statefulset "${statefulset}" -o json |
    jq -r '.status.updateRevision // ""'
})"
if [[ "$(jq 'length' <<<"${baseline_pods}")" -ne "${desired_replicas}" ]] ||
  [[ "$(jq '[.[] | select(any(.status.conditions[]?;
      .type == "Ready" and .status == "True"))] | length' \
      <<<"${baseline_pods}")" -ne "${desired_replicas}" ]] ||
  [[ "$(jq 'length' <<<"${baseline_endpoints}")" -ne "${desired_replicas}" ]] ||
  [[ "$(jq '[.[].status.containerStatuses[]?.restartCount] | add // 0' \
      <<<"${baseline_pods}")" -ne 0 ]]; then
  fail "baseline-pods-not-stable"
fi
if [[ -z "${baseline_revision}" ]] ||
  ! jq -e --arg revision "${baseline_revision}" --argjson desired "${desired_replicas}" '
    length == $desired and
    all(.[]; .metadata.labels["controller-revision-hash"] == $revision)' \
    <<<"${baseline_pods}" >/dev/null; then
  fail "baseline-pods-not-on-update-revision"
fi

record_sample "baseline"
if [[ "${controller_fault}" == LeaderFailover ]]; then
  prepare_leader_failover
fi

if [[ "${hold_stage}" == TargetSelected ]]; then
  exec 3< <(
    kubectl -n "${namespace}" get \
      indexercluster.enterprise.splunk.com "${cr_name}" \
      --watch --output-watch-events -o json |
      jq --unbuffered -c '
        select(.object.status.podUpdate.stage == "TargetSelected") |
        .object.status.podUpdate'
  )
  stage_watch_open=true
  stage_watch_pid="$!"
fi

trigger_patch="$(jq -cn --arg run "${run_id}" \
  '{spec:{podAnnotations:{"qualification.splunk.com/shc85-revision":$run}}}')"
kubectl -n "${namespace}" patch indexercluster.enterprise.splunk.com \
  "${cr_name}" --type merge -p "${trigger_patch}" >/dev/null
if [[ "${hold_stage}" != TargetSelected ]]; then
  record_sample "revision-triggered"
fi

stage_deadline=$((SECONDS + stage_timeout_seconds))
target_operation=""
stage_polls=0
if [[ "${hold_stage}" == TargetSelected ]]; then
  if ! IFS= read -r -t "${stage_timeout_seconds}" target_operation <&3; then
    stop_stage_watch
    fail "TargetSelected-timeout"
  fi

  pause_patch="$(jq -cn --arg annotation "${pause_annotation}" '
    {metadata:{annotations:{($annotation):"true"}}}')"
  target_pause_applied=true
  operator_scaled=true
  controller_stop_started=true
  fault_injection_failed=false

  kubectl -n "${namespace}" patch \
    indexercluster.enterprise.splunk.com "${cr_name}" \
    --type merge -p "${pause_patch}" >/dev/null &
  pause_pid="$!"
  kubectl -n "${operator_namespace}" scale deployment \
    "${operator_deployment}" --replicas=0 >/dev/null &
  scale_pid="$!"
  kubectl -n "${operator_namespace}" delete pod \
    -l "${operator_selector}" --grace-period=0 --force --wait=false \
    >/dev/null &
  delete_pid="$!"

  if ! wait "${pause_pid}"; then
    fault_injection_failed=true
  fi
  if ! wait "${scale_pid}"; then
    fault_injection_failed=true
  fi
  if ! wait "${delete_pid}"; then
    fault_injection_failed=true
  fi
  stop_stage_watch
  if [[ "${fault_injection_failed}" == true ]]; then
    fail "target-selected-fault-injection-failed"
  fi
else
  while ((SECONDS < stage_deadline)); do
    current_update="$(pod_update_json)"
    current_stage="$(jq -r '.stage // ""' <<<"${current_update}")"
    current_observed_decommissioning="$({
      jq -r '.observedDecommissioning // false' <<<"${current_update}"
    })"
    hold_boundary_observed=false
    if [[ "${current_stage}" == "${hold_stage}" ]]; then
      case "${hold_stage}" in
      WithdrawingReadiness)
        candidate_target="$(jq -r '.targetPod // ""' <<<"${current_update}")"
        if [[ -n "${candidate_target}" ]] &&
          kubectl -n "${namespace}" exec "${candidate_target}" -c splunk -- \
            /bin/sh -ec \
            "grep -Fx 'export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true' '${driver_file}'" \
            >/dev/null 2>&1; then
          hold_boundary_observed=true
        fi
        ;;
      Decommissioning)
        if [[ "${current_observed_decommissioning}" == true ]]; then
          hold_boundary_observed=true
        fi
        ;;
      ReadyForReplacement)
        hold_boundary_observed=true
        ;;
      esac
    fi
    if [[ "${hold_boundary_observed}" == true ]]; then
      target_operation="${current_update}"
      break
    fi
    stage_polls=$((stage_polls + 1))
    if ((stage_polls % 25 == 0)); then
      record_sample "waiting-${hold_stage}"
    fi
    sleep 0.2
  done
fi
if [[ -z "${target_operation}" ]]; then
  fail "${hold_stage}-timeout"
fi

operation_id="$(jq -r '.operationID' <<<"${target_operation}")"
target_pod="$(jq -r '.targetPod' <<<"${target_operation}")"
target_uid="$(jq -r '.targetPodUID' <<<"${target_operation}")"
target_ordinal="$(jq -r '.targetOrdinal' <<<"${target_operation}")"
source_revision="$(jq -r '.sourceRevision' <<<"${target_operation}")"
desired_revision="$(jq -r '.desiredRevision' <<<"${target_operation}")"

if [[ "${target_ordinal}" -ne $((desired_replicas - 1)) ]]; then
  fail "first-target-not-highest-ordinal"
fi

target_restart="$({
  jq -r --arg pod "${target_pod}" '
    [.[] | select(.metadata.name == $pod) |
      .status.containerStatuses[]?.restartCount] | add // -1' \
    <<<"${baseline_pods}"
})"
if [[ "${target_restart}" -lt 0 ]]; then
  fail "target-missing-before-controller-hold"
fi

# Apply the requested controller fault immediately after observing the durable
# boundary. ControllerAbsent removes all controller processes. APIDisconnected
# leaves the scheduled Pod in place and rejects only traffic to the in-cluster
# Kubernetes API Service from that Pod network namespace. LeaderFailover force
# deletes the active lease holder while another controller contender is ready.
# In every mode, slower identity and runtime assertions happen only after the
# fault is proven.
if [[ "${controller_fault}" == ControllerAbsent ]]; then
  if [[ "${controller_stop_started}" != true ]]; then
    kubectl -n "${operator_namespace}" scale deployment \
      "${operator_deployment}" --replicas=0 >/dev/null
    operator_scaled=true
    kubectl -n "${operator_namespace}" delete pod \
      -l "${operator_selector}" --grace-period=0 --force --wait=false \
      >/dev/null
  fi

  scale_deadline=$((SECONDS + 180))
  while ((SECONDS < scale_deadline)); do
    deployment_replicas="$({
      kubectl -n "${operator_namespace}" get deployment \
        "${operator_deployment}" -o json | jq -r '.status.replicas // 0'
    })"
    operator_pods="$({
      kubectl -n "${operator_namespace}" get pods -l "${operator_selector}" \
        -o json | jq '.items | length'
    })"
    if [[ "${deployment_replicas}" -eq 0 && "${operator_pods}" -eq 0 ]]; then
      break
    fi
    sleep 1
  done
  if [[ "${deployment_replicas}" -ne 0 || "${operator_pods}" -ne 0 ]]; then
    fail "operator-did-not-become-absent"
  fi
  if [[ "${hold_stage}" == TargetSelected ]]; then
    record_sample "revision-triggered-target-selected-controller-absent"
  fi
elif [[ "${controller_fault}" == APIDisconnected ]]; then
  start_api_disconnect
else
  perform_leader_failover
fi

if [[ "${controller_fault}" == LeaderFailover ]]; then
  : # The resumed controller may already have replaced the original target.
elif [[ "${hold_stage}" == TargetSelected ]]; then
  if kubectl -n "${namespace}" exec "${target_pod}" -c splunk -- \
    /bin/sh -ec "grep -Fx 'export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true' '${driver_file}'" \
    >/dev/null 2>&1; then
    fail "target-selected-has-readiness-withdrawal-marker"
  fi
elif ! kubectl -n "${namespace}" exec "${target_pod}" -c splunk -- \
  /bin/sh -ec "grep -Fx 'export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true' '${driver_file}'" \
  >/dev/null; then
  fail "lifecycle-hold-marker-missing"
fi

if [[ "${hold_stage}" == WithdrawingReadiness ]]; then
  withdrawal_deadline=$((SECONDS + 180))
  withdrawal_observed=false
  while ((SECONDS < withdrawal_deadline)); do
    withdrawal_pods="$(pods_json)"
    withdrawal_endpoints="$(endpoint_pods_json)"
    if ! jq -e --arg pod "${target_pod}" --arg uid "${target_uid}" \
      --argjson restart "${target_restart}" '
        any(.[];
          .metadata.name == $pod and
          .metadata.uid == $uid and
          ([.status.containerStatuses[]?.restartCount] | add // -1) == $restart and
          any(.status.containerStatuses[]?; .state.running != null))' \
      <<<"${withdrawal_pods}" >/dev/null; then
      fail "withdrawing-target-changed-before-readiness-withdrawal"
    fi
    if jq -e --arg pod "${target_pod}" '
        any(.[];
          .metadata.name == $pod and
          (any(.status.conditions[]?;
            .type == "Ready" and .status == "True") | not))' \
        <<<"${withdrawal_pods}" >/dev/null &&
      ! jq -e --arg pod "${target_pod}" 'index($pod) != null' \
        <<<"${withdrawal_endpoints}" >/dev/null; then
      withdrawal_observed=true
      break
    fi
    sleep 1
  done
  if [[ "${withdrawal_observed}" != true ]]; then
    fail "readiness-withdrawal-not-observed-with-controller-absent"
  fi
fi
record_sample "${hold_stage}-controller-${controller_fault}"

if [[ "${controller_fault}" == LeaderFailover ]]; then
  actual_hold_seconds="${leader_failover_seconds}"
else
  controller_fault_start="$(date +%s)"
  hold_deadline=$((controller_fault_start + hold_seconds))
  while [[ "$(date +%s)" -lt "${hold_deadline}" ]]; do
  current_update="$(pod_update_json)"
  if ! jq -e \
    --arg operation "${operation_id}" \
    --arg pod "${target_pod}" \
    --arg uid "${target_uid}" \
    --arg source "${source_revision}" \
    --arg desired "${desired_revision}" \
    --arg stage "${hold_stage}" '
      .operationID == $operation and
      .stage == $stage and
      .targetPod == $pod and
      .targetPodUID == $uid and
      .sourceRevision == $source and
      .desiredRevision == $desired and
      (if $stage == "Decommissioning" then
        .observedDecommissioning == true and
        .decommissionRequestedAt != null
      else true end)' <<<"${current_update}" >/dev/null; then
    fail "durable-operation-changed-during-controller-fault"
  fi

  if [[ "${controller_fault}" == APIDisconnected ]]; then
    current_operator_pod="$({
      kubectl -n "${operator_namespace}" get pod \
        "${operator_fault_pod}" -o json
    })" || fail "operator-pod-missing-during-api-disconnect"
    if [[ "$(jq -r '.metadata.uid' <<<"${current_operator_pod}")" != \
      "${operator_fault_pod_uid}" ]] ||
      ! jq -e --arg container "${api_fault_container}" '
        any(.status.ephemeralContainerStatuses[]?;
          .name == $container and .state.running != null)' \
        <<<"${current_operator_pod}" >/dev/null; then
      fail "operator-pod-or-api-fault-container-changed"
    fi
    if ! kubectl -n "${operator_namespace}" exec \
      "${operator_fault_pod}" -c "${api_fault_container}" -- \
      iptables -C OUTPUT -p tcp -d "${api_service_ip}" --dport 443 \
      -m comment --comment shc85-api-disconnect -j REJECT \
      >/dev/null 2>&1; then
      fail "api-disconnect-rule-lost-during-hold"
    fi
  fi

  current_pods="$(pods_json)"
  current_endpoints="$(endpoint_pods_json)"
  if [[ "${hold_stage}" == TargetSelected ]]; then
    pause_value="$({
      kubectl -n "${namespace}" get \
        indexercluster.enterprise.splunk.com "${cr_name}" -o json |
        jq -r --arg annotation "${pause_annotation}" \
          '.metadata.annotations[$annotation] // ""'
    })"
    if [[ "${pause_value}" != true ]]; then
      fail "target-selected-pause-lost-during-controller-fault"
    fi
    if ! jq -e --arg pod "${target_pod}" --arg uid "${target_uid}" \
      --argjson restart "${target_restart}" '
        any(.[];
          .metadata.name == $pod and
          .metadata.uid == $uid and
          ([.status.containerStatuses[]?.restartCount] | add // -1) == $restart and
          any(.status.containerStatuses[]?; .state.running != null) and
          any(.status.conditions[]?;
            .type == "Ready" and .status == "True"))' \
      <<<"${current_pods}" >/dev/null; then
      fail "target-selected-identity-liveness-or-readiness-changed"
    fi
    if ! jq -e --arg pod "${target_pod}" 'index($pod) != null' \
      <<<"${current_endpoints}" >/dev/null; then
      fail "target-selected-left-service"
    fi
  else
    if ! jq -e --arg pod "${target_pod}" --arg uid "${target_uid}" \
      --argjson restart "${target_restart}" '
        any(.[];
          .metadata.name == $pod and
          .metadata.uid == $uid and
          ([.status.containerStatuses[]?.restartCount] | add // -1) == $restart and
          any(.status.containerStatuses[]?; .state.running != null) and
          (any(.status.conditions[]?;
            .type == "Ready" and .status == "True") | not))' \
      <<<"${current_pods}" >/dev/null; then
      fail "held-target-identity-liveness-or-readiness-changed"
    fi
    if jq -e --arg pod "${target_pod}" 'index($pod) != null' \
      <<<"${current_endpoints}" >/dev/null; then
      fail "held-target-returned-to-service"
    fi
  fi

  while IFS=$'\t' read -r pod uid restart; do
    [[ "${pod}" != "${target_pod}" ]] || continue
    if ! jq -e --arg pod "${pod}" --arg uid "${uid}" \
      --argjson restart "${restart}" '
        any(.[];
          .metadata.name == $pod and
          .metadata.uid == $uid and
          ([.status.containerStatuses[]?.restartCount] | add // -1) == $restart and
          any(.status.conditions[]?;
            .type == "Ready" and .status == "True"))' \
      <<<"${current_pods}" >/dev/null; then
      fail "non-target-pod-changed-during-controller-fault"
    fi
    if ! jq -e --arg pod "${pod}" 'index($pod) != null' \
      <<<"${current_endpoints}" >/dev/null; then
      fail "non-target-left-service-during-controller-fault"
    fi
  done < <(jq -r '.[] | [
      .metadata.name,
      .metadata.uid,
      ([.status.containerStatuses[]?.restartCount] | add // 0)
    ] | @tsv' <<<"${baseline_pods}")

  if [[ "$(liveness_failure_count)" -ne 0 ]]; then
    fail "liveness-probe-failed-during-planned-hold"
  fi
    record_sample "controller-${controller_fault}-hold"
    sleep "${sample_seconds}"
  done

  actual_hold_seconds=$(($(date +%s) - controller_fault_start))
  if ((actual_hold_seconds < hold_seconds)); then
    fail "controller-fault-shorter-than-requested"
  fi
  if [[ "${hold_stage}" == TargetSelected ]]; then
    if kubectl -n "${namespace}" exec "${target_pod}" -c splunk -- \
      /bin/sh -ec "grep -Fx 'export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true' '${driver_file}'" \
      >/dev/null 2>&1; then
      fail "target-selected-gained-readiness-withdrawal-marker"
    fi
  elif ! kubectl -n "${namespace}" exec "${target_pod}" -c splunk -- \
    /bin/sh -ec "grep -Fx 'export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true' '${driver_file}'" \
    >/dev/null; then
    fail "lifecycle-hold-marker-lost-during-controller-fault"
  fi
fi
record_sample "controller-${controller_fault}-complete"

if [[ "${controller_fault}" == APIDisconnected ]]; then
  release_api_disconnect
  if ! grep -q '^API_FAULT_REMOVED after=200$' "${api_fault_log}"; then
    fail "api-disconnect-removal-not-proven"
  fi
  restored_operator_pod="$({
    kubectl -n "${operator_namespace}" get pod \
      "${operator_fault_pod}" -o json
  })" || fail "operator-pod-missing-after-api-reconnect"
  if [[ "$(jq -r '.metadata.uid' <<<"${restored_operator_pod}")" != \
    "${operator_fault_pod_uid}" ]]; then
    fail "operator-pod-replaced-during-api-disconnect"
  fi
elif [[ "${controller_fault}" == ControllerAbsent ]]; then
  restore_operator
fi
kubectl -n "${operator_namespace}" rollout status deployment \
  "${operator_deployment}" --timeout=10m >/dev/null
if [[ "${hold_stage}" == TargetSelected ]]; then
  kubectl -n "${namespace}" annotate \
    indexercluster.enterprise.splunk.com "${cr_name}" \
    "${pause_annotation}-" >/dev/null
  target_pause_applied=false
fi
record_sample "controller-${controller_fault}-restored"

roll_deadline=$((SECONDS + roll_timeout_seconds))
seen_ordinals="${target_ordinal}"
last_ordinal="${target_ordinal}"
target_replaced=false
stable_samples=0

while ((SECONDS < roll_deadline)); do
  if [[ "${controller_fault}" == LeaderFailover ]]; then
    assert_leader_failover_stable
  fi
  current_update="$(pod_update_json)"
  current_stage="$(jq -r '.stage // ""' <<<"${current_update}")"
  current_ordinal="$(jq -r '.targetOrdinal // -1' <<<"${current_update}")"
  if [[ "${current_ordinal}" -ge 0 && "${current_ordinal}" != \
    "${last_ordinal}" ]]; then
    seen_ordinals="${seen_ordinals},${current_ordinal}"
    last_ordinal="${current_ordinal}"
  fi

  current_pods="$(pods_json)"
  current_endpoints="$(endpoint_pods_json)"
  pod_count="$(jq 'length' <<<"${current_pods}")"
  ready_count="$(jq '[.[] | select(any(.status.conditions[]?;
      .type == "Ready" and .status == "True"))] | length' \
    <<<"${current_pods}")"
  endpoint_count="$(jq 'length' <<<"${current_endpoints}")"
  restart_count="$(jq \
    '[.[].status.containerStatuses[]?.restartCount] | add // 0' \
    <<<"${current_pods}")"

  if ((desired_replicas - ready_count > 1)) ||
    ((endpoint_count < desired_replicas - 1)); then
    fail "more-than-one-indexer-unavailable"
  fi
  if ((restart_count != 0)); then
    fail "indexer-container-restarted-during-roll"
  fi
  if jq -e --arg pod "${target_pod}" --arg uid "${target_uid}" '
      any(.[]; .metadata.name == $pod and .metadata.uid != $uid)' \
      <<<"${current_pods}" >/dev/null; then
    target_replaced=true
  fi

  sts_json="$({
    kubectl -n "${namespace}" get statefulset "${statefulset}" -o json
  })"
  update_revision="$(jq -r '.status.updateRevision // ""' <<<"${sts_json}")"
  all_desired_revision="$(jq -e --arg revision "${update_revision}" \
    --argjson desired "${desired_replicas}" '
      length == $desired and
      all(.[]; .metadata.labels["controller-revision-hash"] == $revision)' \
    <<<"${current_pods}" >/dev/null && printf true || printf false)"
  cr_phase="$({
    kubectl -n "${namespace}" get indexercluster.enterprise.splunk.com \
      "${cr_name}" -o json | jq -r '.status.phase'
  })"

  record_sample "roll-${current_stage:-none}"
  if [[ "${current_stage}" == Completed && "${current_ordinal}" -eq 0 &&
      "${cr_phase}" == Ready && "${pod_count}" -eq "${desired_replicas}" &&
      "${ready_count}" -eq "${desired_replicas}" &&
      "${endpoint_count}" -eq "${desired_replicas}" &&
      "${all_desired_revision}" == true && "${target_replaced}" == true ]]; then
    stable_samples=$((stable_samples + 1))
    if ((stable_samples >= stable_samples_required)); then
      break
    fi
  else
    stable_samples=0
  fi
  sleep "${sample_seconds}"
done

if ((stable_samples < stable_samples_required)); then
  fail "full-roll-did-not-converge"
fi
if [[ "${seen_ordinals}" != "3,2,1,0" ]]; then
  fail "unexpected-target-order-${seen_ordinals}"
fi
if [[ -z "$(jq -r '.servingRecoveryObservedAt // ""' \
  <<<"$(pod_update_json)")" ]]; then
  fail "final-remote-serving-recovery-not-recorded"
fi

while IFS= read -r pod; do
  if kubectl -n "${namespace}" exec "${pod}" -c splunk -- \
    /bin/sh -ec "test -r '${driver_file}' && grep -q '^export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true$' '${driver_file}'" \
    >/dev/null 2>&1; then
    fail "lifecycle-hold-marker-survived-pod-replacement"
  fi
done < <(pods_json | jq -r '.[].metadata.name')

if [[ "${controller_fault}" == LeaderFailover ]]; then
  leader_target_event_count_after="$(decommission_event_count "${target_pod}")"
  if [[ "${leader_target_event_count_after}" -ne \
    "${leader_target_event_count_before}" ]]; then
    fail "original-target-decommission-request-was-duplicated"
  fi
  assert_leader_failover_stable
  printf 'LEADER_ROLL_COMPLETED holder=%s transitions=%s targetEventCountBefore=%s targetEventCountAfter=%s order=%s\n' \
    "${leader_after_pod}" "${leader_after_transitions}" \
    "${leader_target_event_count_before}" \
    "${leader_target_event_count_after}" "${seen_ordinals}" \
    >>"${leader_failover_log}"
  record_sample "leader-failover-roll-complete"
  restore_single_operator_leader
fi

record_sample "PASS"
printf 'PASS: stage=%s controllerFault=%s duration=%ss order=%s stableSamples=%s evidence=%s\n' \
  "${hold_stage}" "${controller_fault}" "${actual_hold_seconds}" "${seen_ordinals}" \
  "${stable_samples}" "${evidence_file}"
