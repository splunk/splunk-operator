#!/bin/bash

if [[ -z "${GCP_VPC_PUBLIC_SUBNET_STRING}" ]]; then
  echo "GCP PUBLIC SUBNET STRING not set. Changing to env.sh value"
  export GCP_VPC_PUBLIC_SUBNET_STRING="${VPC_PUBLIC_SUBNET_STRING}"
fi

if [[ -z "${GCP_VPC_PRIVATE_SUBNET_STRING}" ]]; then
  echo "GCP PRIVATE SUBNET STRING not set. Changing to env.sh value"
  export GCP_VPC_PRIVATE_SUBNET_STRING="${VPC_PRIVATE_SUBNET_STRING}"
fi

if [[ -z "${GCR_REPOSITORY}" ]]; then
  echo "GCR_REPOSITORY not set. Changing to env.sh value"
  export GCR_REPOSITORY="${PRIVATE_REGISTRY}"
fi

if [[ -z "${GKE_CLUSTER_K8_VERSION}" ]]; then
  echo "GKE_CLUSTER_K8_VERSION not set. Changing to 1.26"
  export GKE_CLUSTER_K8_VERSION="1.26"
fi

function deleteCluster() {
  echo "Cleanup remaining PVC on the GKE Cluster ${TEST_CLUSTER_NAME}"
  tools/cleanup.sh
  gcloud container clusters delete ${TEST_CLUSTER_NAME} --zone ${GCP_ZONE} --quiet
  if [ $? -ne 0 ]; then
    echo "Unable to delete cluster - ${TEST_CLUSTER_NAME}"
    return 1
  fi
  return 0
}

function createCluster() {
  # Deploy gcloud cluster if not deployed
  rc=$(which gcloud)
  if [ -z "$rc" ]; then
    echo "gcloud is not installed or in the PATH. Please install gcloud from https://cloud.google.com/sdk/docs/install."
    return 1
  fi

  found=$(gcloud container clusters list --filter="name=${TEST_CLUSTER_NAME}" --format="value(name)")
  if [ -z "${found}" ]; then
    gcloud container clusters create ${TEST_CLUSTER_NAME} \
      --num-nodes=${CLUSTER_WORKERS} \
      --zone=${GCP_ZONE} \
      --subnetwork=${GCP_VPC_PUBLIC_SUBNET_STRING} \
      --cluster-version=${GKE_CLUSTER_K8_VERSION} \
      --machine-type=n1-standard-4
    if [ $? -ne 0 ]; then
      echo "Unable to create cluster - ${TEST_CLUSTER_NAME}"
      return 1
    fi
  else
    echo "Retrieving kubeconfig for ${TEST_CLUSTER_NAME}"
    # Cluster exists but kubeconfig may not
    gcloud container clusters get-credentials ${TEST_CLUSTER_NAME} --zone ${GCP_ZONE}
  fi

  echo "Logging in to GCR"
  gcloud auth configure-docker
  if [ $? -ne 0 ]; then
      echo "Unable to configure Docker for GCR"
      return 1
  fi

  # Output
  echo "GKE cluster nodes:"
  kubectl get nodes
}
