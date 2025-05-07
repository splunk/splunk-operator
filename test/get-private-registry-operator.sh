#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..

source ${scriptdir}/env.sh

PRIVATE_SPLUNK_OPERATOR_IMAGE=${SPLUNK_OPERATOR_IMAGE}

# if we are using private registry, we need to pull, tag and push images to it
if [ -n "${PRIVATE_REGISTRY}" ]; then
  echo "Using private registry at ${PRIVATE_REGISTRY}"

  PRIVATE_SPLUNK_OPERATOR_IMAGE=${PRIVATE_REGISTRY}/${SPLUNK_OPERATOR_IMAGE}
  echo "Checking to see if image exists, docker images -q ${PRIVATE_SPLUNK_OPERATOR_IMAGE}"
  # Don't pull splunk operator if exists locally since we maybe building it locally
  if [ -z $(docker images -q ${PRIVATE_SPLUNK_OPERATOR_IMAGE}) ]; then
    echo "Doesn't exist, pulling ${PRIVATE_SPLUNK_OPERATOR_IMAGE}..."
    docker pull ${PRIVATE_SPLUNK_OPERATOR_IMAGE}
    if [ $? -ne 0 ]; then
     echo "Unable to pull ${SPLUNK_OPERATOR_IMAGE}. Exiting..."
     exit 1
    fi
  fi
  
  # Output
  echo "Docker images"
  docker images
fi

# Return the value of PRIVATE_SPLUNK_OPERATOR_IMAGE
echo "${PRIVATE_SPLUNK_OPERATOR_IMAGE}"