#!/usr/bin/env bash

set -euo pipefail

context="${SHC98_KUBE_CONTEXT:-}"
namespace="${SHC98_NAMESPACE:-shc-final-qualification}"
indexercluster_name="${SHC98_IDXC_NAME:-shcfinal-idxc}"
searchheadcluster_name="${SHC98_SHC_NAME:-shcfinal-shc}"
clustermanager_name="${SHC98_CM_NAME:-shcfinal}"
sample_seconds="${SHC98_SAMPLE_SECONDS:-5}"
roll_timeout_seconds="${SHC98_ROLL_TIMEOUT_SECONDS:-7200}"
stable_samples_required="${SHC98_STABLE_SAMPLES:-60}"
snapshot_only="${SHC98_SNAPSHOT_ONLY:-false}"
expected_address_mode="${SHC98_EXPECTED_ADDRESS_MODE:-fqdn}"
run_id="${SHC98_RUN_ID:-shc98-$(date -u +%Y%m%dT%H%M%SZ)}"
evidence_file="${SHC98_EVIDENCE_FILE:-build/_test/shc98/${run_id}.tsv}"
events_file="${SHC98_EVENTS_FILE:-${evidence_file%.tsv}.events.json}"
config_file="${SHC98_CONFIG_FILE:-${evidence_file%.tsv}.config.json}"
secret_name="${SHC98_SECRET_NAME:-splunk-${namespace}-secret}"

indexer_statefulset="splunk-${indexercluster_name}-indexer"
indexer_headless_service="${indexer_statefulset}-headless"
indexer_service="${indexer_statefulset}-service"
indexer_pod_prefix="${indexer_statefulset}-"
search_head_pod_prefix="splunk-${searchheadcluster_name}-search-head-"
cluster_manager_pod="splunk-${clustermanager_name}-cluster-manager-0"

for command in kubectl jq base64; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    printf 'required command is unavailable: %s\n' "${command}" >&2
    exit 2
  fi
done

for value in "${sample_seconds}" "${roll_timeout_seconds}" \
  "${stable_samples_required}"; do
  if ! [[ "${value}" =~ ^[1-9][0-9]*$ ]]; then
    printf 'SHC-98 timing values must be positive integers\n' >&2
    exit 2
  fi
done

case "${snapshot_only}" in
true | false) ;;
*)
  printf 'SHC98_SNAPSHOT_ONLY must be true or false\n' >&2
  exit 2
  ;;
esac

case "${expected_address_mode}" in
fqdn | pod-ip) ;;
*)
  printf 'SHC98_EXPECTED_ADDRESS_MODE must be fqdn or pod-ip\n' >&2
  exit 2
  ;;
esac

kube=(kubectl)
if [[ -n "${context}" ]]; then
  kube+=(--context "${context}")
fi

k() {
  "${kube[@]}" "$@"
}

mkdir -p "$(dirname "${evidence_file}")"
printf '%s\n' \
  $'timestamp\tphase\tindexercluster\tstatefulset\tindexer_pods\tindexer_endpoints\tcluster_health\tcluster_peers\tsearch_head_peers\tevents' \
  >"${evidence_file}"

admin_password="$({
  k -n "${namespace}" get secret "${secret_name}" \
    -o jsonpath='{.data.password}' | base64 --decode
})"
if [[ -z "${admin_password}" ]]; then
  printf 'namespace admin credential is empty\n' >&2
  exit 2
fi
trap 'unset admin_password' EXIT

indexer_replicas="$({
  k -n "${namespace}" get indexercluster.enterprise.splunk.com \
    "${indexercluster_name}" -o json | jq -r '.spec.replicas'
})"
search_head_replicas="$({
  k -n "${namespace}" get searchheadcluster.enterprise.splunk.com \
    "${searchheadcluster_name}" -o json | jq -r '.spec.replicas'
})"
for value in "${indexer_replicas}" "${search_head_replicas}"; do
  if ! [[ "${value}" =~ ^[1-9][0-9]*$ ]]; then
    printf 'cluster replica counts must be positive integers\n' >&2
    exit 2
  fi
done

expected_peer_addresses='[]'

