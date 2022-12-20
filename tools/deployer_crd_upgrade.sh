#!/bin/bash
#description  :This script converts existing Search Head Cluster CRs to deployer CR
#              and search head cluster CR due to the introduction of the deployer CRD
#
# Usage:   ./deployer_crd_upgrade.sh upgrade <shc_yaml_file> <namespace_of_the_SHC_CR> <target_folder_for_yamls>
# We do the following actions to facilitate the upgrade:
# 1. Generate the modified yamls for the SHC and deployer CRs in the target folder
# 2. Deploy the new deployer CR
# 3. Deploy the new SHC, if modified
#
# Requirements
# This script uses JQ

#############################################
# Helper Functions to setup the environment
#############################################

trap '{ echo "Script interrupted by admin" ; exit 1; }' INT

err() {
	echo -e "\n$(date +'%Y-%m-%d-%H:%M:%S%z') - ERROR - $*"
	echo -e "\n********************** Update canceled **********************"
	exit 1
}

prereqs() {
	jq --version >/dev/null 2>&1
}

usage() {
	echo -e "\nUsage: $0 <full_path_of_shc_yaml> <namespace> <full_path_to_target_folder_for_new_yamls>"
	echo -e "Facilitates the entire update including the following actions"
    echo -e "# 1. Generate the modified yamls for the SHC and deployer CRs in the target folder"
    echo -e "# 2. Deploy the new deployer CR"
    echo -e "# 3. Deploy the new SHC, if modified"
    echo -e "Please make sure to pass all the required arguments"
}

# Validate script parameters
validate_script_parameters() {
    prereqs
    if [[ "$?" -ne 0 ]]; then
	    err "This script requires jq to be installed in the current machine. Visit https://stedolan.github.io/jq/download for details."
    fi

    # Validate arguments to the script
    if [[ -z "$1" ]] || [[ -z "$2" ]] || [[ -z "$3" ]]; then
	    usage
	    err "Please provide all necessary arguments for the script run"
    fi

    # Check if the SHC yaml file exists
    if [ ! -f "$1" ]; then
        err "SHC yaml $1 doesn't exist. Please provide valid SHC yaml file"
    fi

    # Check if ns exists
    kubectl get ns $2 | grep -i "Error from server (NotFound):" &> /dev/null
    if [ $? == 0 ]; then
       err "namespace $2 doesn't exist, please provide a valid namespace"
    fi

    # Check if the target folder exists
    if [ ! -f "$3" ]; then
        err "Target folder $3 doesn't exist. Please provide valid target folder"
    fi
}

# Setup target folder
setup_target_folder() {
    # Copy old shc yaml file to target folder
    cp ${SHC_YAML_FILE} ${SHC_YAML_FILE_OLD_COPIED}
}

# Convert metadata.kind from Master to Manager
convert_CR_Kind() {
	FILE_IN=$1
	FILE_OUT=$2
	NEW_TYPE=$3
	#	echo "Converting Kind in ${FILE_IN} to ${FILE_OUT} - TYPE=${NEW_TYPE}"
	eval cat ${FILE_IN} | jq ".kind=\"${NEW_TYPE}\"" >${FILE_OUT}
}

# Generate deployer yaml
generate_deployer_yaml() {
	NEW_VERSION="enterprise.splunk.com/v4"
    eval cat ${SHC_YAML_FILE_OLD_COPIED} | jq ".kind=\"${CR_KIND_DEPLOYER}\"" | | jq ".apiVersion=\"${NEW_VERSION}\"" >${DEPLOYER_YAML_FILE}
}


#############################################
#  Validate script parameters
#############################################

validate_script_parameters()

#############################################
#  Update global variables
#############################################

# Script parameters
export SHC_YAML_FILE = $1
export NS = $2
export TARGET_FOLDER = $3

# CR kinds
export CR_KIND_DEPLOYER = "Deployer"
export CR_KIND_SHC = "SearchHeadCluster"

# SHC, deployer yaml files in target folder
export SHC_YAML_FILE_OLD_COPIED = ${TARGET_FOLDER}/shc_yaml_old_copied.yaml
export SHC_YAML_FILE_MODIFIED = ${TARGET_FOLDER}/shc_yaml_modified.yaml
export DEPLOYER_YAML_FILE = ${TARGET_FOLDER}/deployer.yaml

#############################################
#  Begin Script Execution
#############################################

# Setup target folder
setup_target_folder()

# Generate deployer yaml
generate_deployer_yaml()

# Modify the SHC yaml if required
modify_shc_yaml()

# 





