#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..

source ${scriptdir}/env.sh

# Check if exactly 2 arguments are supplied
if [ "$#" -ne 2 ]; then
  echo "Error: Exactly 2 arguments are required."
  echo "Usage: $0 <PRIVATE_SPLUNK_OPERATOR_IMAGE> <PRIVATE_SPLUNK_ENTERPRISE_IMAGE>"
  exit 1
fi

# Assign arguments to variables
PRIVATE_SPLUNK_OPERATOR_IMAGE="$1"
PRIVATE_SPLUNK_ENTERPRISE_IMAGE="$2"

rc=$(which go)
if [ -z "$rc" ]; then
  echo "go is not installed or in the PATH. Exiting..."
  exit 1
fi

rc=$(which ginkgo)
if [ -z "$rc" ]; then
  echo "ginkgo is not installed or in the PATH. Installing..."
  go get github.com/onsi/ginkgo/ginkgo/v2
  go get github.com/onsi/gomega/...

  go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo@latest
fi

echo "Running test using number of nodes: ${NUM_NODES}"
echo "Running test using these images: ${PRIVATE_SPLUNK_OPERATOR_IMAGE} and ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}..."


# Check if test focus is set
if [[ -z "${TEST_FOCUS}" ]]; then
  TEST_TO_RUN="${TEST_REGEX}"
  echo "Test focus not set running smoke test by default :: ${TEST_TO_RUN}"
else
  TEST_TO_RUN="${TEST_FOCUS}"
  echo "Running following test :: ${TEST_TO_RUN}"
fi

# Set variables
export CLUSTER_PROVIDER="${CLUSTER_PROVIDER}"
case ${CLUSTER_PROVIDER} in
    "eks")
        if [[ -z "${ENTERPRISE_LICENSE_LOCATION}" ]]; then
          echo "License path not set. Changing to default"
          export ENTERPRISE_LICENSE_LOCATION="${ENTERPRISE_LICENSE_S3_PATH}"
        fi

        if [[ -z "${TEST_BUCKET}" ]]; then
          echo "Data bucket not set. Changing to default"
          export TEST_BUCKET="${TEST_S3_BUCKET}"
        fi

        if [[ -z "${TEST_INDEXES_S3_BUCKET}" ]]; then
          echo "Test bucket not set. Changing to default"
          export TEST_INDEXES_S3_BUCKET="${INDEXES_S3_BUCKET}"
        fi

        if [[ -z "${S3_REGION}" ]]; then
          echo "S3 Region not set. Changing to default"
          export S3_REGION="${AWS_S3_REGION}"
        fi
        ;;
    "azure")
        if [[ -z "${ENTERPRISE_LICENSE_LOCATION}" ]]; then
          echo "License path not set. Changing to default"
          export ENTERPRISE_LICENSE_LOCATION="${AZURE_ENTERPRISE_LICENSE_PATH}"
        fi

        if [[ -z "${TEST_CONTAINER}" ]]; then
          echo "Data container not set. Changing to default"
          export TEST_CONTAINER="${AZURE_TEST_CONTAINER}"
        fi

        if [[ -z "${INDEXES_CONTAINER}" ]]; then
          echo "Test container not set. Changing to default"
          export INDEXES_CONTAINER="${AZURE_INDEXES_CONTAINER}"
        fi

        if [[ -z "${REGION}" ]]; then
          echo "Azure Region not set. Changing to default"
          export REGION="${AZURE_REGION}"
        fi

        if [[ -z "${STORAGE_ACCOUNT}" ]]; then
          echo "Azure Storage account not set. Changing to default"
          export STORAGE_ACCOUNT="${AZURE_STORAGE_ACCOUNT}"
        fi

        if [[ -z "${STORAGE_ACCOUNT_KEY}" ]]; then
          echo "Azure Storage account key not set. Changing to default"
          export STORAGE_ACCOUNT_KEY="${AZURE_STORAGE_ACCOUNT_KEY}"
        fi
        ;;
    "gcp")
        if [[ -z "${GCP_ENTERPRISE_LICENSE_LOCATION}" ]]; then
          echo "License path not set. Changing to default"
          export ENTERPRISE_LICENSE_LOCATION="${GCP_ENTERPRISE_LICENSE_LOCATION}"
        fi
        if [[ -z "${ENTERPRISE_LICENSE_LOCATION}" ]]; then
          echo "License path not set. Changing to default"
          export ENTERPRISE_LICENSE_LOCATION="${ENTERPRISE_LICENSE_S3_PATH}"
        fi

        if [[ -z "${TEST_BUCKET}" ]]; then
          echo "Data bucket not set. Changing to default"
          export TEST_BUCKET="${TEST_S3_BUCKET}"
        fi

        if [[ -z "${TEST_INDEXES_S3_BUCKET}" ]]; then
          echo "Test bucket not set. Changing to default"
          export TEST_INDEXES_S3_BUCKET="${INDEXES_S3_BUCKET}"
        fi

        if [[ -z "${S3_REGION}" ]]; then
          echo "S3 Region not set. Changing to default"
          export S3_REGION="${AWS_S3_REGION}"
        fi
        ;;
esac


if [[ -z "${CLUSTER_NODES}" ]]; then
    echo "Test Cluster Nodes Not Set in Environment Variables. Changing to env.sh value"
    export CLUSTER_NODES="${NUM_NODES}"
fi
if [[ -z "${TEST_TO_SKIP}" ]]; then
  echo "TEST_TO_SKIP not set. Changing to default"
  export TEST_TO_SKIP="${SKIP_REGEX}"
fi

if [[ -z "${DEBUG}" ]]; then
  echo "DEBUG not set. Changing to default"
  export DEBUG="${DEBUG_RUN}"
fi



echo "Skipping following test :: ${TEST_TO_SKIP}"

# Running only smoke test cases by default or value passed through TEST_FOCUS env variable. To run different test packages add/remove path from focus argument or TEST_FOCUS variable
echo "ginkgo --junit-report=inttest.xml -vv --keep-going --trace -r --timeout=7h  -nodes=${CLUSTER_NODES} --focus="${TEST_TO_RUN}" --skip="${TEST_TO_SKIP}" --output-interceptor-mode=none --cover ${topdir}/test/ -- -commit-hash=${COMMIT_HASH} -operator-image=${PRIVATE_SPLUNK_OPERATOR_IMAGE}  -splunk-image=${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} -cluster-wide=${CLUSTER_WIDE}"
ginkgo --junit-report=inttest-junit.xml --output-dir=`pwd` -vv --keep-going --trace -r --timeout=7h  -nodes=${CLUSTER_NODES} --focus="${TEST_TO_RUN}" --skip="${TEST_TO_SKIP}" --output-interceptor-mode=none --cover ${topdir}/test/ -- -commit-hash=${COMMIT_HASH} -operator-image=${PRIVATE_SPLUNK_OPERATOR_IMAGE}  -splunk-image=${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} -cluster-wide=${CLUSTER_WIDE}