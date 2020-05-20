#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..
source ${scriptdir}/env.sh

action=${1:-up}    

# source the script file containing the create/delete cluster implementation
srcfile="${scriptdir}/deploy-${CLUSTER_PROVIDER}-cluster.sh"

if [ ! -f "$srcfile" ]; then
    echo "Unknown cluster provider '${CLUSTER_PROVIDER}'"
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