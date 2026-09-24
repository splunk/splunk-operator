#!/usr/bin/env bash
# Clean up GCP resources created by the ephemeral SOK GKE integration tests.
#
# Safety properties:
#   * dry-run by default; deletion requires --execute
#   * only targets cluster names matching gke-<numeric CI job id>
#   * stale-cluster sweeps require a minimum age (12 hours by default)
#   * orphaned GCE resources are removed only when their owning cluster is gone
#   * unlabeled CSI disks require a minimum age and SOK integration-test PVC metadata
set -euo pipefail

PROJECT="${PROJECT:-cmp-gke}"
REGION="${REGION:-us-west2}"
OLDER_THAN_HOURS=12
PARALLELISM="${PARALLELISM:-5}"
EXECUTE=false
TARGET_CLUSTER=""
TARGET_LOCATION=""

usage() {
  cat <<'EOF'
Usage:
  tools/cleanup-gcp-test-resources.sh [options]

Account sweep (dry-run by default):
  --project PROJECT          GCP project (default: cmp-gke)
  --region REGION            GCP region (default: us-west2)
  --older-than-hours HOURS   Delete test clusters/resources at least this old (default: 12)
  --parallelism COUNT        Maximum concurrent deletions (default: 5, maximum: 20)
  --execute                  Perform deletions instead of printing them

Targeted cleanup (used by test/deploy-gcp-cluster.sh):
  --cluster gke-JOB_ID       Clean one test cluster and its orphaned resources
  --location ZONE            Cluster zone, required with --cluster

Examples:
  tools/cleanup-gcp-test-resources.sh
  tools/cleanup-gcp-test-resources.sh --older-than-hours 6 --parallelism 5 --execute
  tools/cleanup-gcp-test-resources.sh --cluster gke-123456 --location us-west2-a --execute
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project)
      [[ $# -ge 2 ]] || { echo "--project requires a value" >&2; exit 2; }
      PROJECT="$2"; shift 2 ;;
    --region)
      [[ $# -ge 2 ]] || { echo "--region requires a value" >&2; exit 2; }
      REGION="$2"; shift 2 ;;
    --older-than-hours)
      [[ $# -ge 2 ]] || { echo "--older-than-hours requires a value" >&2; exit 2; }
      OLDER_THAN_HOURS="$2"; shift 2 ;;
    --parallelism)
      [[ $# -ge 2 ]] || { echo "--parallelism requires a value" >&2; exit 2; }
      PARALLELISM="$2"; shift 2 ;;
    --cluster)
      [[ $# -ge 2 ]] || { echo "--cluster requires a value" >&2; exit 2; }
      TARGET_CLUSTER="$2"; shift 2 ;;
    --location)
      [[ $# -ge 2 ]] || { echo "--location requires a value" >&2; exit 2; }
      TARGET_LOCATION="$2"; shift 2 ;;
    --execute) EXECUTE=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if ! [[ "${OLDER_THAN_HOURS}" =~ ^[0-9]+$ ]]; then
  echo "--older-than-hours must be a non-negative integer" >&2
  exit 2
fi
if ! [[ "${PARALLELISM}" =~ ^[0-9]+$ ]] || (( PARALLELISM < 1 || PARALLELISM > 20 )); then
  echo "--parallelism must be an integer between 1 and 20" >&2
  exit 2
fi
if [[ -n "${TARGET_CLUSTER}" ]]; then
  if ! [[ "${TARGET_CLUSTER}" =~ ^gke-[0-9]+$ ]]; then
    echo "Refusing targeted cleanup: cluster must match gke-<numeric CI job id>" >&2
    exit 2
  fi
  if [[ -z "${TARGET_LOCATION}" ]]; then
    echo "--location is required with --cluster" >&2
    exit 2
  fi
fi

if ! command -v gcloud >/dev/null 2>&1; then
  echo "Required command not found: gcloud" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "Required command not found: python3" >&2
  exit 1
fi

active_pids=()
active_commands=()
deletion_failures=0

wait_for_deletions() {
  local index
  for index in "${!active_pids[@]}"; do
    if ! wait "${active_pids[$index]}"; then
      echo "Deletion failed: ${active_commands[$index]}" >&2
      deletion_failures=1
    fi
  done
  active_pids=()
  active_commands=()
}

run() {
  local command_text
  if ! ${EXECUTE}; then
    printf '[dry-run]'
    printf ' %q' "$@"
    printf '\n'
    return 0
  fi

  printf -v command_text '%q ' "$@"
  echo "Starting deletion: ${command_text}"
  "$@" &
  active_pids+=("$!")
  active_commands+=("${command_text}")
  if (( ${#active_pids[@]} >= PARALLELISM )); then
    wait_for_deletions
  fi
}

timestamp_to_epoch() {
  python3 -c 'import datetime, sys; print(int(datetime.datetime.fromisoformat(sys.argv[1].replace("Z", "+00:00")).timestamp()))' "$1"
}

show_n2_quota() {
  local region_json
  echo "N2 CPU quota in ${REGION}:"
  if ! command -v python3 >/dev/null 2>&1; then
    echo "  python3 is unavailable; skipping quota display"
    return 0
  fi
  if ! region_json=$(gcloud compute regions describe "${REGION}" \
    --project="${PROJECT}" \
    --format=json); then
    echo "  unable to read quota (cleanup can continue)" >&2
    return 0
  fi
  printf '%s' "${region_json}" | python3 -c '
import json, sys
region = json.load(sys.stdin)
for quota in region.get("quotas", []):
    if quota.get("metric") == "N2_CPUS":
        print("  usage={} limit={}".format(quota.get("usage", 0), quota.get("limit", 0)))
        break
'
}

live_clusters=""
refresh_live_clusters() {
  if ! live_clusters=$(gcloud container clusters list \
    --project="${PROJECT}" \
    --format='value(name)'); then
    echo "Unable to list live clusters; aborting to avoid unsafe cleanup" >&2
    exit 1
  fi
}

is_live_cluster() {
  local wanted="$1"
  local cluster
  while IFS= read -r cluster; do
    [[ "${cluster}" == "${wanted}" ]] && return 0
  done <<< "${live_clusters}"
  return 1
}

owner_from_gke_resource_name() {
  local name="$1"
  if [[ "${name}" =~ ^gke-(gke-[0-9]+)- ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
  fi
}

owner_from_disk_attachment_logs() {
  local disk="$1"
  local resource_name node_name
  if ! resource_name=$(gcloud logging read \
    "resource.type=\"gce_instance\" AND protoPayload.methodName=\"v1.compute.instances.attachDisk\" AND protoPayload.request.deviceName=\"${disk}\"" \
    --project="${PROJECT}" \
    --freshness=30d \
    --limit=1 \
    --order=desc \
    --format='value(protoPayload.resourceName)'); then
    return 1
  fi
  resource_name=${resource_name%%$'\n'*}
  [[ -n "${resource_name}" ]] || return 1
  node_name=${resource_name##*/}
  owner_from_gke_resource_name "${node_name}"
}

delete_cluster_if_present() {
  local cluster="$1"
  local location="$2"
  if gcloud container clusters describe "${cluster}" \
    --location="${location}" \
    --project="${PROJECT}" >/dev/null 2>&1; then
    run gcloud container clusters delete "${cluster}" \
      --location="${location}" \
      --project="${PROJECT}" \
      --quiet
    wait_for_deletions
  else
    echo "Cluster ${cluster} does not exist in ${location}; checking orphaned GCE resources"
  fi
}

cleanup_stale_clusters() {
  local rows name location status created created_epoch age_hours now
  now=$(date -u +%s)
  if ! rows=$(gcloud container clusters list \
    --project="${PROJECT}" \
    --filter="name~'^gke-[0-9]+$'" \
    --format='value(name,location,status,createTime)'); then
    echo "Unable to inventory GKE clusters; aborting" >&2
    exit 1
  fi

  while IFS=$'\t' read -r name location status created; do
    [[ -z "${name}" ]] && continue
    if [[ "${location}" != "${REGION}" && "${location}" != "${REGION}-"* ]]; then
      continue
    fi
    if [[ -z "${created}" ]] || ! created_epoch=$(timestamp_to_epoch "${created}" 2>/dev/null); then
      echo "SKIP ${name}: unable to parse creation time '${created}'" >&2
      continue
    fi
    age_hours=$(( (now - created_epoch) / 3600 ))
    if (( age_hours < OLDER_THAN_HOURS )); then
      echo "SKIP ${name}: age=${age_hours}h status=${status} (minimum ${OLDER_THAN_HOURS}h)"
      continue
    fi
    echo "STALE ${name}: location=${location} age=${age_hours}h status=${status}"
    run gcloud container clusters delete "${name}" \
      --location="${location}" \
      --project="${PROJECT}" \
      --quiet
  done <<< "${rows}"
  wait_for_deletions
}

cleanup_orphaned_instance_groups() {
  local only_owner="${1:-}"
  local rows name zone owner
  if ! rows=$(gcloud compute instance-groups managed list \
    --project="${PROJECT}" \
    --filter="name~'^gke-gke-[0-9]+-'" \
    --format='value(name,zone.basename())'); then
    echo "Unable to inventory managed instance groups; aborting" >&2
    exit 1
  fi
  while IFS=$'\t' read -r name zone; do
    [[ -z "${name}" || -z "${zone}" ]] && continue
    [[ "${zone}" != "${REGION}-"* ]] && continue
    owner=$(owner_from_gke_resource_name "${name}")
    [[ -z "${owner}" ]] && continue
    [[ -n "${only_owner}" && "${owner}" != "${only_owner}" ]] && continue
    if is_live_cluster "${owner}"; then
      echo "SKIP managed instance group ${name}: owner ${owner} is live"
      continue
    fi
    echo "ORPHAN managed instance group ${name}: zone=${zone} owner=${owner}"
    run gcloud compute instance-groups managed delete "${name}" \
      --zone="${zone}" \
      --project="${PROJECT}" \
      --quiet
  done <<< "${rows}"
  wait_for_deletions
}

cleanup_orphaned_instances() {
  local only_owner="${1:-}"
  local rows name zone owner
  if ! rows=$(gcloud compute instances list \
    --project="${PROJECT}" \
    --filter="name~'^gke-gke-[0-9]+-'" \
    --format='value(name,zone.basename())'); then
    echo "Unable to inventory GCE instances; aborting" >&2
    exit 1
  fi
  while IFS=$'\t' read -r name zone; do
    [[ -z "${name}" || -z "${zone}" ]] && continue
    [[ "${zone}" != "${REGION}-"* ]] && continue
    owner=$(owner_from_gke_resource_name "${name}")
    [[ -z "${owner}" ]] && continue
    [[ -n "${only_owner}" && "${owner}" != "${only_owner}" ]] && continue
    if is_live_cluster "${owner}"; then
      echo "SKIP instance ${name}: owner ${owner} is live"
      continue
    fi
    echo "ORPHAN instance ${name}: zone=${zone} owner=${owner}"
    run gcloud compute instances delete "${name}" \
      --zone="${zone}" \
      --project="${PROJECT}" \
      --quiet
  done <<< "${rows}"
  wait_for_deletions
}

cleanup_orphaned_pvc_disks() {
  local only_owner="${1:-}"
  local disk_json rows name zone owner created creator namespace pvc_name
  local attachment_owner created_epoch age_hours age_text now
  now=$(date -u +%s)
  if ! disk_json=$(gcloud compute disks list \
    --project="${PROJECT}" \
    --filter="name~'^pvc-' AND -users:*" \
    --format='json(name,zone,labels,creationTimestamp,description)'); then
    echo "Unable to inventory PVC disks; aborting" >&2
    exit 1
  fi

  # GKE's PD CSI provisioner records Kubernetes ownership in the disk
  # description but does not always add goog-k8s-cluster-name. Normalize both
  # sources into delimiter-separated fields so the fallback below recognizes only
  # PVCs created by this repository's integration-test naming conventions.
  if ! rows=$(printf '%s' "${disk_json}" | python3 -c '
import json, sys

for disk in json.load(sys.stdin):
    labels = disk.get("labels") or {}
    description = disk.get("description") or "{}"
    try:
        metadata = json.loads(description) if isinstance(description, str) else description
    except (TypeError, json.JSONDecodeError):
        metadata = {}
    zone = (disk.get("zone") or "").rsplit("/", 1)[-1]
    fields = (
        disk.get("name") or "",
        zone,
        labels.get("goog-k8s-cluster-name") or "",
        disk.get("creationTimestamp") or "",
        metadata.get("storage.gke.io/created-by") or "",
        metadata.get("kubernetes.io/created-for/pvc/namespace") or "",
        metadata.get("kubernetes.io/created-for/pvc/name") or "",
    )
    print("\x1f".join(str(field).replace("\x1f", " ") for field in fields))
'); then
    echo "Unable to parse PVC disk inventory; aborting" >&2
    exit 1
  fi

  while IFS=$'\x1f' read -r name zone owner created creator namespace pvc_name; do
    [[ -z "${name}" || -z "${zone}" ]] && continue
    [[ "${zone}" != "${REGION}-"* ]] && continue
    if ! [[ "${owner}" =~ ^gke-[0-9]+$ ]]; then
      if [[ -n "${owner}" ]]; then
        echo "SKIP disk ${name}: unrecognized test-cluster owner '${owner}'"
        continue
      fi
      if [[ "${creator}" != "pd.csi.storage.gke.io" ]]; then
        echo "SKIP disk ${name}: missing test-cluster owner and unrecognized creator '${creator}'"
        continue
      fi

      # App Framework test namespaces contain the 8-character commit hash and
      # suite name. Require the expected PVC name as a second independent check.
      if [[ "${namespace}" =~ ^[0-9a-f]{8}-(s1appfw|c3appfw|m4appfw)-[a-z0-9]{3}-[a-z0-9]{3}$ || \
              "${namespace}" =~ ^master[0-9a-f]{8}-(c3app|m4app)-[a-z0-9]{3}$ ]]; then
        if [[ "${pvc_name}" != pvc-etc-splunk-"${namespace}"-* && \
              "${pvc_name}" != pvc-var-splunk-"${namespace}"-* ]]; then
          echo "SKIP disk ${name}: CSI metadata has unexpected test PVC '${namespace}/${pvc_name}'"
          continue
        fi
        # Test namespaces prove SOK ownership but not which concurrently running
        # GKE test cluster owns the disk. Targeted cleanup therefore requires an
        # exact cluster match from the disk's most recent attach event.
        if [[ -n "${only_owner}" ]]; then
          if ! attachment_owner=$(owner_from_disk_attachment_logs "${name}") || \
              ! [[ "${attachment_owner}" =~ ^gke-[0-9]+$ ]]; then
            echo "SKIP disk ${name}: unable to prove integration-test cluster ownership from attachment logs"
            continue
          fi
          if [[ "${attachment_owner}" != "${only_owner}" ]]; then
            echo "SKIP disk ${name}: attachment-log owner ${attachment_owner} does not match target ${only_owner}"
            continue
          fi
          if is_live_cluster "${attachment_owner}"; then
            echo "SKIP disk ${name}: attachment-log owner ${attachment_owner} is live"
            continue
          fi
          owner="${attachment_owner}"
        fi
      elif [[ "${namespace}" == "splunk-operator" && "${pvc_name}" == "splunk-operator-app-download" ]]; then
        # The cluster-wide operator namespace is not test-specific. Recover
        # the cluster from the most recent GCE attachDisk audit event, then
        # require both an integration-test name and an absent live cluster.
        if ! attachment_owner=$(owner_from_disk_attachment_logs "${name}") || \
            ! [[ "${attachment_owner}" =~ ^gke-[0-9]+$ ]]; then
          echo "SKIP disk ${name}: unable to prove integration-test cluster ownership from attachment logs"
          continue
        fi
        [[ -n "${only_owner}" && "${attachment_owner}" != "${only_owner}" ]] && continue
        if is_live_cluster "${attachment_owner}"; then
          echo "SKIP disk ${name}: attachment-log owner ${attachment_owner} is live"
          continue
        fi
        owner="${attachment_owner}"
      else
        echo "SKIP disk ${name}: CSI metadata does not identify a SOK integration-test PVC '${namespace}/${pvc_name}'"
        continue
      fi

      if [[ -z "${only_owner}" ]]; then
        if [[ -z "${created}" ]] || ! created_epoch=$(timestamp_to_epoch "${created}" 2>/dev/null); then
          echo "SKIP disk ${name}: unable to parse creation time '${created}'" >&2
          continue
        fi
        age_hours=$(( (now - created_epoch) / 3600 ))
        if (( age_hours < OLDER_THAN_HOURS )); then
          echo "SKIP disk ${name}: unlabeled SOK test PVC age=${age_hours}h (minimum ${OLDER_THAN_HOURS}h)"
          continue
        fi
        age_text="${age_hours}h"
      else
        age_text="targeted"
      fi

      echo "ORPHAN unlabeled SOK test PVC disk ${name}: zone=${zone} age=${age_text} owner=${owner:-test-namespace} pvc=${namespace}/${pvc_name}"
      run gcloud compute disks delete "${name}" \
        --zone="${zone}" \
        --project="${PROJECT}" \
        --quiet
      continue
    fi
    [[ -n "${only_owner}" && "${owner}" != "${only_owner}" ]] && continue
    if is_live_cluster "${owner}"; then
      echo "SKIP disk ${name}: owner ${owner} is live"
      continue
    fi
    echo "ORPHAN PVC disk ${name}: zone=${zone} owner=${owner}"
    run gcloud compute disks delete "${name}" \
      --zone="${zone}" \
      --project="${PROJECT}" \
      --quiet
  done <<< "${rows}"
  wait_for_deletions
}

if ${EXECUTE}; then
  echo "Deletion enabled for project=${PROJECT} region=${REGION} parallelism=${PARALLELISM}"
else
  echo "Dry run for project=${PROJECT} region=${REGION} parallelism=${PARALLELISM}; pass --execute to delete"
fi

show_n2_quota

if [[ -n "${TARGET_CLUSTER}" ]]; then
  delete_cluster_if_present "${TARGET_CLUSTER}" "${TARGET_LOCATION}"
  refresh_live_clusters
  cleanup_orphaned_instance_groups "${TARGET_CLUSTER}"
  # Refresh after deleting instance groups so their managed VMs can disappear.
  refresh_live_clusters
  cleanup_orphaned_instances "${TARGET_CLUSTER}"
  cleanup_orphaned_pvc_disks "${TARGET_CLUSTER}"
else
  cleanup_stale_clusters
  refresh_live_clusters
  cleanup_orphaned_instance_groups
  refresh_live_clusters
  cleanup_orphaned_instances
  cleanup_orphaned_pvc_disks
fi

if ${EXECUTE}; then
  show_n2_quota
fi

if (( deletion_failures != 0 )); then
  echo "One or more deletions failed; review the errors above" >&2
  exit 1
fi
