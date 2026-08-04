#!/usr/bin/env bash

set -euo pipefail

context="${SHC118_KUBE_CONTEXT:?SHC118_KUBE_CONTEXT is required}"
namespace="${SHC118_NAMESPACE:-shc-final-qualification}"
cr_name="${SHC118_CR_NAME:-shcfinal-shc}"
operator_namespace="${SHC118_OPERATOR_NAMESPACE:-splunk-operator}"
operator_deployment="${SHC118_OPERATOR_DEPLOYMENT:-splunk-operator-controller-manager}"
workload_job="${SHC118_WORKLOAD_JOB:-shc98-incluster-workload}"
expected_operator_image="${SHC118_EXPECTED_OPERATOR_IMAGE:?SHC118_EXPECTED_OPERATOR_IMAGE is required}"
withdrawal_seconds="${SHC118_WITHDRAWAL_SECONDS:-120}"
restart_operator="${SHC118_RESTART_OPERATOR:-true}"
preflight_only="${SHC118_PREFLIGHT_ONLY:-false}"
timeout_seconds="${SHC118_TIMEOUT_SECONDS:-7200}"
sample_interval="${SHC118_SAMPLE_INTERVAL_SECONDS:-2}"
stable_samples_required="${SHC118_STABLE_SAMPLES:-12}"
stable_interval="${SHC118_STABLE_INTERVAL_SECONDS:-5}"
evidence_dir="${SHC118_EVIDENCE_DIR:-build/_test/shc118/endpoint-withdrawal}"
pod_prefix="splunk-${cr_name}-search-head-"
statefulset_name="splunk-${cr_name}-search-head"
service_name="splunk-${cr_name}-search-head-service"
serving_condition="enterprise.splunk.com/shc-serving"
trigger_value="$(date -u +%Y%m%dT%H%M%SZ)"
samples_file="${evidence_dir}/samples.tsv"
summary_file="${evidence_dir}/summary.txt"

collected=false
triggered=false
operator_restarted=false
seen_order=""
last_seen_ordinal=""
minimum_endpoints=999
maximum_unready=0

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'FAIL: required command is unavailable: %s\n' "$1" >&2
    exit 1
  }
}

for command_name in kubectl jq date awk sed; do
  require_command "${command_name}"
done

case "${restart_operator}" in
  true | false) ;;
  *)
    printf 'FAIL: SHC118_RESTART_OPERATOR must be true or false\n' >&2
    exit 1
    ;;
esac
case "${preflight_only}" in
  true | false) ;;
  *)
    printf 'FAIL: SHC118_PREFLIGHT_ONLY must be true or false\n' >&2
    exit 1
    ;;
esac

if ! [[ "${withdrawal_seconds}" =~ ^[0-9]+$ ]] ||
  ((withdrawal_seconds < 1 || withdrawal_seconds > 86400)); then
  printf 'FAIL: SHC118_WITHDRAWAL_SECONDS must be between 1 and 86400\n' >&2
  exit 1
fi
if [[ "${restart_operator}" == true && "${withdrawal_seconds}" -lt 60 ]]; then
  printf 'FAIL: controller-replacement qualification requires a withdrawal interval of at least 60 seconds\n' >&2
  exit 1
fi
if ! [[ "${timeout_seconds}" =~ ^[0-9]+$ ]] || ((timeout_seconds < 1)); then
  printf 'FAIL: SHC118_TIMEOUT_SECONDS must be positive\n' >&2
  exit 1
fi

mkdir -p "${evidence_dir}"

kube() {
  kubectl --context "${context}" "$@"
}

shc_kube() {
  kube -n "${namespace}" "$@"
}

operator_kube() {
  kube -n "${operator_namespace}" "$@"
}

