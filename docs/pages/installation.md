---
title: Installation
has_children: true
has_toc: false
nav_order: 11
nav_exclude: true
---

## Prerequisites
- Version >=1.5.0 of the Splunk Operator requires a Kubernetes cluster of version >=1.26.0.
- The Splunk Operator requires SmartStore to be configurted for indexed data storage.
- The use of Persistent Volume Claims requires that your cluster is configured to support one or more Kubernetes persistent Storage Classes


### Kubernetes Platform recommendations

The Splunk Operator should work with any [CNCF certified distribution](https://www.cncf.io/certification/software-conformance/) of Kubernetes. We do not have platform recommendations, but this is a table of platforms that our developers, customers, and partners have used successfully with the Splunk Operator.

<table>
<tr><td> Splunk Development & Testing Platforms </td><td> Amazon Elastic Kubernetes Service (EKS), Google Kubernetes Engine (GKE) </td></tr>
<tr><td> Customer Reported Platforms </td><td> Microsoft Azure Kubernetes Service (AKS), Red Hat OpenShift </td></tr>
<tr><td> Partner Tested Platforms</td><td> HPE Ezmeral</td></tr>
<tr><td> Other Platforms </td><td>CNCF certified distribution</td></tr>
</table>

### Splunk Enterprise Version Compatibility

Each Splunk Operator release has specific Splunk Enterprise compatibility requirements. Splunk Operator can support more than one version of Splunk Enterprise release. Before installing or upgrading the Splunk Operator, review the [release notes](https://github.com/splunk/splunk-operator/releases) to verify version compatibility with Splunk Enterprise releases.


### Hardware Resources Requirements
The resource guidelines for running production Splunk Enterprise instances in pods through the Splunk Operator are the same as running Splunk Enterprise natively on a supported operating system and file system. Refer to the Splunk Enterprise [Reference Hardware documentation](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware) for additional details. We would also recommend following the same guidance on [Splunk Enterprise for disabling Transparent Huge Pages (THP)](https://docs.splunk.com/Documentation/Splunk/latest/ReleaseNotes/SplunkandTHP) for the nodes in your Kubernetes cluster. Please be aware that this may impact performance of other non-Splunk workloads.


#### Minimum Reference Hardware
Based on Splunk Enterprise [Reference Hardware documentation](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware), a summary of the minimum reference hardware requirements is given below.

| Standalone        | Search Head / Search Head Cluster | Indexer Cluster |
| ---------- | ------- | ------- |
| _Each Standalone Pod: 12 Physical CPU Cores or 24 vCPU at 2Ghz or greater per core, 12GB RAM._| _Each Search Head Pod: 16 Physical CPU Cores or 32 vCPU at 2Ghz or greater per core, 12GB RAM._| _Each Indexer Pod: 12 Physical CPU cores, or 24 vCPU at 2GHz or greater per core, 12GB RAM._ |


### Storage guidelines
The Splunk Operator uses Kubernetes [Persistent Volume Claims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims) to store all of your Splunk Enterprise configuration ("$SPLUNK_HOME/etc" path) and event ("$SPLUNK_HOME/var" path) data. If one of the underlying machines fail, Kubernetes will automatically try to recover by restarting the Splunk Enterprise pods on another machine that is able to reuse the same data volumes. This minimizes the maintenance burden on your operations team by reducing the impact of common hardware failures to the equivalent of a service restart.
The use of Persistent Volume Claims requires that your cluster is configured to support one or more Kubernetes persistent [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/). See the [Setting Up a Persistent Storage for Splunk](/configuration/StorageClass) page for more
information.

### What Storage Type To Use?

The Kubernetes infrastructure must have access to storage that meets or exceeds the recommendations provided in the Splunk Enterprise storage type recommendations at [Reference Hardware documentation - what storage type to use for a given role?](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware#What_storage_type_should_I_use_for_a_role.3F) In summary, Indexers with SmartStore need NVMe or SSD storage to provide the necessary IOPs for a successful Splunk Enterprise environment.


### Splunk SmartStore Required
For production environments, we are requiring the use of Splunk SmartStore. As a Splunk Enterprise deployment's data volume increases, demand for storage typically outpaces demand for compute resources. [Splunk's SmartStore Feature](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/AboutSmartStore) allows you to manage your indexer storage and compute resources in a ___cost-effective___ manner by scaling those resources separately. SmartStore utilizes a fast storage cache on each indexer node to keep recent data locally available for search and keep other data in a remote object store. Look into the [SmartStore Resource Guide](/configuration/SmartStore) document for configuring and using SmartStore through operator.
