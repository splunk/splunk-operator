#!/bin/bash

: "${SPLUNK_OPERATOR_IMAGE:=kritiashok/splunk-operator:0.1.0}"
: "${SPLUNK_ENTERPRISE_IMAGE:=kritiashok/ansible:idxc}"
: "${CLUSTER_PROVIDER:=eks}"
: "${CLUSTER_NAME:=attractive-unicorn-1600541478}"
: "${NUM_WORKERS:=3}"
: "${NUM_NODES:=2}"
: "${COMMIT_HASH:=}"
: "${ECR_REGISTRY:=667741767953.dkr.ecr.us-west-2.amazonaws.com/kashok}"

# Docker registry to use to push the test images to and pull from in the cluster
if [ -z "${PRIVATE_REGISTRY}" ]; then
    case ${CLUSTER_PROVIDER} in
      kind)
        PRIVATE_REGISTRY=localhost:5000
        ;;
      eks)
        if [ -z "${ECR_REGISTRY}" ]; then
          echo "Please define ECR_REGISTRY that specified where images are pushed and pulled from."
          exit 1
        fi
        PRIVATE_REGISTRY="${ECR_REGISTRY}"
        ;;
    esac
fi
