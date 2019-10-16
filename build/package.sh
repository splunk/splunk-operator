#!/bin/bash
# Script to export splunk-operator image to tarball and generated combined YAML

VERSION=$1
if [[ "x$VERSION" == "x" ]]; then
    # Use latest commit id if no version is provided
    VERSION=`git rev-parse HEAD | cut -c1-12`
fi
IMAGE_ID=`docker images splunk-operator:latest -q`

echo Tagging image ${IMAGE_ID} as splunk/splunk-operator:${VERSION}
docker tag ${IMAGE_ID} splunk/splunk-operator:${VERSION}

echo Generating splunk-operator-${VERSION}.tar.gz and splunk-operator-${VERSION}.yaml
docker image save splunk/splunk-operator:${VERSION} | gzip -c > splunk-operator-${VERSION}.tar.gz
sed -e "s,image: splunk/splunk-operator.*,image: splunk/splunk-operator:${VERSION}," deploy/all-in-one.yaml > splunk-operator-${VERSION}.yaml
ls -la splunk-operator-${VERSION}.tar.gz splunk-operator-${VERSION}.yaml
