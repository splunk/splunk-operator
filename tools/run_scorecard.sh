#!/bin/bash
# Script to scorecard tests
# Reference: https://github.com/operator-framework/operator-sdk/blob/v0.15.1/doc/test-framework/scorecard.md

# exit when any command fails
set -e

VERSION=`grep "Version.*=.*\".*\"" version/version.go | sed "s,.*Version.*=.*\"\(.*\)\".*,\1,"`
IMAGE="docker.io/splunk/splunk-operator:${VERSION}"
CONFIG_FILE=.osdk-scorecard.yaml
ARGS=$@
if [ $# = 0 ]; then
    ARGS="--output text --verbose"
fi

for crd in deploy/examples/*; do
    for f in $crd/*.yaml; do
        MANIFESTS="${MANIFESTS}          - $f
"
    done
done

cat << EOF >$CONFIG_FILE
scorecard:
    bundle: "deploy/olm-catalog/splunk"
    plugins:
    - basic:
        cr-manifest:
$MANIFESTS
    - olm:
        cr-manifest:
$MANIFESTS
        csv-path: "deploy/olm-catalog/splunk/${VERSION}/splunk.v${VERSION}.clusterserviceversion.yaml"
EOF

operator-sdk scorecard $ARGS

rm $CONFIG_FILE
