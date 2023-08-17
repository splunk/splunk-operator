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
  echo "Cleanup remaining PVC on the EKS Cluster ${TEST_CLUSTER_NAME}"
  tools/cleanup.sh
  NODE_GROUP=$(eksctl get nodegroup --cluster=${TEST_CLUSTER_NAME} | sed -n 4p | awk '{ print $2 }')
  if [[  ! -z "${NODE_GROUP}" ]]; then
    eksctl delete nodegroup --cluster=${TEST_CLUSTER_NAME} --name=${NODE_GROUP}
    if [ $? -ne 0 ]; then
      echo "Unable to delete Nodegroup ${NODE_GROUP}. For Cluster - ${TEST_CLUSTER_NAME}"
    fi
  fi
  eksctl delete cluster --name=${TEST_CLUSTER_NAME}
  if [ $? -ne 0 ]; then
    echo "Unable to delete cluster - ${TEST_CLUSTER_NAME}"
    return 1
  fi
  rolename= echo ${TEST_CLUSTER_NAME} | awk -F- '{print "EBS_" $(NF-1) "_" $(NF)}'
  aws iam delete-role --role-name ${rolename}

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