#!/bin/bash
#description  :This script migrates existing CRs deployed to non-Bias-Language replacements.
#version      :1.0

# Execution Flow
# 1) Retrieve current deployments and saves configs in the "original" folder.
# 2) Convert the configs to use new CRs and creates jobs to Rsync LM and CM. Saves them in the "updated" folder.
# 3) Apply all the new configs in the correct order to migrate the deployment.

# Important Notes
# ==> Requires pod restarts so it should be used during Maintenance window.
# ==> Does not touch PVCs directly, should not cause any data loss.
# ==> Does not delete/remove Statefulsets

#############################################
# Configurable Global Variables
#############################################

export FOLDER=./CR_migrations
export ORIGINAL_FOLDER=${FOLDER}/original
export UPDATED_FOLDER=${FOLDER}/updated
export BCK_FOLDER=${FOLDER}/backup
export TMP_FOLDER=/tmp

export POD_TIMEOUT=600    # 10 minutes
export RSYNC_TIMEOUT=3600 # 1 hour

export CRDs=("LicenseMaster" "ClusterMaster" "IndexerCluster" "SearchHeadCluster" "Standalone" "MonitoringConsole")

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
	for n in $(kubectl get -o=name pvc,configmap,serviceaccount,secret,ingress,service,deployment,statefulset,hpa,job,cronjob); do
		kubectl get -o=yaml $n >${BCK_FOLDER}/$n.yaml
	done
}

# Label the Node so the Manager can use nodeAffinity
label_Node() {
	MSTR_NODE=$1
	kubectl label nodes ${MSTR_NODE} biaslangmasternode=yes >/dev/null 2>&1 # Long label(biaslangmasternode) to avoid conflicts with possibly existing labels
	if [[ "$?" -ne 0 ]]; then
		err "Failed to label node ${MSTR_NODE}"
	fi
}

