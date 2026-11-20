#!/bin/bash

function azureDeleteFailureDetails() {
  local details_file="$1"
  local attempt="$2"
  local status="$3"
  local tracking_lines summary

  tracking_lines=$(grep -Ei 'tracking.{0,8}id|correlation.{0,8}id|x-ms-(correlation-)?request-id' "${details_file}" | tail -n 5 || true)
  summary=$(tail -n 8 "${details_file}" | tr '\n' ' ' | cut -c1-1200)
  echo "Azure AKS delete attempt ${attempt} failed with exit status ${status}" >&2
  if [[ -n "${tracking_lines}" ]]; then
    while IFS= read -r line; do
      echo "AZURE_CLEANUP_FAILURE: ${line}" >&2
    done <<< "${tracking_lines}"
  fi
  echo "AZURE_CLEANUP_FAILURE: Failure summary: ${summary:-Azure CLI returned no diagnostic output}" >&2
}

function waitForVmssUpgradeCompletion() {
  local budget_seconds deadline_seconds query_timeout
  local node_resource_group vmss_names vmss_name status status_rc active

  budget_seconds="$1"
  deadline_seconds=$((SECONDS + budget_seconds))
  query_timeout=$((deadline_seconds - SECONDS))
  (( query_timeout > 0 )) || return 1

  node_resource_group=$(timeout "${query_timeout}s" az aks show \
    --name "${TEST_CLUSTER_NAME}" \
    --resource-group "${AZURE_RESOURCE_GROUP}" \
    --query nodeResourceGroup --output tsv 2>&1) || {
      echo "Unable to read the AKS managed node resource group while waiting for VMSS upgrade completion: ${node_resource_group}" >&2
      return 1
    }
  if [[ -z "${node_resource_group}" || "${node_resource_group}" == "None" ]]; then
    echo "AKS managed node resource group is not available yet; will poll again"
    return 1
  fi

  query_timeout=$((deadline_seconds - SECONDS))
  (( query_timeout > 0 )) || return 1
  vmss_names=$(timeout "${query_timeout}s" az vmss list \
    --resource-group "${node_resource_group}" \
    --query '[].name' --output tsv 2>&1) || {
      echo "Unable to list VM scale sets in ${node_resource_group}: ${vmss_names}" >&2
      return 1
    }
  if [[ -z "${vmss_names}" || "${vmss_names}" == "None" ]]; then
    echo "No VM scale sets found yet in ${node_resource_group}; will poll again"
    return 1
  fi

  active=false
  while IFS= read -r vmss_name; do
    [[ -z "${vmss_name}" ]] && continue
    query_timeout=$((deadline_seconds - SECONDS))
    (( query_timeout > 0 )) || return 1
    if status=$(timeout "${query_timeout}s" az vmss rolling-upgrade get-latest \
      --name "${vmss_name}" \
      --resource-group "${node_resource_group}" \
      --query runningStatus.code --output tsv 2>&1); then
      case "${status}" in
        RollingForward|RollingBack)
          echo "VMSS ${vmss_name} platform upgrade is ${status}"
          active=true
          ;;
        ""|None|Completed|Cancelled|Faulted)
          echo "VMSS ${vmss_name} rolling upgrade status: ${status:-no active upgrade}"
          ;;
        *)
          echo "VMSS ${vmss_name} returned unrecognized rolling upgrade status '${status}'; will poll again"
          active=true
          ;;
      esac
    else
      status_rc=$?
      echo "Unable to read rolling upgrade status for VMSS ${vmss_name} (exit ${status_rc}): ${status}" >&2
      active=true
    fi
  done <<< "${vmss_names}"

  [[ "${active}" == false ]]
}

function remainingCleanupSeconds() {
  local start_seconds="$1"
  local timeout_seconds="$2"
  local elapsed_seconds=$((SECONDS - start_seconds))
  local remaining_seconds=$((timeout_seconds - elapsed_seconds))

  (( remaining_seconds > 0 )) || return 1
  printf '%s' "${remaining_seconds}"
}

