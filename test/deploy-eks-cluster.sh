#!/bin/bash

function deleteCluster() {
  eksctl delete cluster --name=${CLUSTER_NAME}
  if [ $? -ne 0 ]; then
    echo "Unable to delete cluster - ${CLUSTER_NAME}"
    return 1
  fi

  return 0
}

function createCluster() {
  # Deploy eksctl cluster if not deploy
  rc=$(which eksctl)
  if [ -z "$rc" ]; then
    echo "eksctl is not installed or in the PATH. Please install eksctl from https://github.com/weaveworks/eksctl."
    return 1
  fi

  found=$(eksctl get cluster --name "${CLUSTER_NAME}")
  if [ -z "${found}" ]; then
    eksctl create cluster --name=${CLUSTER_NAME} --nodes=${NUM_WORKERS}
    if [ $? -ne 0 ]; then
      echo "Unable to create cluster - ${CLUSTER_NAME}"
      return 1
    fi
  else
    echo "Retrieving kubeconfig for ${CLUSTER_NAME}"
    # Cluster exists but kubeconfig may not
    eksctl utils write-kubeconfig --cluster=${CLUSTER_NAME}
  fi

  echo "Logging in to ECR"
  rc=$(aws ecr get-login-password --region us-west-2 | docker login --username AWS --password-stdin "${PRIVATE_REGISTRY}"/splunk/splunk-operator)
  if [ "$rc" != "Login Succeeded" ]; then
      echo "Unable to login to ECR - $rc"
      return 1
  fi


  # Login to ECR registry so images can be push and pull from later whe
  # Output
  echo "EKS cluster nodes:"
  eksctl get cluster --name=${CLUSTER_NAME}
}
