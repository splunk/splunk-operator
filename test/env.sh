#!/bin/bash

: "${SPLUNK_OPERATOR_IMAGE:=rliebermansplunk/splunk-operator:operator_18_sep_2025}"
: "${SPLUNK_ENTERPRISE_IMAGE:=splunk/splunk:10.0.0}"
: "${CLUSTER_PROVIDER:=}"
: "${CLUSTER_NAME:=integration-test-cluster-eks}"
: "${NUM_WORKERS:=3}"
: "${NUM_NODES:=2}"
: "${COMMIT_HASH:=}"
# AWS specific variables
: "${ECR_REGISTRY:=}"
# CSPL-2920 - default instance type, use .env to set specific types to use in workflows
: "${EKS_INSTANCE_TYPE:=m5.2xlarge}"
: "${VPC_PUBLIC_SUBNET_STRING:=}"
: "${VPC_PRIVATE_SUBNET_STRING:=}"
: "${EKS_CLUSTER_K8_VERSION:=1.31}"
# Below env variables required to run license master test cases
: "${ENTERPRISE_LICENSE_S3_PATH:=test_licenses/}"
: "${TEST_S3_BUCKET:=splk-test-data-bucket}"
# Below env variables required to run remote indexes test cases
: "${INDEXES_S3_BUCKET:=splk-integration-test-bucket}"
: "${AWS_S3_REGION:=us-west-2}"
# Azure specific variables
: "${AZURE_REGION:=}"
: "${AZURE_TEST_CONTAINER:=}"
: "${AZURE_INDEXES_CONTAINER:=}"
: "${AZURE_ENTERPRISE_LICENSE_PATH:=}"
: "${AZURE_RESOURCE_GROUP:=}"
: "${AZURE_CONTAINER_REGISTRY:=}"
: "${AZURE_CONTAINER_REGISTRY_LOGIN_SERVER:=}"
: "${AZURE_CLUSTER_AGENTPOOL:=}"
: "${AZURE_CLUSTER_AGENTPOOL_RG:=}"
: "${AZURE_STORAGE_ACCOUNT:=}"
: "${AZURE_STORAGE_ACCOUNT_KEY:=}"
: "${AZURE_MANAGED_ID_ENABLED:=}"
# GCP specific variables
: "${GCP_REGION:=us-west2}"
: "${GCP_TEST_CONTAINER:=}"
: "${GCP_INDEXES_CONTAINER:=}"
: "${GCP_ENTERPRISE_LICENSE_PATH:=}"
: "${GCP_RESOURCE_GROUP:=}"
: "${GCP_CONTAINER_REGISTRY:=}"
: "${GCP_CONTAINER_REGISTRY_LOGIN_SERVER:=}"
: "${GCP_CLUSTER_AGENTPOOL:=}"
: "${GCP_CLUSTER_AGENTPOOL_RG:=}"
: "${GCP_STORAGE_ACCOUNT:=}"
: "${GCP_STORAGE_ACCOUNT_KEY:=}"
: "${GCP_MANAGED_ID_ENABLED:=}"
# set when operator need to be installed clusterwide
: "${CLUSTER_WIDE:=true}"
# Below env variable can be used to set the test cases to be run. Defaults to smoke test
# Acceptable input is a regex matching test names
# : "${TEST_REGEX:=smoke}"
: "${TEST_FOCUS:=basic}"
# Regex to skip Test Cases
: "${SKIP_REGEX:=}"
# Set to DEBUG_RUN:=True to skip tear down of test environment in case of test failure
: "${DEBUG_RUN:=False}"
# Type of deplyoment, manifest files or helm chart, possible values "manifest" or "helm"
: "${DEPLOYMENT_TYPE:=manifest}"
: "${TEST_CLUSTER_PLATFORM:=eks}"

# Docker registry to use to push the test images to and pull from in the cluster
# if [ -z "${PRIVATE_REGISTRY}" ]; then
#     case ${CLUSTER_PROVIDER} in
#       kind)
#         PRIVATE_REGISTRY=localhost:5000
#         ;;
#       eks)
#         if [ -z "${ECR_REGISTRY}" ]; then
#           echo "Please define ECR_REGISTRY that specified where images are pushed and pulled from."
#           exit 1
#         fi
#         PRIVATE_REGISTRY="${ECR_REGISTRY}"
#         ;;
#       azure)
#         if [ -z "${AZURE_CONTAINER_REGISTRY_LOGIN_SERVER}" ]; then
#           echo "Please define AZURE_CONTAINER_REGISTRY_LOGIN_SERVER that specified where images are pushed and pulled from."
#           exit 1
#         fi
#         PRIVATE_REGISTRY="${AZURE_CONTAINER_REGISTRY_LOGIN_SERVER}"
#          echo "${PRIVATE_REGISTRY}"
#         ;;
#       gcp)
#         if [ -z "${GCP_CONTAINER_REGISTRY_LOGIN_SERVER}" ]; then
#           echo "Please define GCP_CONTAINER_REGISTRY_LOGIN_SERVER that specified where images are pushed and pulled from."
#           exit 1
#         fi
#         PRIVATE_REGISTRY="${GCP_CONTAINER_REGISTRY_LOGIN_SERVER}"
#          echo "${PRIVATE_REGISTRY}"
#         ;;
#     esac
# fi