function deleteCluster() {
  local timeout_seconds interval_seconds start_seconds remaining_seconds
  local attempt delete_output delete_status attempt_details

  timeout_seconds=1800
  interval_seconds=30

  echo "Delete Azure AKS Cluster ${TEST_CLUSTER_NAME}"
  tools/cleanup.sh

  start_seconds=${SECONDS}
  attempt=0
  while true; do
    if ! remaining_seconds=$(remainingCleanupSeconds "${start_seconds}" "${timeout_seconds}"); then
      echo "Azure AKS deletion was not confirmed within ${timeout_seconds}s for ${TEST_CLUSTER_NAME}" >&2
      return 1
    fi

    attempt=$((attempt + 1))
    delete_output=$(mktemp)
    if timeout "${remaining_seconds}s" az aks delete \
      --name "${TEST_CLUSTER_NAME}" \
      --resource-group "${AZURE_RESOURCE_GROUP}" \
      --yes >"${delete_output}" 2>&1; then
      rm -f "${delete_output}"
      echo "Azure AKS deletion confirmed for ${TEST_CLUSTER_NAME}"
      return 0
    else
      delete_status=$?
    fi

    azureDeleteFailureDetails "${delete_output}" "${attempt}" "${delete_status}"
    if grep -Fqi 'VM Agent Rolling Upgrade' "${delete_output}"; then
      rm -f "${delete_output}"
      attempt_details="VM Agent Rolling Upgrade is blocking AKS deletion"
      echo "${attempt_details}; polling managed VMSS rolling upgrade status before retrying"
      while true; do
        if ! remaining_seconds=$(remainingCleanupSeconds "${start_seconds}" "${timeout_seconds}"); then
          echo "Azure AKS deletion remains blocked by ${attempt_details} after ${timeout_seconds}s" >&2
          return 1
        fi

        if waitForVmssUpgradeCompletion "${remaining_seconds}"; then
          break
        fi
        sleep_seconds="${interval_seconds}"
        (( sleep_seconds > remaining_seconds )) && sleep_seconds="${remaining_seconds}"
        sleep "${sleep_seconds}"
      done
      if ! remaining_seconds=$(remainingCleanupSeconds "${start_seconds}" "${timeout_seconds}"); then
        echo "Azure AKS deletion was not confirmed within ${timeout_seconds}s for ${TEST_CLUSTER_NAME}" >&2
        return 1
      fi
      sleep_seconds="${interval_seconds}"
      (( sleep_seconds > remaining_seconds )) && sleep_seconds="${remaining_seconds}"
      echo "Managed VMSS rolling upgrade is no longer active; waiting ${sleep_seconds}s before retrying AKS deletion"
      sleep "${sleep_seconds}"
      continue
    fi

    rm -f "${delete_output}"
    echo "Azure AKS deletion failed for ${TEST_CLUSTER_NAME}; refusing to retry a non-upgrade error" >&2
    return 1
  done
}

function createCluster() {
  # Login to Container Registry (needed to push docker images later on)
  rc=$(az acr login --name ${AZURE_CONTAINER_REGISTRY})
    if [ -z "$rc" ]; then
      echo "Container Registry login issue"
      return 1
    fi

  # Create AKS Cluster
  rc=$(az aks create --resource-group ${AZURE_RESOURCE_GROUP} --name ${TEST_CLUSTER_NAME} --node-count ${CLUSTER_WORKERS} --node-vm-size standard_d8_v3)
  if [[ "${AZURE_MANAGED_ID_ENABLED}" == "true" ]] ;then
    echo "Managed Identity mode: need to assign read access for Kubelet user managed identity to the storage account"
    rc=$(az identity show --name ${AZURE_CLUSTER_AGENTPOOL} --resource-group ${AZURE_CLUSTER_AGENTPOOL_RG} --query 'principalId' --output tsv)
    rc=$(az role assignment create --assignee $rc --role 'Storage Blob Data Reader' --scope /subscriptions/f428689e-c379-4712-a5f4-408c754f16ff/resourceGroups/${AZURE_RESOURCE_GROUP}/providers/Microsoft.Storage/storageAccounts/${AZURE_STORAGE_ACCOUNT})  
  else
    echo "No managed identity to be used, secret-only mode"
  fi
  if [ -z "$rc" ]; then
    echo "AKS Cluster creation issue"
    return 1
  fi

  # Attach Container Registry to AKS Cluster
  rc=$(az aks update --resource-group ${AZURE_RESOURCE_GROUP} --name ${TEST_CLUSTER_NAME} --attach-acr ${AZURE_CONTAINER_REGISTRY})
  rc=$(az aks get-credentials --resource-group ${AZURE_RESOURCE_GROUP} --name ${TEST_CLUSTER_NAME} --overwrite-existing)

  # List created nodes
  rc=$(kubectl get nodes)
  echo "$rc"
}
