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

AWS_REGION="${AWS_REGION:-us-west-2}"
OIDC_PROVIDER_SOFT_LIMIT="${OIDC_PROVIDER_SOFT_LIMIT:-95}"
OIDC_PROVIDER_HARD_LIMIT="${OIDC_PROVIDER_HARD_LIMIT:-100}"

function ensureOIDCQuotaAvailable() {
  local oidc_count
  oidc_count=$(aws iam list-open-id-connect-providers --query 'length(OpenIDConnectProviderList)' --output text 2>/dev/null || true)

  if ! [[ "${oidc_count}" =~ ^[0-9]+$ ]]; then
    echo "Warning: unable to determine current OIDC provider count; continuing."
    return 0
  fi

  echo "Current IAM OIDC providers in account: ${oidc_count}"
  if [ "${oidc_count}" -ge "${OIDC_PROVIDER_HARD_LIMIT}" ]; then
    echo "ERROR: OIDC provider quota reached (${oidc_count}/${OIDC_PROVIDER_HARD_LIMIT})."
    echo "Please clean up stale IAM OIDC providers before running smoke tests."
    echo "Hint: aws iam list-open-id-connect-providers"
    return 1
  fi

  if [ "${oidc_count}" -ge "${OIDC_PROVIDER_SOFT_LIMIT}" ]; then
    echo "Warning: OIDC provider usage is high (${oidc_count}/${OIDC_PROVIDER_HARD_LIMIT}); tests may fail during IRSA setup."
  fi
}

function waitForEBSCSIReady() {
  local timeout_seconds="${1:-600}"
  local waited=0

  echo "Waiting for EBS CSI controller deployment to become available..."
  while ! kubectl get deployment ebs-csi-controller -n kube-system >/dev/null 2>&1; do
    if [ "${waited}" -ge "${timeout_seconds}" ]; then
      echo "ERROR: timed out waiting for deployment/ebs-csi-controller to appear in kube-system."
      return 1
    fi
    sleep 10
    waited=$((waited + 10))
  done

  kubectl rollout status deployment/ebs-csi-controller -n kube-system --timeout="${timeout_seconds}s"
  if [ $? -ne 0 ]; then
    echo "ERROR: deployment/ebs-csi-controller did not become ready."
    kubectl describe deployment ebs-csi-controller -n kube-system || true
    kubectl get pods -n kube-system -l app.kubernetes.io/name=aws-ebs-csi-driver || true
    return 1
  fi

  if kubectl get daemonset ebs-csi-node -n kube-system >/dev/null 2>&1; then
    kubectl rollout status daemonset/ebs-csi-node -n kube-system --timeout="${timeout_seconds}s"
    if [ $? -ne 0 ]; then
      echo "ERROR: daemonset/ebs-csi-node did not become ready."
      kubectl describe daemonset ebs-csi-node -n kube-system || true
      return 1
    fi
  fi

  return 0
}

function setDefaultStorageClass() {
  local sc_name=""
  if kubectl get storageclass gp2 >/dev/null 2>&1; then
    sc_name="gp2"
  elif kubectl get storageclass gp3 >/dev/null 2>&1; then
    sc_name="gp3"
  fi

  if [ -z "${sc_name}" ]; then
    echo "ERROR: neither gp2 nor gp3 storageclass exists in the cluster."
    kubectl get storageclass || true
    return 1
  fi

  kubectl patch storageclass "${sc_name}" -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
  if [ $? -ne 0 ]; then
    echo "ERROR: unable to patch storageclass ${sc_name} as default."
    return 1
  fi
  echo "Patched storageclass ${sc_name} as default."
}