collect_evidence() {
  if [[ "${collected}" == true ]]; then
    return
  fi
  collected=true
  set +e
  shc_kube get searchheadcluster.enterprise.splunk.com "${cr_name}" -o json \
    >"${evidence_dir}/searchheadcluster.json" 2>&1
  shc_kube get statefulset "${statefulset_name}" -o json \
    >"${evidence_dir}/statefulset.json" 2>&1
  shc_kube get pods -o json >"${evidence_dir}/pods.json" 2>&1
  shc_kube get endpointslices.discovery.k8s.io -o json \
    >"${evidence_dir}/endpointslices.json" 2>&1
  shc_kube get events --sort-by=.metadata.creationTimestamp -o json \
    >"${evidence_dir}/events.json" 2>&1
  shc_kube get job "${workload_job}" -o json \
    >"${evidence_dir}/workload-job.json" 2>&1
  shc_kube logs job/"${workload_job}" \
    >"${evidence_dir}/workload.log" 2>&1
  operator_kube get deployment "${operator_deployment}" -o json \
    >"${evidence_dir}/operator-deployment.json" 2>&1
  operator_kube get pods -o json >"${evidence_dir}/operator-pods.json" 2>&1
  operator_kube logs deployment/"${operator_deployment}" --all-containers=true \
    --since=4h >"${evidence_dir}/operator.log" 2>&1
  set -e
}

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  collect_evidence
  exit 1
}

on_exit() {
  local rc=$?
  if ((rc != 0)); then
    collect_evidence
  fi
}
trap on_exit EXIT

pod_condition() {
  local pod_json="$1"
  local condition="$2"
  jq -r --arg condition "${condition}" \
    '[.status.conditions[]? | select(.type == $condition) | .status][0] // "Unknown"' \
    <<<"${pod_json}"
}

routable_endpoint_pods() {
  shc_kube get endpointslices.discovery.k8s.io -o json | jq -c \
    --arg service "${service_name}" \
    '[.items[] |
      select(.metadata.labels["kubernetes.io/service-name"] == $service) |
      .endpoints[]? |
      select(.conditions.ready != false) |
      .targetRef.name] | map(select(. != null)) | unique | sort'
}

search_head_pods() {
  shc_kube get pods -o json | jq -c --arg prefix "${pod_prefix}" \
    '[.items[] | select(.metadata.name | startswith($prefix))] | sort_by(.metadata.name)'
}

active_operator_pod() {
  local selector_json selector
  selector_json="$(operator_kube get deployment "${operator_deployment}" -o json)"
  selector="$(jq -r '.spec.selector.matchLabels | to_entries | map("\(.key)=\(.value)") | join(",")' \
    <<<"${selector_json}")"
  operator_kube get pods -l "${selector}" -o json | jq -c \
    '[.items[] |
      select(.metadata.deletionTimestamp == null) |
      select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] |
      sort_by(.metadata.creationTimestamp) | last // {}'
}

