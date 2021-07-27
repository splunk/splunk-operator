#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..
source ${scriptdir}/env.sh

action=${1:-up}

if [[ -z "${TEST_CLUSTER_PLATFORM}" ]]; then
  echo "Test Cluster Platform Not Set in Environment Variables. Changing to env.sh value"
  export TEST_CLUSTER_PLATFORM="${CLUSTER_PROVIDER}"
fi

if [[ -z "${TEST_CLUSTER_NAME}" ]]; then
  echo "Test Cluster Name Not Set in Environment Variables. Changing to env.sh value"
  export TEST_CLUSTER_NAME="${CLUSTER_NAME}"
fi

if [[ -z "${CLUSTER_WORKERS}" ]]; then
  echo "Test Cluster Workers Not Set in Environment Variables. Changing to env.sh value"
  export CLUSTER_WORKERS="${NUM_WORKERS}"
fi

# source the script file containing the create/delete cluster implementation
srcfile="${scriptdir}/deploy-${TEST_CLUSTER_PLATFORM}-cluster.sh"

if [ ! -f "$srcfile" ]; then
    echo "Unknown cluster provider '${TEST_CLUSTER_PLATFORM}'"
    exit 1
fi
source $srcfile

case ${action} in
    up)
        createCluster
        ;;
    down)
        deleteCluster
        ;;
esac
