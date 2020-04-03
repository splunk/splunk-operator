#!/bin/bash
# Script to export splunk-operator image to tarball and generated combined YAML

# exit when any command fails
set -e

VERSION=$1
if [[ "x$VERSION" == "x" ]]; then
    # Use latest commit id if no version is provided
    VERSION=`git rev-parse HEAD | cut -c1-12`
fi
IMAGE_ID=`docker images splunk/splunk-operator:latest -q`

echo Tagging image ${IMAGE_ID} as splunk/splunk-operator:${VERSION}
docker tag ${IMAGE_ID} splunk/splunk-operator:${VERSION}

echo Generating release-${VERSION}/splunk-operator-${VERSION}.tar.gz
mkdir -p release-${VERSION}
docker image save splunk/splunk-operator:${VERSION} | gzip -c > release-${VERSION}/splunk-operator-${VERSION}.tar.gz

echo Generating release-${VERSION}/splunk-operator-noadmin.yaml
sed -e "s,image: splunk/splunk-operator.*,image: \"splunk/splunk-operator:${VERSION}\"," deploy/namespace_operator.yaml > release-${VERSION}/splunk-operator-noadmin.yaml

echo Generating release-${VERSION}/splunk-operator-rbac.yaml
cp deploy/rbac.yaml release-${VERSION}/splunk-operator-rbac.yaml

echo Generating release-${VERSION}/splunk-operator-crds.yaml
echo "---" > release-${VERSION}/splunk-operator-crds.yaml
cat deploy/crds/enterprise.splunk.com_standalones_crd.yaml >> release-${VERSION}/splunk-operator-crds.yaml
echo "---" >> release-${VERSION}/splunk-operator-crds.yaml
cat deploy/crds/enterprise.splunk.com_licensemasters_crd.yaml >> release-${VERSION}/splunk-operator-crds.yaml
echo "---" >> release-${VERSION}/splunk-operator-crds.yaml
cat deploy/crds/enterprise.splunk.com_searchheadclusters_crd.yaml >> release-${VERSION}/splunk-operator-crds.yaml
echo "---" >> release-${VERSION}/splunk-operator-crds.yaml
cat deploy/crds/enterprise.splunk.com_indexerclusters_crd.yaml >> release-${VERSION}/splunk-operator-crds.yaml
echo "---" >> release-${VERSION}/splunk-operator-crds.yaml
cat deploy/crds/enterprise.splunk.com_sparks_crd.yaml >> release-${VERSION}/splunk-operator-crds.yaml

echo Generating release-${VERSION}/splunk-operator-install.yaml
cat release-${VERSION}/splunk-operator-crds.yaml deploy/rbac.yaml release-${VERSION}/splunk-operator-noadmin.yaml > release-${VERSION}/splunk-operator-install.yaml

echo Rebuilding release-${VERSION}/splunk-operator-cluster.yaml
cat release-${VERSION}/splunk-operator-crds.yaml deploy/rbac.yaml > release-${VERSION}/splunk-operator-cluster.yaml
sed -e "s,image: splunk/splunk-operator.*,image: \"splunk/splunk-operator:${VERSION}\"," deploy/cluster_operator.yaml >> release-${VERSION}/splunk-operator-cluster.yaml

ls -la release-${VERSION}/