record_sample() {
  timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  cr_json="$(shc_kube get searchheadcluster.enterprise.splunk.com "${cr_name}" -o json)"
  sts_json="$(shc_kube get statefulset "${statefulset_name}" -o json)"
  pods_json="$(search_head_pods)"
  endpoint_pods="$(routable_endpoint_pods)"
  op_json="$(jq -c '.status.lifecycleOperation // {}' <<<"${cr_json}")"
  phase="$(jq -r '.status.phase // ""' <<<"${cr_json}")"
  stage="$(jq -r '.stage // ""' <<<"${op_json}")"
  reason="$(jq -r '.reason // ""' <<<"${op_json}")"
  operation_id="$(jq -r '.operationID // ""' <<<"${op_json}")"
  target_ordinal="$(jq -r '.targetOrdinal // ""' <<<"${op_json}")"
  target_pod="$(jq -r '.targetPod // ""' <<<"${op_json}")"
  target_uid="$(jq -r '.targetPodUID // ""' <<<"${op_json}")"
  proof_uid="$(jq -r '.endpointWithdrawalPodUID // ""' <<<"${op_json}")"
  observed_at="$(jq -r '.endpointWithdrawalObservedAt // ""' <<<"${op_json}")"
  proof_deadline="$(jq -r '.endpointWithdrawalDeadline // ""' <<<"${op_json}")"
  proof_sequence="$(jq -r '.endpointWithdrawalSequence // 0' <<<"${op_json}")"
  invalidated_sequence="$(jq -r '.endpointWithdrawalInvalidatedSequence // 0' <<<"${op_json}")"
  detention_at="$(jq -r '.detentionRequestedAt // ""' <<<"${op_json}")"
  endpoint_count="$(jq 'length' <<<"${endpoint_pods}")"
  unready_count="$(jq '[.[] |
    select([.status.conditions[]? |
      select(.type == "Ready") | .status][0] != "True")] | length' \
    <<<"${pods_json}")"
  restart_count="$(jq '[.[] | [.status.containerStatuses[]?.restartCount] | add // 0] | add // 0' \
    <<<"${pods_json}")"
  pod_detail="$(jq -c --arg serving "${serving_condition}" \
    '[.[] | {
      name: .metadata.name,
      uid: .metadata.uid,
      deleting: (.metadata.deletionTimestamp != null),
      ready: ([.status.conditions[]? | select(.type == "Ready") | .status][0] // "Unknown"),
      serving: ([.status.conditions[]? | select(.type == $serving) | .status][0] // "Unknown"),
      restarts: ([.status.containerStatuses[]?.restartCount] | add // 0),
      revision: (.metadata.labels["controller-revision-hash"] // "")
    }]' <<<"${pods_json}")"
  current_revision="$(jq -r '.status.currentRevision // ""' <<<"${sts_json}")"
  update_revision="$(jq -r '.status.updateRevision // ""' <<<"${sts_json}")"
  partition="$(jq -r '.spec.updateStrategy.rollingUpdate.partition // -1' <<<"${sts_json}")"
  ready_replicas="$(jq -r '.status.readyReplicas // 0' <<<"${sts_json}")"

  if ((endpoint_count < minimum_endpoints)); then
    minimum_endpoints="${endpoint_count}"
  fi
  if ((unready_count > maximum_unready)); then
    maximum_unready="${unready_count}"
  fi

  if [[ "${triggered}" == true ]]; then
    if ((endpoint_count < desired_replicas - 1)); then
      fail "fewer than $((desired_replicas - 1)) routable Search Head endpoints"
    fi
    if ((unready_count > 1)); then
      fail "more than one Search Head Pod is unready"
    fi
    if ((restart_count != 0)); then
      fail "a Search Head container restarted inside a Pod"
    fi
    if [[ -n "${target_ordinal}" && "${operation_id}" != "${baseline_operation_id}" &&
      "${target_ordinal}" != "${last_seen_ordinal}" ]]; then
      if [[ -n "${seen_order}" ]]; then
        seen_order+=" "
      fi
      seen_order+="${target_ordinal}"
      last_seen_ordinal="${target_ordinal}"
    fi
    if [[ "${reason}" == "EndpointWithdrawalPropagationPending" ]]; then
      target_json="$(jq -c --arg target "${target_pod}" \
        '[.[] | select(.metadata.name == $target)][0] // {}' <<<"${pods_json}")"
      target_ready="$(pod_condition "${target_json}" Ready)"
      target_serving="$(pod_condition "${target_json}" "${serving_condition}")"
      if [[ "${target_ready}" != False || "${target_serving}" != False ]]; then
        fail "endpoint propagation is pending while the target Pod remains Ready or serving"
      fi
      if jq -e --arg target "${target_pod}" 'index($target) != null' \
        >/dev/null <<<"${endpoint_pods}"; then
        fail "endpoint propagation is pending while the target remains routable"
      fi
    fi
    if [[ -n "${detention_at}" && -n "${proof_deadline}" ]]; then
      detention_epoch="$(date -u -d "${detention_at}" +%s 2>/dev/null ||
        date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "${detention_at}" +%s)"
      proof_deadline_epoch="$(date -u -d "${proof_deadline}" +%s 2>/dev/null ||
        date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "${proof_deadline}" +%s)"
      if ((detention_epoch < proof_deadline_epoch)); then
        fail "detention was requested before the persisted withdrawal deadline"
      fi
    fi
  fi

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${timestamp}" "${phase}" "${stage}" "${reason}" "${operation_id}" \
    "${target_ordinal}" "${target_pod}" "${target_uid}" "${proof_uid}" \
    "${observed_at}" "${proof_deadline}" "${proof_sequence}" \
    "${invalidated_sequence}" "${detention_at}" "${endpoint_count}" \
    "${endpoint_pods}" "${unready_count}" "${current_revision}" \
    "${update_revision}" "${partition}" "${pod_detail}" >>"${samples_file}"
}

