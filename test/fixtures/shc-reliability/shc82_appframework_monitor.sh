#!/usr/bin/env bash
set -euo pipefail

namespace="${SHC82_NAMESPACE:-shc82-afw-baseline}"
samples="${SHC82_SAMPLES:-180}"
interval_seconds="${SHC82_INTERVAL_SECONDS:-5}"
settle_attempts="${SHC82_SETTLE_ATTEMPTS:-24}"
min_free_headroom_mb="${SHC82_MIN_FREE_HEADROOM_MB:-1024}"
run_id="${SHC82_RUN_ID:-shc82-afw-$(date -u +%Y%m%dT%H%M%SZ)}"
evidence_file="${SHC82_EVIDENCE_FILE:-build/_test/shc82/${run_id}.log}"
stack_name="${SHC82_STACK_NAME:-shc82}"
indexercluster_name="${SHC82_IDXC_NAME:-${stack_name}-idxc}"
searchheadcluster_name="${SHC82_SHC_NAME:-${stack_name}-shc}"

probe_pod="${SHC82_PROBE_POD:-splunk-${stack_name}-license-manager-0}"
probe_container="${SHC82_PROBE_CONTAINER:-splunk}"
secret_name="splunk-${namespace}-secret"
hec_service="splunk-${indexercluster_name}-indexer-service"
shc_service="splunk-${searchheadcluster_name}-search-head-service"
shc_instance="splunk-${searchheadcluster_name}-search-head"
deployer_instance="splunk-${searchheadcluster_name}-deployer"
idxc_instance="splunk-${indexercluster_name}-indexer"

for value in "${samples}" "${interval_seconds}" "${settle_attempts}"; do
  if ! printf '%s\n' "${value}" | grep -Eq '^[1-9][0-9]*$'; then
    printf 'sample, interval, and settle values must be positive integers\n' >&2
    exit 2
  fi
done
if ! printf '%s\n' "${min_free_headroom_mb}" | grep -Eq '^[0-9]+$'; then
  printf 'minimum free-space headroom must be a non-negative integer in MB\n' >&2
  exit 2
fi

mkdir -p "$(dirname "${evidence_file}")"
: >"${evidence_file}"

hec_token="$(
  kubectl -n "${namespace}" get secret "${secret_name}" \
    -o jsonpath='{.data.hec_token}' | base64 --decode
)"
admin_password="$(
  kubectl -n "${namespace}" get secret "${secret_name}" \
    -o jsonpath='{.data.password}' | base64 --decode
)"

log_line() {
  printf '%s\n' "$*" | tee -a "${evidence_file}"
}

preflight_indexer_storage() {
  local pod storage_state available_kb min_free_mb required_kb
  local failed=0
  local indexer_pods

  indexer_pods="$(
    kubectl -n "${namespace}" get pods \
      -l app.kubernetes.io/component=indexer,app.kubernetes.io/instance="${idxc_instance}" \
      -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true
  )"
  if [ -z "${indexer_pods}" ]; then
    log_line "preflight=indexer-storage status=fail reason=no-indexer-pods"
    return 1
  fi

  while IFS= read -r pod; do
    # Variables in this command are intentionally evaluated inside the Pod.
    # shellcheck disable=SC2016
    storage_state="$(
      kubectl -n "${namespace}" exec "${pod}" -c splunk -- sh -c '
        available_kb="$(df -Pk /opt/splunk/var | awk "NR == 2 {print \$4}")"
        min_free_mb="$(/opt/splunk/bin/splunk btool server list diskUsage | awk "\$1 == \"minFreeSpace\" {print \$3; exit}")"
        printf "%s %s\n" "${available_kb}" "${min_free_mb}"
      ' 2>/dev/null || true
    )"
    available_kb="${storage_state%% *}"
    min_free_mb="${storage_state#* }"
    if ! printf '%s\n' "${available_kb}" | grep -Eq '^[0-9]+$' ||
      ! printf '%s\n' "${min_free_mb}" | grep -Eq '^[0-9]+$'; then
      log_line "preflight=indexer-storage pod=${pod} status=fail reason=unreadable-state"
      failed=1
      continue
    fi

    required_kb=$(((min_free_mb + min_free_headroom_mb) * 1024))
    if [ "${available_kb}" -le "${required_kb}" ]; then
      log_line "preflight=indexer-storage pod=${pod} status=fail availableKB=${available_kb} minFreeMB=${min_free_mb} headroomMB=${min_free_headroom_mb} requiredKB=${required_kb}"
      failed=1
    else
      log_line "preflight=indexer-storage pod=${pod} status=ok availableKB=${available_kb} minFreeMB=${min_free_mb} headroomMB=${min_free_headroom_mb} requiredKB=${required_kb}"
    fi
  done <<EOF
