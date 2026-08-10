---
title: Password Management
parent: Operate & Manage
nav_order: 5
---


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
  - [Encrypting secrets at rest](#encrypting-secrets-at-rest)
  - [Populating the global secret object using External Secrets Operator (ESO)](#populating-the-global-secret-object-using-external-secrets-operator-eso)
    - [How ESO fits into the secret contract](#how-eso-fits-into-the-secret-contract)
    - [Prerequisites](#prerequisites)
    - [Least privilege and namespace isolation](#least-privilege-and-namespace-isolation)
    - [Example: SecretStore and ExternalSecret (HashiCorp Vault)](#example-secretstore-and-externalsecret-hashicorp-vault)
    - [Validating the sync](#validating-the-sync)
    - [Rotating a password through ESO](#rotating-a-password-through-eso)
    - [Gotchas](#gotchas)
  - [Information for Splunk Enterprise administrator](#information-for-splunk-enterprise-administrator)
  - [Secrets on Docker Splunk](#secrets-on-docker-splunk)
  - [SmartStore Access using AWS IAM Role for Service Account](#smartstore-access-using-aws-iam-role-for-service-account)
  - [Support for AWS IAM Role for Service Account in Splunk Operator Deployment](#support-for-aws-iam-role-for-service-account-in-splunk-operator-deployment)

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

For examples of performing CRUD operations on the global secrets object, see [examples](../reference/Examples.md#managing-global-kubernetes-secret-object). For more information on managing kubernetes secret objects refer [kubernetes.io managing secrets](https://kubernetes.io/docs/tasks/configmap-secret/managing-secret-using-kubectl/)

## Encrypting secrets at rest

**This applies to every secret/password handling path described in this document** — the operator-managed global secret object, CR-scoped versioned secrets, and any value populated manually or through an external secret manager (see [ESO](#populating-the-global-secret-object-using-external-secrets-operator-eso) below). It is not specific to ESO.

By default, Kubernetes `Secret` objects are only **base64-encoded**, not encrypted, when they are written to etcd. Base64 is an encoding, not encryption — anyone with read access to the etcd data store, an etcd snapshot/backup, or a `kubectl get secret -o yaml` on a sufficiently privileged identity can trivially recover the plaintext `hec_token`, `password`, `pass4SymmKey`, `idxc_secret`, and `shc_secret` values that this operator relies on.

**Hard requirement:** any cluster running Splunk Operator in production must enable encryption at rest for Secret resources before secrets are populated.

## Populating the global secret object using External Secrets Operator (ESO)

As noted above, an administrator can pre-populate the global kubernetes secret object before creating any Splunk Enterprise CRs, and the operator will use those values instead of generating its own. Rather than creating and updating that secret manually with `kubectl`, you can use the [External Secrets Operator (ESO)](https://external-secrets.io/) to sync secret values from an external secret manager (Azure Key Vault, HashiCorp Vault, AWS Secrets Manager, GCP Secret Manager, etc.) into the namespace as a native kubernetes `Secret`. This is useful when your organization already manages credentials centrally and wants Splunk Enterprise deployments to consume rotated values automatically instead of tracking them separately in Kubernetes.

### How ESO fits into the secret contract

ESO does not talk to the Splunk Operator directly. It only creates or updates a kubernetes `Secret` object. As far as the Splunk Operator is concerned, an ESO-managed secret is indistinguishable from one created by `kubectl` — the same contract described above applies:

1. The `Secret` must be named `splunk-<namespace>-secret`.
2. It should contain the [Splunk Secret Tokens](#splunk-secret-tokens-in-the-global-secret-object) you want to control: `hec_token`, `password`, `pass4SymmKey`, `idxc_secret`, `shc_secret`.
3. Any token you don't supply is auto-generated by the operator on first reconcile, as usual.

### Prerequisites

- ESO installed in the cluster.
- A `SecretStore` (or cluster-scoped `ClusterSecretStore`) configured for your external secret manager, with the appropriate authentication (service principal, IAM role, Kubernetes auth role, etc.) already validated independently of Splunk.
- The external secret manager already seeded with the values you want to control (at minimum `password`; add the others as needed).

**Note:** `hec_token` must be exactly 36 characters (UUID format), or the operator will reject the secret with `validation failed for secret hec_token: hec token length must be 36`. 

### Least privilege and namespace isolation

Every `SecretStore` (or `ClusterSecretStore`) authenticates to the external secret manager as some identity, and that identity's backing policy/role — not the Kubernetes namespace the `SecretStore` happens to live in — is what actually controls which secret paths an `ExternalSecret` can read. Treat the following as hard requirements, not optional hardening:

- **Scope the backing policy to the one namespace it's for — never a single broad wildcard path shared across namespaces.** If you deploy Splunk into more than one namespace, give each its own role/policy pair rather than reusing one across namespaces.
- **Grant read-only access.** The policy/role backing the `SecretStore` should never have write/delete permission on the secret path — ESO only reads.

**Follow the official ESO documentation to install ESO and configure a `SecretStore`/`ClusterSecretStore` for your provider** — see the [External Secrets Operator guides](https://external-secrets.io/latest/introduction/getting-started/) and the [provider list](https://external-secrets.io/latest/provider/aws-secrets-manager/) (Vault, Azure Key Vault, AWS Secrets Manager, GCP Secret Manager, etc.) for install steps, authentication options, and provider-specific fields. The example below shows only how the resulting `SecretStore`/`ExternalSecret` need to be shaped to satisfy the SOK secret contract; it is not a substitute for the official setup/authentication guide.

### Example: SecretStore and ExternalSecret (HashiCorp Vault)

The `ExternalSecret`'s `target.name` must resolve to `splunk-<namespace>-secret`, and its `data[].secretKey` entries must use the exact token key names from the [contract](#splunk-secret-tokens-in-the-global-secret-object) above — the `remoteRef` on the right-hand side maps to whatever key/path convention your external secret manager uses.

The example below uses the generic `vault` provider with Kubernetes auth; the same pattern applies to `azurekv`, `awssm`, `gcpsm`, and other ESO providers — only the `spec.provider` block changes. The Vault role `eso-sok` here is scoped, per the [previous section](#least-privilege-and-namespace-isolation), to a read-only policy on this one namespace's path — do not reuse the same role/policy across other namespaces.

```yaml
apiVersion: external-secrets.io/v1
kind: SecretStore
metadata:
  name: vault-store
  namespace: <namespace>
spec:
  provider:
    vault:
      server: "https://vault.vault.svc:8200"
      path: "kv-splunk-secrets"
      version: "v2"
      caProvider:
        type: Secret
        name: vault-ca-cert
        namespace: <namespace>
        key: ca.crt
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "eso-sok"
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: splunk-namespace-secret
  namespace: <namespace>
spec:
  refreshInterval: 30s
  secretStoreRef:
    kind: SecretStore
    name: vault-store
  target:
    name: splunk-<namespace>-secret
    creationPolicy: Owner
    template:
      type: Opaque
  data:
    - secretKey: hec_token
      remoteRef: {key: splunk/<namespace>, property: hec_token}
    - secretKey: password
      remoteRef: {key: splunk/<namespace>, property: password}
    - secretKey: pass4SymmKey
      remoteRef: {key: splunk/<namespace>, property: pass4SymmKey}
    - secretKey: idxc_secret
      remoteRef: {key: splunk/<namespace>, property: idxc_secret}
    - secretKey: shc_secret
      remoteRef: {key: splunk/<namespace>, property: shc_secret}
```

Apply the `SecretStore` and `ExternalSecret` **before** creating any Splunk Enterprise CR in the namespace, so the operator picks up the pre-populated secret on its first reconcile instead of generating its own.

### Validating the sync

Confirm the `ExternalSecret` is healthy and the target secret contains the expected keys:

```bash
kubectl -n <namespace> wait externalsecret/splunk-namespace-secret --for=condition=Ready --timeout=180s
kubectl -n <namespace> get secret splunk-<namespace>-secret
```

Then create your Splunk Enterprise CR as usual, and verify the value propagated all the way to the CR-scoped secret and the running pod:

```bash
kubectl -n <namespace> get secret splunk-<namespace>-secret -o jsonpath='{.data.password}' | base64 --decode && echo
kubectl -n <namespace> get secret splunk-<cr-name>-<kind>-secret-v1 -o jsonpath='{.data.password}' | base64 --decode && echo
kubectl -n <namespace> exec <pod-name> -- cat /mnt/splunk-secrets/password
```

All three values should match.

### Rotating a password through ESO

To rotate a token, update the value in your external secret manager only — never edit the ESO-managed `Secret` or the operator-generated versioned secrets directly, per the same rule that applies to manually-managed secrets (see [Information for Splunk Enterprise administrator](#information-for-splunk-enterprise-administrator)).

1. Update the value at the source (e.g. `vault kv put kv-splunk-secrets/splunk/<namespace> password=<new_value> ...`, re-supplying unchanged keys since most KV backends replace the entire value on write).
2. ESO picks up the change within its `refreshInterval` (30s in the example above) and updates `splunk-<namespace>-secret` automatically.
3. The operator detects the change to the global secret object and creates a new versioned secret (`...-secret-v2`) for each affected CR, then performs a rolling restart of the affected pods to pick up the change.
4. Confirm the rotated value is live by checking the versioned secret and the pod mount, as shown above, and by authenticating against the running instance with the new password.

Depending on the operator version, the update to the global secret may need to be picked up by the next regular reconcile of the CR. If a CR has not moved to the new secret version after ESO has synced the change, you can force a reconcile by patching a benign annotation on the CR, for example:

```bash
kubectl -n <namespace> annotate standalone <cr-name> secret-rotation="$(date +%s)" --overwrite
```

### Gotchas

- **`ServiceAccountRef` on a namespaced `SecretStore` is restricted to its own namespace — and this is a security control, not just a webhook quirk.** ESO's admission webhook rejects a namespaced `SecretStore` whose `auth.<provider>.serviceAccountRef.namespace` doesn't match the `SecretStore`'s own namespace, which prevents one namespace from borrowing another namespace's ServiceAccount identity to authenticate to the provider. If your provider identity is instead bound to the ESO controller's own ServiceAccount (e.g. a Vault Kubernetes-auth role with `bound_service_account_namespaces` set to the `external-secrets` namespace), omit `serviceAccountRef` entirely — ESO will use its controller pod identity automatically. Either way, because the resulting identity is shared across whatever `SecretStore`/`ExternalSecret` objects can reach it, who can create/edit those objects is itself access control — see [Least privilege and namespace isolation](#least-privilege-and-namespace-isolation).
- **Always use `https://` and a `caProvider` for the `SecretStore`'s `server`/endpoint field.** A plaintext `http://` endpoint (easy to reach for in a dev/test writeup) exposes the Vault token and the secret values themselves in transit. Validate the external secret manager's TLS certificate via `caProvider` (or the provider-specific equivalent) rather than disabling verification.
- **`hec_token` length.** As noted in [Prerequisites](#prerequisites), it must be exactly 36 characters or CR creation will fail validation.
- **Never rely on the manual annotation step as the only rotation trigger.** Some operator versions reconcile the global secret change and roll out a new versioned secret automatically without any annotation; treat the annotation as a fallback for triggering a deterministic reconcile, not a required step.
- **Order of operations matters.** Create the `SecretStore`/`ExternalSecret` and confirm the target secret exists with all desired keys before creating the Splunk Enterprise CR. If the CR is created first, the operator will have already generated its own values for any missing tokens.

## Information for Splunk Enterprise administrator

- The default administrator account cannot be disabled on any Splunk Enterprise instance. The kubernetes operator uses this account to interact with all Splunk Enterprise instances in the namespace.
- The passwords managed using the global kubernetes secret object should never be changed using Splunk Enterprise tools (CLI, UI.)
- The default administrator account must use the global kubernetes secret object for any password changes. See [managing global kubernetes secret object](../reference/Examples.md#managing-global-kubernetes-secret-object)
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
