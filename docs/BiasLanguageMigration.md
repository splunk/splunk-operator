# FAQ - Biased Language Migration


## Motivation

Institutionalized bias has no place in our products, our documentation, our language or our actions. As technology leaders, we must be aware of these issues and fix them.
Learn more about our company-wide efforts [Annoucements](https://www.splunk.com/en_us/blog/leadership/biased-language-has-no-place-in-tech.html) and familiarize yourself with our
[Bias language guide](https://docs.splunk.com/Documentation/StyleGuide/latest/StyleGuide/Inclusivity) documentation.

## What's happening?

Starting in the Operator version 2.1.0 we are introducing new CRDs for ClusterManager and LicenseManager. These CRDs have equal capabilities and configurations as their predecessors ClusterMaster, and LicenseMaster. Existing deployments are not affected, but encouraged to plan a migration to the new CRDs in the future. 

## Which CRD should I choose for a new deployment?

It is recommended that all new deployments use the new CRDs. The examples in our documentation have been updated to use the new CRDs. Please check [Configuring Splunk Enterprise Deployments](https://github.com/splunk/splunk-operator/blob/master/docs/Examples.md) for details.

## How to plan for a migration?

Migrating existing deployments might cause search & indexing service interruptions due to the Statefulset being restarted. We recommend performing it during the maintenance window with your admins in place validate the environment post-migration. Please make sure you follow these recommendations:

1) Enable maintenance mode if using IndexerCLuster deployment.
2) Perform a backup of your data in case any unexpected problem occurs during the migration.
3) Make sure you have the requirements such jq installed.
4) Use the script in a development environment before migrating production stacks.


## How to use the migration script?

Run the script in tools/bias_lang_migration.sh passing which CR should be migrated or "All" to migrate both, as well as the namespace in which the CRs are deployed. 

The script should be run as follows:
```
./tools/bias_lang_migration.sh <ClusterMaster|LicenseMaster|All> <Namespace>
```


## Troubleshooting

We added a log file that saves information about the script’s execution. This log should contain error messages for the most common issues, as well as it shows the Kubernetes outputs for each command executed. We also collect all configs and currently deployed CRs in the following structure:


```
splunk-operator/
├── original_CRs
│       ├── <CRs>.json
│       └── bck
│            ├── <configs>.json
├── updated_CRs
│       ├── ClusterManager.json
│       ├── LicenseManager.json
```