operator_deployment_json="$(operator_kube get deployment "${operator_deployment}" -o json)"
operator_replicas="$(jq -r '.spec.replicas // 1' <<<"${operator_deployment_json}")"
operator_image="$(jq -r '[.spec.template.spec.containers[] |
  select(.name == "manager") | .image][0] // ""' <<<"${operator_deployment_json}")"
if [[ "${operator_replicas}" -ne 1 ]]; then
  fail "qualification requires exactly one Operator replica"
fi
if [[ "${operator_image}" != "${expected_operator_image}" ]]; then
  fail "Operator image does not match the exact expected digest"
fi
workload_job_json="$(shc_kube get job "${workload_job}" -o json)"
if [[ "$(jq -r '.status.active // 0' <<<"${workload_job_json}")" -ne 1 ]]; then
  fail "API-independent HEC/search workload Job is not active"
fi
workload_ready_pods="$(shc_kube get pods -l "job-name=${workload_job}" -o json | jq \
  '[.items[] |
    select(.metadata.deletionTimestamp == null) |
    select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] |
    length')"
if [[ "${workload_ready_pods}" -ne 1 ]]; then
  fail "API-independent HEC/search workload Pod is not Ready"
fi

baseline_cr_json="$(shc_kube get searchheadcluster.enterprise.splunk.com "${cr_name}" -o json)"
desired_replicas="$(jq -r '.spec.replicas // 0' <<<"${baseline_cr_json}")"
if [[ "${desired_replicas}" -ne 3 ]]; then
  fail "qualification requires an established three-member Search Head Cluster"
fi
detention_seconds="$(jq -r '.spec.lifecyclePolicy.detentionTimeoutSeconds // 180' \
  <<<"${baseline_cr_json}")"
if ((withdrawal_seconds >= detention_seconds)); then
  fail "withdrawal interval must be shorter than the effective detention timeout"
fi
if [[ "$(jq -r '.spec.lifecyclePolicy.podUpdateStrategy // "OnDelete"' \
  <<<"${baseline_cr_json}")" != RollingUpdate ]]; then
  fail "qualification requires the opt-in RollingUpdate lifecycle strategy"
fi

initial_pods_json="$(search_head_pods)"
initial_endpoint_pods="$(routable_endpoint_pods)"
initial_sts_json="$(shc_kube get statefulset "${statefulset_name}" -o json)"
baseline_event_json="$(shc_kube get events --field-selector \
  involvedObject.kind=SearchHeadCluster,involvedObject.name="${cr_name}" -o json)"
