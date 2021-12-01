#!/bin/bash

set -e

function pushit() {
    echo "Pushing ${IMAGE_NAME} to $1"
    ssh $1 "mkdir -p /tmp/${IMAGE_DIR}"
    rsync --delete -az /tmp/${IMAGE_DIR} $1:/tmp/
    ssh $1 "cd /tmp/${IMAGE_DIR} && tar -cf - * | docker load"
}

IMAGE_NAME=$1

if [[ ! -f push_targets ]]; then
    echo "push_images.sh: no push targets"
    exit 0
fi

if [[ "x" == "x${IMAGE_NAME}" ]]; then
    echo "push_images.sh: missing image name argument"
    exit 1
fi

if [[ ! `echo $IMAGE_NAME | grep ':'` ]]; then
    IMAGE_NAME="${IMAGE_NAME}:latest"
fi

IMAGE_DIR=`echo $IMAGE_NAME | sed -e 's,/,_,g'`

echo "Exporting image layers for ${IMAGE_NAME} to /tmp/${IMAGE_DIR}/"
rm -rf /tmp/${IMAGE_DIR}
mkdir -p /tmp/${IMAGE_DIR}
docker image save ${IMAGE_NAME} | tar -C /tmp/${IMAGE_DIR} -xmf -

exec 4<push_targets
while read -u4 target ; do
    if [[ ! `echo $target | grep '^#'` ]]; then
        pushit "$target"
    fi
done
