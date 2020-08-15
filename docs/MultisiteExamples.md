# Configuring Splunk Enterprise Multisite Deployments

This document provides examples to configure a multisite cluster using the splunk-operator.

* [Multisite Indexer Clusters in Kubernetes](#multisite-indexer-clusters-in-kubernetes)
* [Multipart IndexerCluster](#multipart-indexercluster)
* [Single IndexerCluster resource and Downward API](#single-indexercluster-resource-and-downward-api)
* [Connecting a search-head cluster to a multisite indexer-cluster](#connecting-a-search-head-cluster-to-a-multisite-indexer-cluster)

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

Description: connect multiple IndexerCluster resources, each constrained to run within a dedicated zone and
configured with a hardcoded site.

Advantages:
- some operations are performed per site which mitigates the risk of impact on the whole cluster (e.g. Splunk upgrades, scaling up resources)
- specific indexer services are created per site allowing to send events to the indexers located in the same zone, avoiding possible cost of cross-zone traffic. Indexer discovery from cluster-master can do this for forwarders, but this solution also covers http/HEC traffic

Limitation: all the IndexerCluster resources must be located in the same namespace

#### Deploy the cluster-master

Note: the image version is defined in these resources as this allows to control the upgrade cycle 

```yaml
cat <<EOF | kubectl apply -f -
---
kind: IndexerCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 0
  image: "splunk/splunk:8.0.4"
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
EOF
```

#### Deploy the indexer sites

Site / zone mapping must be adapted for each site.

```yaml
cat <<EOF | kubectl apply -f -
---
kind: IndexerCluster
metadata:
  name: example-site1
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 2
  image: "splunk/splunk:8.0.4"
  indexerClusterRef:
    name: example
  defaults: |-
    splunk:
      multisite_master: splunk-example-cluster-master-service
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

## Single IndexerCluster resource and Downward API

Description: indexers automatically identify the zone in which they are deployed and map it to a Splunk site.

Advantages:
- fewer Kubernetes resources to maintain / operate
- autoscaling easier to control and balance

Drawbacks:
- heavier configuration

#### Service account configuration

The default service account of the namespace running the cluster must be granted node-reader ClusterRole and pod-reader Role (Splunk CRDs currently don't allow to specify a different service account).

**Note:** the pod-reader role is needed as the IndexerCluster CRD doesn't provide a way to define environment variables on pods, which is the [only option to expose spec.nodeName](https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/#capabilities-of-the-downward-api) through the Kubernetes downward API.

```yaml
cat <<EOF | kubectl apply -f -
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-reader
rules:
- apiGroups: [""] # "" indicates the core API group
  resources: ["pods"]
  verbs: ["get", "watch", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: splunk-node-reader-view-pods
  namespace: splunk
subjects:
- kind: ServiceAccount
  name: default
  namespace: splunk
roleRef:
  kind: Role
  name: pod-reader
---
# Requires cluster admin priviledges
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
 name: splunk-node-reader
subjects:
- kind: ServiceAccount
  name: default
  namespace: splunk
roleRef:
  kind: ClusterRole
  name: system:node-reader
EOF
```

#### Installing the openshift python module in the docker image

This is the client library used by the [ansible k8s lookup](https://docs.ansible.com/ansible/latest/plugins/lookup/k8s.html#requirements) (lookup not specific to openshift).

```
FROM splunk/splunk:8.0.4
USER root
RUN pip2 install openshift
USER ansible
```

#### IndexerCluster definition

The namespace and name properties of the pods are exposed through volumes using the Kubernetes downward API (as spec.nodeName can only be exposed through environment variable, which is not yet supported by the IndexerCluster CRD).

Note: multipart cluster could still be used there to control the cluster-master independently from the indexers.

```yaml
cat <<EOF | kubectl apply -f -
---
apiVersion: enterprise.splunk.com/v1alpha2
kind: IndexerCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  # The custom image built at previous step
  image: "splunk/splunk:8.0.4_openshift_client"
  volumes:
    - name: podinfo
      downwardAPI:
        # type has to be defined because of bug in OCP 3.11: https://github.com/kubernetes/kubernetes/issues/68466
        # Because of the same bug IndexerCluster deletion also ends up in error when downwardAPI is used and finalizer
        # enterprise.splunk.com/delete-pvc is defined. Removing finalizer allows deleting the idxc.
        type: array
        # WARNING: reconciliation permanently fails if defaultMode or apiVersion in items not specified
        #          default value are automatically added in k8s, which causes the operator to always find diff
        defaultMode: 420
        items:
          - path: pod
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
          - path: namespace
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.namespace
  defaults: |-
    # The mapping must be configured with the names of the zones or your Kubernetes cluster
    zone_site_map:
      zone-1a: site1
      zone-1b: site2
      zone-1c: site3
    k8s_token: "{{ lookup('file', '/var/run/secrets/kubernetes.io/serviceaccount/token') }}"
    k8s_namespace: "{{ lookup('file', '/mnt/podinfo/namespace') }}"
    k8s_pod_name: "{{ lookup('file', '/mnt/podinfo/pod') }}"
    k8s_node_name: "{{ lookup('k8s', api_key=k8s_token, kind='Pod', namespace=k8s_namespace, resource_name=k8s_pod_name).spec.nodeName }}"
    # In latest version this label is being replaced with topology.kubernetes.io/zone
    k8s_zone: "{{ lookup('k8s', api_key=k8s_token, kind='Node', resource_name=k8s_node_name).metadata.labels['failure-domain.beta.kubernetes.io/zone'] }}"
    splunk:
      # As this configuration is shared between master and indexers:
      # The cluster-master pod can't reach it's own API through the cluster-master Service before the pod is flagged as ready
      # so at setup time, ansible must use localhost on cluster-master
      multisite_master: "{{ (lookup('env', 'SPLUNK_ROLE') == 'splunk_cluster_master') | ternary('localhost', 'splunk-example-cluster-master-service') }}"
      site: "{{ zone_site_map[k8s_zone] }}"
      all_sites: site1,site2,site3
      multisite_replication_factor_origin: 1
      multisite_replication_factor_total: 2
      multisite_search_factor_origin: 1
      multisite_search_factor_total: 2
      idxc:
        search_factor: 2
        replication_factor: 2
EOF
```

## Connecting a search-head cluster to a multisite indexer-cluster

[Search head clusters do not have site awareness](
https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/DeploymultisiteSHC#Search_head_clusters_do_not_have_site_awareness)
for artifact replication, so mapping Splunk sites to Kubernetes zones is not relevant in that context.

SearchHeadCluster resources can be connected to a multisite indexer cluster the same way as for single site.
The name of the IndexerCluster part containing the cluster master must be referenced in parameter `indexerClusterRef`.

Additional ansible default parameters must be set to activate multisite:
* `multisite_master`: which should reference the cluster-master service of the target indexer cluster
* `site`: which should in general be set to `site: site0` to disable search affinity ([documentation for more details]
(https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/DeploymultisiteSHC#Integrate_a_search_head_cluster_with_a_multisite_indexer_cluster))

```yaml
cat <<EOF | kubectl apply -f -
---
kind: SearchHeadCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 3
  image: "splunk/splunk:8.0.4"
  indexerClusterRef:
    name: example
  defaults: |-
    splunk:
      multisite_master: splunk-example-cluster-master-service
      site: site0
EOF
```

