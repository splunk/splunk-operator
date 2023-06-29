# Splunk Operator Helm Installation

## Splunk Operator Helm chart Repository

Add the Splunk Operator and Enterprise charts to your Helm repository.

```
helm repo add splunk https://splunk.github.io/splunk-operator/
helm repo update
```

The ```splunk``` chart repository contains the ```splunk/splunk-operator``` chart to deploy the Splunk Operator and the ```splunk/splunk-enterprise``` chart to deploy Splunk Enterprise custom resources.

Currently only latest version splunk operator is hosted on `https://splunk.github.io/splunk-operator`. For previous version of helm chart, checkout release branch code. For example, for 2.0.0 release, follow the below steps -

```
git clone https://github.com/splunk/splunk-operator.git .
git checkout release/2.0.0
helm install splunk-s1 helm-chart/splunk-enterprise ...
```

Helm provides a long list of commands to manage your deployment, we'll be going over a few useful ones in the sections to come. You can learn more about supported commands [here](https://helm.sh/docs/helm/helm/).

## Splunk Operator deployments

Installing the ```splunk/splunk-operator``` chart deploys the Splunk Operator with cluster-wide access. View the configurable values for the chart using the following command:

```
helm show values splunk/splunk-operator
```

### Configuring Splunk Operator deployments

There are a couple ways you can configure your operator deployment

1. Using a ```new_values.yaml``` file to override default values (Recommended)
```
helm install -f new_values.yaml <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

2. Using the Helm CLI directly to set new values
```
helm install --set <KEY>=<VALUE> <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

If the release already exists, we can use ```helm upgrade``` to configure and upgrade the deployment using a file or the CLI directly as above.

```
helm upgrade -f new_values.yaml <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

Read more about configuring values [here](https://helm.sh/docs/intro/using_helm/).

In the following example, we will install and upgrade the Splunk Operator.

Specify the release name and namespace to install the Operator:

```
helm install splunk-operator-test splunk/splunk-operator -n splunk-operator
```
```
NAME: splunk-operator-test
LAST DEPLOYED: Tue Aug 23 12:47:57 2022
NAMESPACE: splunk-operator
STATUS: deployed
REVISION: 1
TEST SUITE: None
```
```
NAME                                                  READY   STATUS    RESTARTS   AGE
splunk-operator-controller-manager-545cccf79f-9xpll   2/2     Running   0          2m14s
```
The ```helm list``` command can be used to retrieve all deployed releases.

By default, the Splunk Operator has cluster-wide access. Let's upgrade the ```splunk-operator-test``` release by revoking cluster-wide access:
```
helm upgrade --set splunkOperator.clusterWideAccess=false splunk-operator-test splunk/splunk-operator -n splunk-operator
```
```
NAME: splunk-operator-test
LAST DEPLOYED: Tue Aug 23 12:53:08 2022
NAMESPACE: splunk-operator
STATUS: deployed
REVISION: 2
TEST SUITE: None
```
Finally, let's terminate the Splunk Operator by uninstalling the ```splunk-operator-test``` release:
```
helm uninstall splunk-operator-test -n splunk-operator
```
```
release "splunk-operator-test" uninstalled
```

## Splunk Enterprise deployments

The Splunk Enterprise chart allows you to install and configure Splunk Enterprise custom resources. The ```splunk/splunk-enterprise``` chart has the ```splunk/splunk-operator``` chart as a dependency by default. To satisfy the dependencies please use the following command:
```
helm dependency build splunk/splunk-enterprise
```
If the operator is already installed then you will need to disable the dependency:
```
helm install --set splunk-operator.enabled=false <RELEASE_NAME> splunk/splunk-enterprise -n <RELEASE_NAMESPACE>
```
Installing ```splunk/splunk-enterprise``` will deploy Splunk Enterprise custom resources according to your configuration, the following ```new_values.yaml``` file specifies override configurations to deploy a Cluster Manager, an Indexer Cluster and a Search Head Cluster.

```
clusterMaster:
  enabled: true
  name: cm-test

indexerCluster:
  enabled: true
  name: idxc-test

searchHeadCluster:
  enabled: true
  name: shc-test
```
The configurations above will override values in ```splunk/splunk-enterprise``` values file.  To see all configurable values contained in the ```values.yaml``` file:
```
helm show values splunk/splunk-enterprise
```

To install a Splunk Enterprise deployment according to our configurations above:
```
helm install -f new_values.yaml splunk-enterprise-test splunk/splunk-enterprise -n splunk-operator
```
```
NAME: splunk-enterprise-test
LAST DEPLOYED: Tue Aug 23 12:11:48 2022
NAMESPACE: splunk-operator
STATUS: deployed
REVISION: 1
TEST SUITE: None
```
```
splunk-cm-test-cluster-master-0                       1/1     Running   0               11m
splunk-idxc-test-indexer-0                            1/1     Running   0               5m49s
splunk-idxc-test-indexer-1                            1/1     Running   0               5m49s
splunk-idxc-test-indexer-2                            1/1     Running   0               5m49s
splunk-operator-controller-manager-54979b7d88-9c54b   2/2     Running   0               11m
splunk-shc-test-deployer-0                            1/1     Running   0               11m
splunk-shc-test-search-head-0                         1/1     Running   0               11m
splunk-shc-test-search-head-1                         1/1     Running   0               11m
splunk-shc-test-search-head-2                         1/1     Running   0               11m
```
We can clean-up the deployed resources quickly by uninstalling the release:
```
helm uninstall splunk-enterprise-test -n splunk-operator
```
```
release "splunk-enterprise-test" uninstalled
```
```helm uninstall``` terminates all resources deployed by Helm including Persistent Volume Claims created for Splunk Enterprise resources.

Note: Helm by default does not cleanup Custom Resource Definitions and Persistent Volume Claims. Splunk Admin needs to manually clean them.

## Splunk Validated Architecture deployments

The Splunk Enterprise chart has support for three Splunk Validated Architectures:

- [Single Server Deployment (S1)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=9)
- [Distributed Clustered Deployment + SHC - Single Site (C3)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=14)
- [Distributed Clustered Deployment + SHC - Multi-Site (M4)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=20)

Install a Standalone deployment using the following command:
```
helm install --set s1.enabled=true <RELEASE_NAME> splunk/splunk-enterprise -n <RELEASE_NAMESPACE>
```
Visit the Splunk Operator github repository to learn more about the configurable values of [splunk/splunk-operator](https://github.com/splunk/splunk-operator/blob/develop/helm-chart/splunk-operator/values.yaml) and [splunk/splunk-enterprise](https://github.com/splunk/splunk-operator/blob/develop/helm-chart/splunk-enterprise/values.yaml).