baseline_observed_events="$(jq '[.items[] |
  select(.reason == "SearchHeadEndpointWithdrawalObserved")] |
  map(.count // 1) | add // 0' <<<"${baseline_event_json}")"
baseline_invalidated_events="$(jq '[.items[] |
  select(.reason == "SearchHeadEndpointWithdrawalInvalidated")] |
  map(.count // 1) | add // 0' <<<"${baseline_event_json}")"
baseline_operation_id="$(jq -r '.status.lifecycleOperation.operationID // ""' \
  <<<"${baseline_cr_json}")"
expected_order=""
for ((ordinal = desired_replicas - 1; ordinal >= 0; ordinal--)); do
  if [[ -n "${expected_order}" ]]; then
    expected_order+=" "
  fi
  expected_order+="${ordinal}"
done

initial_count="$(jq 'length' <<<"${initial_pods_json}")"
if [[ "${initial_count}" -ne "${desired_replicas}" ||
  "$(jq 'length' <<<"${initial_endpoint_pods}")" -ne "${desired_replicas}" ]]; then
  fail "baseline does not contain three Pods and three routable endpoints"
fi
if ! jq -e --arg condition "${serving_condition}" \
  'all(.[];
    .metadata.deletionTimestamp == null and
    any(.status.conditions[]?; .type == "Ready" and .status == "True") and
    any(.status.conditions[]?; .type == $condition and .status == "True") and
    ([.status.containerStatuses[]?.restartCount] | add // 0) == 0)' \
  >/dev/null <<<"${initial_pods_json}"; then
  fail "Search Head Pod baseline is not Ready, serving, and zero-restart"
fi
if [[ "$(jq -r '.status.phase // ""' <<<"${baseline_cr_json}")" != Ready ||
  "$(jq -r '.status.lifecycleOperation.stage // ""' <<<"${baseline_cr_json}")" != Completed ]]; then
  fail "Search Head Cluster baseline is not Ready with a completed lifecycle operation"
fi
initial_current_revision="$(jq -r '.status.currentRevision // ""' <<<"${initial_sts_json}")"
initial_update_revision="$(jq -r '.status.updateRevision // ""' <<<"${initial_sts_json}")"
if [[ "${initial_current_revision}" != "${initial_update_revision}" ]]; then
  fail "StatefulSet baseline revisions do not match"
fi
if [[ "$(jq -r '.spec.updateStrategy.rollingUpdate.partition // -1' \
  <<<"${initial_sts_json}")" -ne "${desired_replicas}" ]]; then
  fail "StatefulSet baseline partition is not closed"
fi
if ! jq -e --arg condition "${serving_condition}" \
  '.spec.template.spec.readinessGates | any(.conditionType == $condition)' \
  >/dev/null <<<"${initial_sts_json}"; then
  fail "Search Head serving readiness gate is not configured"
fi

printf '%s\n' \
  $'timestamp\tphase\tstage\treason\toperation_id\ttarget_ordinal\ttarget_pod\ttarget_uid\tproof_uid\tobserved_at\tproof_deadline\tproof_sequence\tinvalidated_sequence\tdetention_requested_at\tendpoint_count\tendpoint_pods\tunready_count\tcurrent_revision\tupdate_revision\tpartition\tpod_detail' \
  >"${samples_file}"
printf '%s\n' "${baseline_cr_json}" >"${evidence_dir}/baseline-searchheadcluster.json"
printf '%s\n' "${initial_sts_json}" >"${evidence_dir}/baseline-statefulset.json"
printf '%s\n' "${initial_pods_json}" >"${evidence_dir}/baseline-pods.json"
printf '%s\n' "${initial_endpoint_pods}" >"${evidence_dir}/baseline-endpoints.json"
printf '%s\n' "${baseline_event_json}" >"${evidence_dir}/baseline-events.json"
printf '%s\n' "${workload_job_json}" >"${evidence_dir}/baseline-workload-job.json"

if [[ "${preflight_only}" == true ]]; then
  collect_evidence
  cat >"${summary_file}" <<EOF
PREFLIGHT PASS
context=${context}
namespace=${namespace}
searchHeadCluster=${cr_name}
operatorImage=${expected_operator_image}
desiredReplicas=${desired_replicas}
routableEndpoints=$(jq 'length' <<<"${initial_endpoint_pods}")
EOF
  printf 'PREFLIGHT PASS: context=%s namespace=%s shc=%s evidence=%s\n' \
    "${context}" "${namespace}" "${cr_name}" "${evidence_dir}"
  exit 0
fi

patch_json="$(jq -cn --argjson delay "${withdrawal_seconds}" --arg trigger "${trigger_value}" \
  '{spec:{
    lifecyclePolicy:{endpointWithdrawalDelaySeconds:$delay},
    podAnnotations:{"qualification.splunk.com/shc118-endpoint-withdrawal":$trigger}
  }}')"
shc_kube patch searchheadcluster.enterprise.splunk.com "${cr_name}" \
  --type=merge -p "${patch_json}" >/dev/null
triggered=true
start_epoch="$(date -u +%s)"

proof_operation_id=""
while (($(date -u +%s) - start_epoch < timeout_seconds)); do
  record_sample
  if [[ "${reason}" == EndpointWithdrawalPropagationPending ]]; then
    if [[ -z "${operation_id}" || -z "${target_uid}" ||
      "${proof_uid}" != "${target_uid}" || -z "${observed_at}" ||
      -z "${proof_deadline}" || "${proof_sequence}" -le "${invalidated_sequence}" ]]; then
      fail "persisted endpoint-withdrawal proof is incomplete"
    fi
    proof_operation_id="${operation_id}"
    proof_target_uid="${target_uid}"
    proof_observed_at="${observed_at}"
    proof_deadline_value="${proof_deadline}"
    proof_sequence_value="${proof_sequence}"
    break
  fi
  sleep "${sample_interval}"
