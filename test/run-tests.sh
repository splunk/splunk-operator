#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..

source ${scriptdir}/env.sh

PRIVATE_SPLUNK_OPERATOR_IMAGE=${SPLUNK_OPERATOR_IMAGE}
PRIVATE_SPLUNK_ENTERPRISE_IMAGE=${SPLUNK_ENTERPRISE_IMAGE}

rc=$(which go)
if [ -z "$rc" ]; then
  echo "go is not installed or in the PATH. Exiting..."
  exit 1
fi

# if we are using private registry, we need to pull, tag and push images to it
if [ -n "${PRIVATE_REGISTRY}" ]; then
  echo "Using private registry at ${PRIVATE_REGISTRY}"

  PRIVATE_SPLUNK_OPERATOR_IMAGE=${PRIVATE_REGISTRY}/${SPLUNK_OPERATOR_IMAGE}
  PRIVATE_SPLUNK_ENTERPRISE_IMAGE=${PRIVATE_REGISTRY}/${SPLUNK_ENTERPRISE_IMAGE}
  echo "docker images -q ${SPLUNK_OPERATOR_IMAGE}"
  # Don't pull splunk operator if exists locally since we maybe building it locally
  if [ -z $(docker images -q ${SPLUNK_OPERATOR_IMAGE}) ]; then 
    docker pull ${SPLUNK_OPERATOR_IMAGE}
    if [ $? -ne 0 ]; then
     echo "Unable to pull ${SPLUNK_OPERATOR_IMAGE}. Exiting..."
     exit 1
    fi
  fi

  docker tag ${SPLUNK_OPERATOR_IMAGE} ${PRIVATE_SPLUNK_OPERATOR_IMAGE}
  docker push ${PRIVATE_SPLUNK_OPERATOR_IMAGE}
  if [ $? -ne 0 ]; then
    echo "Unable to push ${PRIVATE_SPLUNK_OPERATOR_IMAGE}. Exiting..."
    exit 1
  fi

  # Always attempt to pull splunk enterprise image
  docker pull ${SPLUNK_ENTERPRISE_IMAGE}
  if [ $? -ne 0 ]; then
    echo "Unable to pull ${SPLUNK_ENTERPRISE_IMAGE}. Exiting..."
    exit 1
  fi
  docker tag ${SPLUNK_ENTERPRISE_IMAGE} ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}
  docker push ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}
  if [ $? -ne 0 ]; then
    echo "Unable to push ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}. Exiting..."
    exit 1
  fi

  # Output
  echo "Docker images"
  docker images
fi


# Install the CRDs
echo "Installing enterprise CRDs..."
echo "Installing enterprise opearator from ${PRIVATE_SPLUNK_OPERATOR_IMAGE}..."
make deploy IMG=${PRIVATE_SPLUNK_OPERATOR_IMAGE} SPLUNK_ENTERPRISE_IMAGE=${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} WATCH_NAMESPACE=""
if [ $? -ne 0 ]; then
  echo "Unable to install the operator. Exiting..."
  exit 1
fi

echo "wait for operator pod to be ready..."
kubectl wait --for=condition=ready pod -l control-plane=controller-manager --timeout=600s -n splunk-operator
if [ $? -ne 0 ]; then
  echo "Operator installation not ready..."
  exit 1
fi

rc=$(which ginkgo)
if [ -z "$rc" ]; then
  echo "ginkgo is not installed or in the PATH. Installing..."
  go get github.com/onsi/ginkgo/ginkgo
  go get github.com/onsi/gomega/...

  go install github.com/onsi/ginkgo/ginkgo
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

# Set env variable for LM
if [[ -z "${ENTERPRISE_LICENSE_LOCATION}" ]]; then
  echo "License path not set. Changing to default"
  export ENTERPRISE_LICENSE_LOCATION="${ENTERPRISE_LICENSE_PATH}"
fi

# Set env s3 env variables
if [[ -z "${TEST_BUCKET}" ]]; then
  echo "Data bucket not set. Changing to default"
  export TEST_BUCKET="${TEST_S3_BUCKET}"
fi

if [[ -z "${TEST_INDEXES_S3_BUCKET}" ]]; then
  echo "Test bucket not set. Changing to default"
  export TEST_INDEXES_S3_BUCKET="${INDEXES_S3_BUCKET}"
fi

if [[ -z "${CLUSTER_NODES}" ]]; then
  echo "Test Cluster Nodes Not Set in Environment Variables. Changing to env.sh value"
  export CLUSTER_NODES="${NUM_NODES}"
fi

if [[ -z "${S3_REGION}" ]]; then
  echo "S3 Region not set. Changing to default"
  export S3_REGION="${AWS_S3_REGION}"
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
ginkgo -v --trace --failFast -progress -r -nodes=${CLUSTER_NODES} --noisyPendings=false --reportPassed --focus="${TEST_TO_RUN}" --skip="${TEST_TO_SKIP}" ${topdir}/test/s1/appframework -- -commit-hash=${COMMIT_HASH} -operator-image=${PRIVATE_SPLUNK_OPERATOR_IMAGE}  -splunk-image=${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}