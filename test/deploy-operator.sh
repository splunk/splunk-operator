#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..

source ${scriptdir}/env.sh

# Check if exactly 2 arguments are supplied
if [ "$#" -ne 2 ]; then
  echo "Error: Exactly 2 arguments are required."
  echo "Usage: $0 <PRIVATE_SPLUNK_OPERATOR_IMAGE> <PRIVATE_SPLUNK_ENTERPRISE_IMAGE>"
  exit 1
fi

# Assign arguments to variables
PRIVATE_SPLUNK_OPERATOR_IMAGE="$1"
PRIVATE_SPLUNK_ENTERPRISE_IMAGE="$2"

if [  "${DEPLOYMENT_TYPE}" == "helm" ]; then
  echo "Installing Splunk Operator using Helm charts"
  helm uninstall splunk-operator -n splunk-operator
  # Install the CRDs
  echo "Installing enterprise CRDs..."
  make kustomize
  make uninstall
  make install
  if [ "${CLUSTER_WIDE}" != "true" ]; then
    helm install splunk-operator --create-namespace --namespace splunk-operator --set splunkOperator.clusterWideAccess=false --set splunkOperator.image.repository=${PRIVATE_SPLUNK_OPERATOR_IMAGE} --set image.repository=${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} --set splunkOperator.splunkGeneralTerms="--accept-sgt-current-at-splunk-com" helm-chart/splunk-operator
  else
    helm install splunk-operator --create-namespace --namespace splunk-operator --set splunkOperator.image.repository=${PRIVATE_SPLUNK_OPERATOR_IMAGE} --set image.repository=${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} --set splunkOperator.splunkGeneralTerms="--accept-sgt-current-at-splunk-com" helm-chart/splunk-operator
  fi
elif [  "${CLUSTER_WIDE}" != "true" ]; then
  # Install the CRDs
  echo "Installing enterprise CRDs..."
  make kustomize
  make uninstall
  bin/kustomize build config/crd | kubectl create -f -
else
  echo "Installing enterprise operator from ${PRIVATE_SPLUNK_OPERATOR_IMAGE} using enterprise image from ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}..."
  make deploy IMG=${PRIVATE_SPLUNK_OPERATOR_IMAGE} SPLUNK_ENTERPRISE_IMAGE=${PRIVATE_SPLUNK_ENTERPRISE_IMAGE} SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com" WATCH_NAMESPACE="" ENVIRONMENT=debug
fi

if [ $? -ne 0 ]; then
  echo "Unable to install the operator. Exiting..."
  kubectl describe pod -n splunk-operator
  exit 1
fi

echo "Dumping operator config here..."
kubectl describe deployment splunk-operator-controller-manager -n splunk-operator


if [  "${CLUSTER_WIDE}" == "true" ]; then
  echo "wait for operator pod to be ready..."
  # sleep before checking for deployment, in slow clusters deployment call may not even started
  # in those cases, kubectl will fail with error:  no matching resources found
  sleep 2
  kubectl wait --for=condition=ready pod -l control-plane=controller-manager --timeout=600s -n splunk-operator
  if [ $? -ne 0 ]; then
    echo "kubectl get pods -n kube-system ---"
    kubectl get pods -n kube-system
    echo "kubectl get deployement ebs-csi-controller -n kube-system ---"
    kubectl get deployement ebs-csi-controller -n kube-system
    echo "kubectl describe pvc -n splunk-operator ---"
    kubectl describe pvc -n splunk-operator
    echo "kubectl describe pv ---"
    kubectl describe pv
    echo "kubectl describe pod -n splunk-operator ---"
    kubectl describe pod -n splunk-operator
    echo "Operator installation not ready..."
    exit 1
  fi
fi
