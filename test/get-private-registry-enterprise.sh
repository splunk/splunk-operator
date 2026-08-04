#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..

# Keep stdout reserved for the final image reference so callers can safely use
# command substitution without parsing Docker output.
source ${scriptdir}/env.sh >&2

PRIVATE_SPLUNK_ENTERPRISE_IMAGE=${SPLUNK_ENTERPRISE_IMAGE}

# if we are using private registry, we need to pull, tag and push images to it
if [ -n "${PRIVATE_REGISTRY}" ]; then

  source_image_leaf="${SPLUNK_ENTERPRISE_IMAGE##*/}"
  case "${source_image_leaf}" in
    *:*) source_image_tag="${source_image_leaf##*:}" ;;
    *) source_image_tag="latest" ;;
  esac

  if [ "$ARM64" = "true" ]; then
    # CSPL-2920: ARM64 support.
    # EKS worker nodes pull only from ECR (via the node IAM role) and have no
    # credentials for the arm64 source registry (e.g. Artifactory). Mirror the
    # arm64 image into the pre-provisioned ECR splunk/splunk repo under a
    # distinct "-arm64" tag so it never clobbers the amd64 image. imagetools
    # copies registry-to-registry and preserves the source platform, so the
    # amd64 runner can publish the arm64 manifest without pulling it locally.
    PRIVATE_SPLUNK_ENTERPRISE_IMAGE="${PRIVATE_REGISTRY}/splunk/splunk:${source_image_tag}-arm64"

    echo "check if image exists, docker manifest inspect $PRIVATE_SPLUNK_ENTERPRISE_IMAGE" >&2
    if docker manifest inspect "$PRIVATE_SPLUNK_ENTERPRISE_IMAGE" > /dev/null 2>&1; then
      echo "Image $PRIVATE_SPLUNK_ENTERPRISE_IMAGE already present in ECR." >&2
    else
      echo "Mirroring ${SPLUNK_ENTERPRISE_IMAGE} -> ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}" >&2
      docker buildx imagetools create --tag "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}" "${SPLUNK_ENTERPRISE_IMAGE}" >&2
      if [ $? -ne 0 ]; then
        echo "Unable to mirror ${SPLUNK_ENTERPRISE_IMAGE} into ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}. Exiting..." >&2
        exit 1
      fi
    fi
  else
    # Cloud registries pre-provision the stable splunk/splunk repository. Do
    # not copy an Artifactory source's full repository hierarchy into ECR.
    PRIVATE_SPLUNK_ENTERPRISE_IMAGE="${PRIVATE_REGISTRY}/splunk/splunk:${source_image_tag}"

    # Always attempt to pull splunk enterprise image
    echo "check if image exists, docker manifest inspect $PRIVATE_SPLUNK_ENTERPRISE_IMAGE" >&2
    if docker manifest inspect "$PRIVATE_SPLUNK_ENTERPRISE_IMAGE" > /dev/null 2>&1; then
      echo "Image $PRIVATE_SPLUNK_ENTERPRISE_IMAGE exists on the remote repository." >&2
      docker pull ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} >&2
      if [ $? -ne 0 ]; then
        echo "Unable to pull ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}. Exiting..." >&2
        exit 1
      fi
    else
      echo "Image $PRIVATE_SPLUNK_ENTERPRISE_IMAGE does not exist on the remote repository." >&2
      docker pull ${SPLUNK_ENTERPRISE_IMAGE} >&2
      if [ $? -ne 0 ]; then
        echo "Unable to pull ${SPLUNK_ENTERPRISE_IMAGE}. Exiting..." >&2
        exit 1
      fi
      docker tag ${SPLUNK_ENTERPRISE_IMAGE} ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} >&2
      if [ $? -ne 0 ]; then
        echo "Unable to tag ${SPLUNK_ENTERPRISE_IMAGE} as ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}. Exiting..." >&2
        exit 1
      fi
      docker push ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} >&2
      if [ $? -ne 0 ]; then
        echo "Unable to push ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}. Exiting..." >&2
        exit 1
      fi
    fi
  fi

  # Output
  echo "Docker images" >&2
  docker images >&2
fi

# Return the value of PRIVATE_SPLUNK_ENTERPRISE_IMAGE
echo "${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}"
