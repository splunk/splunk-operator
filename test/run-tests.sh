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
kubectl apply -f ${topdir}/deploy/crds
if [ $? -ne 0 ]; then
  echo "Unable to install the CRDs. Exiting..."
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
# run Ginkgo
ginkgo -v -progress -r -stream -nodes=${NUM_NODES} -skipPackage=example ${topdir}/test -- -commit-hash=${COMMIT_HASH} -operator-image=${PRIVATE_SPLUNK_OPERATOR_IMAGE}  -splunk-image=${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}
