# Splunk Operator Helm Installation

## Splunk Operator Helm chart Repository

Add the Splunk Operator and Enterprise charts to your Helm repository.

```
helm repo add splunk https://splunk.github.io/splunk-operator/
helm repo update
```

The ```splunk``` chart repository contains the ```splunk/splunk-operator``` chart to deploy the Operator and the ```splunk/splunk-enterprise``` chart to deploy Splunk Custom Resources.

Helm provides a long list of commands to manage your deployment, we'll be going over a few useful ones in the sections to come. You can learn more about supported commands [here](https://helm.sh/docs/helm/helm/).

## Splunk Operator deployments

To install the operator:

```
helm install <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

To upgrade the operator, specify override configurations through the CLI or a new file:

```
helm upgrade --set splunkOperator.clusterWideAccess=false <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

Here we've upgraded the operator by revoking cluster-wide access.

To rollback the operator to a previous release:

```
helm rollback <RELEASE_NAME> <REVISION_NUMBER> -n <RELEASE_NAMESPACE>
```

To clean-up resources and uninstall the operator:

```
helm uninstall <RELEASE_NAME> -n <RELEASE_NAMESPACE>
```

### Configuring Splunk Operator deployments

There are a couple ways you can configure your operator deployment

1. Using a ```new_values.yaml``` file to override default values (Recommended)
```
helm install -f new_values.yaml <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

2. Using the Helm CLI to set new values
```
helm install --set <KEY>=<VALUE> <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

Read more about configuring values [here](https://helm.sh/docs/intro/using_helm/).

## Splunk Enterprise deployments

The Splunk Enterprise chart allows you to install and configure Splunk custom resources.

First build the Splunk Operator chart as a dependency:
```
helm dependency build splunk/splunk-enterprise
```

To install a configured Enterprise deployment:
```
helm install <RELEASE_NAME> splunk/splunk-enterprise -n <RELEASE_NAMESPACE>
```

This chart installs the Splunk Operator as a dependency by default.

If the operator is already installed then you will need to disable the dependency:
```
helm install --set splunk-operator.enabled=false <RELEASE_NAME> splunk/splunk-enterprise -n <RELEASE_NAMESPACE>
```

To see all configurable values contained in the ```values.yaml``` file:
```
helm show values splunk/splunk-enterprise
```
## Splunk Validated Architecture deployments

The Splunk Enterprise chart has support for three Splunk Validated Architectures:

- [Single Server Deployment (S1)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=9)
- [Distributed Clustered Deployment + SHC - Single Site (C3)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=14)
- [Distributed Clustered Deployment + SHC - Multi-Site (M4)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=20)

Install a Single Server Deployment release using the following command:
```
helm install --set s1.enabled=true <RELEASE_NAME> splunk/splunk-enterprise -n <RELEASE_NAMESPACE>
```
Visit the Splunk Operator github repository to learn more about the configurable values of [splunk/splunk-operator]() and [splunk/splunk-enterprise]().









