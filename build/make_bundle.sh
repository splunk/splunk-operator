#!/bin/bash
# Script to update OLM bundle in deploy/olm-catalog/splunk-operator

# exit when any command fails
set -e

VERSION=`grep "Version.*=.*\".*\"" version/version.go | sed "s,.*Version.*=.*\"\(.*\)\".*,\1,"`
IMAGE="docker.io/splunk/splunk-operator:${VERSION}"
YAML_SCRIPT_FILE=.yq_script.yaml

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
  value: $IMAGE
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
  path: spec.customresourcedefinitions.owned[3].displayName
  value: Spark
- command: update
  path: spec.customresourcedefinitions.owned[4].displayName
  value: Standalone
- command: update
  path: metadata.annotations.alm-examples
  value: |-
    [{
      "apiVersion": "enterprise.splunk.com/v1alpha2",
      "kind": "IndexerCluster",
      "metadata": {
        "name": "example"
      },
      "spec": {}
    },
    {
      "apiVersion": "enterprise.splunk.com/v1alpha2",
      "kind": "LicenseMaster",
      "metadata": {
        "name": "example"
      },
      "spec": {}
    },
    {
      "apiVersion": "enterprise.splunk.com/v1alpha2",
      "kind": "SearchHeadCluster",
      "metadata": {
        "name": "example"
      },
      "spec": {}
    },
    {
      "apiVersion": "enterprise.splunk.com/v1alpha2",
      "kind": "Spark",
      "metadata": {
        "name": "example"
      },
      "spec": {}
    },
    {
      "apiVersion": "enterprise.splunk.com/v1alpha2",
      "kind": "Standalone",
      "metadata": {
        "name": "example"
      },
      "spec": {}
    }]
EOF

operator-sdk generate csv --csv-version $VERSION --operator-name splunk --update-crds --verbose
yq w -i -s $YAML_SCRIPT_FILE deploy/olm-catalog/splunk/$VERSION/splunk.v${VERSION}.clusterserviceversion.yaml

rm -f $YAML_SCRIPT_FILE