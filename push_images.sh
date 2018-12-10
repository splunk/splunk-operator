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
    rsync -vz /tmp/${IMAGE_NAME}.tar.gz $1:/tmp/${IMAGE_NAME}.tar.gz
    ssh $1 "gzip -dc /tmp/${IMAGE_NAME}.tar.gz |docker load"
}

echo "Exporting ${IMAGE_NAME} to /tmp/${IMAGE_NAME}.tar.gz"
docker image save ${IMAGE_NAME}:latest | gzip -c > /tmp/${IMAGE_NAME}.tar.gz

exec 4<push_targets
while read -u4 target ; do
    pushit "$target"
done