function deleteCluster() {
  echo "Cleanup role, security-group, open-id ${TEST_CLUSTER_NAME}"
  account_id=$(aws sts get-caller-identity --query "Account" --output text)
  rolename=$(echo ${TEST_CLUSTER_NAME} | awk -F- '{print "EBS_" $(NF-1) "_" $(NF)}')
  
  # Detach role policies
  role_attached_policies=$(aws iam list-attached-role-policies --role-name ${rolename} --query 'AttachedPolicies[*].PolicyArn' --output text)
  for policy_arn in ${role_attached_policies}; do
    aws iam detach-role-policy --role-name ${rolename} --policy-arn ${policy_arn}
  done

  # Delete IAM role
  aws iam delete-role --role-name ${rolename}

  # Delete OIDC provider
  oidc_id=$(aws eks describe-cluster --name ${TEST_CLUSTER_NAME} --query "cluster.identity.oidc.issuer" --output text | cut -d '/' -f 5)
  aws iam delete-open-id-connect-provider --open-id-connect-provider-arn arn:aws:iam::${account_id}:oidc-provider/oidc.eks.us-west-2.amazonaws.com/id/${oidc_id}

  # Get security group ID
  security_group_id=$(aws eks describe-cluster --name ${TEST_CLUSTER_NAME} --query "cluster.resourcesVpcConfig.securityGroupIds[0]" --output text)

  # Cleanup remaining PVCs on the EKS Cluster
  echo "Cleanup remaining PVC on the EKS Cluster ${TEST_CLUSTER_NAME}"
  tools/cleanup.sh

  # Get node group
  NODE_GROUP=$(eksctl get nodegroup --cluster=${TEST_CLUSTER_NAME} | sed -n 4p | awk '{ print $2 }')

  # Delete the node group to ensure no EC2 instances are using the security group
  echo "Deleting node group - ${NODE_GROUP}"
  eksctl delete nodegroup --cluster=${TEST_CLUSTER_NAME} --name=${NODE_GROUP}

  # Delete cluster
  echo "Deleting cluster - ${TEST_CLUSTER_NAME}"
  eksctl delete cluster --name ${TEST_CLUSTER_NAME}

  if [ $? -ne 0 ]; then
    echo "Unable to delete cluster - ${TEST_CLUSTER_NAME}"
    return 1
  fi

  # Wait for the cluster resources to be fully released before deleting security group
  echo "Waiting for resources to be detached from security group - ${security_group_id}"
  while true; do
    ENIs=$(aws ec2 describe-network-interfaces --filters "Name=group-id,Values=${security_group_id}" --query "NetworkInterfaces[*].NetworkInterfaceId" --output text)
    if [ -z "${ENIs}" ]; then
      break
    fi
    echo "ENIs still attached to security group: ${ENIs}. Waiting for cleanup..."
    sleep 10
  done

  # Delete security group
  aws ec2 delete-security-group --group-id ${security_group_id}

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
    ensureOIDCQuotaAvailable || return 1

    eksctl create cluster --name=${TEST_CLUSTER_NAME} --nodes=${CLUSTER_WORKERS} --vpc-public-subnets=${EKS_VPC_PUBLIC_SUBNET_STRING} --vpc-private-subnets=${EKS_VPC_PRIVATE_SUBNET_STRING} --instance-types=${EKS_INSTANCE_TYPE} --version=${EKS_CLUSTER_K8_VERSION}
    if [ $? -ne 0 ]; then
      echo "Unable to create cluster - ${TEST_CLUSTER_NAME}"
      return 1
    fi

    oidc_output=$(eksctl utils associate-iam-oidc-provider --cluster=${TEST_CLUSTER_NAME} --approve 2>&1)
    if [ $? -ne 0 ]; then
      echo "${oidc_output}"
      echo "ERROR: unable to associate IAM OIDC provider for cluster ${TEST_CLUSTER_NAME}."
      if echo "${oidc_output}" | grep -qi "LimitExceeded"; then
        echo "OIDC quota is exhausted; clean up stale IAM OIDC providers and retry."
      fi
      return 1
    fi

    oidc_id=$(aws eks describe-cluster --name ${TEST_CLUSTER_NAME} --query "cluster.identity.oidc.issuer" --output text | cut -d '/' -f 5)
    if [ -z "${oidc_id}" ]; then
      echo "ERROR: unable to resolve OIDC ID for cluster ${TEST_CLUSTER_NAME}."
      return 1
    fi
    account_id=$(aws sts get-caller-identity --query "Account" --output text)
    oidc_provider=$(aws eks describe-cluster --name ${TEST_CLUSTER_NAME} --region "${AWS_REGION}" --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")
    namespace=kube-system
    service_account=ebs-csi-controller-sa

    kubectl get serviceaccount "${service_account}" --namespace "${namespace}" >/dev/null 2>&1 || kubectl create serviceaccount "${service_account}" --namespace "${namespace}"

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

    rolename=$(echo ${TEST_CLUSTER_NAME} | awk -F- '{print "EBS_" $(NF-1) "_" $(NF)}')

    aws iam get-role --role-name "${rolename}" >/dev/null 2>&1 || \
      aws iam create-role --role-name "${rolename}" --assume-role-policy-document file://aws-ebs-csi-driver-trust-policy.json --description "irsa role for ${TEST_CLUSTER_NAME}"
    if [ $? -ne 0 ]; then
      echo "ERROR: unable to create/get IAM role ${rolename}."
      return 1
    fi

    aws iam attach-role-policy --policy-arn arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy --role-name "${rolename}"
    if [ $? -ne 0 ]; then
      echo "ERROR: unable to attach EBS CSI policy to role ${rolename}."
      return 1
    fi

    kubectl annotate serviceaccount -n "${namespace}" "${service_account}" eks.amazonaws.com/role-arn=arn:aws:iam::${account_id}:role/${rolename} --overwrite
    if [ $? -ne 0 ]; then
      echo "ERROR: unable to annotate serviceaccount ${namespace}/${service_account} with IRSA role."
      return 1
    fi

    eksctl create addon --name aws-ebs-csi-driver --cluster "${TEST_CLUSTER_NAME}" --service-account-role-arn arn:aws:iam::${account_id}:role/${rolename} --force
    if [ $? -ne 0 ]; then
      echo "ERROR: unable to create/update aws-ebs-csi-driver addon."
      return 1
    fi

    waitForEBSCSIReady 900 || return 1

    eksctl utils update-cluster-logging --cluster ${TEST_CLUSTER_NAME}
    setDefaultStorageClass || return 1
  else
    echo "Retrieving kubeconfig for ${TEST_CLUSTER_NAME}"
    # Cluster exists but kubeconfig may not
    eksctl utils write-kubeconfig --cluster=${TEST_CLUSTER_NAME}
    waitForEBSCSIReady 900 || return 1
    setDefaultStorageClass || return 1
  fi

  echo "Logging in to ECR"
  rc=$(aws ecr get-login-password --region "${AWS_REGION}" | docker login --username AWS --password-stdin "${ECR_REPOSITORY}"/splunk/splunk-operator)
  if [ "$rc" != "Login Succeeded" ]; then
      echo "Unable to login to ECR - $rc"
      return 1
  fi


  # Login to ECR registry so images can be push and pull from later whe
  # Output
  echo "EKS cluster nodes:"
  eksctl get cluster --name=${TEST_CLUSTER_NAME}
}
