#!/bin/bash

if [[ -z "${EKS_VPC_PUBLIC_SUBNET_STRING}" ]]; then
  echo "EKS PUBLIC SUBNET STRING not set. Changing to env.sh value"
  export EKS_VPC_PUBLIC_SUBNET_STRING="${VPC_PUBLIC_SUBNET_STRING}"
fi

if [[ -z "${EKS_VPC_PRIVATE_SUBNET_STRING}" ]]; then
  echo "EKS PRIVATE SUBNET STRING not set. Changing to env.sh value"
  export EKS_VPC_PRIVATE_SUBNET_STRING="${VPC_PRIVATE_SUBNET_STRING}"
fi

if [[ -z "${ECR_REPOSITORY}" ]]; then
  echo "ECR_REPOSITORY not set. Changing to env.sh value"
  export ECR_REPOSITORY="${PRIVATE_REGISTRY}"
fi

if [[ -z "${EKS_CLUSTER_K8_VERSION}" ]]; then
  echo "EKS_CLUSTER_K8_VERSION not set. Changing to 1.26"
  export EKS_CLUSTER_K8_VERSION="1.26"
fi

function ebsCsiRoleName() {
  local cluster_name="$1"
  local safe_cluster_name

  safe_cluster_name=$(printf '%s' "${cluster_name}" | tr -c 'A-Za-z0-9+=,.@_-' '_')
  if [ $((${#safe_cluster_name} + 4)) -gt 64 ]; then
    echo "EBS CSI role name would exceed 64 characters: EBS_${safe_cluster_name}" >&2
    return 1
  fi

  printf 'EBS_%s' "${safe_cluster_name}"
}

function clusterExists() {
  local region="${AWS_DEFAULT_REGION:-us-west-2}"
  aws eks describe-cluster --region "${region}" --name "${TEST_CLUSTER_NAME}" >/dev/null 2>&1
}

function deleteOidcProviderForCluster() {
  local region="${AWS_DEFAULT_REGION:-us-west-2}"
  local account_id oidc_issuer oidc_provider

  if ! clusterExists; then
    echo "Cluster ${TEST_CLUSTER_NAME} not found while deleting OIDC provider; skipping cluster-scoped OIDC cleanup"
    return 0
  fi

  account_id=$(aws sts get-caller-identity --query "Account" --output text)
  oidc_issuer=$(aws eks describe-cluster --region "${region}" --name "${TEST_CLUSTER_NAME}" --query "cluster.identity.oidc.issuer" --output text 2>/dev/null || true)
  if [ -z "${oidc_issuer}" ] || [ "${oidc_issuer}" = "None" ]; then
    echo "Cluster ${TEST_CLUSTER_NAME} does not have an OIDC issuer; skipping cluster-scoped OIDC cleanup"
    return 0
  fi

  oidc_provider="${oidc_issuer#https://}"
  aws iam delete-open-id-connect-provider --open-id-connect-provider-arn "arn:aws:iam::${account_id}:oidc-provider/${oidc_provider}" || true
}

function deleteCluster() {
  local region="${AWS_DEFAULT_REGION:-us-west-2}"
  echo "Cleanup role, security-group, open-id ${TEST_CLUSTER_NAME}"
  account_id=$(aws sts get-caller-identity --query "Account" --output text)
  rolename=$(ebsCsiRoleName "${TEST_CLUSTER_NAME}") || return 1

  # Detach role policies
  role_attached_policies=$(aws iam list-attached-role-policies --role-name ${rolename} --query 'AttachedPolicies[*].PolicyArn' --output text 2>/dev/null || true)
  for policy_arn in ${role_attached_policies}; do
    aws iam detach-role-policy --role-name ${rolename} --policy-arn ${policy_arn}
  done

  # Delete IAM role
  aws iam delete-role --role-name ${rolename} 2>/dev/null || true

  # Delete OIDC provider
  deleteOidcProviderForCluster

  # Get security group ID
  security_group_id=$(aws eks describe-cluster --region "${region}" --name ${TEST_CLUSTER_NAME} --query "cluster.resourcesVpcConfig.securityGroupIds[0]" --output text 2>/dev/null || true)

  # Cleanup remaining PVCs on the EKS Cluster
  echo "Cleanup remaining PVC on the EKS Cluster ${TEST_CLUSTER_NAME}"
  if clusterExists; then
    tools/cleanup.sh
  fi

  # Get node group
  NODE_GROUP=""
  if clusterExists; then
    NODE_GROUP=$(eksctl get nodegroup --cluster=${TEST_CLUSTER_NAME} | sed -n 4p | awk '{ print $2 }')
  fi

  # Delete the node group to ensure no EC2 instances are using the security group
  if [ -n "${NODE_GROUP}" ]; then
    echo "Deleting node group - ${NODE_GROUP}"
    eksctl delete nodegroup --cluster=${TEST_CLUSTER_NAME} --name=${NODE_GROUP}
  fi

  # Delete cluster
  if clusterExists; then
    echo "Deleting cluster - ${TEST_CLUSTER_NAME}"
    eksctl delete cluster --name ${TEST_CLUSTER_NAME}
  else
    echo "Cluster ${TEST_CLUSTER_NAME} already absent; skipping cluster delete"
  fi

  if [ $? -ne 0 ]; then
    echo "Unable to delete cluster - ${TEST_CLUSTER_NAME}"
    return 1
  fi

  # Wait for the cluster resources to be fully released before deleting security group
  echo "Waiting for resources to be detached from security group - ${security_group_id}"
  while [ -n "${security_group_id}" ] && [ "${security_group_id}" != "None" ]; do
    ENIs=$(aws ec2 describe-network-interfaces --region "${region}" --filters "Name=group-id,Values=${security_group_id}" --query "NetworkInterfaces[*].NetworkInterfaceId" --output text)
    if [ -z "${ENIs}" ]; then
      break
    fi
    echo "ENIs still attached to security group: ${ENIs}. Waiting for cleanup..."
    sleep 10
  done

  # Delete security group
  if [ -n "${security_group_id}" ] && [ "${security_group_id}" != "None" ]; then
    aws ec2 delete-security-group --region "${region}" --group-id ${security_group_id} || true
  fi

  return 0
}


function createCluster() {
  local region="${AWS_DEFAULT_REGION:-us-west-2}"
  # Deploy eksctl cluster if not deploy
  rc=$(which eksctl)
  if [ -z "$rc" ]; then
    echo "eksctl is not installed or in the PATH. Please install eksctl from https://github.com/eksctl-io/eksctl."
    return 1
  fi

  found=$(eksctl get cluster --name "${TEST_CLUSTER_NAME}" -v 0)
  if [ -z "${found}" ]; then
    eksctl create cluster --name=${TEST_CLUSTER_NAME} --nodes=${CLUSTER_WORKERS} --vpc-public-subnets=${EKS_VPC_PUBLIC_SUBNET_STRING} --vpc-private-subnets=${EKS_VPC_PRIVATE_SUBNET_STRING} --instance-types=${EKS_INSTANCE_TYPE} --version=${EKS_CLUSTER_K8_VERSION}
    if [ $? -ne 0 ]; then
      echo "Unable to create cluster - ${TEST_CLUSTER_NAME}"
      return 1
    fi
    if ! eksctl utils associate-iam-oidc-provider --cluster=${TEST_CLUSTER_NAME} --approve; then
      echo "Unable to associate IAM OIDC provider for ${TEST_CLUSTER_NAME}; deleting the cluster OIDC provider and retrying once"
      deleteOidcProviderForCluster
      if ! eksctl utils associate-iam-oidc-provider --cluster=${TEST_CLUSTER_NAME} --approve; then
        echo "Unable to associate IAM OIDC provider after cleanup - ${TEST_CLUSTER_NAME}"
        return 1
      fi
    fi
    oidc_id=$(aws eks describe-cluster --region "${region}" --name ${TEST_CLUSTER_NAME} --query "cluster.identity.oidc.issuer" --output text | cut -d '/' -f 5)
    account_id=$(aws sts get-caller-identity --query "Account" --output text)
    oidc_provider=$(aws eks describe-cluster --name ${TEST_CLUSTER_NAME}  --region "${region}" --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")
    namespace=kube-system
    service_account=ebs-csi-controller-sa
    kubectl create serviceaccount ${service_account} --namespace ${namespace}
    echo "{
      \"Version\": \"2012-10-17\",
      \"Statement\": [
        {
          \"Effect\": \"Allow\",
          \"Principal\": {
            \"Federated\": \"arn:aws:iam::${account_id}:oidc-provider/${oidc_provider}\"
          },
          \"Action\": \"sts:AssumeRoleWithWebIdentity\",
          \"Condition\": {
            \"StringEquals\": {
              \"${oidc_provider}:aud\": \"sts.amazonaws.com\",
              \"${oidc_provider}:sub\": \"system:serviceaccount:${namespace}:${service_account}\"
            }
          }
        }
      ]
    }"  >aws-ebs-csi-driver-trust-policy.json
    rolename=$(ebsCsiRoleName "${TEST_CLUSTER_NAME}") || return 1
    aws iam create-role --role-name ${rolename} --assume-role-policy-document file://aws-ebs-csi-driver-trust-policy.json --description "irsa role for ${TEST_CLUSTER_NAME}"
    aws iam attach-role-policy  --policy-arn arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy  --role-name ${rolename}
    kubectl annotate serviceaccount -n ${namespace} ${service_account} eks.amazonaws.com/role-arn=arn:aws:iam::${account_id}:role/${rolename}
    eksctl create addon --name aws-ebs-csi-driver --cluster ${TEST_CLUSTER_NAME} --service-account-role-arn arn:aws:iam::${account_id}:role/${rolename} --force
    # CSPL-2887 - Patch the default storage class to gp2
    kubectl patch storageclass gp2 -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
  else
    echo "Retrieving kubeconfig for ${TEST_CLUSTER_NAME}"
    # Cluster exists but kubeconfig may not
    eksctl utils write-kubeconfig --cluster=${TEST_CLUSTER_NAME}
  fi

  echo "Logging in to ECR"
  rc=$(aws ecr get-login-password --region "${region}" | docker login --username AWS --password-stdin "${ECR_REPOSITORY}"/splunk/splunk-operator)
  if [ "$rc" != "Login Succeeded" ]; then
      echo "Unable to login to ECR - $rc"
      return 1
  fi


  # Login to ECR registry so images can be push and pull from later whe
  # Output
  echo "EKS cluster nodes:"
  eksctl get cluster --name=${TEST_CLUSTER_NAME}
}
