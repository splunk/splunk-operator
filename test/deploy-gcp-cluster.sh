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
  echo "GKE_CLUSTER_K8_VERSION not set. Changing to 1.34"
  export GKE_CLUSTER_K8_VERSION="1.34"
fi

function deleteCluster() {
  cluster_location=$(gcloud container clusters list \
    --project "${GCP_PROJECT_ID}" \
    --filter="name=${TEST_CLUSTER_NAME}" \
    --format="value(location)" 2>/dev/null)
  cluster_location=${cluster_location%%$'\n'*}
  cluster_location=${cluster_location:-${GCP_ZONE}}
  echo "Cleaning up GKE test cluster ${TEST_CLUSTER_NAME} in ${cluster_location}"

  # Best-effort Kubernetes cleanup gives CSI provisioners a chance to delete
  # volumes before the cluster disappears. Failed cluster creation may not have
  # produced a usable API server, so do not let this block infrastructure cleanup.
  if gcloud container clusters get-credentials "${TEST_CLUSTER_NAME}" \
    --location "${cluster_location}" \
    --project "${GCP_PROJECT_ID}" >/dev/null 2>&1; then
    tools/cleanup.sh || true
  fi

  # Delete the cluster even when creation failed partway, then remove only GCE
  # resources whose names/labels identify this exact CI job's cluster.
  tools/cleanup-gcp-test-resources.sh \
    --project "${GCP_PROJECT_ID}" \
    --region "${GCP_REGION:-us-west2}" \
    --cluster "${TEST_CLUSTER_NAME}" \
    --location "${cluster_location}" \
    --execute
}

function addCandidateZone() {
  local candidate="$1"
  local existing
  [[ -z "${candidate}" ]] && return
  for existing in "${candidate_zones[@]:-}"; do
    [[ "${existing}" == "${candidate}" ]] && return
  done
  candidate_zones+=("${candidate}")
}

function createClusterInZone() {
  local zone="$1"
  local create_log create_rc
  create_log=$(mktemp "/tmp/${TEST_CLUSTER_NAME}-${zone}.XXXXXX.log")

  echo "Creating cluster ${TEST_CLUSTER_NAME} in ${zone} with n2-standard-8..."
  gcloud container clusters create "${TEST_CLUSTER_NAME}" \
    --project="${GCP_PROJECT_ID}" \
    --num-nodes="${CLUSTER_WORKERS}" \
    --zone="${zone}" \
    --disk-size=30 \
    --network="${GCP_NETWORK}" \
    --subnetwork="${GCP_SUBNETWORK}" \
    --machine-type=n2-standard-8 \
    --cluster-version="${GKE_CLUSTER_K8_VERSION}" \
    --no-enable-cloud-logging \
    --no-enable-cloud-monitoring \
    --scopes="https://www.googleapis.com/auth/cloud-platform" \
    --enable-ip-alias 2>&1 | tee "${create_log}"
  create_rc=${PIPESTATUS[0]}

  if [ ${create_rc} -eq 0 ]; then
    rm -f "${create_log}"
    return 0
  fi

  if grep -Eq 'GCE_STOCKOUT|does not have enough resources available' "${create_log}"; then
    GCP_CREATE_STOCKOUT=true
  else
    GCP_CREATE_STOCKOUT=false
  fi
  rm -f "${create_log}"
  return ${create_rc}
}

function createCluster() {
  # Deploy gcloud cluster if not deployed
  if ! command -v gcloud >/dev/null 2>&1; then
    echo "gcloud is not installed or in the PATH. Please install gcloud from https://cloud.google.com/sdk/docs/install."
    return 1
  fi

  found_location=$(gcloud container clusters list \
    --project "${GCP_PROJECT_ID}" \
    --filter="name=${TEST_CLUSTER_NAME}" \
    --format="value(location)")
  found_location=${found_location%%$'\n'*}
  if [ -z "${found_location}" ]; then
    candidate_zones=()
    addCandidateZone "${GCP_ZONE}"
    if [ -n "${GCP_ZONE_FALLBACKS:-}" ]; then
      for fallback_zone in ${GCP_ZONE_FALLBACKS}; do
        addCandidateZone "${fallback_zone}"
      done
    else
      addCandidateZone "${GCP_REGION:-us-west2}-a"
      addCandidateZone "${GCP_REGION:-us-west2}-b"
      addCandidateZone "${GCP_REGION:-us-west2}-c"
    fi

    for candidate_zone in "${candidate_zones[@]}"; do
      export GCP_ZONE="${candidate_zone}"
      GCP_CREATE_STOCKOUT=false
      createClusterInZone "${candidate_zone}"
      create_rc=$?
      if [ ${create_rc} -eq 0 ]; then
        found_location="${candidate_zone}"
        break
      fi
      echo "Unable to create cluster ${TEST_CLUSTER_NAME} in ${candidate_zone}"
      echo "Attempting cleanup of resources left by the failed create operation"
      deleteCluster || echo "Cleanup after failed cluster creation also failed"
      if [ "${GCP_CREATE_STOCKOUT}" != "true" ]; then
        return ${create_rc}
      fi
      echo "GCE stockout detected in ${candidate_zone}; trying the next configured zone"
    done
    if [ -z "${found_location}" ]; then
      echo "Unable to create cluster ${TEST_CLUSTER_NAME}: all candidate zones reported insufficient capacity"
      return 1
    fi
  else
    export GCP_ZONE="${found_location}"
    echo "Retrieving kubeconfig for ${TEST_CLUSTER_NAME} in ${found_location}"
    # Cluster exists but kubeconfig may not
    gcloud container clusters get-credentials "${TEST_CLUSTER_NAME}" \
      --location "${found_location}" \
      --project "${GCP_PROJECT_ID}"
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