${indexer_pods}
EOF

  [ "${failed}" -eq 0 ]
}

submit_event() {
  local sequence="$1"
  local payload response
  payload="$(
    printf '{"event":{"shc82_run":"%s","seq":%d},"sourcetype":"_json","index":"main"}' \
      "${run_id}" "${sequence}"
  )"
  # Variables in this command are intentionally expanded inside the Pod.
  # shellcheck disable=SC2016
  response="$(
    printf '%s\n%s\n%s\n' "${hec_token}" "${payload}" "${hec_service}" |
      kubectl -n "${namespace}" exec -i "${probe_pod}" -c "${probe_container}" -- sh -c '
        IFS= read -r token
        IFS= read -r payload
        IFS= read -r service
        curl -sk --connect-timeout 3 --max-time 15 \
          -H "Authorization: Splunk ${token}" \
          -H "Content-Type: application/json" \
          --data-binary "${payload}" \
          "https://${service}:8088/services/collector/event"
      ' 2>/dev/null || true
  )"
  printf '%s' "${response}" | grep -q '"code":0'
}

search_sequences() {
  # Variables in this command are intentionally expanded inside the Pod.
  # shellcheck disable=SC2016
  printf '%s\n%s\n%s\n' "${admin_password}" "${run_id}" "${shc_service}" |
    kubectl -n "${namespace}" exec -i "${probe_pod}" -c "${probe_container}" -- sh -c '
      IFS= read -r password
      IFS= read -r run_id
      IFS= read -r service
      curl -skS --connect-timeout 3 --max-time 20 \
        -u "admin:${password}" \
        -X POST "https://${service}:8089/services/search/jobs/export" \
        --data-urlencode "search=search index=main earliest=-24h shc82_run=\"${run_id}\" | stats count min(seq) as min max(seq) as max dc(seq) as distinct" \
        --data "output_mode=json" 2>&1
    ' 2>/dev/null || true
}

extract_field() {
  local field="$1"
  local response="$2"
  printf '%s' "${response}" |
    sed -n "s/.*\"${field}\":\"\\([0-9][0-9]*\\)\".*/\\1/p" |
    tail -1
}

resource_state() {
  local resource="$1"
  local name="$2"
  local template="$3"
  kubectl -n "${namespace}" get "${resource}" "${name}" \
    -o "jsonpath=${template}" 2>/dev/null || printf 'Unavailable'
}

baseline_uids="$(
  kubectl -n "${namespace}" get pods \
    -o jsonpath='{range .items[*]}{.metadata.name}={.metadata.uid}{";"}{end}'
)"
log_line "run=${run_id} start=$(date -u +%Y-%m-%dT%H:%M:%SZ) namespace=${namespace}"
log_line "baselinePodUIDs=${baseline_uids}"
if ! preflight_indexer_storage; then
  log_line "run=${run_id} end=$(date -u +%Y-%m-%dT%H:%M:%SZ) complete=false reason=indexer-storage-preflight"
  exit 2
fi

hec_failures=0
search_failures=0
last_count=0
last_distinct=0
last_max=0

