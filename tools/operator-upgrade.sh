#!/usr/bin/env bash

#
# Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Upgrading the Splunk Operator to Version 2.0.0 is a new installation rather than an 
# upgrade from the current operator. The older Splunk Operator must be cleaned up before 
# installing the new version. This script helps you to do the cleanup. The script expects 
# the current namespace where the operator is installed and the path to the 2.0.0 
# manifest file. The script performs the following steps
# * Backup of all the operator resources within the namespace like
#   - service-account, deployment, role, role-binding, cluster-role, cluster-role-binding
# * Deletes all the old Splunk Operator resources and deployment
# * Installs the operator 2.0.0 in Splunk-operator namespace. 
# 
# By default Splunk Operator 2.0.0 will be installed to watch cluster-wide
# Steps for upgrade from 1.0.5 to 2.0.0
# Set KUBECONFIG environment variable and run operator-upgrade.sh script with the following mandatory arguments
# current_namespace: current namespace where operator is installed
# manifest_file: path where 2.0.0 Splunk Operator manifest file exist
#
# example 
# >operator-upgrade.sh --current_namespace=splunk-operator manifest_file=splunk-operator-install.yaml
#
# Note: This script can be run from Mac or Linux system. To run this script on Windows, use cygwin.
#
# Configuring Operator to watch specific namespace:
# Edit config-map splunk-operator-config in splunk-operator namespace, set WATCH_NAMESPACE 
# field to the namespace operator need to watch
#
# apiVersion: v1
# data:
#   OPERATOR_NAME: '"splunk-operator"'
#   RELATED_IMAGE_SPLUNK_ENTERPRISE: splunk/splunk:latest
#   WATCH_NAMESPACE: "add namespace here"
# kind: ConfigMap
# metadata:
#   labels:
#     name: splunk-operator
#   name: splunk-operator-config
#   namespace: splunk-operator

readonly CURRENT_TIME=$(date +%Y-%m-%d_%H%M-%Z)
readonly PROGRAM_NAME=$(basename "$0")

help() {
  echo ""
  echo "USAGE: ${PROGRAM_NAME} --help [ --current_namespace=<namespacename> ] [ --manifest_file=<fulepath with filename of Splunk Operator 1.1.0 manfiests file>] "
  echo ""
  echo "OPTIONS:"
  echo ""
  echo -e "   --current_namespace specifiy the current namespace where operator is installed, \n" \
       "                          script will delete existing serviceaccount, deployment, role and " \
       "                          rolebinding and install the operator in splunk-operator namespace"
  echo ""
  echo "   --manifest_file Splunk Operator 1.1.0 manifest file path, this can be url link or full path of the file"
  echo ""
  echo ""
  echo "   --help  Show this help message."
  echo ""
}

parse_options() {
  local count="$#"

  for i in $(seq "${count}"); do
    eval arg="\$$i"
    param="$(echo "${arg}" | awk -F '=' '{print $1}' | sed -e 's|--||')"
    val="$(echo "${arg}" | awk -F '=' '{print $2}')"

    case "${param}" in
      current_namespace)
        eval "${param}"="${val}"
        ;;
      manifest_file)
        eval "${param}"="${val}"
        ;;
      help)
        help && exit 0
        ;;
      *)
        echo "Parameter not found: '$param'"
        help && exit 1
        ;;
    esac
  done

  if [[ -z "$current_namespace" ]]; then
    echo "Must provide current_namespace" 1>&2
    help
    exit 1
  fi
  if [[ -z "$manifest_file" ]]; then
    echo "Must provide manifest_file" 1>&2
    help
    exit 1
  fi
 
}

namespace_exist() {
    echo "check if namespace ${current_namespace} exist"
    kubectl get namespace ${current_namespace}
    if [ $? != 0 ]
    then
        echo "namespace ${current_namespace} do not exist, exiting..."
        exit 13 
    fi
}

backup() {
    backup_file_name=backup_${CURRENT_TIME}.yaml
    echo "--------------------------------------------------------------"
    echo "taking backup of existing operator installation manifest files"
    echo "backup namespace"
    echo "---" >> ${backup_file_name}
    kubectl get namespace ${current_namespace} -o yaml >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup serviceaccount details"
    kubectl get serviceaccount ${current_namespace} -n splunk-operator -o yaml >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup if there are any role defined for Splunk Operator"
    kubectl get role splunk:operator:namespace-manager -n ${current_namespace} -o yaml >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup if there are any role-biding defined for Splunk Operator"
    kubectl get rolebinding splunk:operator:namespace-manager -n ${current_namespace}  -o yaml >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup if there are any cluster-role defined for Splunk Operator"
    kubectl get clusterrole splunk:operator:resource-manager -o yaml  >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup if there are any cluster-role-binding defined for Splunk Operator"
    kubectl get clusterrolebinding splunk:operator:resource-manager -o yaml >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup deployment details"
    kubectl get deployment splunk-operator -n ${current_namespace} -o yaml >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "--------------------------------------------------------------"
    echo "backup of all the previsou Splunk Operator installation is complete, backup file is found in current diretory ${backup_file_name}"
}

delete_operator() {
    echo "--------------------------------------------------------------"
    echo "deleting all the previsous Splunk Operator resources....."
    echo "deletign deployment"
    kubectl delete deployment splunk-operator -n ${current_namespace}
    echo "deleting clusterrole"
    kubectl delete clusterrole splunk:operator:resource-manager 
    echo "deleting cluster rolebinding"
    kubectl delete clusterrolebinding splunk:operator:resource-manager 
    
    echo "deleting serviceaccount"
    kubectl delete serviceaccount splunk-operator -n ${current_namespace}
    echo "deleting role"
    kubectl delete role splunk:operator:namespace-manager -n ${current_namespace}
    echo "deleting rolebinding"
    kubectl delete rolebinding splunk:operator:namespace-manager -n ${current_namespace}
    echo "--------------------------------------------------------------"
    echo "previous instance of Splunk Operator removed"
}

deploy_operator() {
    echo "--------------------------------------------------------------"
    echo "installing Splunk Operator 1.1.0....." 
    kubectl apply -f ${manifest_file}
    echo "--------------------------------------------------------------"
  echo "wait for operator pod to be ready..."
  # sleep before checking for deployment, in slow clusters deployment call may not even started
  # in those cases, kubectl will fail with error:  no matching resources found
  sleep 2
  kubectl wait --for=condition=ready pod -l control-plane=controller-manager --timeout=600s -n splunk-operator
  if [ $? -ne 0 ]; then
    echo "Operator installation not ready..."
    exit 1
  fi
  echo "deployment of new Splunk Operator 1.1.0 complete"
}

parse_options "$@"
namespace_exist
backup
delete_operator
deploy_operator



