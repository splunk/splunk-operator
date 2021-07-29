#!/bin/bash
# Script to export splunk-operator image to tarball and generated combined YAML

# exit when any command fails
set -e

VERSION=$1
if [[ "x$VERSION" == "x" ]]; then
    # Use latest commit id if no version is provided
    VERSION=`git rev-parse HEAD | cut -c1-12`
fi
IMAGE="docker.io/splunk/splunk-operator:${VERSION}"
IMAGE_ID=`docker images splunk/splunk-operator:latest -q`

echo Tagging image ${IMAGE_ID} as splunk/splunk-operator:${VERSION}
docker tag ${IMAGE_ID} splunk/splunk-operator:${VERSION}

echo Generating release-${VERSION}/splunk-operator-${VERSION}.tar.gz
mkdir -p release-${VERSION}
rm -f release-${VERSION}/*
docker image save splunk/splunk-operator:${VERSION} | gzip -c > release-${VERSION}/splunk-operator-${VERSION}.tar.gz

echo Generating release-${VERSION}/splunk-operator-crds.yaml
touch release-${VERSION}/splunk-operator-crds.yaml
for i in `ls deploy/crds/|grep crd.yaml`
do
    echo "---" >> release-${VERSION}/splunk-operator-crds.yaml
    cat deploy/crds/$i >> release-${VERSION}/splunk-operator-crds.yaml
done

echo Generating release-${VERSION}/splunk-operator-noadmin.yaml
cat deploy/service_account.yaml deploy/role.yaml deploy/role_binding.yaml > release-${VERSION}/splunk-operator-noadmin.yaml
echo "---" >> release-${VERSION}/splunk-operator-noadmin.yaml
yq w deploy/operator.yaml "spec.template.spec.containers[0].image" $IMAGE >> release-${VERSION}/splunk-operator-noadmin.yaml

echo Generating release-${VERSION}/splunk-operator-install.yaml
cat release-${VERSION}/splunk-operator-crds.yaml release-${VERSION}/splunk-operator-noadmin.yaml > release-${VERSION}/splunk-operator-install.yaml

echo Rebuilding release-${VERSION}/splunk-operator-cluster.yaml
cat release-${VERSION}/splunk-operator-crds.yaml deploy/namespace.yaml > release-${VERSION}/splunk-operator-cluster.yaml
echo "---" >> release-${VERSION}/splunk-operator-cluster.yaml
yq w deploy/service_account.yaml metadata.namespace splunk-operator >> release-${VERSION}/splunk-operator-cluster.yaml
echo "---" >> release-${VERSION}/splunk-operator-cluster.yaml
yq w deploy/role.yaml kind ClusterRole >> release-${VERSION}/splunk-operator-cluster.yaml
echo "---" >> release-${VERSION}/splunk-operator-cluster.yaml
yq w deploy/role_binding.yaml metadata.namespace splunk-operator | yq w - roleRef.kind ClusterRole >> release-${VERSION}/splunk-operator-cluster.yaml
cat deploy/cluster_role.yaml deploy/cluster_role_binding.yaml >> release-${VERSION}/splunk-operator-cluster.yaml
echo "---" >> release-${VERSION}/splunk-operator-cluster.yaml
yq w deploy/operator.yaml metadata.namespace splunk-operator | yq w - "spec.template.spec.containers[0].image" $IMAGE | yq w - "spec.template.spec.containers[0].env[0].value" "" | yq d - "spec.template.spec.containers[0].env[0].valueFrom" >> release-${VERSION}/splunk-operator-cluster.yaml

ls -la release-${VERSION}/
