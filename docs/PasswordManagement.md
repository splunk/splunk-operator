# Password Management

- [Password Management](#password-management)
  - [Global kubernetes secret object](#global-kubernetes-secret-object)
  - [Default behavior of global kubernetes secret object](#default-behavior-of-global-kubernetes-secret-object)
    - [Splunk Secret Tokens in the global secret object](#splunk-secret-tokens-in-the-global-secret-object)
      - [HEC Token](#hec-token)
      - [Default administrator password](#default-administrator-password)
      - [pass4Symmkey](#pass4symmkey)
      - [IDXC pass4Symmkey](#idxc-pass4symmkey)
      - [SHC pass4Symmkey](#shc-pass4symmkey)
  - [Information for Splunk Enterprise administrator](#information-for-splunk-enterprise-administrator)
  - [Secrets on Docker Splunk](#secrets-on-docker-splunk)
  - [SmartStore Access using AWS IAM Role for Service Account](#smartstore-access-using-aws-iam-role-for-service-account)
  - [Support for AWS IAM Role for Service Account in Splunk Operator Deployment](#support-for-aws-iam-role-for-service-account-in-splunk-operator-deployment)
  - [Support for Hashicorp Vault](#support-for-hashicorp-vault-in-splunk-operator-deployment)

## Global kubernetes secret object
A global kubernetes secret object acts as the source of secret tokens for a kubernetes namespace used by all Splunk Enterprise CR's. It's name follows the format `splunk-<namespace>-secret` where `<namespace`> represents the namespace we are operating in. The contents of this object are volume mounted on all the pods within a kubernetes namespace.

This approach:
  - Eliminates any mismatch between operator-generated secrets and admin provided secrets as all secrets are synced into a common object.
  - Allows for dynamic adoption and modification of secrets.

## Default behavior of global kubernetes secret object

Upon the creation of the first Splunk Enterprise CR in a given namespace, the operator checks for the existence of a global kubernetes secret object:

- If the object does not exist,
    - It creates the global kubernetes secret object with name splunk-`<namespace`>-secret
    - It auto-generates, encodes and stores all Splunk Enterprise secret tokens into a global kubernetes secret object for the namespace. Note: SmartStore secret tokens are not generated, and must be created manually.
- If the object exists,
    - It checks for the existence of the Splunk Enterprise secret tokens by key value.
    - It auto-generates, encodes, and stores a Splunk Enterprise secret token for any empty keys. Note: SmartStore secret tokens are not generated, and must be created manually.

Note: Before the creation of any Splunk deployments in the kubernetes namespace, the admin can create a global kubernetes secret object using the tokens mentioned below. The operator will use these pre-populated values to deploy.

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

For examples of performing CRUD operations on the global secrets object, see [examples](Examples.md#managing-global-kubernetes-secret-object). For more information on managing kubernetes secret objects refer [kubernetes.io managing secrets](https://kubernetes.io/docs/tasks/configmap-secret/managing-secret-using-kubectl/)

## Information for Splunk Enterprise administrator

- The default administrator account cannot be disabled on any Splunk Enterprise instance. The kubernetes operator uses this account to interact with all Splunk Enterprise instances in the namespace.
- The passwords managed using the global kubernetes secret object should never be changed using Splunk Enterprise tools (CLI, UI.)
- The default administrator account must use the global kubernetes secret object for any password changes. See [managing global kubernetes secret object](Examples.md#managing-global-kubernetes-secret-object)
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


```
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

```
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

## Support for HashiCorp Vault in Splunk Operator Deployment

The Splunk Operator for Kubernetes now offers native support for HashiCorp Vault to manage and inject secrets into Splunk Enterprise deployments. This integration provides a secure and centralized method to handle sensitive information, complementing the existing global Kubernetes secret object approach.

**Note:** This integration relies on the Vault Agent Injector mechanism. It requires that a Vault Agent is present within the Pod for secret injection and that a Vault Operator is running in your Kubernetes cluster to manage Vault interactions.

### How Vault Integration Works with Password Management

When Vault integration is enabled in the Splunk Custom Resource (CR), the operator will:

1. **Leverage Vault for Secrets:**  
   Instead of solely relying on the global Kubernetes secret object, the operator can retrieve secret tokens directly from a HashiCorp Vault server. This is particularly useful for managing sensitive tokens such as the default administrator password, HEC token, and various `pass4Symmkey` values.

2. **Vault Agent Injector and Automated Secret Injection:**  
   - The Splunk Operator adds specific annotations to Splunk Pods upon their creation or update.
   - The Vault Agent Injector, running as part of the Vault ecosystem in your Kubernetes cluster, detects these annotations and:
     - Injects a Vault Agent sidecar container into the Pod.
     - The Vault Agent sidecar authenticates with Vault using the Kubernetes service account token.
     - It then fetches necessary secret tokens (e.g., `hec_token`, `password`, `pass4Symmkey`, `idxc_secret`, `shc_secret`) from the specified Vault path.
     - The agent injects these secrets into the Pod at runtime, typically by writing them to a shared volume mounted at `/mnt/splunk-secrets`.
   - The primary Splunk container accesses these secrets from the mounted volume, avoiding hardcoding or manual handling of credentials.

3. **Secret Update Detection and Pod Restarts:**  
   - The operator continuously checks Vault for updates or changes in secret versions.
   - If a secret changes (for example, a password is updated), the operator will automatically:
     - Update annotations on the affected StatefulSet to reflect new secret versions.
     - Trigger a rolling restart of the impacted Pods.
   - This ensures that Splunk Enterprise always operates with the latest secrets from Vault without manual intervention.

### Configuring Vault Integration

To configure HashiCorp Vault for your Splunk deployment, update your Splunk CR to include the `vaultIntegration` section as follows:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SplunkEnterprise
metadata:
  name: my-splunk
spec:
  vaultIntegration:
    enable: true                     # Enable Vault support
    address: "https://vault.example.com"  # Vault server address
    role: "splunk-role"              # Vault role for Kubernetes authentication
    secretPath: "secret/data/splunk" # Base path in Vault for Splunk secrets
  # ... other spec fields ...
```

**Key Configuration Fields:**
- **enable:** Set to `true` to activate Vault integration.
- **address:** The URL of your Vault server.
- **role:** The Vault role used for Kubernetes-based authentication.
- **secretPath:** The base path in Vault where Splunk-related secrets are stored.

### Prerequisites and Best Practices

- **Vault Server Setup:**
  - Ensure your Vault server is accessible from the Kubernetes cluster.
  - Configure the Kubernetes authentication method in Vault and set up a role that grants access to the necessary secret paths.

- **Vault Agent Injector and Vault Operator:**
  - The Vault Agent Injector must be installed and configured in your Kubernetes cluster.
  - A Vault Operator should be running to manage interactions with the Vault server, including deploying Vault Agents into Pods for secret injection.

- **Permissions:**
  - The Kubernetes service account used by the Splunk Operator must have permissions to read its own service account token for Vault authentication.
  - Vault policies associated with the specified role should grant read access to secrets stored under the defined `secretPath`.

- **Security Considerations:**
  - Use proper Vault policies to enforce least-privilege access.
  - Ensure secure network connectivity between your Kubernetes cluster and the Vault server.
  - Regularly audit and rotate credentials in Vault as part of your security best practices.

### Benefits of Using Vault Integration for Password Management

- **Centralized Secret Management:**  
  Secrets are managed centrally in Vault, reducing reliance on distributed Kubernetes secrets and simplifying credential rotation.

- **Enhanced Security:**  
  Secrets are fetched securely at runtime using a Vault Agent, minimizing exposure and mitigating the risk of secrets being stored in plain text within Kubernetes objects.

- **Automated Updates:**  
  The operator automatically detects changes in Vault secrets and restarts affected Pods to apply updates, ensuring Splunk always runs with current credentials.

By integrating HashiCorp Vault with the Splunk Operator and leveraging the Vault Agent Injector, administrators can enhance security and streamline secret management in their Splunk Enterprise deployments on Kubernetes.