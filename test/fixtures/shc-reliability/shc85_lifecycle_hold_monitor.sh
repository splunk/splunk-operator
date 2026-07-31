#!/usr/bin/env bash

set -euo pipefail

namespace="${SHC85_NAMESPACE:-shc85-lifecycle-hold}"
cr_name="${SHC85_CR_NAME:-shc85-idxc}"
operator_namespace="${SHC85_OPERATOR_NAMESPACE:-splunk-operator}"
operator_deployment="${SHC85_OPERATOR_DEPLOYMENT:-splunk-operator-controller-manager}"
hold_seconds="${SHC85_HOLD_SECONDS:-300}"
hold_stage="${SHC85_HOLD_STAGE:-ReadyForReplacement}"
sample_seconds="${SHC85_SAMPLE_SECONDS:-2}"
stage_timeout_seconds="${SHC85_STAGE_TIMEOUT_SECONDS:-1800}"
roll_timeout_seconds="${SHC85_ROLL_TIMEOUT_SECONDS:-7200}"
stable_samples_required="${SHC85_STABLE_SAMPLES:-10}"
run_id="${SHC85_RUN_ID:-shc85-hold-$(date -u +%Y%m%dT%H%M%SZ)}"
evidence_file="${SHC85_EVIDENCE_FILE:-build/_test/shc85/${run_id}.tsv}"

statefulset="splunk-${cr_name}-indexer"
service_name="${statefulset}-service"
pod_prefix="${statefulset}-"
driver_file="/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"

for command in kubectl jq; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    printf 'required command is unavailable: %s\n' "${command}" >&2
    exit 2
  fi
done

for value in "${hold_seconds}" "${sample_seconds}" \
  "${stage_timeout_seconds}" "${roll_timeout_seconds}" \
  "${stable_samples_required}"; do
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

mkdir -p "$(dirname "${evidence_file}")"
printf '%s\n' \
  $'timestamp\tphase\toperator_replicas\tpod_update\tpods\tendpoints\tliveness_failures' \
  >"${evidence_file}"

operator_scaled=false
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
trap restore_operator EXIT

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
  current_pods="$(pods_json)"
  current_endpoints="$(endpoint_pods_json)"
  current_update="$(pod_update_json)"
  operator_replicas="$({
    kubectl -n "${operator_namespace}" get deployment \
      "${operator_deployment}" -o json | jq -r '.status.replicas // 0'
  })"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${phase}" \
    "${operator_replicas}" "${current_update}" "${current_pods}" \
    "${current_endpoints}" "$(liveness_failure_count)" \
    >>"${evidence_file}"
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

baseline_pods="$(pods_json)"
baseline_endpoints="$(endpoint_pods_json)"
if [[ "$(jq 'length' <<<"${baseline_pods}")" -ne "${desired_replicas}" ]] ||
  [[ "$(jq '[.[] | select(any(.status.conditions[]?;
      .type == "Ready" and .status == "True"))] | length' \
      <<<"${baseline_pods}")" -ne "${desired_replicas}" ]] ||
  [[ "$(jq 'length' <<<"${baseline_endpoints}")" -ne "${desired_replicas}" ]] ||
  [[ "$(jq '[.[].status.containerStatuses[]?.restartCount] | add // 0' \
      <<<"${baseline_pods}")" -ne 0 ]]; then
  fail "baseline-pods-not-stable"
fi

record_sample "baseline"

trigger_patch="$(jq -cn --arg run "${run_id}" \
  '{spec:{podAnnotations:{"qualification.splunk.com/shc85-revision":$run}}}')"
kubectl -n "${namespace}" patch indexercluster.enterprise.splunk.com \
  "${cr_name}" --type merge -p "${trigger_patch}" >/dev/null
record_sample "revision-triggered"

stage_deadline=$((SECONDS + stage_timeout_seconds))
target_operation=""
stage_polls=0
while ((SECONDS < stage_deadline)); do
  current_update="$(pod_update_json)"
  current_stage="$(jq -r '.stage // ""' <<<"${current_update}")"
  current_observed_decommissioning="$({
    jq -r '.observedDecommissioning // false' <<<"${current_update}"
  })"
  hold_boundary_observed=false
  if [[ "${current_stage}" == "${hold_stage}" ]]; then
    case "${hold_stage}" in
    TargetSelected)
      hold_boundary_observed=true
      ;;
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

# Stop the controller immediately after observing the requested persisted
# stage. TargetSelected is captured before any readiness withdrawal is
# requested, so every peer must remain ready and serving while the controller
# is absent. WithdrawingReadiness is captured as soon as its explicit lifecycle
# marker exists; external readiness and Service withdrawal are then required
# with no controller present. Decommissioning is accepted only after Splunk has
# been observed in that state, not merely after the request was issued. All
# slower runtime and identity checks happen after the Deployment has no
# remaining controller Pod, which minimizes the race with the next
# reconciliation.
kubectl -n "${operator_namespace}" scale deployment \
  "${operator_deployment}" --replicas=0 >/dev/null
operator_scaled=true
kubectl -n "${operator_namespace}" delete pod \
  -l "${operator_selector}" --grace-period=0 --force --wait=false \
  >/dev/null

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
record_sample "${hold_stage}-controller-absent"

operator_absent_start="$(date +%s)"
hold_deadline=$((operator_absent_start + hold_seconds))
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
    fail "durable-operation-changed-during-absence"
  fi

  current_pods="$(pods_json)"
  current_endpoints="$(endpoint_pods_json)"
  if [[ "${hold_stage}" == TargetSelected ]]; then
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
      fail "non-target-pod-changed-during-absence"
    fi
    if ! jq -e --arg pod "${pod}" 'index($pod) != null' \
      <<<"${current_endpoints}" >/dev/null; then
      fail "non-target-left-service-during-absence"
    fi
  done < <(jq -r '.[] | [
      .metadata.name,
      .metadata.uid,
      ([.status.containerStatuses[]?.restartCount] | add // 0)
    ] | @tsv' <<<"${baseline_pods}")

  if [[ "$(liveness_failure_count)" -ne 0 ]]; then
    fail "liveness-probe-failed-during-planned-hold"
  fi
  record_sample "operator-absent-hold"
  sleep "${sample_seconds}"
done

actual_hold_seconds=$(($(date +%s) - operator_absent_start))
if ((actual_hold_seconds < hold_seconds)); then
  fail "operator-absence-shorter-than-requested"
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
  fail "lifecycle-hold-marker-lost-during-absence"
fi
record_sample "operator-absence-complete"

restore_operator
kubectl -n "${operator_namespace}" rollout status deployment \
  "${operator_deployment}" --timeout=10m >/dev/null
record_sample "operator-restored"

roll_deadline=$((SECONDS + roll_timeout_seconds))
seen_ordinals="${target_ordinal}"
last_ordinal="${target_ordinal}"
target_replaced=false
stable_samples=0

while ((SECONDS < roll_deadline)); do
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

record_sample "PASS"
printf 'PASS: stage=%s operator absence=%ss order=%s stableSamples=%s evidence=%s\n' \
  "${hold_stage}" "${actual_hold_seconds}" "${seen_ordinals}" \
  "${stable_samples}" "${evidence_file}"
