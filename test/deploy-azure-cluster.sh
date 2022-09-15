#!/bin/bash

function deleteCluster() {
  echo "Delete Azure AKS Cluster ${TEST_CLUSTER_NAME}"
  tools/cleanup.sh
  rc=$(az aks delete --name ${TEST_CLUSTER_NAME} --resource-group ${AZURE_RESOURCE_GROUP} --yes)
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
