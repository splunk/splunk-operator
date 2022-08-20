#!/bin/bash
#description  :This script migrates existing CRs deployed to non-Bias-Language replacements.
#version      :1.0

#
# Execution Modes
#
# Generate:  ./bias_lang_migration.sh generate <namespace>
# We only generate the config files for the migration, but do not apply them
# The driver code for generate is get_current_deployment() in which we retrieve the current CRs and convert them to non-bias Language counterparts
#
# Migrate:   ./bias_lang_migration.sh migrate <namespace>
# We generate the config files and apply them to the cluster in order
# The driver for migrate is apply_new_CRs() which loops over the updated folder and applies the CRs in the correct order
#
# Requirements
# This script uses JQ and requires new Managers to be deployed in the same node as the original Master CR

#############################################
# Helper Functions to setup the environment
#############################################

trap '{ echo "Script interrupted by admin" ; exit 1; }' INT

err() {
	echo -e "\n$(date +'%Y-%m-%d-%H:%M:%S%z') - ERROR - $*"
	echo -e "\n********************** Migration canceled **********************"
	unlabel_Nodes >/dev/null 2>&1
	exit 1
}

prereqs() {
	jq --version >/dev/null 2>&1
}

usage() {
	echo -e "\nUsage: $0 <generate|migrate> <namespace>"
	echo -e "generate => create migration files only. Does not apply them."
	echo -e "migrate  => create migration and apply them in the correct order."
}

backup_configs() {
	echo "Backing up current configs"
	for n in $(kubectl -n ${NS} get -o=name pvc,configmap,serviceaccount,secret,ingress,service,deployment,statefulset,hpa,job,cronjob); do
		mkdir -p ${BCK_FOLDER}/$(echo $n | cut -d "/" -f 1)
		kubectl -n ${NS} get -o=yaml $n >${BCK_FOLDER}/$n.yaml
	done
}

# Label the Node so the Manager can use nodeAffinity
label_Node() {
	MSTR_NODE=$1
	kubectl -n ${NS} label nodes ${MSTR_NODE} biasLangMasterNode=yes --overwrite >/dev/null 2>&1 # Long label(biasLangMasterNode) to avoid conflicts with possibly existing labels
	if [[ "$?" -ne 0 ]]; then
		err "Failed to label node ${MSTR_NODE}"
	fi
}

unlabel_Nodes() {
	for node in $(kubectl -n ${NS} get nodes -o json | jq ".items[].metadata.name" -r); do
		kubectl -n ${NS} label node $node biasLangMasterNode-
	done
}

create_folders() {
	if [[ -d ${FOLDER} ]]; then rm -r $FOLDER; fi
	mkdir -p $UPDATED_FOLDER
	mkdir -p $ORIGINAL_FOLDER
	mkdir -p $BCK_FOLDER
}

# Convert spec.kind from Master to Manager
convert_CR_Spec() {
	FILE_IN=$1
	FILE_OUT=$2
	DEPLMT=$3
	#	echo "Converting Spec in ${FILE_IN} to ${FILE_OUT} for ${DEPLMT}"
	eval cat ${FILE_IN} | jq ".spec.${DEPLMT}ManagerRef.name= .spec.${DEPLMT}MasterRef.name | del (.spec.${DEPLMT}MasterRef)" >${FILE_OUT}
}

# Convert metadata.kind from Master to Manager
convert_CR_Kind() {
	FILE_IN=$1
	FILE_OUT=$2
	NEW_TYPE=$3
	#	echo "Converting Kind in ${FILE_IN} to ${FILE_OUT} - TYPE=${NEW_TYPE}"
	eval cat ${FILE_IN} | jq ".kind=\"${NEW_TYPE}\"" >${FILE_OUT}
}

# Convert versions
convert_CR_version() {
	FILE_IN=$1
	CR_TYPE=$2
	TMP_FILE="${TMP_FOLDER}/${CR_TYPE}.${NS}.${CR_NAME}.${TT}.versioning.json"
	NEW_VERSION="enterprise.splunk.com/v4"
	cp ${FILE_IN} ${TMP_FILE}
	eval cat ${TMP_FILE} | jq ".apiVersion=\"${NEW_VERSION}\"" >${FILE_IN}
}

# Convert multisite ref to manager service
convert_multisite() {
	FILE_IN=$1

	if [[ "$(uname | grep -c Darwin)" -ge 1 ]]; then
		sed -i '' 's/cluster-master-service/cluster-manager-service/1' ${FILE_IN}
	else
		sed -i 's/cluster-master-service/cluster-manager-service/1' ${FILE_IN}
	fi
}

