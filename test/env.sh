#!/bin/bash

: "${SPLUNK_OPERATOR_IMAGE:=splunk/splunk-operator:latest}"
: "${SPLUNK_ENTERPRISE_IMAGE:=splunk/splunk:latest}"
: "${CLUSTER_PROVIDER:=eks}"
: "${CLUSTER_NAME:=kuttTestsBiasLang}"
: "${NUM_WORKERS:=3}"
: "${NUM_NODES:=2}"
: "${COMMIT_HASH:=}"
# set when operator need to be installed clusterwide
: "${CLUSTER_WIDE:=false}"
: "${ECR_REGISTRY:=667741767953.dkr.ecr.us-west-2.amazonaws.com/mgaldino}"
: "${VPC_PUBLIC_SUBNET_STRING:=subnet-0921cea9bcffd7b77,subnet-0dbbc27abdf4a416e,subnet-0dec6ad34b32e791f}"
: "${VPC_PRIVATE_SUBNET_STRING:=subnet-0c068ca7d9c468e09,subnet-0b9a43cb73e3f9799,subnet-0fa22fc6046b4591f}"
# Below env variables required to run license master test cases
: "${ENTERPRISE_LICENSE_PATH:=/test_licenses}"
: "${TEST_S3_BUCKET:=splk-test-data-bucket}"
# Below env variables requried to run remote indexes test cases
: "${INDEXES_S3_BUCKET:=splk-integration-test-bucket}"
: "${AWS_S3_REGION:=us-west-2}"
# Below env variable can be used to set the test cases to be run. Defaults to smoke test
# Acceptable input is a regex matching test names
: "${TEST_REGEX:=smoke}"
# Regex to skip Test Cases
: "${SKIP_REGEX:=}"
# Set to DEBUG_RUN:=True to skip tear down of test environment in case of test failure
: "${DEBUG_RUN:=False}"

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
