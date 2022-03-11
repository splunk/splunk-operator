#!/usr/bin/env bash

#
#Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
#
#Licensed under the Apache License, Version 2.0 (the "License");
#you may not use this file except in compliance with the License.
#You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
#Unless required by applicable law or agreed to in writing, software
#distributed under the License is distributed on an "AS IS" BASIS,
#WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#See the License for the specific language governing permissions and
#limitations under the License.

# This is an upgrade script for splunk operator 
# Version 1.1.0 is a new installation rather than upgarde for current operator 
# Due to this user should cleanup the older script and install version 1.1.0
# The script will help customer in doing these steps
# This script ask current namespace where operator is installed , and 
# * it first takes backup of all the operator resources within the namespace
# * like serviceaccount, deployment, role, rolebinding, clusterrole, clusterrolebinding 
# * it then deletes all the resources and installs the opeartor in splunk-operator namespace
# * by default splunk-opeartor 1.1.0 will be installed to watch clusterwide, 

readonly CURRENT_TIME=$(date +%Y-%m-%d_%H%M-%Z)
readonly PROGRAM_NAME=$(basename "$0")

help() {
  echo ""
  echo "USAGE: ${PROGRAM_NAME} --help [ --current_namespace=<namespacename> ] [ --manifest_file=<fulepath with filename of splunk operator 1.1.0 manfiests file>] "
  echo ""
  echo "OPTIONS:"
  echo ""
  echo -e "   --current_namespace specifiy the current namespace where operator is installed, \n" \
       "                          script will delete existing serviceaccount, deployment, role and " \
       "                          rolebinding and install the operator in splunk-operator namespace"
  echo ""
  echo "   --manifest_file splunk operator 1.1.0 manifest file path, this can be url link or full path of the file"
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
    echo "backup if there are any role defined for splunk operator"
    kubectl get role splunk:operator:namespace-manager -n ${current_namespace} -o yaml >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup if there are any role-biding defined for splunk operator"
    kubectl get rolebinding splunk:operator:namespace-manager -n ${current_namespace}  -o yaml >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup if there are any cluster-role defined for splunk operator"
    kubectl get clusterrole splunk:operator:resource-manager -o yaml  >> ${backup_file_name}
    if [ $? == 0 ] 
    then 
        echo "" >> ${backup_file_name}
        echo "---" >> ${backup_file_name}
    fi
    echo "backup if there are any cluster-role-binding defined for splunk operator"
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
    echo "backup of all the previsou splunk opeartor installation is complete, backup file is found in current diretory ${backup_file_name}"
}

delete_operator() {
    echo "--------------------------------------------------------------"
    echo "deleting all the previsous splunk operator resources....."
    echo "deleting clusterrole"
    kubectl delete clusterrole splunk:operator:resource-manager 
    echo "deleting cluster rolebinding"
    kubectl delete clusterrolebinding splunk:operator:resource-manager 
    echo "deletign deployment"
    kubectl delete deployment splunk-operator -n ${current_namespace}
    echo "deleting serviceaccount"
    kubectl delete serviceaccount splunk-operator -n ${current_namespace}
    echo "deleting role"
    kubectl delete role splunk:operator:namespace-manager -n ${current_namespace}
    echo "deleting rolebinding"
    kubectl delete rolebinding splunk:operator:namespace-manager -n ${current_namespace}
    echo "--------------------------------------------------------------"
    echo "previous instance of splunk operator removed"
}

deploy_operator() {
    echo "--------------------------------------------------------------"
    echo "installing splunk operator 1.1.0....." 
    kubectl apply -f ${manifest_file}
    echo "--------------------------------------------------------------"
    echo "deployment new splunk opearator 1.1.0 complete"
}

parse_options "$@"
backup
delete_operator
deploy_operator
