# Splunk LDAP Authentication on Kubernetes - Reference Architecture

## Table of Contents
1. [Overview](#overview)
2. [Architecture Diagram](#architecture-diagram)
3. [Prerequisites](#prerequisites)
4. [Component Overview](#component-overview)
5. [Step-by-Step Implementation](#step-by-step-implementation)
6. [Testing and Verification](#testing-and-verification)
7. [Troubleshooting](#troubleshooting)
8. [Production Considerations](#production-considerations)

---

## Overview

This document provides a complete reference architecture for implementing LDAP authentication in Splunk Enterprise running on Kubernetes, managed by the Splunk Operator for Kubernetes.

### What This Guide Covers

- Deploying a test OpenLDAP server in Kubernetes
- Configuring Splunk LDAP authentication using App Framework
- Setting up IRSA (IAM Roles for Service Accounts) for secure S3 access
- Role mapping from LDAP groups to Splunk roles
- End-to-end testing and verification

### Use Cases

- **Development/Testing**: Use OpenLDAP as a local Identity Provider (IDP)
- **Production**: Replace OpenLDAP configuration with your enterprise LDAP/Active Directory
- **Proof of Concept**: Demonstrate LDAP integration with Splunk on Kubernetes

---

## Architecture Diagram

### Deployment Architecture

```d2
direction: right

user: User {
  shape: person
  style.fill: "#E8F5E9"
}

splunk_pod: Splunk Enterprise Pod {
  shape: rectangle
  style.fill: "#E3F2FD"

  splunk_app: Splunk Process {
    shape: hexagon
  }

  auth_config: authentication.conf {
    shape: document
    style.fill: "#FFF9C4"
  }

  app_dir: /opt/splunk/etc/apps/\nldap-auth-config-app/ {
    shape: stored_data
  }
}

operator_pod: Splunk Operator Pod {
  shape: rectangle
  style.fill: "#F3E5F5"

  controller: Controller Manager {
    shape: hexagon
  }

  app_framework: App Framework\nScheduler {
    shape: diamond
  }
}

ldap_pod: OpenLDAP Pod {
  shape: rectangle
  style.fill: "#FFF3E0"

  ldap_service: LDAP Service {
    shape: cylinder
  }

  users: Users OU {
    shape: stored_data
  }

  groups: Groups OU {
    shape: stored_data
  }
}

s3: AWS S3 Bucket {
  shape: cloud
  style.fill: "#FFEBEE"

  ldap_app: ldap-auth-config-app.tgz {
    shape: document
  }
}

iam: AWS IAM {
  shape: cloud
  style.fill: "#FCE4EC"

  role: IAM Role\n(IRSA) {
    shape: hexagon
  }

  trust_policy: Trust Policy\n(OIDC) {
    shape: document
  }
}

k8s: Kubernetes Cluster {
  shape: rectangle
  style.stroke-dash: 3

  sa_operator: ServiceAccount\nsplunk-operator-\ncontroller-manager {
    shape: page
  }

  sa_splunk: ServiceAccount\nsplunk-app-\ns3-reader {
    shape: page
  }
}

# User authentication flow
user -> splunk_pod.splunk_app: 1. Login with\nLDAP credentials {
  style.stroke: "#4CAF50"
  style.stroke-width: 3
}

splunk_pod.auth_config -> splunk_pod.splunk_app: 2. Read LDAP\nconfig {
  style.stroke: "#2196F3"
}

splunk_pod.splunk_app -> ldap_pod.ldap_service: 3. Authenticate\nuser {
  style.stroke: "#FF9800"
  style.stroke-width: 3
}

ldap_pod.ldap_service -> ldap_pod.users: 4. Validate\ncredentials {
  style.stroke: "#9C27B0"
}

ldap_pod.ldap_service -> ldap_pod.groups: 5. Get group\nmembership {
  style.stroke: "#9C27B0"
}

ldap_pod.ldap_service -> splunk_pod.splunk_app: 6. Return user\n+ groups {
  style.stroke: "#FF9800"
  style.stroke-width: 3
}

splunk_pod.splunk_app -> user: 7. Grant access\nwith mapped role {
  style.stroke: "#4CAF50"
  style.stroke-width: 3
}

# App Framework deployment flow
operator_pod.controller -> operator_pod.app_framework: Monitor\nStandalone CR {
  style.stroke: "#607D8B"
}

operator_pod.app_framework -> k8s.sa_operator: Use IRSA\ncredentials {
  style.stroke: "#795548"
}

k8s.sa_operator -> iam.role: Assume\nrole {
  style.stroke: "#795548"
}

iam.trust_policy -> iam.role: OIDC\nvalidation {
  style.stroke: "#E91E63"
}

operator_pod.app_framework -> s3: Download\nLDAP app {
  style.stroke: "#00BCD4"
  style.stroke-width: 2
}

s3 -> operator_pod.app_framework: Return\napp package {
  style.stroke: "#00BCD4"
}

operator_pod.app_framework -> splunk_pod.app_dir: Install\napp {
  style.stroke: "#3F51B5"
  style.stroke-width: 2
}

splunk_pod.app_dir -> splunk_pod.auth_config: Contains {
  style.stroke: "#2196F3"
}
```

### Component Flow

```d2
direction: down

title: LDAP Authentication Flow {
  style.font-size: 24
  style.bold: true
}

step1: User Login Attempt {
  shape: rectangle
  style.fill: "#E8F5E9"
}

step2: Splunk Reads\nauthentication.conf {
  shape: rectangle
  style.fill: "#E3F2FD"
}

step3: Connect to\nLDAP Server {
  shape: rectangle
  style.fill: "#FFF9C4"
}

step4: Bind with\nService Account {
  shape: diamond
  style.fill: "#FFECB3"
}

step5: Search User\nin userBaseDN {
  shape: rectangle
  style.fill: "#FFF3E0"
}

step6: Validate\nPassword {
  shape: diamond
  style.fill: "#FFE0B2"
}

step7: Get User's\nGroup Membership {
  shape: rectangle
  style.fill: "#FFCCBC"
}

step8: Map Groups\nto Splunk Roles {
  shape: rectangle
  style.fill: "#D1C4E9"
}

step9: Grant Access {
  shape: rectangle
  style.fill: "#C5E1A5"
}

error: Authentication\nFailed {
  shape: rectangle
  style.fill: "#FFCDD2"
}

step1 -> step2
step2 -> step3
step3 -> step4
step4 -> step5: Success
step4 -> error: Bind Failed
step5 -> step6: User Found
step5 -> error: User Not Found
step6 -> step7: Password Valid
step6 -> error: Invalid Password
step7 -> step8
step8 -> step9
```

---

## Prerequisites

### Required Tools

- `kubectl` - Kubernetes CLI
- `aws` CLI - AWS command-line interface
- `tar` - For packaging Splunk apps
- Access to AWS EKS cluster with OIDC provider enabled

### Required Permissions

- AWS IAM permissions to create roles and policies
- Kubernetes cluster-admin or namespace admin permissions
- S3 bucket access for storing Splunk apps

### Existing Resources

- Splunk Operator installed in cluster
- S3 bucket for App Framework storage
- EKS cluster with OIDC provider configured

---

## Component Overview

### 1. OpenLDAP Server

A lightweight LDAP server for testing purposes. Contains:
- Test users with different role assignments
- Organizational Units (OUs) for users and groups
- Group memberships for role mapping

### 2. Splunk Operator

Manages Splunk Enterprise deployment:
- Downloads apps from S3 using IRSA
- Installs apps in Splunk pods
- Manages configuration lifecycle

### 3. LDAP Authentication App

A Splunk app containing `authentication.conf`:
- LDAP server connection settings
- User/group search base DNs
- Role mapping configuration

### 4. IAM Role (IRSA)

AWS IAM role for secure S3 access:
- Trusted by Kubernetes ServiceAccounts via OIDC
- Grants S3 read permissions
- No static credentials required

---

## Step-by-Step Implementation

### Step 1: Deploy OpenLDAP Server

Create OpenLDAP deployment with test data.

**File:** `openldap-deployment.yaml`

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: openldap-bootstrap
  namespace: splunk-operator
data:
  bootstrap.ldif: |
    dn: ou=users,dc=splunktest,dc=local
    objectClass: organizationalUnit
    ou: users

    dn: ou=groups,dc=splunktest,dc=local
    objectClass: organizationalUnit
    ou: groups

    dn: uid=john.doe,ou=users,dc=splunktest,dc=local
    objectClass: inetOrgPerson
    objectClass: posixAccount
    objectClass: shadowAccount
    uid: john.doe
    cn: John Doe
    sn: Doe
    givenName: John
    mail: john.doe@splunktest.local
    displayName: John Doe
    uidNumber: 10001
    gidNumber: 10001
    homeDirectory: /home/john.doe
    userPassword: SplunkAdmin123

    dn: uid=jane.smith,ou=users,dc=splunktest,dc=local
    objectClass: inetOrgPerson
    objectClass: posixAccount
    objectClass: shadowAccount
    uid: jane.smith
    cn: Jane Smith
    sn: Smith
    givenName: Jane
    mail: jane.smith@splunktest.local
    displayName: Jane Smith
    uidNumber: 10002
    gidNumber: 10002
    homeDirectory: /home/jane.smith
    userPassword: SplunkPower123

    dn: uid=bob.user,ou=users,dc=splunktest,dc=local
    objectClass: inetOrgPerson
    objectClass: posixAccount
    objectClass: shadowAccount
    uid: bob.user
    cn: Bob User
    sn: User
    givenName: Bob
    mail: bob.user@splunktest.local
    displayName: Bob User
    uidNumber: 10003
    gidNumber: 10003
    homeDirectory: /home/bob.user
    userPassword: SplunkUser123

    dn: cn=Splunk Admins,ou=groups,dc=splunktest,dc=local
    objectClass: groupOfNames
    cn: Splunk Admins
    member: uid=john.doe,ou=users,dc=splunktest,dc=local

    dn: cn=Splunk Power Users,ou=groups,dc=splunktest,dc=local
    objectClass: groupOfNames
    cn: Splunk Power Users
    member: uid=jane.smith,ou=users,dc=splunktest,dc=local

    dn: cn=Splunk Users,ou=groups,dc=splunktest,dc=local
    objectClass: groupOfNames
    cn: Splunk Users
    member: uid=bob.user,ou=users,dc=splunktest,dc=local

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: openldap
  namespace: splunk-operator
  labels:
    app: openldap
spec:
  replicas: 1
  selector:
    matchLabels:
      app: openldap
  template:
    metadata:
      labels:
        app: openldap
    spec:
      containers:
      - name: openldap
        image: osixia/openldap:1.5.0
        args: ["--copy-service", "--loglevel", "debug"]
        env:
        - name: LDAP_ORGANISATION
          value: "Splunk Test"
        - name: LDAP_DOMAIN
          value: "splunktest.local"
        - name: LDAP_ADMIN_PASSWORD
          value: "admin"
        - name: LDAP_CONFIG_PASSWORD
          value: "config"
        - name: LDAP_TLS
          value: "false"
        - name: LDAP_SEED_INTERNAL_LDIF_PATH
          value: "/bootstrap/"
        ports:
        - name: ldap
          containerPort: 389
          protocol: TCP
        volumeMounts:
        - name: bootstrap
          mountPath: /bootstrap
          readOnly: true
        - name: data
          mountPath: /var/lib/ldap
        - name: config
          mountPath: /etc/ldap/slapd.d
        readinessProbe:
          tcpSocket:
            port: 389
          initialDelaySeconds: 15
          periodSeconds: 10
        livenessProbe:
          tcpSocket:
            port: 389
          initialDelaySeconds: 20
          periodSeconds: 10
      volumes:
      - name: bootstrap
        configMap:
          name: openldap-bootstrap
      - name: data
        emptyDir: {}
      - name: config
        emptyDir: {}

---
apiVersion: v1
kind: Service
metadata:
  name: openldap
  namespace: splunk-operator
  labels:
    app: openldap
spec:
  type: ClusterIP
  ports:
  - name: ldap
    port: 389
    targetPort: 389
    protocol: TCP
  selector:
    app: openldap
```

**Deploy OpenLDAP:**

```bash
kubectl apply -f openldap-deployment.yaml
```

**Verify OpenLDAP is running:**

```bash
# Check pod status
kubectl get pods -n splunk-operator -l app=openldap

# Expected output: Running with 1/1 ready
# NAME                        READY   STATUS    RESTARTS   AGE
# openldap-xxxxxxxxxx-xxxxx   1/1     Running   0          2m
```

**Verify LDAP service:**

```bash
# Check service endpoint
kubectl get svc -n splunk-operator openldap

# Expected output:
# NAME       TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)   AGE
# openldap   ClusterIP   10.100.xxx.xxx   <none>        389/TCP   2m
```

**Verify test users exist:**

```bash
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=users,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(uid=*)" uid cn mail

# Expected output: Should show john.doe, jane.smith, bob.user
```

---

### Step 2: Create LDAP Authentication App

Create a Splunk app with LDAP configuration.

**Create app directory structure:**

```bash
mkdir -p /tmp/ldap-auth-config-app/local
mkdir -p /tmp/ldap-auth-config-app/metadata
mkdir -p /tmp/ldap-auth-config-app/default
```

**File:** `/tmp/ldap-auth-config-app/default/app.conf`

```ini
[install]
is_configured = true
state = enabled

[ui]
is_visible = false
label = LDAP Authentication Configuration

[launcher]
author = Splunk Administrator
description = LDAP authentication configuration for Splunk Enterprise
version = 1.0.0
```

**File:** `/tmp/ldap-auth-config-app/local/authentication.conf`

```ini
# LDAP Authentication Configuration
# This file configures LDAP authentication for Splunk Enterprise
# Deployed via Splunk Operator App Framework

[authentication]
authType = LDAP
authSettings = corporate-ldap

[corporate-ldap]
# LDAP Server Configuration - Test OpenLDAP
SSLEnabled = 0
host = openldap.splunk-operator.svc.cluster.local
port = 389

# Bind Configuration
bindDN = cn=admin,dc=splunktest,dc=local
bindDNpassword = admin

# User Configuration
userBaseDN = ou=users,dc=splunktest,dc=local
userBaseFilter = (objectclass=inetOrgPerson)
userNameAttribute = uid

# Group Configuration
groupBaseDN = ou=groups,dc=splunktest,dc=local
groupBaseFilter = (objectclass=groupOfNames)
groupMemberAttribute = member
groupNameAttribute = cn
groupMappingAttribute = dn

# Additional Attributes
emailAttribute = mail
realNameAttribute = displayName
charset = utf8
timelimit = 15

# Role Mapping
# Users in the "Splunk Admins" LDAP group will be mapped to Splunk admin role
[roleMap_corporate-ldap]
admin = Splunk Admins
power = Splunk Power Users
user = Splunk Users
```

**For Production LDAP/Active Directory, modify these settings:**

```ini
# Example Active Directory Configuration
[corporate-ldap]
SSLEnabled = 1
host = ad.company.com
port = 636

# Use service account for binding
bindDN = CN=SplunkService,OU=ServiceAccounts,DC=company,DC=com
bindDNpassword = <your-service-account-password>

# User search configuration
userBaseDN = OU=Users,DC=company,DC=com
userBaseFilter = (&(objectClass=user)(!(objectClass=computer)))
userNameAttribute = sAMAccountName

# Group search configuration
groupBaseDN = OU=Groups,DC=company,DC=com
groupBaseFilter = (objectClass=group)
groupMemberAttribute = member
groupNameAttribute = cn

# Map AD groups to Splunk roles
[roleMap_corporate-ldap]
admin = CN=Splunk-Admins,OU=Groups,DC=company,DC=com
power = CN=Splunk-PowerUsers,OU=Groups,DC=company,DC=com
user = CN=Splunk-Users,OU=Groups,DC=company,DC=com
```

**Package the app:**

```bash
cd /tmp
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/

# Verify package created
ls -lh ldap-auth-config-app.tgz
```

---

### Step 3: Configure AWS IAM for IRSA

Set up IAM role for secure S3 access from Kubernetes.

**Get your EKS OIDC provider:**

```bash
# Replace with your cluster name and region
CLUSTER_NAME="your-cluster-name"
REGION="us-west-2"
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

# Get OIDC provider
OIDC_PROVIDER=$(aws eks describe-cluster \
  --name $CLUSTER_NAME \
  --region $REGION \
  --query "cluster.identity.oidc.issuer" \
  --output text | sed -e "s/^https:\/\///")

echo "OIDC Provider: $OIDC_PROVIDER"
```

**Create IAM trust policy:**

**File:** `/tmp/trust-policy.json`

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Federated": "arn:aws:iam::ACCOUNT_ID:oidc-provider/OIDC_PROVIDER"
    },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "OIDC_PROVIDER:sub": [
          "system:serviceaccount:splunk-operator:splunk-app-s3-reader",
          "system:serviceaccount:splunk-operator:splunk-operator-controller-manager"
        ],
        "OIDC_PROVIDER:aud": "sts.amazonaws.com"
      }
    }
  }]
}
```

**Replace placeholders:**

```bash
sed -i '' "s|ACCOUNT_ID|$ACCOUNT_ID|g" /tmp/trust-policy.json
sed -i '' "s|OIDC_PROVIDER|$OIDC_PROVIDER|g" /tmp/trust-policy.json

# Verify trust policy
cat /tmp/trust-policy.json
```

**Create S3 access policy:**

**File:** `/tmp/s3-policy.json`

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::YOUR-BUCKET-NAME",
        "arn:aws:s3:::YOUR-BUCKET-NAME/*"
      ]
    }
  ]
}
```

**Replace bucket name:**

```bash
BUCKET_NAME="your-bucket-name"
sed -i '' "s|YOUR-BUCKET-NAME|$BUCKET_NAME|g" /tmp/s3-policy.json
```

**Create IAM role and policies:**

```bash
# Create IAM role
aws iam create-role \
  --role-name splunk-ldap-s3-read \
  --assume-role-policy-document file:///tmp/trust-policy.json

# Create S3 policy
aws iam create-policy \
  --policy-name SplunkLDAPAppS3ReadPolicy \
  --policy-document file:///tmp/s3-policy.json

# Attach policy to role
POLICY_ARN="arn:aws:iam::${ACCOUNT_ID}:policy/SplunkLDAPAppS3ReadPolicy"
aws iam attach-role-policy \
  --role-name splunk-ldap-s3-read \
  --policy-arn $POLICY_ARN

# Get role ARN
ROLE_ARN=$(aws iam get-role \
  --role-name splunk-ldap-s3-read \
  --query 'Role.Arn' \
  --output text)

echo "IAM Role ARN: $ROLE_ARN"
```

---

### Step 4: Upload LDAP App to S3

Upload the packaged app to your S3 bucket.

```bash
# Upload app to S3
S3_BUCKET="your-bucket-name"
S3_PREFIX="splunk-apps/authAppsLoc"

aws s3 cp /tmp/ldap-auth-config-app.tgz \
  s3://${S3_BUCKET}/${S3_PREFIX}/ldap-auth-config-app.tgz

# Verify upload
aws s3 ls s3://${S3_BUCKET}/${S3_PREFIX}/
```

---

### Step 5: Create Splunk Standalone Custom Resource

Create Standalone CR with App Framework configuration.

**File:** `splunk-standalone-ldap.yaml`

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: splunk-ldap-test
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1

  # Splunk image configuration
  image: splunk/splunk:10.0.0
  imagePullPolicy: IfNotPresent

  # License configuration (use your license or free trial)
  licenseUrl: /mnt/licenses/enterprise.lic

  # ServiceAccount with IRSA annotation
  serviceAccount: splunk-app-s3-reader

  # App Framework configuration
  appRepo:
    appSources:
      - name: authApps
        location: authAppsLoc/
        scope: local
        volumeName: auth-apps

    defaults:
      volumeName: app-defaults
      scope: local

    volumes:
      - name: auth-apps
        storageType: s3
        provider: aws
        path: YOUR-BUCKET-NAME
        endpoint: https://s3.us-west-2.amazonaws.com
        region: us-west-2

      - name: app-defaults
        storageType: s3
        provider: aws
        path: YOUR-BUCKET-NAME
        endpoint: https://s3.us-west-2.amazonaws.com
        region: us-west-2

  # Resources
  resources:
    requests:
      memory: "4Gi"
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"

  # Storage
  etcVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 10Gi

  varVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 100Gi
```

**Update ServiceAccount annotations:**

```bash
# Annotate ServiceAccount with IAM role
kubectl annotate serviceaccount -n splunk-operator splunk-app-s3-reader \
  eks.amazonaws.com/role-arn=$ROLE_ARN --overwrite

# Verify annotation
kubectl get serviceaccount -n splunk-operator splunk-app-s3-reader -o yaml | grep role-arn
```

**Apply Standalone CR:**

```bash
# Replace bucket name in YAML
sed -i '' "s|YOUR-BUCKET-NAME|$BUCKET_NAME|g" splunk-standalone-ldap.yaml

# Apply configuration
kubectl apply -f splunk-standalone-ldap.yaml
```

**Monitor deployment:**

```bash
# Watch pod creation
kubectl get pods -n splunk-operator -w

# Check operator logs for app download
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f
```

---

### Step 6: Verify App Framework Installation

Check that the LDAP app was downloaded and installed.

**Wait for Splunk pod to be ready:**

```bash
# Wait for pod to be Running
kubectl wait --for=condition=Ready \
  pod/splunk-ldap-test-standalone-0 \
  -n splunk-operator \
  --timeout=600s
```

**Check operator logs:**

```bash
# Look for app download success
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager \
  | grep -i "ldap-auth-config-app"

# Should see messages like:
# "Successfully downloaded app: ldap-auth-config-app.tgz"
# "App installation completed"
```

**Verify app is installed in Splunk:**

```bash
# Check app directory exists
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  ls -la /opt/splunk/etc/apps/ | grep ldap

# Verify authentication.conf exists
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  ls -la /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf
```

**Verify Splunk recognizes LDAP configuration:**

```bash
# Check authentication configuration
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list authentication

# Expected output:
# [authentication]
# authSettings = corporate-ldap
# authType = LDAP
```

---

## Testing and Verification

### Test 1: Verify LDAP Connectivity

Test network connectivity from Splunk to OpenLDAP.

```bash
# Test LDAP connection from Splunk pod
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -v telnet://openldap.splunk-operator.svc.cluster.local:389

# Expected: Connection successful
```

### Test 2: Verify LDAP Users Exist

Query OpenLDAP to verify test users.

```bash
# List all users
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=users,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(uid=*)" uid cn mail displayName

# Expected output: john.doe, jane.smith, bob.user
```

### Test 3: Verify LDAP Groups

Query OpenLDAP to verify group memberships.

```bash
# List all groups
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=groups,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(objectClass=groupOfNames)" cn member

# Expected output: Splunk Admins, Splunk Power Users, Splunk Users
```

### Test 4: Test Authentication via REST API

Test LDAP authentication for all users via Splunk REST API.

**Test admin user (john.doe):**

```bash
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -k -s -u john.doe:SplunkAdmin123 \
  https://localhost:8089/services/authentication/current-context \
  | grep -E "(username|type|roles)"

# Expected output:
# <s:key name="username">john.doe</s:key>
# <s:key name="type">LDAP</s:key>
# <s:key name="roles"><s:list><s:item>admin</s:item></s:list></s:key>
```

**Test power user (jane.smith):**

```bash
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -k -s -u jane.smith:SplunkPower123 \
  https://localhost:8089/services/authentication/current-context \
  | grep -E "(username|type|roles)"

# Expected output:
# <s:key name="username">jane.smith</s:key>
# <s:key name="type">LDAP</s:key>
# <s:key name="roles"><s:list><s:item>power</s:item></s:list></s:key>
```

**Test regular user (bob.user):**

```bash
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -k -s -u bob.user:SplunkUser123 \
  https://localhost:8089/services/authentication/current-context \
  | grep -E "(username|type|roles)"

# Expected output:
# <s:key name="username">bob.user</s:key>
# <s:key name="type">LDAP</s:key>
# <s:key name="roles"><s:list><s:item>user</s:item></s:list></s:key>
```

### Test 5: Test via Splunk Web UI

Access Splunk Web interface and login with LDAP credentials.

**Port-forward to Splunk pod:**

```bash
kubectl port-forward -n splunk-operator splunk-ldap-test-standalone-0 8000:8000
```

**Open browser:**

Navigate to: http://localhost:8000

**Try logging in with test users:**

1. **john.doe / SplunkAdmin123** → Should login with admin role
2. **jane.smith / SplunkPower123** → Should login with power role
3. **bob.user / SplunkUser123** → Should login with user role

**Verify authentication method:**

1. Go to: **Settings → Access controls → Authentication method**
2. Verify: "External auth system type: LDAP"
3. Strategy name: "corporate-ldap"

### Test Results Summary

| User | Password | LDAP Group | Expected Role | Capabilities |
|------|----------|------------|---------------|--------------|
| john.doe | SplunkAdmin123 | Splunk Admins | admin | Full administrative access |
| jane.smith | SplunkPower123 | Splunk Power Users | power | Create/edit searches, reports, dashboards |
| bob.user | SplunkUser123 | Splunk Users | user | View dashboards, run searches |

---

## Troubleshooting

### Issue 1: App Not Downloaded

**Symptoms:**
- Operator logs show "Unable to get apps list from remote storage"
- App not appearing in Splunk pod

**Diagnosis:**

```bash
# Check operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager \
  | grep -i error

# Check IAM role exists
aws iam get-role --role-name splunk-ldap-s3-read

# Check ServiceAccount annotation
kubectl get sa -n splunk-operator splunk-app-s3-reader -o yaml
```

**Solutions:**

1. **Missing IAM Role:**
   ```bash
   # Verify role exists and has correct trust policy
   aws iam get-role --role-name splunk-ldap-s3-read
   ```

2. **ServiceAccount not annotated:**
   ```bash
   # Add annotation
   kubectl annotate serviceaccount -n splunk-operator splunk-app-s3-reader \
     eks.amazonaws.com/role-arn=$ROLE_ARN --overwrite

   # Restart operator
   kubectl rollout restart deployment -n splunk-operator splunk-operator-controller-manager
   ```

3. **S3 permissions issue:**
   ```bash
   # Test S3 access from operator pod
   kubectl exec -n splunk-operator deployment/splunk-operator-controller-manager -- \
     aws s3 ls s3://${BUCKET_NAME}/${S3_PREFIX}/
   ```

---

### Issue 2: LDAP Authentication Failing

**Symptoms:**
- Users cannot login with LDAP credentials
- Splunk logs show "Can't contact LDAP server"

**Diagnosis:**

```bash
# Check Splunk logs
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  tail -100 /opt/splunk/var/log/splunk/splunkd.log | grep -i ldap

# Test network connectivity
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -v telnet://openldap.splunk-operator.svc.cluster.local:389
```

**Solutions:**

1. **OpenLDAP not ready:**
   ```bash
   # Check OpenLDAP pod status
   kubectl get pods -n splunk-operator -l app=openldap

   # If not ready, check logs
   kubectl logs -n splunk-operator deployment/openldap -c openldap
   ```

2. **Wrong LDAP hostname:**
   - Verify `host` in authentication.conf: `openldap.splunk-operator.svc.cluster.local`
   - For production, use your actual LDAP server hostname

3. **Bind DN credentials wrong:**
   ```bash
   # Test bind from OpenLDAP pod
   kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
     ldapsearch -x -H ldap://localhost \
     -D "cn=admin,dc=splunktest,dc=local" \
     -w admin \
     -b "dc=splunktest,dc=local"
   ```

---

### Issue 3: User Found But Wrong Role

**Symptoms:**
- User can login but has wrong Splunk role
- User should be admin but appears as user

**Diagnosis:**

```bash
# Check user's group membership in LDAP
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=groups,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(member=uid=john.doe,ou=users,dc=splunktest,dc=local)"

# Check role mapping in Splunk
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list roleMap_corporate-ldap
```

**Solutions:**

1. **Check role mapping configuration:**

   In `authentication.conf`:
   ```ini
   [roleMap_corporate-ldap]
   admin = Splunk Admins
   power = Splunk Power Users
   user = Splunk Users
   ```

   Group names must match exactly (case-sensitive).

2. **Verify user is in correct LDAP group:**
   ```bash
   # Check group membership
   kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
     ldapsearch -x -H ldap://localhost \
     -b "ou=groups,dc=splunktest,dc=local" \
     -D "cn=admin,dc=splunktest,dc=local" \
     -w admin \
     "(cn=Splunk Admins)"
   ```

---

### Issue 4: OpenLDAP Pod CrashLooping

**Symptoms:**
- OpenLDAP pod shows `CrashLoopBackOff` or `Error` status
- Init container failing

**Diagnosis:**

```bash
# Check pod status
kubectl get pods -n splunk-operator -l app=openldap

# Check logs
kubectl logs -n splunk-operator deployment/openldap -c openldap --tail=50
```

**Common Causes:**

1. **Domain configuration error:**
   - Check `LDAP_DOMAIN` environment variable
   - Should be `splunktest.local` (not `splunktest.dc=local`)

2. **Bootstrap LDIF errors:**
   ```bash
   # Check ConfigMap
   kubectl get configmap -n splunk-operator openldap-bootstrap -o yaml

   # Verify LDIF syntax is correct
   ```

3. **Volume mount issues:**
   - Ensure emptyDir volumes are properly configured
   - Check for read-only mount conflicts

**Solution:**

```bash
# Delete and recreate OpenLDAP deployment
kubectl delete -f openldap-deployment.yaml
kubectl apply -f openldap-deployment.yaml

# Watch pod start up
kubectl get pods -n splunk-operator -l app=openldap -w
```

---

### Issue 5: AWS Credentials Conflict

**Symptoms:**
- Operator cannot access S3
- Errors about invalid AWS credentials

**Solution:**

```bash
# Unset environment AWS credentials before kubectl commands
unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_SESSION_TOKEN
unset AWS_PROFILE

# Then run kubectl commands
kubectl get pods -n splunk-operator
```

---

## Production Considerations

### Security Best Practices

1. **Use SSL/TLS for LDAP:**
   ```ini
   [corporate-ldap]
   SSLEnabled = 1
   port = 636
   ```

2. **Use Kubernetes Secrets for Passwords:**
   - Store `bindDNpassword` in Kubernetes Secret
   - Reference in authentication.conf via environment variable

3. **Least Privilege IAM:**
   - Only grant minimum required S3 permissions
   - Use bucket policies to restrict access

4. **Network Policies:**
   ```yaml
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: splunk-ldap-policy
     namespace: splunk-operator
   spec:
     podSelector:
       matchLabels:
         app.kubernetes.io/component: standalone
     policyTypes:
     - Egress
     egress:
     - to:
       - podSelector:
           matchLabels:
             app: openldap
       ports:
       - protocol: TCP
         port: 389
   ```

### High Availability

1. **Multiple LDAP Servers:**
   ```ini
   [corporate-ldap]
   host = ldap1.company.com;ldap2.company.com;ldap3.company.com
   ```

2. **Splunk Clustering:**
   - Use ClusterManager and SearchHeadCluster CRs
   - Deploy LDAP app to all search heads

3. **Persistent Storage:**
   - Use persistent volumes for Splunk data
   - Regular backups of LDAP authentication config

### Monitoring

1. **Authentication Metrics:**
   ```bash
   # Monitor failed login attempts
   kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
     /opt/splunk/bin/splunk search \
     'index=_internal source=*splunkd.log "authentication failed"' \
     -auth admin:password
   ```

2. **LDAP Server Health:**
   ```bash
   # Check LDAP connectivity
   kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
     curl -v telnet://ldap.company.com:636
   ```

3. **Operator Health:**
   ```bash
   # Monitor operator logs
   kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f
   ```

### Backup and Recovery

1. **Backup LDAP Config:**
   ```bash
   # Export authentication.conf
   kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
     cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf \
     > authentication-backup.conf
   ```

2. **Keep Local Admin Account:**
   - Always keep a local Splunk admin account enabled
   - Store credentials securely (e.g., AWS Secrets Manager)

3. **Test Rollback:**
   - Document procedure to revert to local authentication
   - Test rollback in non-production environment

### Updating LDAP Configuration

To update LDAP settings:

1. **Modify authentication.conf** in app source
2. **Repackage app:**
   ```bash
   tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/
   ```
3. **Upload to S3:**
   ```bash
   aws s3 cp ldap-auth-config-app.tgz s3://${BUCKET_NAME}/${S3_PREFIX}/
   ```
4. **Trigger app update:**
   ```bash
   # Delete app status to force redownload
   kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
     rm -rf /opt/splunk/etc/apps/ldap-auth-config-app

   # Restart Splunk pod
   kubectl delete pod splunk-ldap-test-standalone-0 -n splunk-operator
   ```

---

## Summary

This reference architecture demonstrates:

✅ **Complete LDAP integration** using Splunk Operator App Framework
✅ **Secure credential management** with IRSA (no static AWS keys)
✅ **Automated app deployment** from S3
✅ **Role-based access control** via LDAP group mapping
✅ **Test environment** with OpenLDAP for validation
✅ **Production-ready pattern** adaptable to enterprise LDAP/AD

### Key Benefits

- **Infrastructure as Code**: All configuration in YAML
- **Secure**: IRSA for AWS access, no static credentials
- **Automated**: Operator manages app lifecycle
- **Scalable**: Works with single instance or clusters
- **Flexible**: Easy to adapt for production LDAP servers

### Next Steps

1. Test with your production LDAP/Active Directory
2. Implement SSL/TLS for LDAP connection
3. Configure additional role mappings
4. Set up monitoring and alerting
5. Document runbooks for operations team

For questions or issues, refer to:
- [Splunk Operator Documentation](https://splunk.github.io/splunk-operator/)
- [Splunk LDAP Authentication Guide](https://docs.splunk.com/Documentation/Splunk/latest/Security/SetupuserauthenticationwithLDAP)
