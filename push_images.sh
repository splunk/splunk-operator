#!/bin/bash

set -e

IMAGE_NAME=$1

if [[ ! -f push_targets ]]; then
    echo "push_images.sh: no push targets"
    exit 0
fi

if [[ "x" == "x${IMAGE_NAME}" ]]; then
    echo "push_images.sh: missing image name argument"
    exit 1
fi

function pushit() {
    echo "Pushing ${IMAGE_NAME} to $1"
    ssh $1 "mkdir -p /tmp/${IMAGE_NAME}"
    rsync --delete -az /tmp/${IMAGE_NAME} $1:/tmp/
    ssh $1 "cd /tmp/${IMAGE_NAME} && tar -cf - * | docker load"
}

echo "Exporting image layers for ${IMAGE_NAME} to /tmp/${IMAGE_NAME}/"
rm -rf /tmp/${IMAGE_NAME}
mkdir -p /tmp/${IMAGE_NAME}
docker image save ${IMAGE_NAME}:latest | tar -C /tmp/${IMAGE_NAME} -xmf -

exec 4<push_targets
while read -u4 target ; do
    pushit "$target"
done