unlabel_Nodes() {
	for node in $(kubectl get nodes -o json | jq ".items[].metadata.name" -r); do
		kubectl label node $node biaslangmasternode-
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

# Block execution until POD is Ready or timeout
is_pod_ready() {
	pod=$1
	timeout=$POD_TIMEOUT # Reset local copy for each restart

	echo "Waiting for Pod=${pod} to become available - timeout=${POD_TIMEOUT}"

	while [[ $(kubectl get pod ${pod} -o 'jsonpath={..status.conditions[?(@.type=="Ready")].status}') != "True" ]] && [[ "${timeout}" -gt 0 ]]; do
		sleep 1
		let timeout--
	done

	if [[ "${timeout}" -eq 0 ]]; then
		err "Pod failed to become Ready. Use kubectl describe pod ${pod} for more details"
	fi
}

# Verify if CR created is valid with dry-run
dry_run() {
	kubectl apply -f $1 --dry-run=server >/dev/null 2>&1
	if [[ "$?" -ne 0 ]]; then
		err "Dry run failed for ${updated_name} - Please check the file for incorrectness"
	fi
}

apply_manager_CR() {
	FILE=$1

	kubectl apply -f ${FILE}

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

	kubectl apply -f ${FILE}

	name=$(cat ${FILE} | jq '.metadata.name' -r)

	for job in ${UPDATED_FOLDER}/*rsync.*${name}*; do
		job_name=$(cat ${job} | jq '.metadata.name' -r)
		timeout=$RSYNC_TIMEOUT # Local copy reset on each copy

		echo "Will apply file=${job} name=${job_name}"
		kubectl apply -f ${job}

		#	Wait for copy to finish
		while [[ $(kubectl get jobs ${job_name} -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}') != "True" ]] && [[ "${timeout}" -gt 0 ]]; do
			sleep 1
			let timeout--
		done

		if [[ "${timeout}" -eq 0 ]]; then
			err "Unable to migrate data for job ${job_name} - check file in ${job}"
		fi

		# Remove completed job after
		kubectl delete job ${job_name}

	done
}

# Performs a rolling restart
# This is needed for Peer Pods to sync up with updated STS configs
# We only block execution to validate the first and last Pods
rolling_restart_my_pods() {
	NAME=$1
	TYPE=$2

	case $TYPE in
	IndexerCluster)
		target="indexer"
		;;
	SearchHeadCluster)
		target="search-head"
		;;
	Standalone)
		target="standalone"
		;;
	*)
		echo -n "invalid restart request for CR type ${type}"
		;;
	esac

	RESTARTED_PODS=0
	PODS=$(kubectl get pods | grep ${NAME} | grep ${target} | awk '{print $1}')
	N_PODS=$(echo $PODS | wc -w)

	for pod in ${PODS}; do
		let RESTARTED_PODS++

		echo "Restarting pod ${pod}"
		kubectl delete pod $pod
		sleep 10

		# We hold execution for first and last pod to ensure they were successfully restarted
		if [[ "$RESTARTED_PODS" -eq "1" ]]; then
			is_pod_ready ${pod}
		elif [[ "$RESTARTED_PODS" -ge "$N_PODS" ]]; then
			is_pod_ready ${pod}
		fi
	done
}

add_node_affinity() {
	FILE=$1
	cp ${FILE} ${TMP_FOLDER}/temp.node.info
	cat ${TMP_FOLDER}/temp.node.info | jq '.spec += {
          "affinity": {
              "nodeAffinity": {
                    "requiredDuringSchedulingIgnoredDuringExecution": {
                          "nodeSelectorTerms": [
                    {
                "matchExpressions": [
                  {
                    "key": "biaslangmasternode",
                    "operator": "In",
                    "values": [
                        "yes"
                    ]
                  }
                ]
              }
          ]}}}}' >${FILE}
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

	kubectl delete pod "splunk-${name}-${target}-0"
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

# Copies PVCs for /etc and /var mounts
# SRC  = Master CR
# DEST = Manager CR
migrate_pvc() {
	NAME=$1

	SRC=$2
	SRC_PVC_ETC="pvc-etc-splunk-${NAME}-${SRC}-0"
	SRC_PVC_VAR="pvc-var-splunk-${NAME}-${SRC}-0"

	DEST=$3
	DEST_PVC_ETC="pvc-etc-splunk-${NAME}-${DEST}-0"
	DEST_PVC_VAR="pvc-var-splunk-${NAME}-${DEST}-0"

	create_job ${SRC_PVC_ETC} ${DEST_PVC_ETC} etc ${NAME}
	create_job ${SRC_PVC_VAR} ${DEST_PVC_VAR} var ${NAME}
}

# For Indexer Cluster is recommended to enable Maintenance due to CM migration
setMaintenanceMode() {
	pod=$1
	enable=$2

	secret=$(kubectl get secret splunk-default-secret -o jsonpath='{.data.password}' | base64 --decode)
	command="/opt/splunk/bin/splunk ${enable} maintenance-mode --answer-yes -auth admin:${secret}"
	kubectl exec -it ${pod} -- /bin/bash -c "${command}"

	if [[ "$?" -ne 0 ]] && [[ "${enable}" != "disable" ]]; then
		err "Failed to set maintenance mode for ${pod}"
	fi
}

get_current_deployment() {

	echo -e "\nRetrieving current deployed Splunk Instances: \n"

	# Retrieve all CRs currently deployed
	for CR in "${CRDs[@]}"; do
		ALL_CR_NAMES=$(kubectl get ${CR} -o json | jq ".items[].metadata.name" -r)
		for CR_NAME in $ALL_CR_NAMES; do

			original_name="${ORIGINAL_FOLDER}/${CR}.original.${CR_NAME}.json"
			updated_name="${UPDATED_FOLDER}/${CR}.updated.${CR_NAME}.json"
			tmp_name="${TMP_FOLDER}/${CR}.tmp.${CR_NAME}.json"
			lm_cm_ex_name="${TMP_FOLDER}/${CR}.lm_cm_exception.${CR_NAME}.json"

			# Retrieve current CR and save in original folder
			if [[ ! -z ${CR_NAME} ]]; then
				echo "Found CR=${CR} name=${CR_NAME}"
				kubectl get ${CR} ${CR_NAME} -o json | jq "
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

			# Converts Refs if exists
			if [[ "$(grep -c licenseMasterRef ${original_name})" -ne "0" ]] &&
				[[ "$(grep -c clusterMasterRef ${original_name})" -ne "0" ]]; then
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

			# Converts Kind to Manager and add labels to the Node
			# The new Manager needs to be in the same Node for RSYNC
			if [[ "${CR}" == "LicenseMaster" ]]; then
				export HAS_LM=true

				convert_CR_Kind ${original_name} ${updated_name} "LicenseManager"
				migrate_pvc ${CR_NAME} "license-master" "license-manager"
				add_node_affinity ${updated_name}

			elif [[ "${CR}" == "ClusterMaster" ]]; then
				export HAS_CM=true
				node=$(kubectl get pod splunk-${CR_NAME}-cluster-master-0 -o json | jq '.spec.nodeName' -r)
				label_Node ${node}

				# Exception where CM has a LM reference
				if [[ -f "${lm_cm_ex_name}" ]]; then
					convert_CR_Kind ${lm_cm_ex_name} ${updated_name} "ClusterManager"
				else
					convert_CR_Kind ${original_name} ${updated_name} "ClusterManager"
				fi

				migrate_pvc ${CR_NAME} "cluster-master" "cluster-manager"
				add_node_affinity ${updated_name}
			fi

			# Validate the CR is valid
			dry_run ${updated_name}

		done
	done

	if [[ $(ls ${ORIGINAL_FOLDER} | wc -l) -eq 0 ]]; then
		echo "WARNING - No CRs found, check if the namespace used is correct"
	fi

}

apply_new_CRs() {
	echo -e "\nApplying new CRs generated:\n"
	# Apply new Updated CRs

	HAS_LM=true
	HAS_CM=true

	# Get LM first
	if [[ ${HAS_LM} ]]; then
		for file in ${UPDATED_FOLDER}/*LicenseMaster.updated*; do
			if [[ -f ${file} ]]; then
				echo ${file}
				CR_NAME="$(cat ${file} | jq '.metadata.name' -r)"
				node=$(kubectl get pod splunk-${CR_NAME}-license-master-0 -o json | jq '.spec.nodeName' -r)
				label_Node ${node}
				apply_manager_CR ${file}
				apply_manager_jobs ${file}
				#				                reset_my_CR ${file}
				unlabel_Nodes >/dev/null 2>&1 # Needed when LM and CM are in different nodes
				echo "Finished LM migration using file ${file}"
			fi
		done
	fi

	# Get CM second
	if [[ ${HAS_CM} ]]; then
		for file in ${UPDATED_FOLDER}/*ClusterMaster.updated*; do
			if [[ -f ${file} ]]; then
				CR_NAME="$(cat ${file} | jq '.metadata.name' -r)"
				node=$(kubectl get pod splunk-${CR_NAME}-cluster-master-0 -o json | jq '.spec.nodeName' -r)
				label_Node ${node}
				setMaintenanceMode "splunk-${CR_NAME}-cluster-master-0" "enable"
				disable_maintenance_mode_arr=("${arr[@]}" "splunk-${CR_NAME}-cluster-manager-0")
				echo " each ${disable_maintenance_mode_arr}"
				apply_manager_CR ${file}
				apply_manager_jobs ${file}
				reset_manager_CR ${file}
				echo "Finished CM migration using file ${file}"
			fi
		done
	fi

	# Update Peers
	for file in ${UPDATED_FOLDER}/*; do
		if [[ -f ${file} ]]; then
			if [[ "$(echo "${file}" | grep -c "ClusterMaster.updated")" == "0" ]] &&
				[[ "$(echo "${file}" | grep -c "LicenseMaster.updated")" == "0" ]] &&
				[[ "$(echo "${file}" | grep -c "rsync.pvc")" == "0" ]]; then
				echo "Restarting Peers - This can take a while"
				kubectl apply -f ${file}
				name=$(cat ${file} | jq '.metadata.name' -r)
				type=$(cat ${file} | jq '.kind' -r)
				rolling_restart_my_pods ${name} ${type}
			fi
		fi
	done

	# Disable maintenance mode in all CMs migrated
	for CM in "${disable_maintenance_mode_arr[@]}"; do
		echo "Going to Disable ${CM}"
		setMaintenanceMode "${CM}" "disable"
	done

}

#############################################
#  Begin Script Execution
#############################################

# Script requires jq to be installed
prereqs
if [[ "$?" -ne 0 ]]; then
	err "This script requires jq to be installed in the currenct machine. Visit https://stedolan.github.io/jq/download for details."
fi

# Validate arguments for CR and Namespace
if [[ -z "$1" ]]; then
	usage
	err "Please choose a migration option."
fi

if [[ "$1" != "migrate" ]] && [[ "$1" != "generate" ]]; then
	usage
	err "$1 is an invalid migration option."
fi

if [[ -z "$2" ]]; then
	usage
	err "Please enter the namespace of the deployment to be migrated."
else
	kubectl config set-context --current --namespace=$2 >/dev/null 2>&1
fi

# User warning for optional backup of PVs
echo -e "\n\n************************** Important Notice **************************\n"
echo -e "1 - This script should be used during Maintenance hours because it requires pod restarts."
echo -e "2 - Interrupting the execution of this script can leave your deployment at a bad state."
echo -e "3 - Large deployments should review timeout variables at the top of the script."
echo -e "\n Do you wish to proceed with the migration for option=$1 NS=$2?"
echo -e "\n Press Enter to continue or Crtl+C to cancel"
read

# Start logging file
echo -e "\n*** Starting Execution ***\n\n$(date "+%F-%H:%M:%S")- Current namespace=$2"

# cleans biasLang labels from previous runs
unlabel_Nodes >/dev/null 2>&1

# Reset require folders
create_folders

# Retrieve all current deployment
get_current_deployment

if [[ "$1" == "migrate" ]]; then
	apply_new_CRs
fi

# clean up labels after execution
unlabel_Nodes >/dev/null 2>&1

echo -e "\nOriginal files saved in ${ORIGINAL_FOLDER}"
echo -e "Generated files saved in ${UPDATED_FOLDER}"
echo -e "\n*** Migration script finished execution ***\n"