done
if [[ -z "${proof_operation_id}" ]]; then
  fail "endpoint-withdrawal propagation stage was not observed"
fi

if [[ "${restart_operator}" == true ]]; then
  old_operator_json="$(active_operator_pod)"
  old_operator_name="$(jq -r '.metadata.name // ""' <<<"${old_operator_json}")"
  old_operator_uid="$(jq -r '.metadata.uid // ""' <<<"${old_operator_json}")"
  if [[ -z "${old_operator_name}" || -z "${old_operator_uid}" ]]; then
    fail "no Ready Operator Pod was found before controller replacement"
  fi
  printf '%s\n' "${old_operator_json}" >"${evidence_dir}/operator-before-restart.json"
  printf '%s\n' "${op_json}" >"${evidence_dir}/withdrawal-proof-before-restart.json"
  operator_kube logs "${old_operator_name}" --all-containers=true --since=4h \
    >"${evidence_dir}/operator-before-restart.log" 2>&1
  proof_deadline_epoch="$(date -u -d "${proof_deadline_value}" +%s 2>/dev/null ||
    date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "${proof_deadline_value}" +%s)"
  if (($(date -u +%s) >= proof_deadline_epoch)); then
    fail "withdrawal deadline elapsed before controller replacement began"
  fi
  operator_kube delete pod "${old_operator_name}" --wait=false >/dev/null
  operator_kube rollout status deployment/"${operator_deployment}" \
    --timeout=300s >/dev/null

  replacement_deadline=$(($(date -u +%s) + 300))
  new_operator_uid=""
  while (($(date -u +%s) < replacement_deadline)); do
    new_operator_json="$(active_operator_pod)"
    new_operator_uid="$(jq -r '.metadata.uid // ""' <<<"${new_operator_json}")"
    if [[ -n "${new_operator_uid}" && "${new_operator_uid}" != "${old_operator_uid}" ]]; then
      break
    fi
    sleep 2
  done
  if [[ -z "${new_operator_uid}" || "${new_operator_uid}" == "${old_operator_uid}" ]]; then
    fail "Operator replacement did not produce a new Ready Pod UID"
  fi
  if (($(date -u +%s) >= proof_deadline_epoch)); then
    fail "replacement Operator did not become Ready before the persisted withdrawal deadline"
  fi
  operator_restarted=true
  record_sample
  printf '%s\n' "${new_operator_json}" >"${evidence_dir}/operator-after-restart.json"
  printf '%s\n' "${op_json}" >"${evidence_dir}/withdrawal-proof-after-restart.json"
  if [[ "${operation_id}" != "${proof_operation_id}" ||
    "${target_uid}" != "${proof_target_uid}" ||
    "${observed_at}" != "${proof_observed_at}" ||
    "${proof_deadline}" != "${proof_deadline_value}" ||
    "${proof_sequence}" != "${proof_sequence_value}" ]]; then
    fail "controller replacement changed durable endpoint-withdrawal ownership or deadline"
  fi
fi

completed=false
while (($(date -u +%s) - start_epoch < timeout_seconds)); do
  record_sample
  all_ready="$(jq --arg condition "${serving_condition}" \
    'all(.[];
      .metadata.deletionTimestamp == null and
      any(.status.conditions[]?; .type == "Ready" and .status == "True") and
      any(.status.conditions[]?; .type == $condition and .status == "True"))' \
    <<<"${pods_json}")"
  if [[ "${phase}" == Ready && "${stage}" == Completed &&
    "${current_revision}" == "${update_revision}" &&
    "${partition}" -eq "${desired_replicas}" &&
    "${ready_replicas}" -eq "${desired_replicas}" &&
    "${endpoint_count}" -eq "${desired_replicas}" &&
    "${all_ready}" == true && "${seen_order}" == "${expected_order}" ]]; then
    completed=true
    break
  fi
  sleep "${sample_interval}"
