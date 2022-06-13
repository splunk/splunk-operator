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
  rc=$(az aks create --resource-group ${AZURE_RESOURCE_GROUP} --name ${TEST_CLUSTER_NAME} --node-count ${CLUSTER_WORKERS} --node-vm-size Standard_D8_v3)
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
