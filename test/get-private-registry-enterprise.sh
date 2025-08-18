#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..

source ${scriptdir}/env.sh

PRIVATE_SPLUNK_ENTERPRISE_IMAGE=${SPLUNK_ENTERPRISE_IMAGE}

# if we are using private registry, we need to pull, tag and push images to it
if [ -n "${PRIVATE_REGISTRY}" ]; then

  # CSPL-2920: ARM64 support
  if [ "$ARM64" != "true" ]; then
    PRIVATE_SPLUNK_ENTERPRISE_IMAGE=${PRIVATE_REGISTRY}/${SPLUNK_ENTERPRISE_IMAGE}
  fi
  # Always attempt to pull splunk enterprise image
  echo "check if image exists, docker manifest inspect $PRIVATE_SPLUNK_ENTERPRISE_IMAGE"
  if docker manifest inspect "$PRIVATE_SPLUNK_ENTERPRISE_IMAGE" > /dev/null 2>&1; then
    echo "Image $PRIVATE_SPLUNK_ENTERPRISE_IMAGE exists on the remote repository."
    docker pull ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}
    if [ $? -ne 0 ]; then
      echo "Unable to pull ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}. Exiting..."
      exit 1
    fi
  else
    echo "Image $PRIVATE_SPLUNK_ENTERPRISE_IMAGE does not exist on the remote repository."
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
  fi
  
  # Output
  echo "Docker images"
  docker images
fi

# Return the value of PRIVATE_SPLUNK_ENTERPRISE_IMAGE
echo "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
