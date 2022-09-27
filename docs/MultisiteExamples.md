# Configuring Splunk Enterprise Multisite Deployments

This document provides examples to configure a multisite cluster using the splunk-operator.


- [Configuring Splunk Enterprise Multisite Deployments](#configuring-splunk-enterprise-multisite-deployments)
  - [Multisite Indexer Clusters in Kubernetes](#multisite-indexer-clusters-in-kubernetes)
  - [Multipart IndexerCluster](#multipart-indexercluster)
      - [Deploy the cluster-manager](#deploy-the-cluster-manager)
      - [Deploy the indexer sites](#deploy-the-indexer-sites)
  - [Connecting a search-head cluster to a multisite indexer-cluster](#connecting-a-search-head-cluster-to-a-multisite-indexer-cluster)

Please refer to the [Configuring Splunk Enterprise Deployments Guide](Example.md)
for more information and examples about deploying the different Splunk resources
in single site mode.

## Multisite Indexer Clusters in Kubernetes

[Multisite indexer cluster architecture](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Multisitearchitecture)
can be used for various purposes. However, Kubernetes clusters are generally deployed at the scale of
a region, and the main purpose of this documentation is to cover the topic of building high-available
indexer clusters allowing to control the placement of bucket replicas across multiple
[availability zones](https://kubernetes.io/docs/setup/best-practices/multiple-zones/#introduction),
to prevent the failure of a single zone from causing events to be unavailable for search or permanently lost.

Designing applications requiring high-availability to support the loss of a zone is a recommendation
from all cloud providers (e.g. [GCP](https://cloud.google.com/solutions/scalable-and-resilient-apps),
[AWS](https://aws.amazon.com/about-aws/global-infrastructure/regions_az/#Availability_Zones),
[Azure](https://docs.microsoft.com/en-us/azure/availability-zones/az-overview#availability-zones)).
In a private datacenter, multisite indexer clusters can be used to support the loss of a room or rack.
In the case of dedicated hardware with local storage used for Splunk (or various datastores),
a multisite indexer cluster allows to support regular maintenance (e.g. OS upgrades) in the case of
multiple indexer pods scheduled on the same host.

## Multipart IndexerCluster

Description: connect multiple IndexerCluster resources to ClusterManager resource, each constrained to run within a dedicated zone and
configured with a hardcoded site.

Advantages:
- some operations are performed per site which mitigates the risk of impact on the whole cluster (e.g. Splunk upgrades, scaling up resources)
- specific indexer services are created per site allowing to send events to the indexers located in the same zone, avoiding possible cost of cross-zone traffic. Indexer discovery from cluster-manager can do this for forwarders, but this solution also covers http/HEC traffic

Limitation: all the IndexerCluster resources must be located in the same namespace

#### Deploy the cluster-manager

Note: the image version is defined in these resources as this allows to control the upgrade cycle 

```yaml
cat <<EOF | kubectl apply -n splunk-operator -f -
---
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  defaults: |-
    splunk:
      site: site1
      multisite_master: localhost
      all_sites: site1,site2,site3
      multisite_replication_factor_origin: 1
      multisite_replication_factor_total: 2
      multisite_search_factor_origin: 1
      multisite_search_factor_total: 2
      idxc:
        search_factor: 2
        replication_factor: 2
      # Apps defined here are deployed to the indexers of all the sites
      apps_location:
        - "https://example.com/splunk-apps/app3.tgz"
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: failure-domain.beta.kubernetes.io/zone
            operator: In
            values:
            - zone-1a
EOF
```

#### Deploy the indexer sites

```yaml
cat <<EOF | kubectl apply -n splunk-operator -f -
---
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example-site1
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 2
  clusterManagerRef:
    name: example
  defaults: |-
    splunk:
      multisite_master: splunk-example-cluster-manager-service
      site: site1
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: failure-domain.beta.kubernetes.io/zone
            operator: In
            values:
            - zone-1a
EOF
```
Create IndexerCluster CR for each required site with zone affinity specified as needed

Note:
* The value of label for zone i.e. `zone-1a` for label `failure-domain.beta.kubernetes.io/zone` is specific to each cloud provider and should be changed based on the cloud provider you are using
* Starting in Kubernetes v1.17, the label `failure-domain.beta.kubernetes.io/zone` is deprecated in favor of `topology.kubernetes.io/zone`. See the [official documentation](https://kubernetes.io/docs/reference/labels-annotations-taints/#failure-domainbetakubernetesiozone)

## Connecting a search-head cluster to a multisite indexer-cluster

[Search head clusters do not have site awareness](
https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/DeploymultisiteSHC#Search_head_clusters_do_not_have_site_awareness)
for artifact replication, so mapping Splunk sites to Kubernetes zones is not relevant in that context.

SearchHeadCluster resources can be connected to a multisite indexer cluster the same way as for single site.
The name of the IndexerCluster part containing the cluster manager must be referenced in parameter `clusterManagerRef`.

Additional ansible default parameters must be set to activate multisite:
* `multisite_master`: which should reference the cluster-manager service of the target indexer cluster
* `site`: which should in general be set to `site: site0` to disable search affinity ([documentation for more details]
(https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/DeploymultisiteSHC#Integrate_a_search_head_cluster_with_a_multisite_indexer_cluster))

```yaml
cat <<EOF | kubectl apply -n splunk-operator -f -
---
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 3
  image: "splunk/splunk:9.0.0"
  clusterManagerRef:
    name: example
  defaults: |-
    splunk:
      multisite_master: splunk-example-cluster-manager-service
      site: site0
EOF
```

