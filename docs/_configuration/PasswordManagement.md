---
title: Password Management
nav_order: 26
#nav_exclude: true
---

# Password Management
{: .no_toc }

A global kubernetes secret object acts as the source of secret tokens for a kubernetes namespace used by all Splunk Enterprise CR's. It's name follows the format `splunk-<namespace>-secret` where `<namespace>` represents the namespace we are operating in. The contents of this object are volume mounted on all the pods within a kubernetes namespace.

This approach:
  - Eliminates any mismatch between operator-generated secrets and admin provided secrets as all secrets are synced into a common object.
  - Allows for dynamic adoption and modification of secrets.
<br/><br/>
  
#### Table of contents
{: .no_toc }
- TOC
{:toc}


## Default behavior of global kubernetes secret object

Upon the creation of the first Splunk Enterprise CR in a given namespace, the operator checks for the existence of a global kubernetes secret object:

- If the object does not exist,
    - It creates the global kubernetes secret object with name `splunk-<namespace>-secret`
    - It auto-generates, encodes and stores all Splunk Enterprise secret tokens into a global kubernetes secret object for the namespace.     
- If the object exists,
    - It checks for the existence of the Splunk Enterprise secret tokens by key value.
    - It auto-generates, encodes, and stores a Splunk Enterprise secret token for any empty keys.

{: .note }
SmartStore secret tokens are not generated, and must be created manually.<br/><br/>
Before the creation of any Splunk deployments in the kubernetes namespace, the admin can create a global kubernetes secret object using the tokens mentioned below. The operator will use these pre-populated values to deploy.

### Splunk Secret Tokens in the global secret object
The configurable Splunk Secret Tokens include:

#### HEC Token
**Key name in global kubernetes secret object**: `hec_token`
**Description**: hec_token is used to authenticate clients sending data into Splunk Enterprise via HTTP connections.

#### Default administrator password
**Key name in global kubernetes secret object**: `password`
**Description**: password refers to the default administrator password for Splunk.

#### pass4Symmkey
**Key name in global kubernetes secret object**: `pass4Symmkey`
**Description**: pass4Symmkey is an authentication token for inter-communication within Splunk Enterprise.

#### IDXC pass4Symmkey
**Key name in global kubernetes secret object**: `idxc.secret`
**Description**: idxc.secret is an authentication token for inter-communication specifically for indexer clustering in Splunk Enterprise.

#### SHC pass4Symmkey
**Key name in global kubernetes secret object**: `shc.secret`
**Description**: shc.secret is an authentication token for inter-communication specifically for search head clustering in Splunk Enterprise.