# Block execution until POD is Ready or timeout
is_pod_ready() {
	pod=$1
	timeout=$POD_TIMEOUT # Reset local copy for each restart

	echo "Waiting for Pod=${pod} to become available - timeout=${POD_TIMEOUT} secs"
	sleep 5

	while [[ $(kubectl -n ${NS} get pod ${pod} -o 'jsonpath={..status.conditions[?(@.type=="Ready")].status}') != "True" ]] && [[ "${timeout}" -gt 0 ]]; do
		sleep 1
		((timeout--))
	done

	if [[ "${timeout}" -eq 0 ]]; then
		err "Pod failed to become Ready. Use kubectl describe pod ${pod} for more details"
	fi
}

# Performs a rolling restart
# We only block execution to validate the last Pod
rolling_restart_my_pods() {
	NAME=$1
	TARGET=$2

	RESTARTED_PODS=0
	PODS=$(kubectl -n ${NS} get pods | grep ${NAME} | grep ${TARGET} | awk '{print $1}')
	N_PODS=$(echo $PODS | wc -w)

	for pod in ${PODS}; do
		kubectl -n ${NS} delete pod $pod
		sleep 10
		let RESTARTED_PODS++
		if [[ "$RESTARTED_PODS" -ge "$N_PODS" ]]; then
			is_pod_ready ${pod}
		fi

	done
}

# Verify if CR created is valid with dry-run
dry_run() {
	kubectl -n ${NS} apply -f $1 --dry-run=server >/dev/null 2>&1
	if [[ "$?" -ne 0 ]]; then
		err "Dry run failed for ${updated_name} - Please check the file for incorrectness"
	fi
}

apply_manager_CR() {
	FILE=$1

	kubectl -n ${NS} apply -f ${FILE}

	name=$(cat ${FILE} | jq '.metadata.name' -r)
	type=$(cat ${FILE} | jq '.kind' -r)

	case $type in
	ClusterManager)
		target="cluster-manager"
		;;
	LicenseManager)
		target="license-manager"
		;;
	*)
		echo -n "invalid migration request for CR type ${type}"
		;;
	esac

	# Wait for pod to become available before returning
	is_pod_ready "splunk-${name}-${target}-0"

}

