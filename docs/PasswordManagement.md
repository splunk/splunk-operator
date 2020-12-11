# Password Management

- [Global kubernetes secret object](#global-kubernetes-secret-object)
- [Default behavior of global kubernetes secret object](#default-behavior-of-global-kubernetes-secret-object)
  - [Splunk Secret Tokens in the global secret object](#splunk-secret-tokens-in-the-global-secret-object)
    - [HEC Token](#hec-token)
    - [Default administrator password](#default-administrator-password)
    - [pass4Symmkey](#pass4Symmkey)
    - [IDXC pass4Symmkey](#idxc-pass4Symmkey)
    - [SHC pass4Symmkey](#shc-pass4Symmkey)
- [Information for Splunk Enterprise administrator](#information-for-splunk-enterprise-administrator)
- [Secrets on Docker Splunk](#secrets-on-docker-splunk)

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