indexercluster_json() {
  k -n "${namespace}" get indexercluster.enterprise.splunk.com \
    "${indexercluster_name}" -o json | jq -c '{
      generation: .metadata.generation,
      observedGeneration: (.status.observedGeneration // 0),
      phase: (.status.phase // ""),
      replicas: (.status.replicas // 0),
      readyReplicas: (.status.readyReplicas // 0),
      podUpdate: (.status.podUpdate // {})
    }'
}

statefulset_json() {
  k -n "${namespace}" get statefulset "${indexer_statefulset}" -o json |
    jq -c '{
      generation: .metadata.generation,
      observedGeneration: (.status.observedGeneration // 0),
      replicas: (.status.replicas // 0),
      readyReplicas: (.status.readyReplicas // 0),
      currentReplicas: (.status.currentReplicas // 0),
      updatedReplicas: (.status.updatedReplicas // 0),
      currentRevision: (.status.currentRevision // ""),
      updateRevision: (.status.updateRevision // ""),
      strategy: .spec.updateStrategy.type,
      partition: (.spec.updateStrategy.rollingUpdate.partition // null)
    }'
}

indexer_pods_json() {
  k -n "${namespace}" get pods -o json | jq -c \
    --arg prefix "${indexer_pod_prefix}" \
    --arg headless "${indexer_headless_service}" \
    --arg namespace "${namespace}" '
      [.items[] |
        select(.metadata.name | startswith($prefix)) | {
          name: .metadata.name,
          uid: .metadata.uid,
          podIP: (.status.podIP // ""),
          node: (.spec.nodeName // ""),
          ready: any(.status.conditions[]?;
            .type == "Ready" and .status == "True"),
          restartCount:
            ([.status.containerStatuses[]?.restartCount] | add // 0),
          revision: (.metadata.labels["controller-revision-hash"] // ""),
          image: (.status.containerStatuses[0].image // ""),
          imageID: (.status.containerStatuses[0].imageID // ""),
          deletionTimestamp: (.metadata.deletionTimestamp // ""),
          expectedFQDN:
            (.metadata.name + "." + $headless + "." + $namespace +
              ".svc.cluster.local"),
          pvcClaims: ([.spec.volumes[]? |
            select(.persistentVolumeClaim != null) |
            .persistentVolumeClaim.claimName] | sort)
        }] | sort_by(.name)'
}

refresh_expected_peer_addresses() {
  case "${expected_address_mode}" in
  fqdn)
    expected_peer_addresses="$(jq -c '
      [.[] | "\(.expectedFQDN):8089"] | sort' \
      <<<"${current_indexer_pods}")"
    ;;
  pod-ip)
    expected_peer_addresses="$(jq -c '
      [.[] | select(.podIP != "") | "\(.podIP):8089"] | sort' \
      <<<"${current_indexer_pods}")"
    ;;
  esac
}

indexer_endpoints_json() {
  k -n "${namespace}" get endpointslices.discovery.k8s.io -o json |
    jq -c --arg service "${indexer_service}" '
      [.items[] |
        select(.metadata.labels["kubernetes.io/service-name"] == $service) |
        .endpoints[]? | {
          pod: (.targetRef.name // ""),
          addresses: (.addresses // []),
          ready: (.conditions.ready // false),
          serving: (.conditions.serving // null),
          terminating: (.conditions.terminating // false)
        }] | sort_by(.pod)'
}

splunk_rest_json() {
  local pod="$1"
  local path="$2"
  # Variables in this command are intentionally expanded inside the Pod.
  # shellcheck disable=SC2016
  printf '%s\n' "${admin_password}" |
    k -n "${namespace}" exec -i "${pod}" -c splunk -- sh -c '
      IFS= read -r password
      curl -skS --connect-timeout 3 --max-time 20 \
        -u "admin:${password}" "https://127.0.0.1:8089${1}"
    ' sh "${path}" 2>/dev/null
}

cluster_health_json() {
  local response
  if response="$(splunk_rest_json "${cluster_manager_pod}" \
    '/services/cluster/manager/health?output_mode=json')" &&
    jq -e '.entry | type == "array"' <<<"${response}" >/dev/null 2>&1; then
    jq -c '{
      available: true,
      replicationFactorMet:
        (.entry[0].content.replication_factor_met // ""),
      searchFactorMet: (.entry[0].content.search_factor_met // ""),
      siteReplicationFactorMet:
        (.entry[0].content.site_replication_factor_met // ""),
      siteSearchFactorMet:
        (.entry[0].content.site_search_factor_met // "")
    }' <<<"${response}"
  else
    printf '%s\n' '{"available":false}'
  fi
}

cluster_peers_json() {
  local response
  if response="$(splunk_rest_json "${cluster_manager_pod}" \
    '/services/cluster/manager/peers?output_mode=json&count=0')" &&
    jq -e '.entry | type == "array"' <<<"${response}" >/dev/null 2>&1; then
    jq -c '{
      available: true,
      peers: [.entry[] | {
        guid: .name,
        label: (.content.label // ""),
        status: (.content.status // ""),
        isSearchable: (.content.is_searchable // false),
        hostPortPair: (.content.host_port_pair // ""),
        registerSearchAddress:
          (.content.register_search_address // "")
      }] | sort_by(.label)
    }' <<<"${response}"
  else
    printf '%s\n' '{"available":false,"peers":[]}'
  fi
}

search_head_peers_json() {
  local pods pod response peers entry inventory='[]'
  pods="$({
    k -n "${namespace}" get pods -o json | jq -r \
      --arg prefix "${search_head_pod_prefix}" '
        [.items[] | select(.metadata.name | startswith($prefix)) |
          .metadata.name] | sort[]'
  })"
  for pod in ${pods}; do
    if response="$(splunk_rest_json "${pod}" \
      '/services/search/distributed/peers?output_mode=json&count=0')" &&
      jq -e '.entry | type == "array"' <<<"${response}" >/dev/null 2>&1; then
      peers="$({
        jq -c '[.entry[] | {
          address: .name,
          guid: (.content.guid // ""),
          status: (.content.status // ""),
          statusDetails: (.content.status_details // []),
          host: (.content.host // ""),
          hostFQDN: (.content.host_fqdn // ""),
          peerName: (.content.peerName // ""),
          disabled: (.content.disabled // false)
        }] | sort_by(.address)' <<<"${response}"
      })"
      entry="$(jq -cn --arg pod "${pod}" --argjson peers "${peers}" \
        '{pod:$pod,available:true,peers:$peers}')"
    else
      entry="$(jq -cn --arg pod "${pod}" \
        '{pod:$pod,available:false,peers:[]}')"
    fi
    inventory="$(jq -cn --argjson inventory "${inventory}" \
      --argjson entry "${entry}" '$inventory + [$entry]')"
  done
  jq -c 'sort_by(.pod)' <<<"${inventory}"
}

event_summary_json() {
  k -n "${namespace}" get events -o json | jq -c \
    --arg indexer "${indexer_pod_prefix}" \
    --arg sh "${search_head_pod_prefix}" '
      [.items[] |
        select(
          (.involvedObject.name | startswith($indexer)) or
          (.involvedObject.name | startswith($sh)) or
          .involvedObject.kind == "IndexerCluster" or
          .involvedObject.kind == "SearchHeadCluster") | {
            type,
            reason,
            objectKind: .involvedObject.kind,
            objectName: .involvedObject.name,
            count: (.series.count // .count // 1),
            lastObserved:
              (.series.lastObservedTime // .lastTimestamp // .eventTime // "")
          }] | sort_by(.lastObserved, .objectName, .reason)'
}

collect_sample() {
  current_indexercluster="$(indexercluster_json)"
  current_statefulset="$(statefulset_json)"
  current_indexer_pods="$(indexer_pods_json)"
  refresh_expected_peer_addresses
  current_indexer_endpoints="$(indexer_endpoints_json)"
  current_cluster_health="$(cluster_health_json)"
  current_cluster_peers="$(cluster_peers_json)"
  current_search_head_peers="$(search_head_peers_json)"
  current_events="$(event_summary_json)"
}

record_sample() {
  local phase="$1"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${phase}" \
    "${current_indexercluster}" "${current_statefulset}" \
    "${current_indexer_pods}" "${current_indexer_endpoints}" \
    "${current_cluster_health}" "${current_cluster_peers}" \
    "${current_search_head_peers}" "${current_events}" \
    >>"${evidence_file}"
}

peers_match_expected() {
  jq -e --argjson expected "${expected_peer_addresses}" \
    --argjson replicas "${search_head_replicas}" '
      length == $replicas and
      all(.[];
        .available == true and
        ([.peers[].address] | sort) == $expected and
        all(.peers[]; .status == "Up" and .disabled == false))' \
    <<<"${current_search_head_peers}" >/dev/null
}

rolled_peer_converged_on_all_search_heads() {
  local ordinal="$1" label expected guid
  label="${indexer_pod_prefix}${ordinal}"
  case "${expected_address_mode}" in
  fqdn)
    expected="${label}.${indexer_headless_service}.${namespace}.svc.cluster.local:8089"
    ;;
  pod-ip)
    expected="$(jq -r --arg label "${label}" '
      [.[] | select(.name == $label) | "\(.podIP):8089"] |
      if length == 1 then .[0] else "" end' <<<"${current_indexer_pods}")"
    ;;
  esac
  if [[ -z "${expected}" ]]; then
    return 1
  fi
  guid="$(jq -r --arg label "${label}" '
    [.peers[] | select(.label == $label) | .guid] | unique |
    if length == 1 then .[0] else "" end' <<<"${current_cluster_peers}")"
  if [[ -z "${guid}" ]]; then
    return 1
  fi

  jq -e --arg guid "${guid}" --arg expected "${expected}" \
    --argjson replicas "${search_head_replicas}" '
      length == $replicas and
      all(.[];
        .available == true and
        ([.peers[] | select(.guid == $guid)] as $identity |
          ($identity | length) == 1 and
          $identity[0].address == $expected and
          $identity[0].status == "Up" and
          $identity[0].disabled == false))' \
    <<<"${current_search_head_peers}" >/dev/null
}

cluster_matches_expected() {
  jq -e --argjson expected "${expected_peer_addresses}" \
    --argjson replicas "${indexer_replicas}" '
      .available == true and
      (.peers | length) == $replicas and
      ([.peers[].registerSearchAddress] | sort) == $expected and
      all(.peers[]; .status == "Up" and .isSearchable == true)' \
    <<<"${current_cluster_peers}" >/dev/null &&
    jq -e '
      .available == true and
      .replicationFactorMet == "1" and
      .searchFactorMet == "1" and
      .siteReplicationFactorMet == "1" and
      .siteSearchFactorMet == "1"' \
      <<<"${current_cluster_health}" >/dev/null
}

statefulset_revision_converged() {
  local strategy current_revision update_revision
  strategy="$(jq -r '.strategy' <<<"${current_statefulset}")"
  current_revision="$(jq -r '.currentRevision' <<<"${current_statefulset}")"
  update_revision="$(jq -r '.updateRevision' <<<"${current_statefulset}")"

  if ! jq -e --arg revision "${update_revision}" \
    --argjson replicas "${indexer_replicas}" '
      length == $replicas and
      all(.[]; .revision == $revision)' \
    <<<"${current_indexer_pods}" >/dev/null; then
    return 1
  fi

  case "${strategy}" in
  OnDelete)
    # Kubernetes can retain the older status.currentRevision even after every
    # manually replaced Pod is on status.updateRevision.
    return 0
    ;;
  RollingUpdate)
    [[ "${current_revision}" == "${update_revision}" ]]
    ;;
  *)
    return 1
    ;;
  esac
}

indexer_config_json() {
  local pod expected output register_address system_fqdn resolved_ip
  local entry inventory='[]'
  while IFS=$'\t' read -r pod expected; do
    # Variables in this command are intentionally expanded inside the Pod.
    # shellcheck disable=SC2016
    output="$({
      k -n "${namespace}" exec "${pod}" -c splunk -- sh -c '
        register_address="$($SPLUNK_HOME/bin/splunk btool server list clustering 2>/dev/null |
          awk -F " = " '\''$1 == "register_search_address" {print $2}'\'' |
          tail -1)"
        $SPLUNK_HOME/bin/python3 -c '\''
import socket
fqdn = socket.getfqdn()
print(fqdn)
print(socket.gethostbyname(fqdn))
'\''
        printf "%s\n" "${register_address}"
      ' 2>/dev/null
    })" || output=$'\n\n'
    system_fqdn="$(sed -n '1p' <<<"${output}")"
    resolved_ip="$(sed -n '2p' <<<"${output}")"
    register_address="$(sed -n '3p' <<<"${output}")"
    entry="$(jq -cn --arg pod "${pod}" --arg expected "${expected}" \
      --arg configured "${register_address}" --arg fqdn "${system_fqdn}" \
      --arg resolved "${resolved_ip}" --arg mode "${expected_address_mode}" '{
        pod:$pod,
        expectedFQDN:$expected,
        registerSearchAddress:$configured,
        systemFQDN:$fqdn,
        resolvedIP:$resolved,
        matches: (
          $fqdn == $expected and
          (if $mode == "fqdn" then
             $configured == $expected
           else
             $configured == ""
           end))
      }')"
    inventory="$(jq -cn --argjson inventory "${inventory}" \
      --argjson entry "${entry}" '$inventory + [$entry]')"
  done < <(jq -r '.[] | [.name, .expectedFQDN] | @tsv' \
    <<<"${current_indexer_pods}")
  jq -c 'sort_by(.pod)' <<<"${inventory}"
}

fail() {
  local reason="$1"
  record_sample "FAIL-${reason}" || true
  printf 'FAIL: %s; evidence=%s\n' "${reason}" "${evidence_file}" >&2
  exit 1
}

collect_sample
record_sample baseline
baseline_indexer_pods="${current_indexer_pods}"
baseline_update_revision="$(jq -r '.updateRevision' \
  <<<"${current_statefulset}")"

if ! jq -e --argjson replicas "${indexer_replicas}" '
    .phase == "Ready" and .readyReplicas == $replicas' \
    <<<"${current_indexercluster}" >/dev/null ||
  ! jq -e --argjson replicas "${indexer_replicas}" '
    length == $replicas and all(.[]; .ready and .restartCount == 0)' \
    <<<"${current_indexer_pods}" >/dev/null ||
  ! jq -e --argjson replicas "${indexer_replicas}" '
    ([.[] | select(.ready == true)] | length) == $replicas' \
    <<<"${current_indexer_endpoints}" >/dev/null; then
  fail baseline-not-stable
fi

if [[ "${snapshot_only}" == true ]]; then
  indexer_config_json | jq . >"${config_file}"
  printf 'PASS: SHC-98 snapshot captured; evidence=%s config=%s\n' \
    "${evidence_file}" "${config_file}"
  exit 0
fi

roll_started=false
seen_ordinals='[]'
previous_ordinal=-1
ordinal_convergence_violation=''
stable_samples=0
deadline=$((SECONDS + roll_timeout_seconds))

while ((SECONDS < deadline)); do
  collect_sample
  current_stage="$(jq -r '.podUpdate.stage // ""' \
    <<<"${current_indexercluster}")"
  current_ordinal="$(jq -r '.podUpdate.targetOrdinal // -1' \
    <<<"${current_indexercluster}")"
  update_revision="$(jq -r '.updateRevision' <<<"${current_statefulset}")"
  ready_count="$(jq '[.[] | select(.ready)] | length' \
    <<<"${current_indexer_pods}")"
  endpoint_count="$(jq '[.[] | select(.ready == true)] | length' \
    <<<"${current_indexer_endpoints}")"
  restart_count="$(jq '[.[].restartCount] | add // 0' \
    <<<"${current_indexer_pods}")"

  if [[ "${update_revision}" != "${baseline_update_revision}" ]] ||
    ! jq -e --argjson baseline "${baseline_indexer_pods}" '
      all(.[]; . as $pod | any($baseline[];
        .name == $pod.name and .uid == $pod.uid))' \
      <<<"${current_indexer_pods}" >/dev/null; then
    roll_started=true
  fi

  if ((current_ordinal >= 0 && previous_ordinal >= 0 &&
    current_ordinal != previous_ordinal)) &&
    ! rolled_peer_converged_on_all_search_heads "${previous_ordinal}"; then
    if [[ -z "${ordinal_convergence_violation}" ]]; then
      ordinal_convergence_violation="next-target-before-search-head-peer-${previous_ordinal}-converged"
      record_sample "VIOLATION-${ordinal_convergence_violation}"
    fi
  fi

  if ((current_ordinal >= 0)) &&
    ! jq -e --argjson ordinal "${current_ordinal}" \
      'index($ordinal) != null' <<<"${seen_ordinals}" >/dev/null; then
    seen_ordinals="$(jq -cn --argjson seen "${seen_ordinals}" \
      --argjson ordinal "${current_ordinal}" '$seen + [$ordinal]')"
  fi
  if ((current_ordinal >= 0)); then
    previous_ordinal="${current_ordinal}"
  fi

  record_sample "roll-${current_stage:-none}"

  if ((indexer_replicas - ready_count > 1 ||
    endpoint_count < indexer_replicas - 1)); then
    fail more-than-one-indexer-unavailable
  fi
  if ((restart_count != 0)); then
    fail indexer-container-restarted
  fi

  if [[ "${roll_started}" == true && "${current_stage}" == Completed &&
    "${update_revision}" != "${baseline_update_revision}" &&
    "${ready_count}" -eq "${indexer_replicas}" &&
    "${endpoint_count}" -eq "${indexer_replicas}" ]] &&
    statefulset_revision_converged &&
    peers_match_expected && cluster_matches_expected; then
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
  fail full-roll-did-not-converge
fi

expected_ordinals="$(jq -cn --argjson replicas "${indexer_replicas}" \
  '[range($replicas - 1; -1; -1)]')"
if [[ "${seen_ordinals}" != "${expected_ordinals}" ]]; then
  fail "unexpected-target-order-${seen_ordinals}"
fi

if ! jq -n -e --argjson baseline "${baseline_indexer_pods}" \
  --argjson final "${current_indexer_pods}" '
    ($baseline | length) == ($final | length) and
    all($baseline[]; . as $old | any($final[];
        .name == $old.name and .uid != $old.uid and
        .pvcClaims == $old.pvcClaims))' >/dev/null; then
  fail pod-or-pvc-identity-transition-invalid
fi

final_config="$(indexer_config_json)"
printf '%s\n' "${final_config}" | jq . >"${config_file}"
if ! jq -e --argjson replicas "${indexer_replicas}" '
    length == $replicas and all(.[]; .matches == true)' \
    <<<"${final_config}" >/dev/null; then
  fail final-indexer-config-or-fqdn-invalid
fi

k -n "${namespace}" get events -o json | jq \
  --arg indexer "${indexer_pod_prefix}" \
  --arg sh "${search_head_pod_prefix}" '
    [.items[] |
      select(
        (.involvedObject.name | startswith($indexer)) or
        (.involvedObject.name | startswith($sh)) or
        .involvedObject.kind == "IndexerCluster" or
        .involvedObject.kind == "SearchHeadCluster") | {
          type,
          reason,
          message,
          objectKind: .involvedObject.kind,
          objectName: .involvedObject.name,
          count: (.series.count // .count // 1),
          firstObserved:
            (.firstTimestamp // .metadata.creationTimestamp // ""),
          lastObserved:
            (.series.lastObservedTime // .lastTimestamp // .eventTime // "")
        }] | sort_by(.lastObserved, .objectName, .reason)' >"${events_file}"

if [[ -n "${ordinal_convergence_violation}" ]]; then
  fail "${ordinal_convergence_violation}"
fi

printf 'PASS: SHC-98 address roll converged; mode=%s revision=%s ordinals=%s stableSamples=%s finalConfig=%s evidence=%s events=%s config=%s\n' \
  "${expected_address_mode}" \
  "$(jq -r '.currentRevision' <<<"${current_statefulset}")" \
  "${seen_ordinals}" "${stable_samples}" "${final_config}" \
  "${evidence_file}" "${events_file}" "${config_file}"
