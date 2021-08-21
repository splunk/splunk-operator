#!/bin/bash

if [[ -z "${EKS_VPC_PUBLIC_SUBNET_STRING}" ]]; then
  echo "EKS PUBLIC SUBNET STRING not set. Chaning to env.sh value"
  export EKS_VPC_PUBLIC_SUBNET_STRING="${VPC_PUBLIC_SUBNET_STRING}"
fi

if [[ -z "${EKS_VPC_PRIVATE_SUBNET_STRING}" ]]; then
  echo "EKS PRIVATE SUBNET STRING not set. Chaning to env.sh value"
  export EKS_VPC_PRIVATE_SUBNET_STRING="${VPC_PRIVATE_SUBNET_STRING}"
fi

if [[ -z "${ECR_REPOSITORY}" ]]; then
  echo "ECR_REPOSITORY not set. Chaning to env.sh value"
  export ECR_REPOSITORY="${PRIVATE_REGISTRY}"
fi

function deleteCluster() {
  eksctl delete cluster --name=${TEST_CLUSTER_NAME}
  if [ $? -ne 0 ]; then
    echo "Unable to delete cluster - ${TEST_CLUSTER_NAME}"
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

  found=$(eksctl get cluster --name "${TEST_CLUSTER_NAME}" -v 0)
  if [ -z "${found}" ]; then
    eksctl create cluster --name=${TEST_CLUSTER_NAME} --nodes=${CLUSTER_WORKERS} --vpc-public-subnets=${EKS_VPC_PUBLIC_SUBNET_STRING} --vpc-private-subnets=${EKS_VPC_PRIVATE_SUBNET_STRING}
    if [ $? -ne 0 ]; then
      echo "Unable to create cluster - ${TEST_CLUSTER_NAME}"
      return 1
    fi
  else
    echo "Retrieving kubeconfig for ${TEST_CLUSTER_NAME}"
    # Cluster exists but kubeconfig may not
    eksctl utils write-kubeconfig --cluster=${TEST_CLUSTER_NAME}
  fi

  echo "Logging in to ECR"
  rc=$(aws ecr get-login-password --region us-west-2 | docker login --username AWS --password-stdin "${ECR_REPOSITORY}"/splunk/splunk-operator)
  if [ "$rc" != "Login Succeeded" ]; then
      echo "Unable to login to ECR - $rc"
      return 1
  fi


  # Login to ECR registry so images can be push and pull from later whe
  # Output
  echo "EKS cluster nodes:"
  eksctl get cluster --name=${TEST_CLUSTER_NAME}
}
