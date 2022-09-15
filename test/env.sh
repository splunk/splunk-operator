#!/bin/bash

: "${SPLUNK_OPERATOR_IMAGE:=splunk/splunk-operator:latest}"
: "${SPLUNK_ENTERPRISE_IMAGE:=splunk/splunk:latest}"
: "${CLUSTER_PROVIDER:=kind}"
: "${CLUSTER_NAME:=integration-test-cluster-eks}"
: "${NUM_WORKERS:=3}"
: "${NUM_NODES:=2}"
: "${COMMIT_HASH:=}"
# AWS specific variables
: "${ECR_REGISTRY:=}"
: "${VPC_PUBLIC_SUBNET_STRING:=}"
: "${VPC_PRIVATE_SUBNET_STRING:=}"
# Azure specific variables
: "${AZURE_RESOURCE_GROUP:=}"
: "${AZURE_CONTAINER_REGISTRY:=}"
: "${AZURE_CONTAINER_REGISTRY_LOGIN_SERVER:=}"
: "${AZURE_CLUSTER_AGENTPOOL:=}"
: "${AZURE_CLUSTER_AGENTPOOL_RG:=}"
: "${AZURE_STORAGE_ACCOUNT:=}"
: "${AZURE_MANAGED_ID_ENABLED:=}"
# Below env variables required to run license master test cases
: "${ENTERPRISE_LICENSE_PATH:=}"
: "${TEST_S3_BUCKET:=}"
# Below env variables required to run remote indexes test cases
: "${INDEXES_S3_BUCKET:=}"
: "${AWS_S3_REGION:=}"
# Azure specific variables
: "${AZURE_REGION:=}"
: "${TEST_AZURE_CONTAINER:=}"
: "${INDEXES_AZURE_CONTAINER:=}"
: "${ENTERPRISE_LICENSE_AZURE_LOCATION:=}"
: "${AZURE_RESOURCE_GROUP:=}"
: "${AZURE_CONTAINER_REGISTRY:=}"
: "${AZURE_CONTAINER_REGISTRY_LOGIN_SERVER:=}"
: "${AZURE_CLUSTER_AGENTPOOL:=}"
: "${AZURE_CLUSTER_AGENTPOOL_RG:=}"
: "${AZURE_STORAGE_ACCOUNT:=}"
: "${AZURE_STORAGE_ACCOUNT_KEY:=}"
: "${AZURE_MANAGED_ID_ENABLED:=}"
# set when operator need to be installed clusterwide
: "${CLUSTER_WIDE:=false}"
# Below env variable can be used to set the test cases to be run. Defaults to smoke test
# Acceptable input is a regex matching test names
: "${TEST_REGEX:=smoke}"
# Regex to skip Test Cases
: "${SKIP_REGEX:=}"
# Set to DEBUG_RUN:=True to skip tear down of test environment in case of test failure
: "${DEBUG_RUN:=False}"
# Type of deplyoment, manifest files or helm chart, possible values "manifest" or "helm"
: "${DEPLOYMENT_TYPE:=manifest}"

# Docker registry to use to push the test images to and pull from in the cluster
if [ -z "${PRIVATE_REGISTRY}" ]; then
    case ${CLUSTER_PROVIDER} in
      kind)
        PRIVATE_REGISTRY=localhost:5000
        ;;
      eks)
        if [ -z "${ECR_REGISTRY}" ]; then
          echo "Please define ECR_REGISTRY that specified where images are pushed and pulled from."
          exit 1
        fi
        PRIVATE_REGISTRY="${ECR_REGISTRY}"
        ;;
      azure)
        if [ -z "${AZURE_CONTAINER_REGISTRY_LOGIN_SERVER}" ]; then
          echo "Please define AZURE_CONTAINER_REGISTRY_LOGIN_SERVER that specified where images are pushed and pulled from."
          exit 1
        fi
        PRIVATE_REGISTRY="${AZURE_CONTAINER_REGISTRY_LOGIN_SERVER}"
         echo "${PRIVATE_REGISTRY}"
        ;;
    esac
fi
