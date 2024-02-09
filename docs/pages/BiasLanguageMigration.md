---
title: FAQ - Biased Language Migration
nav_order: 100
#nav_exclude: true
---

# FAQ - Biased Language Migration


## Motivation

Institutionalized bias has no place in our products, our documentation, our language or our actions. As technology leaders, we must be aware of these issues and fix them.
Learn more about our company-wide efforts [Annoucements](https://www.splunk.com/en_us/blog/leadership/biased-language-has-no-place-in-tech.html) and familiarize yourself with our
[Bias language guide](https://docs.splunk.com/Documentation/StyleGuide/latest/StyleGuide/Inclusivity) documentation.

## What's happening?

Starting in the Operator version 2.1.0 we are introducing new CRDs for ClusterManager and LicenseManager. These CRDs have equal capabilities and configurations as their predecessors ClusterMaster, and LicenseMaster. Existing deployments are not affected, but encouraged to plan a migration to the new CRDs in the future. 

## Which CRD should I choose for a new deployment?

All new deployments are recommended to use the new ClusterManager and LicenseManager CRDs. The examples in our documentation have been updated to use the new CRDs. Please check [Configuring Splunk Enterprise Deployments](https://github.com/splunk/splunk-operator/blob/master/docs/Examples.md) for details.

## How to plan for a migration?

We are providing a script to assist you in migrating your deployment. This script should be used during a maintenance window because Search and Indexing Services might become unavailable for a period of time.
No data loss is expected but it is recommended you ensure your backup policy is up-to-date prior to running this script. 

**There are two options to perform the migration:**
1) You can use our script to generate all yaml files needed. Then you apply them yourself while monitoring / validating your environment at each step.
2) The script will generate the yaml files, and apply them automatically to migrate your deployment. You can validate the environment at the end.

**Our suggestion:** Run the script with `generate` and check the files created in the `udpated` folder. After the validation, run the script with the `migrate` option.

## How to remove old CRs after migration?

The script will not delete the older CRs even after the deployment is migrated. This allows you to validate and verify the environment once again before fully removing the CRs. 
The script will save the CRs that can be removed in `to_remove_CRs`, you can remove them manually using `kubectl delete -f to_remove_CRs/<CR-Name>` after validation.

## Execution Modes

### Generate Mode

Run the script in `tools/bias_lang_migration.sh` passing `generate` as an argument as well as the `namespace` of the current deployment.

Example:
```
./tools/bias_lang_migration.sh generate default
```

This example will generate the yaml needed for migrating a deployment in the default namespace. 
The script will generate the following folder structure:

```
current_folder/
├── default.CR_migrations
│    ├── backup  
│    ├── original
│    ├── updated
│    ├── to_remove_CRs    
```
**backup:** Backup configs from the current deployment

**original:** Stores All CRs as currently deployed

**updated:** Stores the converted CRs to be used in the migration

**to_remove_CRs:** Stores CRs that can be removed (old Masters) after the migration


#### After the generation is completed
The `updated` folder has all of the files needed for the migration. The next step is to apply each one of these files following the order below. This order ensures Statefulsets are updated correctly. If any of these CRs are not deployed in your environment, you can skip to the next item.

**Order for migration:**

1) Apply the label biaslangmasternode=yes in the Node where LicenseMaster is deployed
2) Apply the LicenseManager CR generated
3) Apply LicenseManager Rsync Job for /etc PVC
4) Apply LicenseManager Rsync Job for /var PVC
5) Remove the label biaslangmasternode=yes
6) Apply the label biaslangmasternode=yes in the Node where ClusterMaster is deployed
7) Apply the ClusterManager CR generated
8) Apply ClusterManager Rsync Job for /etc PVC
9) Apply ClusterManager Rsync Job for /var PVC 
10) Apply IndexerCluster CR
11) Apply SearchHeadCluster CR
12) Apply Standalone CR

Once you validate your environment, you can delete the CM and LM CRs using the files in the folder `to_remove_CRs`. 
Example: `kubectl delete -f ./to_remove_CRs/<filename>`

For more details on how to apply labels check: [K8s docs](https://kubernetes.io/docs/tasks/configure-pod-container/assign-pods-nodes/)


### Migrate Mode

Run the script in `tools/bias_lang_migration.sh` passing "migrate" as an argument as well as the namespace of the current deployment.

Example:
```
./tools/bias_lang_migration.sh migrate default
```

This will start a migration where we clone deployed ClusterMaster/LicenseMaster into a new independent ClusterManager/LicenseManager. Then we use a Rsync job to copy the data from Master to Manager.
Next script updates originally deployed Statefulsets for all CRs that references the CM/LM. 

Once the migration is completed the operator will restart Indexer pods to update environment variables. While the Indexer and Search services should be available, it will take some time until RF/SF are re-established. We recommend you do not apply any other changes until the Operator completes the restart of all Indexers and RF/SF are met. Once the entire environment is stable and validated, we recommend you to delete the CRs using the files created in the `to_remove_CRs` folder.

**Important:** Do not interrupt the script execution once it starts because it might leave your deployment in a bad state if some operations are not fully completed.

## Script Requirements

1) Your deployment must meet Replication Factor(RF) and Search Factor(SF) minimums for IndexerCluster and SearchHeadCluster.
2) This script is only supported in the 2.1.0 Operator version. Make sure you upgrade the operator and have the new CRDs installed before running the script.
3) The new Manager CRs need to be deployed in the same Node as the current CRs. Make sure the Node is not reaching any capacity limitation and can handle new pods.
4) JQ is required to run this script. Installation guide can be found here:  https://stedolan.github.io/jq/download/