apply_manager_jobs() {
	FILE=$1

	kubectl -n ${NS} apply -f ${FILE}

	name=$(cat ${FILE} | jq '.metadata.name' -r)

	for job in ${UPDATED_FOLDER}/*rsync.*${name}*; do
		job_name=$(cat ${job} | jq '.metadata.name' -r)
		timeout=$RSYNC_TIMEOUT # Local copy reset on each copy

		kubectl -n ${NS} apply -f ${job}

		#	Wait for the copy to finish before proceeding
		while [[ $(kubectl -n ${NS} get jobs ${job_name} -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}') != "True" ]] && [[ "${timeout}" -gt 0 ]]; do
			sleep 1
			((timeout--))
		done

		if [[ "${timeout}" -eq 0 ]]; then
			err "Unable to migrate data for job ${job_name} - check file in ${job}"
		fi

		# Remove completed job after
		echo "Completed job name=${job_name}"
		kubectl -n ${NS} delete job ${job_name}

	done
}

# spec.defaults are used by Ansible and are opaque to the Operator
# Updating the STS is not enough, so we need to update .conf files manually
update_defaults_to_manager() {
	NAME=$1
	PODS=$(kubectl -n ${NS} get pods | grep ${NAME} | grep "search-head" | awk '{print $1}')
	command="find /opt/splunk/etc/system/local/ -type f -exec sed -i \"s/cluster-master-service/cluster-manager-service/g\" {} +"

	for pod in ${PODS}; do
		echo "Executing command ${command} on pod=${pod}"
		kubectl -n ${NS} exec -i ${pod} -- /bin/bash -c "${command}"
		if [[ "$?" -ne 0 ]]; then
			echo "Failed to execute command ${command} on pod=${pod}"
		fi
	done
}

add_peer_to_manager() {
	NAME=$1
	TYPE=$2
	MANAGER=$3
	SITE=$4

	PODS=$(kubectl -n ${NS} get pods | grep ${NAME} | grep indexer | awk '{print $1}')
	secret=$(kubectl -n ${NS} get secret splunk-${NS}-secret -o jsonpath='{.data.password}' | base64 --decode)

	if [[ "${MULTISITE}" == "true"  ]]; then
		echo "Multisite configured site_name=${SITE}"
		command="/opt/splunk/bin/splunk edit cluster-config -mode slave -site ${SITE} -master_uri https://splunk-${MANAGER}-cluster-manager-service:8089 -replication_port 9887 -secret \$(cat /mnt/splunk-secrets/idxc_secret) -auth admin:${secret}"
	else
		echo "Single site configuration"
		command="/opt/splunk/bin/splunk edit cluster-config -mode slave -master_uri https://splunk-${MANAGER}-cluster-manager-service:8089 -replication_port 9887 -secret \$(cat /mnt/splunk-secrets/idxc_secret) -auth admin:${secret}"
	fi

	for pod in ${PODS}; do
		echo "Adding pod ${pod} to ${MANAGER}"
		kubectl -n ${NS} exec -i ${pod} -- /bin/bash -c "${command}"
		if [[ "$?" -ne 0 ]]; then
			echo "Failed to add peer to new Manager"
		fi
	done
}

add_node_affinity() {
	FILE=$1
	cp ${FILE} ${TMP_FOLDER}/temp.node.info.${NS}.${TT}
	if [[ "${MULTISITE}" == "true"  ]]; then
		cat ${TMP_FOLDER}/temp.node.info.${NS}.${TT} | jq '.spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions +=
    	        [{
                        "key": "biasLangMasterNode",
                        "operator": "In",
                        "values": [
                            "yes"
                        ]
                }]' >${FILE}
	else
		cat ${TMP_FOLDER}/temp.node.info.${NS}.${TT} | jq '.spec += {
              "affinity": {
                  "nodeAffinity": {
                        "requiredDuringSchedulingIgnoredDuringExecution": {
                              "nodeSelectorTerms": [
                        {
                    "matchExpressions": [
                      {
                        "key": "biasLangMasterNode",
                        "operator": "In",
                        "values": [
                            "yes"
                        ]
                      }
                    ]
                  }
              ]}}}}' >${FILE}
	fi
}

reset_manager_CR() {
	FILE=$1
	name=$(cat ${FILE} | jq '.metadata.name' -r)
	type=$(cat ${FILE} | jq '.kind' -r)

	case $type in

	ClusterManager)
		target="cluster-manager"
		;;

	LicenseManager)
		target="license-manager"
		;;

	*)
		echo -n "invalid reset request for CR type ${type}"
		;;
	esac

	kubectl -n ${NS} delete pod "splunk-${name}-${target}-0"
	is_pod_ready "splunk-${name}-${target}-0"
}

# Template to create Rsync Jobs from Master to Manager
create_job() {
	json='{
          "apiVersion": "batch/v1",
          "kind": "Job",
          "metadata": {
            "name": "rsync-$3-pvc-$4",
            "finalizers": [
              "foregroundDeletion"
            ]
          },
          "spec": {
            "template": {
              "spec": {
                "volumes": [
                  {
                    "name": "src-dir",
                    "persistentVolumeClaim": {
                      "claimName": "$1"
                    }
                  },
                  {
                    "name": "dest-dir",
                    "persistentVolumeClaim": {
                      "claimName": "$2"
                    }
                  }
                ],
                "containers": [
                  {
                    "name": "alpine",
                    "image": "alpine:latest",
                    "command": [
                      "sh",
                      "-c",
                      "apk add --update rsync && rsync -azPv /src-dir/ /dest-dir && ls -la /dest-dir"
                    ],
                    "volumeMounts": [
                      {
                        "mountPath": "/src-dir",
                        "name": "src-dir"
                      },
                      {
                        "mountPath": "/dest-dir",
                        "name": "dest-dir"
                      }
                    ],
                    "resources": {
                      "limits": {
                        "cpu": "4000m",
                        "memory": "4Gi"
                      },
                      "requests": {
                        "cpu": "500m",
                        "memory": "500Mi"
                      }
                    }
                  }
                ],
                "restartPolicy": "Never"
              }
            },
            "backoffLimit": 1
          }
        }'

	echo $json | jq ".spec.template.spec.volumes[0].persistentVolumeClaim.claimName=\"${1}\" | .spec.template.spec.volumes[1].persistentVolumeClaim.claimName=\"${2}\" | .metadata.name=\"rsync-${3}-${4}\" " >${UPDATED_FOLDER}/rsync.$1.to.$2

	dry_run ${UPDATED_FOLDER}/rsync.$1.to.$2

}

migrate_pvc() {
	NAME=$1

	SRC=$2 # SRC = Master CR
	SRC_PVC_ETC="pvc-etc-splunk-${NAME}-${SRC}-0"
	SRC_PVC_VAR="pvc-var-splunk-${NAME}-${SRC}-0"

	DEST=$3 # DEST = Manager CR
	DEST_PVC_ETC="pvc-etc-splunk-${NAME}-${DEST}-0"
	DEST_PVC_VAR="pvc-var-splunk-${NAME}-${DEST}-0"

	create_job ${SRC_PVC_ETC} ${DEST_PVC_ETC} etc ${NAME}
	create_job ${SRC_PVC_VAR} ${DEST_PVC_VAR} var ${NAME}
}

# For Indexer Cluster is recommended to enable Maintenance during CM migration
setMaintenanceMode() {
	pod=$1
	enable=$2

	secret=$(kubectl -n ${NS} get secret splunk-${NS}-secret -o jsonpath='{.data.password}' | base64 --decode)
	command="/opt/splunk/bin/splunk ${enable} maintenance-mode --answer-yes -auth admin:${secret}"
	kubectl -n ${NS} exec -i ${pod} -- /bin/bash -c "${command}"

	if [[ "$?" -ne 0 ]] && [[ "${enable}" != "disable" ]]; then
		err "Failed to set maintenance mode for ${pod}"
	fi
}

to_delete_CRs() {
	# Only create this folder if script reach the end of execution
	mkdir -p ${TO_REMOVE_CRs}
	if [[ ${HAS_LM} ]]; then
		for file in ${ORIGINAL_FOLDER}/*LicenseMaster.original*; do
			if [[ -f ${file} ]]; then
				cp ${file} ${TO_REMOVE_CRs}/
			fi
		done
	fi
	if [[ ${HAS_CM} ]]; then
		for file in ${ORIGINAL_FOLDER}/*ClusterMaster.original*; do
			if [[ -f ${file} ]]; then
				cp ${file} ${TO_REMOVE_CRs}/
			fi
		done
	fi
}

#############################################
# Driver Functions
#############################################

# Driver for Generate Mode
get_current_deployment() {
	echo -e "\nRetrieving current deployed Splunk Instances: \n"

	# Retrieve all CRs currently deployed
	for CR in "${CRDs[@]}"; do
		ALL_CR_NAMES=$(kubectl -n ${NS} get ${CR} -o json | jq ".items[].metadata.name" -r)
		for CR_NAME in $ALL_CR_NAMES; do

			original_name="${ORIGINAL_FOLDER}/${CR}.original.${CR_NAME}.json" # original -> As currently deployed
			updated_name="${UPDATED_FOLDER}/${CR}.updated.${CR_NAME}.json"    # updated  -> Already converted to new CRDs
			tmp_name="${TMP_FOLDER}/${CR}.tmp.${NS}.${CR_NAME}.${TT}.json"
			lm_cm_ex_name="${TMP_FOLDER}/${CR}.lm_cm_exception.${CR_NAME}.${TT}.json"

			# Retrieve current CR and save in original folder
			if [[ ! -z ${CR_NAME} ]]; then
				echo "Found CR=${CR} name=${CR_NAME}"
				kubectl -n ${NS} get ${CR} ${CR_NAME} -o json | jq "
                    with_entries(
                        select([.key] |
                          inside([\"metadata\", \"spec\", \"apiVersion\", \"kind\"])
                             )
                         )
                    | del(
                        .metadata.annotations, .metadata.creationTimestamp, .metadata.finalizers, .metadata.resourceVersion, .metadata.selfLink, .metadata.uid
                      )
                    " >${original_name}
			else
				echo "Did not find CR=${CR} deployed"
				continue
			fi

			# Handles Refs conversion
			if [[ "$(grep -c licenseMasterRef ${original_name})" -ne "0" ]] && [[ "$(grep -c clusterMasterRef ${original_name})" -ne "0" ]]; then
				convert_CR_Spec ${original_name} ${tmp_name} "license"
				convert_CR_Spec ${tmp_name} ${updated_name} "cluster"
			elif [[ "$(grep -c licenseMasterRef ${original_name})" -ne "0" ]]; then
				if [[ "${CR}" == "ClusterMaster" ]]; then
					convert_CR_Spec ${original_name} ${lm_cm_ex_name} "license"
				else
					convert_CR_Spec ${original_name} ${updated_name} "license"
				fi
			elif [[ "$(grep -c clusterMasterRef ${original_name})" -ne "0" ]]; then
				convert_CR_Spec ${original_name} ${updated_name} "cluster"
			fi

			# Handles Multisite References
			if [[ "$(grep -c multisite_master ${original_name})" -ne "0" ]]; then
				export MULTISITE=true
				if [[ "${CR}" != "LicenseMaster" ]] && [[ "${CR}" != "ClusterMaster" ]]; then
					convert_multisite ${updated_name}
				fi
			fi

			# Handles Kind conversion
			if [[ "${CR}" == "LicenseMaster" ]]; then
				export HAS_LM=true
				convert_CR_Kind ${original_name} ${updated_name} "LicenseManager"
				migrate_pvc ${CR_NAME} "license-master" "license-manager"
				add_node_affinity ${updated_name}
			elif [[ "${CR}" == "ClusterMaster" ]]; then
				export HAS_CM=true
				if [[ -f "${lm_cm_ex_name}" ]]; then # Exception where CM has a LM reference
					convert_CR_Kind ${lm_cm_ex_name} ${updated_name} "ClusterManager"
				else
					convert_CR_Kind ${original_name} ${updated_name} "ClusterManager"
				fi
				migrate_pvc ${CR_NAME} "cluster-master" "cluster-manager"
				add_node_affinity ${updated_name}
			fi

      # Updates CR to use new version (v4)
      convert_CR_version ${updated_name} ${CR}

			# Validate the updated CR is valid
			dry_run ${updated_name}

		done
	done

	# Warning for incorrect namespace
	if [[ $(ls ${ORIGINAL_FOLDER} | wc -l) -eq 0 ]]; then
		echo "WARNING - No CRs found, check if the namespace used is correct"
	fi

}

# Driver for Migration Mode
apply_new_CRs() {
	echo -e "\nApplying new CRs generated:\n"

	# Update LM first
	if [[ ${HAS_LM} ]]; then
		for file in ${UPDATED_FOLDER}/*LicenseMaster.updated*; do
			if [[ -f ${file} ]]; then
				echo ${file}
				CR_NAME="$(cat ${file} | jq '.metadata.name' -r)"
				node=$(kubectl -n ${NS} get pod splunk-${CR_NAME}-license-master-0 -o json | jq '.spec.nodeName' -r)
				label_Node ${node}
				apply_manager_CR ${file}
				apply_manager_jobs ${file}
				reset_manager_CR ${file}      # Bring configs copied from original into memory
				unlabel_Nodes >/dev/null 2>&1 # Needed when LM and CM are in different nodes
				echo "Finished LM migration using file ${file}"
			fi
		done
	fi

	# Update CM second
	if [[ ${HAS_CM} ]]; then
		for file in ${UPDATED_FOLDER}/*ClusterMaster.updated*; do
			if [[ -f ${file} ]]; then
				CR_NAME="$(cat ${file} | jq '.metadata.name' -r)"
				node=$(kubectl -n ${NS} get pod splunk-${CR_NAME}-cluster-master-0 -o json | jq '.spec.nodeName' -r)
				label_Node ${node}
				setMaintenanceMode "splunk-${CR_NAME}-cluster-master-0" "enable"
				to_disable_maint_mode=("${arr[@]}" "splunk-${CR_NAME}-cluster-manager-0")
				apply_manager_CR ${file}
				apply_manager_jobs ${file}
				reset_manager_CR ${file} # Bring configs copied from original CM into memory
				echo "Finished CM migration using file ${file}"
			fi
		done
	fi

	# Update SHs third
	for file in ${UPDATED_FOLDER}/*; do
		if [[ -f ${file} ]]; then
			if [[ "$(echo "${file}" | grep -c "SearchHeadCluster.updated")" != "0" ]]; then
				name=$(cat ${file} | jq '.metadata.name' -r)
				target="search-head"
				# Exception for spec.defaults used by ansible
				if [[ "$(cat ${file} | jq '.spec.defaults')" != "" ]]; then
					update_defaults_to_manager ${name}
				fi
				kubectl -n ${NS} apply -f ${file}
				rolling_restart_my_pods ${name} ${target}
				echo "Finished SHC migration using file ${file}"
			fi
		fi
	done

	# Update all others
	for file in ${UPDATED_FOLDER}/*; do
		if [[ -f ${file} ]]; then
			if [[ "$(echo "${file}" | grep -c "ClusterMaster.updated")" == "0" ]] &&
				[[ "$(echo "${file}" | grep -c "LicenseMaster.updated")" == "0" ]] &&
				[[ "$(echo "${file}" | grep -c "SearchHeadCluster.updated")" == "0" ]] &&
				[[ "$(echo "${file}" | grep -c "rsync.pvc")" == "0" ]]; then
				echo "Configuring Peers - This can take a while"
				kubectl -n ${NS} apply -f ${file}
				if [[ "$(cat ${file} | jq '.kind' -r)" == "IndexerCluster" ]]; then
					name=$(cat ${file} | jq '.metadata.name' -r)
					manager=$(cat ${file} | jq '.spec.clusterManagerRef.name' -r)
					site=$(cat ${file} | jq '.spec.defaults' -r | grep site | awk -F "site:" '{print $2}')
					add_peer_to_manager ${name} "indexer" ${manager} ${site}
				fi
			fi
		fi
	done

	# Disable maintenance mode in all CMs migrated
	for CM in "${to_disable_maint_mode[@]}"; do
		echo "Going to Disable maintenance mode for {CM}"
		setMaintenanceMode "${CM}" "disable"
	done

}

#############################################
#  Begin Script Execution
#############################################

# Script requires jq to be installed
prereqs
if [[ "$?" -ne 0 ]]; then
	err "This script requires jq to be installed in the current machine. Visit https://stedolan.github.io/jq/download for details."
fi

# Validate arguments for CR and Namespace
if [[ -z "$1" ]]; then
	usage
	err "Please choose a migration option."
fi

if [[ "$1" != "migrate" ]] && [[ "$1" != "generate" ]] && [[ "$1" != "test" ]]; then
	usage
	err "$1 is an invalid migration option."
fi

if [[ -z "$2" ]]; then
	usage
	err "Please enter the namespace of the deployment to be migrated."
else
	export NS="$2"
	kubectl -n ${NS} config set-context --current --namespace=${NS} >/dev/null 2>&1
fi

if [[ "$1" != "test" ]]; then
  # User warning for optional backup of PVs
  echo -e "\n\n************************** Important Notice **************************\n"
  echo -e "1 - This script should be used during Maintenance hours because it requires pod restarts."
  echo -e "2 - Interrupting the execution of this script can leave your deployment in a bad state."
  echo -e "3 - Large deployments should review timeout variables for POD restarts and Rsync."
  echo -e "\n Do you wish to proceed with the migration for mode=$1 NS=${NS}?"
  echo -e "\n Press Enter to continue or Crtl+C to cancel"
  read
else
  echo "Executing script in test mode - NS=${NS}"
fi

#############################################
# Configurable Global Variables
#############################################

export FOLDER=./${NS}.CR_migrations
export ORIGINAL_FOLDER=${FOLDER}/original
export UPDATED_FOLDER=${FOLDER}/updated
export TO_REMOVE_CRs=${FOLDER}/to_remove_CRs
export BCK_FOLDER=${FOLDER}/backup
export TMP_FOLDER=/tmp
export POD_TIMEOUT=600    # 10 minutes
export RSYNC_TIMEOUT=3600 # 1 hour
export TT=$(date +'%Y.%m.%d.%H%M%S')
export CRDs=("LicenseMaster" "ClusterMaster" "IndexerCluster" "SearchHeadCluster" "Standalone" "MonitoringConsole")
export MULTISITE=false

# Start logging file
echo -e "\n*** Starting Execution ***\n\n$(date "+%F-%H:%M:%S") - Current namespace=${NS}"

# cleans biasLang labels from previous runs
unlabel_Nodes >/dev/null 2>&1

# Reset require folders
create_folders

# Retrieve all current deployment
get_current_deployment

# Apply generated CRs on Migrate mode
if [[ "$1" == "migrate" ]]; then
	backup_configs
	apply_new_CRs
	to_delete_CRs
fi

# Automated tests with Kuttl
if [[ "$1" == "test" ]]; then
	apply_new_CRs
fi

# clean up labels after execution
unlabel_Nodes >/dev/null 2>&1

echo -e "\nExecution Completed. Once you validate your environment you can remove ClusterMasters and LicenseMasters stored in ${TO_REMOVE_CRs}"
echo -e "\n*** Migration script finished execution ***\n"
