#!/bin/bash

: "${SPLUNK_OPERATOR_IMAGE:=splunk/splunk-operator:latest}"
: "${SPLUNK_ENTERPRISE_IMAGE:=splunk/splunk:latest}"
: "${CLUSTER_PROVIDER:=kind}"
: "${CLUSTER_NAME:=integration-test-cluster-eks}"
: "${NUM_WORKERS:=3}"
: "${NUM_NODES:=2}"
: "${COMMIT_HASH:=}"
: "${ECR_REGISTRY:=}"
: "${VPC_PUBLIC_SUBNET_STRING:=}"
: "${VPC_PRIVATE_SUBNET_STRING:=}"
# Below env variables required to run license master test cases
: "${ENTERPRISE_LICENSE_PATH:=}"
: "${TEST_S3_BUCKET:=}"
# Below env variables requried to run remote indexes test cases
: "${INDEXES_S3_BUCKET:=}"
: "${AWS_S3_REGION:=}"
# Below env variable can be used to set the test cases to be run. Defaults to smoke test
# Acceptable input is a regex matching test names
: "${TEST_REGEX:=smoke}"
# Regex to skip Test Cases
: "${SKIP_REGEX:=}"

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
    esac
fi