for sequence in $(seq 1 "${samples}"); do
  timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  if submit_event "${sequence}"; then
    hec_state="ok"
  else
    hec_state="fail"
    hec_failures=$((hec_failures + 1))
  fi

  search_response="$(search_sequences)"
  count="$(extract_field count "${search_response}")"
  minimum="$(extract_field min "${search_response}")"
  maximum="$(extract_field max "${search_response}")"
  distinct="$(extract_field distinct "${search_response}")"
  if [ -n "${count}" ] && [ -n "${distinct}" ] &&
    { [ "${count}" -eq 0 ] || [ -n "${maximum}" ]; }; then
    search_state="ok"
    search_detail="result"
    minimum="${minimum:-0}"
    maximum="${maximum:-0}"
    last_count="${count}"
    last_distinct="${distinct}"
    last_max="${maximum}"
  else
    search_state="fail"
    search_detail="$(
      printf '%s' "${search_response}" |
        tr '\r\n\t ' '_' |
        sed 's/__*/_/g; s/^$/empty/; s/^\(.\{160\}\).*/\1/'
    )"
    search_failures=$((search_failures + 1))
    count="${last_count}"
    distinct="${last_distinct}"
    maximum="${last_max}"
    minimum="unknown"
  fi

  sh_containers_ready="$(
    kubectl -n "${namespace}" get pods \
      -l app.kubernetes.io/component=search-head,app.kubernetes.io/instance="${shc_instance}" \
      -o jsonpath='{range .items[*]}{.status.containerStatuses[0].ready}{"\n"}{end}' \
      2>/dev/null | grep -c '^true$' || true
  )"
  sh_pods_ready="$(
    kubectl -n "${namespace}" get pods \
      -l app.kubernetes.io/component=search-head,app.kubernetes.io/instance="${shc_instance}" \
      -o jsonpath='{range .items[*]}{range .status.conditions[?(@.type=="Ready")]}{.status}{"\n"}{end}{end}' \
      2>/dev/null | grep -c '^True$' || true
  )"
  sh_serving_ready="$(
    kubectl -n "${namespace}" get pods \
      -l app.kubernetes.io/component=search-head,app.kubernetes.io/instance="${shc_instance}" \
      -o jsonpath='{range .items[*]}{range .status.conditions[?(@.type=="enterprise.splunk.com/shc-serving")]}{.status}{"\n"}{end}{end}' \
      2>/dev/null | grep -c '^True$' || true
  )"
  sh_endpoints="$(
    kubectl -n "${namespace}" get endpointslice \
      -l kubernetes.io/service-name="${shc_service}" \
      -o jsonpath='{range .items[*].endpoints[*]}{.conditions.ready}{"\n"}{end}' \
      2>/dev/null | grep -c '^true$' || true
  )"
  sh_endpoint_pods="$(
    kubectl -n "${namespace}" get endpointslice \
      -l kubernetes.io/service-name="${shc_service}" \
      -o jsonpath='{range .items[*].endpoints[*]}{.targetRef.name}={.conditions.ready}{";"}{end}' \
      2>/dev/null || printf 'Unavailable'
  )"
  sh_serving_conditions="$(
    kubectl -n "${namespace}" get pods \
      -l app.kubernetes.io/component=search-head,app.kubernetes.io/instance="${shc_instance}" \
      -o jsonpath='{range .items[*]}{.metadata.name}={range .status.conditions[?(@.type=="enterprise.splunk.com/shc-serving")]}{.status}/{.reason}{end}{";"}{end}' \
      2>/dev/null || printf 'Unavailable'
  )"
  sh_member_states="$(
    resource_state searchheadcluster.enterprise.splunk.com "${searchheadcluster_name}" \
      '{range .status.members[*]}{.name}={.status}/{.restart_state}/{.captain_status}{";"}{end}'
  )"
  idx_endpoints="$(
    kubectl -n "${namespace}" get endpointslice \
      -l kubernetes.io/service-name="${hec_service}" \
      -o jsonpath='{range .items[*].endpoints[*]}{.conditions.ready}{"\n"}{end}' \
      2>/dev/null | grep -c '^true$' || true
  )"
  restarts="$(
    kubectl -n "${namespace}" get pods \
      -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.restartCount}{"\n"}{end}{end}' \
      2>/dev/null | awk '{sum += $1} END {print sum + 0}'
  )"
  shc="$(
    resource_state searchheadcluster.enterprise.splunk.com "${searchheadcluster_name}" \
      '{.status.phase}/{.status.readyReplicas}/{.status.replicas}/{.status.captain}'
  )"
  idxc="$(
    resource_state indexercluster.enterprise.splunk.com "${indexercluster_name}" \
      '{.status.phase}/{.status.readyReplicas}/{.status.replicas}'
  )"
  shc_app="$(
    resource_state searchheadcluster.enterprise.splunk.com "${searchheadcluster_name}" \
      '{.status.appContext.isDeploymentInProgress}/{.status.appContext.bundlePushStatus.bundlePushStage}'
  )"
  shc_app_packages="$(
    resource_state searchheadcluster.enterprise.splunk.com "${searchheadcluster_name}" \
      '{range .status.appContext.appSrcDeployStatus.*.appDeploymentInfo[*]}{.appName}={.deployStatus}/{.phaseInfo.phase}/{.phaseInfo.status}/{.repoState}/{.objectHash}{";"}{end}'
  )"
  shc_app_revisions="$(
    resource_state searchheadcluster.enterprise.splunk.com "${searchheadcluster_name}" \
      '{.status.appFrameworkBundleRevision}/{.status.appFrameworkRestartObservedRevision}/{.status.appFrameworkRestartRevision}'
  )"
  sh_revision="$(
    resource_state statefulset.apps "${shc_instance}" \
      '{.spec.updateStrategy.rollingUpdate.partition}/{.status.currentRevision}/{.status.updateRevision}/{.status.updatedReplicas}'
  )"
  deployer_pod="$(
    resource_state pod "${deployer_instance}-0" \
      '{.metadata.uid}/{.metadata.labels.controller-revision-hash}/{.status.containerStatuses[0].ready}/{.status.containerStatuses[0].restartCount}'
  )"
  deployer_revision="$(
    resource_state statefulset.apps "${deployer_instance}" \
      '{.spec.updateStrategy.type}/{.status.currentRevision}/{.status.updateRevision}/{.status.readyReplicas}/{.status.updatedReplicas}'
  )"
  cm_app="$(
    resource_state clustermanager.enterprise.splunk.com "${stack_name}" \
      '{.status.appContext.isDeploymentInProgress}/{.status.appContext.bundlePushStatus.bundlePushStage}'
  )"

  log_line "${timestamp} seq=${sequence} hec=${hec_state} search=${search_state}/${search_detail} count=${count} min=${minimum:-unknown} max=${maximum} distinct=${distinct} shContainersReady=${sh_containers_ready} shPodsReady=${sh_pods_ready} shServingReady=${sh_serving_ready} shEndpoints=${sh_endpoints} shEndpointPods=${sh_endpoint_pods} shServingConditions=${sh_serving_conditions} shMembers=${sh_member_states} idxEndpoints=${idx_endpoints} restarts=${restarts} shc=${shc} shRevision=${sh_revision} deployerPod=${deployer_pod} deployerRevision=${deployer_revision} idxc=${idxc} shcApp=${shc_app} shcAppRevisions=${shc_app_revisions} shcAppPackages=${shc_app_packages} cmApp=${cm_app}"
  sleep "${interval_seconds}"