For examples of performing CRUD operations on the global secrets object, see [examples](/configuration/Examples#managing-global-kubernetes-secret-object). For more information on managing kubernetes secret objects refer [kubernetes.io managing secrets](https://kubernetes.io/docs/tasks/configmap-secret/managing-secret-using-kubectl/)

## Information for Splunk Enterprise administrator

- The default administrator account cannot be disabled on any Splunk Enterprise instance. The kubernetes operator uses this account to interact with all Splunk Enterprise instances in the namespace.
- The passwords managed using the global kubernetes secret object should never be changed using Splunk Enterprise tools (CLI, UI.)
- The default administrator account must use the global kubernetes secret object for any password changes. See [managing global kubernetes secret object](/configuration/Examples#managing-global-kubernetes-secret-object)
- After initiating a update/delete operation on the global secrets object, the operator will require time to finish setting the changes on the all Splunk Enterprise instances in the namespace during which disruption of splunk services can be expected while the secret updates are happening. A status check on all the Splunk Enterprise cluster tiers is required.

## Secrets on Docker Splunk
When Splunk Enterprise is deployed on a docker container, ansible playbooks are used to setup Splunk. Ansible playbooks interpret the environment variable SPLUNK_DEFAULTS_URL in the container as the location to read the Splunk Secret Tokens from. The tokens are used to setup Splunk Instances running on containers inside pods.

## SmartStore Access using AWS IAM Role for Service Account

Splunk 9.0.5 Supports Smartstore Access using AWS IAM Role for Service Account.

- AWS Identity and Access Management (IAM) provides fine-grained access control where you can specify who can access which AWS service or resources, ensuring the principle of least privilege.
- Kubernetes Pods are given an identity through a Kubernetes concept called a Kubernetes Service Account. When a Service Account is created, a JWT token is automatically created as a Kubernetes Secret. This Secret can then be mounted into Pods and used by that Service Account to authenticate to the Kubernetes API Server.
- AWS introduced IAM Roles for Service Accounts (IRSA), leveraging AWS Identity APIs, an OpenID Connect (OIDC) identity provider, and Kubernetes Service Accounts to apply fine-grained access controls to Kubernetes pods.
- In Kubernetes,  ProjectedServiceAccountToken feature allows a fully compliant OIDC JWT token issued by the TokenRequest API of Kubernetes to be mounted into the Pod as a Projected Volume. The relevant Service Account Token Volume Projection flags are enabled by default on an EKS cluster. Therefore, fully compliant OIDC JWT Service Account tokens are being projected into each pod instead of the JWT token
- AWS has created an identity webhook that comes preinstalled in an EKS cluster.
- This webhook listens to create pod API calls and can inject an additional Token into splunkd pods. This webhook can also be installed into self-managed Kubernetes clusters on AWS using [this guide](https://github.com/aws/amazon-eks-pod-identity-webhook/blob/master/SELF_HOSTED_SETUP.md)

Below Example explains the steps required for setting up IAM Service Account

- Follow the steps defined [here to create IAM Role for Service Account](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)

- Make sure IAM Role only has least amount of privilege necessary for smartstore to work.

- Make sure the service account is used in custom resources where its required
 Once the Service Account is created, make sure it is annotated with specific IAM Role. Once everything looks good, add service account to splunk custom resource. here is the example for adding it to `Standalone` instance


```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: test
spec:
  serviceAccount: oidc-service-account
  smartstore:
    defaults:
        volumeName: test-cluster-bucket
    indexes:
      - name: main
        remotePath: $_index_name
        volumeName: test-cluster-bucket
      - name: cloudwatch
        remotePath: $_index_name
        volumeName: test-cluster-bucket
    volumes:
      - name: test-cluster-bucket
        path: test-cluster-bucket/smartstore
        endpoint: https://s3-us-west-2.amazonaws.com
```

- Make sure the IAM service account is used only in required custom resources

- When Splunk pod is running AWS webhook service injects 2 new environment variables `AWS_WEB_IDENTITY_TOKEN_FILE` and `AWS_ROLE_ARN` along with JWS Token file. `splunk` pod reads these environment variables to get temporary AWS credentials from AWS IAM service to access smartstore buckets

***OIDC key management***
The proper Key management of OIDC is outside of Splunk installation. The customer is responsible to use a properly configured OIDC using certificates from a trusted CA.

***Self signed certificate***
The OIDC should not use self-signed certificates but rather utilize an existing PKI infrastructure, e.g. have the OIDC certificate issued and signed by your organization's CA with proper certificate signature chains and key expiation policies.

***Sharing OIDC token file***
Make sure the token file mentioned in AWS_WEB_IDENTITY_TOKEN_FILE location is only accessible inside of the pod and is not mapped or shared outside of the pod

## Support for AWS IAM Role for Service Account in Splunk Operator Deployment

Follow the steps mentioned above for creating AWS IAM Service Account. Make sure IAM Role only has least amount of privilege necessary reading apps from S3 bucket. Once the service account is created, map this service account to `splunk-operator` deployment. Below is the example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: splunk-operator-controller-manager
  namespace: splunk-operator
spec:
  progressDeadlineSeconds: 600
  replicas: 1
  revisionHistoryLimit: 10
  ...
  spec:
    containers:
    -
      ...
      serviceAccount: oidc-service-account
      serviceAccountName: oidc-service-account
      terminationGracePeriodSeconds: 10
      volumes:
      - name: app-staging
        persistentVolumeClaim:
          claimName: splunk-operator-app-download
      ...
```