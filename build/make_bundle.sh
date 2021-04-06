#!/bin/bash
# Script to update OLM bundles in deploy/olm-catalog and deploy/olm-certified

# exit when any command fails
set -e

VERSION=`grep "Version.*=.*\".*\"" version/version.go | sed "s,.*Version.*=.*\"\(.*\)\".*,\1,"`
OLD_VERSIONS="v1beta1 v1alpha3 v1alpha2"
DOCKER_IO_PATH="docker.io/splunk"
REDHAT_REGISTRY_PATH="registry.connect.redhat.com/splunk"
OPERATOR_IMAGE="$DOCKER_IO_PATH/splunk-operator:${VERSION}"
OLM_CATALOG=deploy/olm-catalog
OLM_CERTIFIED=deploy/olm-certified
YAML_SCRIPT_FILE=.yq_script.yaml

# create yq template to append older CRD versions
rm -f $YAML_SCRIPT_FILE
for v in $OLD_VERSIONS; do
  cat << EOF >>$YAML_SCRIPT_FILE
- command: update
  path: spec.versions[+]
  value:
    name: $v
    served: true
    storage: false
EOF
done

# append older versions to CRD files
for crd in deploy/crds/*_crd.yaml; do
  yq w -i -s $YAML_SCRIPT_FILE $crd
done

RESOURCES="
  - kind: StatefulSets
    version: apps/v1
  - kind: Deployments
    version: apps/v1
  - kind: Pods
    version: v1
  - kind: Services
    version: v1
  - kind: ConfigMaps
    version: v1
  - kind: Secrets
    version: v1
"

cat << EOF >$YAML_SCRIPT_FILE
- command: update
  path: spec.install.spec.deployments[0].spec.template.spec.containers[0].image
  value: $OPERATOR_IMAGE
- command: update
  path: spec.install.spec.permissions[0].serviceAccountName
  value: splunk-operator
- command: update
  path: spec.customresourcedefinitions.owned[0].resources
  value: $RESOURCES
- command: update
  path: spec.customresourcedefinitions.owned[1].resources
  value: $RESOURCES
- command: update
  path: spec.customresourcedefinitions.owned[2].resources
  value: $RESOURCES
- command: update
  path: spec.customresourcedefinitions.owned[3].resources
  value: $RESOURCES
- command: update
  path: spec.customresourcedefinitions.owned[4].resources
  value: $RESOURCES
- command: update
  path: spec.customresourcedefinitions.owned[0].displayName
  value: IndexerCluster
- command: update
  path: spec.customresourcedefinitions.owned[1].displayName
  value: LicenseMaster
- command: update
  path: spec.customresourcedefinitions.owned[2].displayName
  value: SearchHeadCluster
- command: update
  path: spec.customresourcedefinitions.owned[4].displayName
  value: Standalone
- command: update
  path: metadata.annotations.alm-examples
  value: |-
    [{
      "apiVersion": "enterprise.splunk.com/v1",
      "kind": "IndexerCluster",
      "metadata": {
        "name": "example",
        "finalizers": [ "enterprise.splunk.com/delete-pvc" ]
      },
      "spec": {
        "replicas": 1
      }
    },
    {
      "apiVersion": "enterprise.splunk.com/v1",
      "kind": "LicenseMaster",
      "metadata": {
        "name": "example",
        "finalizers": [ "enterprise.splunk.com/delete-pvc" ]
      },
      "spec": {}
    },
    {
      "apiVersion": "enterprise.splunk.com/v1",
      "kind": "SearchHeadCluster",
      "metadata": {
        "name": "example",
        "finalizers": [ "enterprise.splunk.com/delete-pvc" ]
      },
      "spec": {
        "replicas": 1
      }
    },
    {
      "apiVersion": "enterprise.splunk.com/v1",
      "kind": "Standalone",
      "metadata": {
        "name": "example",
        "finalizers": [ "enterprise.splunk.com/delete-pvc" ]
      },
      "spec": {}
    }]
EOF

echo Updating $OLM_CATALOG
operator-sdk generate csv --csv-version $VERSION --operator-name splunk --update-crds --make-manifests=false --verbose
yq w -i -s $YAML_SCRIPT_FILE $OLM_CATALOG/splunk/$VERSION/splunk.v${VERSION}.clusterserviceversion.yaml
rm -f $YAML_SCRIPT_FILE

echo Updating $OLM_CERTIFIED
rm -rf $OLM_CERTIFIED
mkdir -p $OLM_CERTIFIED/splunk
cp $OLM_CATALOG/splunk/$VERSION/*_crd.yaml $OLM_CERTIFIED/splunk/
yq w $OLM_CATALOG/splunk/$VERSION/splunk.v${VERSION}.clusterserviceversion.yaml metadata.certified "true" > $OLM_CERTIFIED/splunk/splunk.v${VERSION}.clusterserviceversion.yaml
yq w $OLM_CATALOG/splunk/splunk.package.yaml packageName "splunk-certified" > $OLM_CERTIFIED/splunk/splunk.package.yaml

# Mac OS expects sed -i '', Linux expects sed -i''. To workaround this, using .bak
zip $OLM_CERTIFIED/splunk.zip -j $OLM_CERTIFIED/splunk $OLM_CERTIFIED/splunk/*