done
if [[ "${completed}" != true ]]; then
  fail "Search Head rollout did not complete within the qualification timeout"
fi

final_pods_json="${pods_json}"
unchanged_uids="$(jq -cn --argjson before "${initial_pods_json}" \
  --argjson after "${final_pods_json}" \
  '[
    $before[] as $old |
    $after[] |
    select(.metadata.name == $old.metadata.name and .metadata.uid == $old.metadata.uid) |
    .metadata.name
  ]')"
if [[ "$(jq 'length' <<<"${unchanged_uids}")" -ne 0 ]]; then
  fail "one or more Search Head Pods were not replaced by the full roll"
fi

stable_uids="$(jq -c '[.[] | {name:.metadata.name,uid:.metadata.uid}] | sort_by(.name)' \
  <<<"${final_pods_json}")"
for ((stable_sample = 1; stable_sample <= stable_samples_required; stable_sample++)); do
  sleep "${stable_interval}"
  record_sample
  current_uids="$(jq -c '[.[] | {name:.metadata.name,uid:.metadata.uid}] | sort_by(.name)' \
    <<<"${pods_json}")"
  if [[ "${phase}" != Ready || "${stage}" != Completed ||
    "${endpoint_count}" -ne "${desired_replicas}" ||
    "${current_uids}" != "${stable_uids}" ]]; then
    fail "post-roll stability sample regressed"
  fi
done

event_json="$(shc_kube get events --field-selector \
  involvedObject.kind=SearchHeadCluster,involvedObject.name="${cr_name}" -o json)"
observed_events="$(jq '[.items[] | select(.reason == "SearchHeadEndpointWithdrawalObserved")] |
  map(.count // 1) | add // 0' <<<"${event_json}")"
invalidated_events="$(jq '[.items[] | select(.reason == "SearchHeadEndpointWithdrawalInvalidated")] |
  map(.count // 1) | add // 0' <<<"${event_json}")"
observed_event_delta=$((observed_events - baseline_observed_events))
invalidated_event_delta=$((invalidated_events - baseline_invalidated_events))
if ((observed_event_delta < desired_replicas)); then
  fail "fewer endpoint-withdrawal observation Events than replaced Search Heads"
fi
if ((invalidated_event_delta != 0)); then
  fail "endpoint-withdrawal proof was invalidated during the healthy-path roll"
fi
workload_log="$(shc_kube logs job/"${workload_job}")"
workload_sample_count="$(awk '/^[0-9][0-9][0-9][0-9]-/{count++} END{print count+0}' \
  <<<"${workload_log}")"
workload_hec_failures="$(awk '/hec=fail/{count++} END{print count+0}' \
  <<<"${workload_log}")"
workload_search_failures="$(awk '/search=fail/{count++} END{print count+0}' \
  <<<"${workload_log}")"
if ((workload_sample_count == 0 || workload_hec_failures != 0 ||
  workload_search_failures != 0)); then
  fail "API-independent workload recorded no samples or a request failure"
fi

collect_evidence
cat >"${summary_file}" <<EOF
PASS
context=${context}
namespace=${namespace}
searchHeadCluster=${cr_name}
operatorImage=${expected_operator_image}
withdrawalSeconds=${withdrawal_seconds}
operatorRestarted=${operator_restarted}
ordinalOrder=${seen_order}
minimumRoutableEndpoints=${minimum_endpoints}
maximumUnreadyPods=${maximum_unready}
observedEventDelta=${observed_event_delta}
invalidatedEventDelta=${invalidated_event_delta}
workloadSamplesAtRollCompletion=${workload_sample_count}
workloadHecFailures=${workload_hec_failures}
workloadSearchFailures=${workload_search_failures}
stableSamples=${stable_samples_required}
EOF
printf 'PASS: order=%s minEndpoints=%s maxUnready=%s operatorRestarted=%s evidence=%s\n' \
  "${seen_order}" "${minimum_endpoints}" "${maximum_unready}" \
  "${operator_restarted}" "${evidence_dir}"