done

final_complete=false
for _ in $(seq 1 "${settle_attempts}"); do
  search_response="$(search_sequences)"
  count="$(extract_field count "${search_response}")"
  minimum="$(extract_field min "${search_response}")"
  maximum="$(extract_field max "${search_response}")"
  distinct="$(extract_field distinct "${search_response}")"
  if [ "${count:-0}" -eq "${samples}" ] &&
    [ "${minimum:-0}" -eq 1 ] &&
    [ "${maximum:-0}" -eq "${samples}" ] &&
    [ "${distinct:-0}" -eq "${samples}" ]; then
    final_complete=true
    break
  fi
  sleep 5
done

final_uids="$(
  kubectl -n "${namespace}" get pods \
    -o jsonpath='{range .items[*]}{.metadata.name}={.metadata.uid}{";"}{end}'
)"
log_line "finalPodUIDs=${final_uids}"
log_line "run=${run_id} end=$(date -u +%Y-%m-%dT%H:%M:%SZ) submitted=${samples} hecFailures=${hec_failures} searchFailures=${search_failures} finalCount=${count:-0} finalMin=${minimum:-0} finalMax=${maximum:-0} finalDistinct=${distinct:-0} complete=${final_complete}"

if [ "${hec_failures}" -ne 0 ] ||
  [ "${search_failures}" -ne 0 ] ||
  [ "${final_complete}" != true ]; then
  exit 1
fi
