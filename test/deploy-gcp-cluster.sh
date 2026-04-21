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
  echo "GKE_CLUSTER_K8_VERSION not set. Changing to 1.29"
  export GKE_CLUSTER_K8_VERSION="1.29"
fi

function setupServiceAccountAuth() {
  echo "Creating long-lived service account for CI test runs..."

  kubectl create serviceaccount ci-test-runner -n kube-system 2>/dev/null || true
  kubectl create clusterrolebinding ci-test-runner-admin \
    --clusterrole=cluster-admin \
    --serviceaccount=kube-system:ci-test-runner 2>/dev/null || true

  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: ci-test-runner-token
  namespace: kube-system
  annotations:
    kubernetes.io/service-account.name: ci-test-runner
type: kubernetes.io/service-account-token
EOF

  echo "Waiting for service account token to be populated..."
  TOKEN=""
  for i in $(seq 1 30); do
    TOKEN=$(kubectl get secret ci-test-runner-token -n kube-system -o jsonpath='{.data.token}' 2>/dev/null | base64 -d 2>/dev/null)
    if [ -n "${TOKEN}" ]; then
      break
    fi
    sleep 2
  done

  if [ -z "${TOKEN}" ]; then
    echo "WARNING: Failed to obtain service account token, keeping exec-based auth"
    return 0
  fi

  kubectl config set-credentials ci-test-runner --token="${TOKEN}"
  kubectl config set-context --current --user=ci-test-runner

  echo "Switched kubeconfig to long-lived service account token"
  kubectl cluster-info
}

function deleteCluster() {
  echo "Cleanup remaining PVC on the GKE Cluster ${TEST_CLUSTER_NAME}"
  tools/cleanup.sh
  gcloud container clusters delete ${TEST_CLUSTER_NAME} --zone ${GCP_ZONE} --project ${GCP_PROJECT_ID} --quiet
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
      --disk-size 50 \
      --network ${GCP_NETWORK} \
      --subnetwork ${GCP_SUBNETWORK} \
      --machine-type n2-standard-8  \
      --scopes "https://www.googleapis.com/auth/cloud-platform" \
      --enable-ip-alias \
      --cluster-version=${GKE_CLUSTER_K8_VERSION}
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

  setupServiceAccountAuth

  echo "GKE cluster nodes:"
  kubectl get nodes
}
