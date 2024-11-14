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
  echo "ECR_REPOSITORY not set. Changing to env.sh value"
  export ECR_REPOSITORY="${PRIVATE_REGISTRY}"
fi

if [[ -z "${EKS_CLUSTER_K8_VERSION}" ]]; then
  echo "EKS_CLUSTER_K8_VERSION not set. Changing to 1.26"
  export EKS_CLUSTER_K8_VERSION="1.26"
fi

function deleteCluster() {
  echo "Cleanup role, security-group, open-id ${TEST_CLUSTER_NAME}"
  account_id=$(aws sts get-caller-identity --query "Account" --output text)
  rolename=$(echo ${TEST_CLUSTER_NAME} | awk -F- '{print "EBS_" $(NF-1) "_" $(NF)}')
  
  # Detach role policies
  role_attached_policies=$(aws iam list-attached-role-policies --role-name $rolename --query 'AttachedPolicies[*].PolicyArn' --output text)
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
    eksctl create cluster --name=${TEST_CLUSTER_NAME} --nodes=${CLUSTER_WORKERS} --vpc-public-subnets=${EKS_VPC_PUBLIC_SUBNET_STRING} --vpc-private-subnets=${EKS_VPC_PRIVATE_SUBNET_STRING} --instance-types=m5.2xlarge --version=${EKS_CLUSTER_K8_VERSION}
    if [ $? -ne 0 ]; then
      echo "Unable to create cluster - ${TEST_CLUSTER_NAME}"
      return 1
    fi
    eksctl utils associate-iam-oidc-provider --cluster=${TEST_CLUSTER_NAME}  --approve
    oidc_id=$(aws eks describe-cluster --name ${TEST_CLUSTER_NAME} --query "cluster.identity.oidc.issuer" --output text | cut -d '/' -f 5)
    account_id=$(aws sts get-caller-identity --query "Account" --output text)
    oidc_provider=$(aws eks describe-cluster --name ${TEST_CLUSTER_NAME}  --region "us-west-2" --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")
    namespace=kube-system
    service_account=ebs-csi-controller-sa
    kubectl create serviceaccount ${service_account} --namespace ${namespace}
    echo "{
      \"Version\": \"2012-10-17\",
      \"Statement\": [
        {
          \"Effect\": \"Allow\",
          \"Principal\": {
            \"Federated\": \"arn:aws:iam::$account_id:oidc-provider/$oidc_provider\"
          },
          \"Action\": \"sts:AssumeRoleWithWebIdentity\",
          \"Condition\": {
            \"StringEquals\": {
              \"$oidc_provider:aud\": \"sts.amazonaws.com\",
              \"$oidc_provider:sub\": \"system:serviceaccount:$namespace:$service_account\"
            }
          }
        }
      ]
    }"  >aws-ebs-csi-driver-trust-policy.json
    rolename=$(echo ${TEST_CLUSTER_NAME} | awk -F- '{print "EBS_" $(NF-1) "_" $(NF)}')
    aws iam create-role --role-name ${rolename} --assume-role-policy-document file://aws-ebs-csi-driver-trust-policy.json --description "irsa role for ${TEST_CLUSTER_NAME}"
    aws iam attach-role-policy  --policy-arn arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy  --role-name ${rolename}
    kubectl annotate serviceaccount -n $namespace $service_account eks.amazonaws.com/role-arn=arn:aws:iam::$account_id:role/${rolename}
    eksctl create addon --name aws-ebs-csi-driver --cluster ${TEST_CLUSTER_NAME} --service-account-role-arn arn:aws:iam::$account_id:role/${rolename} --force
    eksctl utils update-cluster-logging --cluster ${TEST_CLUSTER_NAME}
    # CSPL-2887 - Patch the default storage class to gp2
    kubectl patch storageclass gp2 -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
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