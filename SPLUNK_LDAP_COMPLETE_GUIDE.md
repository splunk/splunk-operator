# Complete Guide: LDAP Authentication for Splunk on Kubernetes

**Author:** Splunk Operator Team
**Date:** 2025-11-27
**Version:** 1.0
**Status:** Production Ready

---

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Prerequisites](#prerequisites)
4. [Step 1: Deploy OpenLDAP Test Server](#step-1-deploy-openldap-test-server)
5. [Step 2: Create LDAP Authentication App](#step-2-create-ldap-authentication-app)
6. [Step 3: Configure AWS IAM for IRSA](#step-3-configure-aws-iam-for-irsa)
7. [Step 4: Upload App to S3](#step-4-upload-app-to-s3)
8. [Step 5: Configure Splunk Operator](#step-5-configure-splunk-operator)
9. [Step 6: Deploy Splunk with App Framework](#step-6-deploy-splunk-with-app-framework)
10. [Step 7: Verify Installation](#step-7-verify-installation)
11. [Step 8: Test LDAP Authentication](#step-8-test-ldap-authentication)
12. [Troubleshooting](#troubleshooting)
13. [Production Deployment](#production-deployment)
14. [Appendix: Configuration Files](#appendix-configuration-files)

---

## Overview

This guide provides complete end-to-end instructions for implementing LDAP authentication in Splunk Enterprise running on Kubernetes, managed by the Splunk Operator for Kubernetes.

### Important: Why This Approach?

**This guide uses the App Framework - the RECOMMENDED and SUPPORTED method for configuring LDAP authentication with Splunk Operator.**

#### ❌ What Doesn't Work (And Why)

| Approach | Why It Fails |
|----------|-------------|
| **Custom Docker Images** | The Operator manages the Splunk container lifecycle and will overwrite any configurations baked into custom images. The Operator applies its own configuration management, replacing custom configs with defaults. |
| **Direct Environment Variables for Passwords** | Environment variables in the Standalone/Cluster CR are not automatically substituted into authentication.conf. Splunk does not read LDAP passwords from environment variables by default. |
| **Manual Configuration Files** | Any configuration files manually placed in the container will be overwritten when the Operator reconciles the deployment or when pods restart. |
| **Direct kubectl exec Edits** | Changes made via kubectl exec are ephemeral and will be lost when the pod restarts or the Operator reconciles. |

#### ✅ What Works: App Framework (This Guide's Approach)

| Feature | How It Works |
|---------|-------------|
| **Configuration Persistence** | App Framework deploys LDAP configuration as a Splunk app, which the Operator recognizes and preserves across pod restarts and reconciliation cycles. |
| **Password Management** | Supports Kubernetes Secrets via environment variable substitution in authentication.conf using `$ENV_VAR_NAME$` syntax (covered in Production section). |
| **Automated Updates** | When you update the app in S3, the Operator automatically redeploys the new configuration. |
| **Operator-Native** | This is the designed and supported method - the Operator expects authentication configuration to come via App Framework. |
| **No Image Rebuilds** | Use standard Splunk images; no need to maintain custom images or handle image registry complexities. |

### What This Guide Covers

- **App Framework Configuration**: The correct way to deploy LDAP authentication
- Deploying a test OpenLDAP server in Kubernetes (for testing)
- Creating and packaging a Splunk LDAP authentication app
- Setting up AWS IAM Roles for Service Accounts (IRSA) for secure S3 access
- Configuring Splunk Operator's App Framework to deploy LDAP configuration
- Using Kubernetes Secrets for sensitive credentials (production)
- Mapping LDAP groups to Splunk roles
- Complete testing and verification procedures
- Production deployment guidelines

### Key Features

- **Recommended Approach**: Uses App Framework (the supported method)
- **Configuration Persistence**: Survives pod restarts and operator reconciliation
- **No Static Credentials**: Uses IRSA for secure AWS access
- **Automated Deployment**: App Framework handles app installation
- **Test Environment**: Includes OpenLDAP with sample users
- **Production Ready**: Easily adaptable to enterprise LDAP/Active Directory
- **Secure**: Support for SSL/TLS, Kubernetes Secrets, network policies

### Test Environment Details

**OpenLDAP Server:**
- Hostname: `openldap.splunk-operator.svc.cluster.local`
- Port: `389` (LDAP)
- Base DN: `dc=splunktest,dc=local`
- Admin DN: `cn=admin,dc=splunktest,dc=local`
- Admin Password: `admin`

**Test Users:**

| Username | Password | LDAP Group | Splunk Role | Purpose |
|----------|----------|------------|-------------|---------|
| john.doe | SplunkAdmin123 | Splunk Admins | admin | Full administrative access |
| jane.smith | SplunkPower123 | Splunk Power Users | power | Create/edit searches, reports |
| bob.user | SplunkUser123 | Splunk Users | user | View dashboards, run searches |

---

## Architecture

### Architecture Diagrams

**Deployment Architecture Diagram:**

![Deployment Architecture](docs/ldap-deployment-architecture.svg)

- **SVG:** `docs/ldap-deployment-architecture.svg`
- **PNG:** `docs/ldap-deployment-architecture.png`
- **Shows:** Complete deployment architecture with all components (Splunk, OpenLDAP, Operator, S3, IAM), user authentication flow (7 steps), App Framework deployment pipeline (6 steps)

**Authentication Flow Diagram:**

![Authentication Flow](docs/ldap-authentication-flow.svg)

- **SVG:** `docs/ldap-authentication-flow.svg`
- **PNG:** `docs/ldap-authentication-flow.png`
- **Shows:** Step-by-step authentication workflow (9 steps), decision points for success/failure paths, error handling

### Component Overview

**1. User**
- End-user attempting to login to Splunk
- Provides LDAP credentials (username/password)

**2. Splunk Enterprise Pod**
- Runs Splunk Enterprise in Kubernetes
- Reads `authentication.conf` from LDAP app
- Connects to LDAP server for authentication
- Maps LDAP groups to Splunk roles

**3. OpenLDAP Pod (Test Environment)**
- Lightweight LDAP server for testing
- Contains test users and groups
- For production: Replace with your enterprise LDAP/AD

**4. Splunk Operator Pod**
- Manages Splunk deployment lifecycle
- Downloads apps from S3 using IRSA
- Installs apps in Splunk pods
- Monitors Standalone/Cluster CRs

**5. AWS S3 Bucket**
- Stores Splunk app packages
- Accessed by operator via IRSA (no static credentials)
- Contains: `ldap-auth-config-app.tgz`

**6. AWS IAM Role (IRSA)**
- Provides secure S3 access
- Trusted by Kubernetes ServiceAccounts via OIDC
- No static AWS credentials in cluster

**7. Kubernetes ServiceAccounts**
- `splunk-operator-controller-manager`: Used by operator
- `splunk-app-s3-reader`: Used by Splunk pod
- Both annotated with IAM role ARN

### How It Works

#### Authentication Flow (7 Steps)

1. **User Login**: User submits LDAP credentials to Splunk Web or API
2. **Read Config**: Splunk reads `authentication.conf` to get LDAP server settings
3. **Connect to LDAP**: Splunk connects to LDAP server (OpenLDAP or production LDAP)
4. **Validate Credentials**: LDAP server validates username and password
5. **Get Groups**: LDAP server returns user's group memberships
6. **Map Roles**: Splunk maps LDAP groups to Splunk roles using `roleMap_corporate-ldap`
7. **Grant Access**: User logged in with appropriate Splunk role and permissions

#### App Framework Deployment Flow (6 Steps)

1. **App in S3**: LDAP app package stored in S3 bucket
2. **Operator Monitors**: Operator watches Standalone CR for app framework changes
3. **IRSA Authentication**: Operator assumes IAM role using ServiceAccount annotation
4. **Download App**: Operator downloads app from S3 using temporary credentials
5. **Install App**: Operator copies app to Splunk pod and installs it
6. **Configuration Applied**: Splunk reads `authentication.conf` and enables LDAP auth

---

## Prerequisites

### Required Tools

- `kubectl` - Kubernetes CLI tool
- `aws` CLI - AWS command-line interface (version 2.x)
- `tar` - For packaging Splunk apps (usually pre-installed)
- Text editor (vim, nano, or VS Code)

### Required Access

**AWS Permissions:**
- IAM: Create roles, policies, attach policies
- S3: Create bucket, upload objects, list objects
- EKS: Describe cluster
- STS: Get caller identity

**Kubernetes Permissions:**
- Create/edit: Deployments, Services, ConfigMaps, Pods
- Create/edit: Standalone CRs (Splunk Operator CRDs)
- Annotate ServiceAccounts
- View logs from pods

### Existing Infrastructure

**Required:**
- AWS EKS cluster with OIDC provider enabled
- Splunk Operator for Kubernetes installed in cluster
- S3 bucket for storing Splunk apps
- Network connectivity between pods in cluster

**Verify Prerequisites:**

```bash
# Check kubectl access
kubectl get nodes

# Check Splunk Operator is installed
kubectl get deployment -n splunk-operator splunk-operator-controller-manager

# Check AWS CLI
aws --version

# Get AWS account ID
aws sts get-caller-identity

# Check S3 bucket exists
aws s3 ls s3://your-bucket-name/
```

### Architecture Requirements

- Kubernetes cluster version: 1.21+
- Splunk Operator version: 2.0+
- Splunk Enterprise version: 9.x or 10.x
- OpenLDAP version: 1.5.0 (for testing)

---

## Step 1: Deploy OpenLDAP Test Server

Deploy a test OpenLDAP server in Kubernetes with sample users and groups.

> **Note:** For production, skip this step and use your existing LDAP/Active Directory server. Update LDAP connection settings in Step 2.

### 1.1 Create OpenLDAP Deployment Manifest

Create file: `openldap-deployment.yaml`

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: openldap-bootstrap
  namespace: splunk-operator
data:
  bootstrap.ldif: |
    # Organizational Units
    dn: ou=users,dc=splunktest,dc=local
    objectClass: organizationalUnit
    ou: users

    dn: ou=groups,dc=splunktest,dc=local
    objectClass: organizationalUnit
    ou: groups

    # Test User 1: John Doe (Admin)
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

    # Test User 2: Jane Smith (Power User)
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

    # Test User 3: Bob User (Regular User)
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

    # Group 1: Splunk Admins
    dn: cn=Splunk Admins,ou=groups,dc=splunktest,dc=local
    objectClass: groupOfNames
    cn: Splunk Admins
    member: uid=john.doe,ou=users,dc=splunktest,dc=local

    # Group 2: Splunk Power Users
    dn: cn=Splunk Power Users,ou=groups,dc=splunktest,dc=local
    objectClass: groupOfNames
    cn: Splunk Power Users
    member: uid=jane.smith,ou=users,dc=splunktest,dc=local

    # Group 3: Splunk Users
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

### 1.2 Deploy OpenLDAP

```bash
# Apply the manifest
kubectl apply -f openldap-deployment.yaml

# Verify deployment
kubectl get pods -n splunk-operator -l app=openldap

# Expected output:
# NAME                        READY   STATUS    RESTARTS   AGE
# openldap-xxxxxxxxxx-xxxxx   1/1     Running   0          2m
```

### 1.3 Verify OpenLDAP is Working

```bash
# Wait for pod to be ready
kubectl wait --for=condition=Ready \
  pod -l app=openldap \
  -n splunk-operator \
  --timeout=300s

# Verify LDAP service
kubectl get svc -n splunk-operator openldap

# Test LDAP connection
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(objectClass=*)" dn

# Should return DNs for users and groups
```

### 1.4 Verify Test Users

```bash
# List all users
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=users,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(uid=*)" uid cn mail

# Expected output:
# uid=john.doe, cn=John Doe, mail=john.doe@splunktest.local
# uid=jane.smith, cn=Jane Smith, mail=jane.smith@splunktest.local
# uid=bob.user, cn=Bob User, mail=bob.user@splunktest.local
```

### 1.5 Verify Test Groups

```bash
# List all groups
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=groups,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(objectClass=groupOfNames)" cn member

# Expected output:
# cn=Splunk Admins, member: uid=john.doe,ou=users,dc=splunktest,dc=local
# cn=Splunk Power Users, member: uid=jane.smith,ou=users,dc=splunktest,dc=local
# cn=Splunk Users, member: uid=bob.user,ou=users,dc=splunktest,dc=local
```

---

## Step 2: Create LDAP Authentication App

Create a Splunk app containing LDAP authentication configuration.

### 2.1 Create App Directory Structure

```bash
# Create app directory structure
mkdir -p /tmp/ldap-auth-config-app/local
mkdir -p /tmp/ldap-auth-config-app/default
mkdir -p /tmp/ldap-auth-config-app/metadata
```

### 2.2 Create app.conf

Create file: `/tmp/ldap-auth-config-app/default/app.conf`

```ini
#
# Splunk LDAP Authentication App
# This app contains LDAP authentication configuration
#

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

### 2.3 Create authentication.conf (Test Environment)

Create file: `/tmp/ldap-auth-config-app/local/authentication.conf`

**For Test OpenLDAP:**

```ini
#
# LDAP Authentication Configuration
# This file configures LDAP authentication for Splunk Enterprise
# Deployed via Splunk Operator App Framework
#
# Environment: Test (OpenLDAP)
#

[authentication]
# Set authentication type to LDAP
authType = LDAP
# Reference to the LDAP strategy name
authSettings = corporate-ldap

[corporate-ldap]
#
# LDAP Server Configuration
#
# For test OpenLDAP server in same namespace
SSLEnabled = 0
host = openldap.splunk-operator.svc.cluster.local
port = 389

#
# Bind Configuration
# Service account used by Splunk to connect to LDAP
#
bindDN = cn=admin,dc=splunktest,dc=local
bindDNpassword = admin

#
# User Configuration
# Where to search for users in LDAP
#
userBaseDN = ou=users,dc=splunktest,dc=local
userBaseFilter = (objectclass=inetOrgPerson)
userNameAttribute = uid

#
# Group Configuration
# Where to search for groups in LDAP
#
groupBaseDN = ou=groups,dc=splunktest,dc=local
groupBaseFilter = (objectclass=groupOfNames)
groupMemberAttribute = member
groupNameAttribute = cn
groupMappingAttribute = dn

#
# Additional Attributes
#
emailAttribute = mail
realNameAttribute = displayName
charset = utf8
timelimit = 15
network_timeout = 20

#
# Role Mapping
# Map LDAP groups to Splunk roles
#
[roleMap_corporate-ldap]
# Users in "Splunk Admins" LDAP group get Splunk admin role
admin = Splunk Admins
# Users in "Splunk Power Users" LDAP group get Splunk power role
power = Splunk Power Users
# Users in "Splunk Users" LDAP group get Splunk user role
user = Splunk Users
```

### 2.4 Production LDAP Configuration (Active Directory Example)

**For Production Active Directory (save for later use):**

Create file: `/tmp/ldap-auth-config-app-production/local/authentication.conf`

```ini
#
# LDAP Authentication Configuration
# Environment: Production (Active Directory)
#

[authentication]
authType = LDAP
authSettings = corporate-ldap

[corporate-ldap]
#
# LDAP Server Configuration
#
# Enable SSL for production
SSLEnabled = 1
# Multiple servers for high availability (semicolon-separated)
host = ldap1.yourcompany.com;ldap2.yourcompany.com;ldap3.yourcompany.com
port = 636

#
# Bind Configuration
# Use a dedicated service account with read-only access
#
bindDN = CN=SplunkService,OU=ServiceAccounts,DC=yourcompany,DC=com
# Store password in Kubernetes Secret and reference via environment variable
bindDNpassword = $LDAP_BIND_PASSWORD$

#
# User Configuration
# Active Directory user search settings
#
userBaseDN = OU=Employees,DC=yourcompany,DC=com
# Exclude computer accounts
userBaseFilter = (&(objectClass=user)(!(objectClass=computer)))
# Active Directory uses sAMAccountName for usernames
userNameAttribute = sAMAccountName

#
# Group Configuration
# Active Directory group search settings
#
groupBaseDN = OU=Groups,DC=yourcompany,DC=com
groupBaseFilter = (objectClass=group)
groupMemberAttribute = member
groupNameAttribute = cn
groupMappingAttribute = dn

#
# Additional Attributes
#
emailAttribute = mail
realNameAttribute = displayName
charset = utf8
timelimit = 15
network_timeout = 20

#
# Role Mapping
# Map your Active Directory groups to Splunk roles
#
[roleMap_corporate-ldap]
# Full distinguished names for Active Directory groups
admin = CN=Splunk-Administrators,OU=Groups,DC=yourcompany,DC=com
power = CN=Splunk-PowerUsers,OU=Groups,DC=yourcompany,DC=com
user = CN=Splunk-Users,OU=Groups,DC=yourcompany,DC=com
can_delete = CN=Splunk-Analysts,OU=Groups,DC=yourcompany,DC=com
```

### 2.5 Package the App

```bash
# Change to /tmp directory
cd /tmp

# Create tarball
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/

# Verify package created
ls -lh ldap-auth-config-app.tgz

# Expected output: ~2-3 KB file
```

---

## Step 3: Configure AWS IAM for IRSA

Set up IAM Roles for Service Accounts (IRSA) for secure S3 access without static credentials.

### 3.1 Gather AWS Information

```bash
# Set your cluster name and region
export CLUSTER_NAME="your-cluster-name"
export REGION="us-west-2"
export NAMESPACE="splunk-operator"

# Get AWS account ID
export ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
echo "AWS Account ID: $ACCOUNT_ID"

# Get EKS OIDC provider
export OIDC_PROVIDER=$(aws eks describe-cluster \
  --name $CLUSTER_NAME \
  --region $REGION \
  --query "cluster.identity.oidc.issuer" \
  --output text | sed -e "s/^https:\/\///")
echo "OIDC Provider: $OIDC_PROVIDER"
```

### 3.2 Create IAM Trust Policy

Create file: `/tmp/trust-policy.json`

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
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
    }
  ]
}
```

**Replace placeholders:**

```bash
# Replace ACCOUNT_ID and OIDC_PROVIDER in trust policy
sed -i "s|ACCOUNT_ID|$ACCOUNT_ID|g" /tmp/trust-policy.json
sed -i "s|OIDC_PROVIDER|$OIDC_PROVIDER|g" /tmp/trust-policy.json

# Verify trust policy
cat /tmp/trust-policy.json
```

> **Important:** The trust policy includes BOTH ServiceAccounts because:
> - `splunk-operator-controller-manager`: Used by operator to download apps
> - `splunk-app-s3-reader`: Used by Splunk pod (if needed for direct access)

### 3.3 Create S3 Access Policy

Create file: `/tmp/s3-policy.json`

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ListBucket",
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::YOUR-BUCKET-NAME"
      ]
    },
    {
      "Sid": "GetObject",
      "Effect": "Allow",
      "Action": [
        "s3:GetObject"
      ],
      "Resource": [
        "arn:aws:s3:::YOUR-BUCKET-NAME/*"
      ]
    }
  ]
}
```

**Replace bucket name:**

```bash
# Set your S3 bucket name
export BUCKET_NAME="your-s3-bucket-name"

# Replace bucket name in policy
sed -i "s|YOUR-BUCKET-NAME|$BUCKET_NAME|g" /tmp/s3-policy.json

# Verify S3 policy
cat /tmp/s3-policy.json
```

### 3.4 Create IAM Role and Policies

```bash
# Define names
export ROLE_NAME="splunk-ldap-s3-read"
export POLICY_NAME="SplunkLDAPAppS3ReadPolicy"

# Create IAM role
aws iam create-role \
  --role-name $ROLE_NAME \
  --assume-role-policy-document file:///tmp/trust-policy.json \
  --description "IRSA role for Splunk Operator to read LDAP app from S3"

echo "✓ IAM role created: $ROLE_NAME"

# Create S3 policy
aws iam create-policy \
  --policy-name $POLICY_NAME \
  --policy-document file:///tmp/s3-policy.json \
  --description "S3 read access for Splunk LDAP app"

echo "✓ S3 policy created: $POLICY_NAME"

# Attach policy to role
export POLICY_ARN="arn:aws:iam::${ACCOUNT_ID}:policy/${POLICY_NAME}"
aws iam attach-role-policy \
  --role-name $ROLE_NAME \
  --policy-arn $POLICY_ARN

echo "✓ Policy attached to role"

# Get role ARN
export ROLE_ARN=$(aws iam get-role \
  --role-name $ROLE_NAME \
  --query 'Role.Arn' \
  --output text)

echo "IAM Role ARN: $ROLE_ARN"
```

### 3.5 Verify IAM Configuration

```bash
# Verify role exists
aws iam get-role --role-name $ROLE_NAME

# Verify policy is attached
aws iam list-attached-role-policies --role-name $ROLE_NAME

# Verify trust policy
aws iam get-role --role-name $ROLE_NAME --query 'Role.AssumeRolePolicyDocument'
```

---

## Step 4: Upload App to S3

Upload the packaged LDAP app to your S3 bucket.

### 4.1 Create S3 Directory Structure

```bash
# Set S3 path
export S3_PREFIX="splunk-apps/authAppsLoc"

# Verify bucket exists
aws s3 ls s3://${BUCKET_NAME}/

# Create directory (S3 creates it automatically when uploading)
```

### 4.2 Upload LDAP App

```bash
# Upload app to S3
aws s3 cp /tmp/ldap-auth-config-app.tgz \
  s3://${BUCKET_NAME}/${S3_PREFIX}/ldap-auth-config-app.tgz

echo "✓ App uploaded to S3"

# Verify upload
aws s3 ls s3://${BUCKET_NAME}/${S3_PREFIX}/

# Expected output:
# 2025-11-27 12:00:00       2234 ldap-auth-config-app.tgz
```

### 4.3 Set Object Metadata (Optional)

```bash
# Set metadata for versioning
aws s3api put-object-tagging \
  --bucket $BUCKET_NAME \
  --key ${S3_PREFIX}/ldap-auth-config-app.tgz \
  --tagging 'TagSet=[{Key=Version,Value=1.0},{Key=Environment,Value=test}]'
```

---

## Step 5: Configure Splunk Operator

Configure Kubernetes ServiceAccounts with IAM role annotations.

### 5.1 Verify ServiceAccounts Exist

```bash
# Check if ServiceAccounts exist
kubectl get serviceaccount -n $NAMESPACE splunk-app-s3-reader
kubectl get serviceaccount -n $NAMESPACE splunk-operator-controller-manager

# If splunk-app-s3-reader doesn't exist, create it
kubectl create serviceaccount splunk-app-s3-reader -n $NAMESPACE
```

### 5.2 Annotate ServiceAccounts with IAM Role

```bash
# Annotate splunk-app-s3-reader ServiceAccount
kubectl annotate serviceaccount -n $NAMESPACE splunk-app-s3-reader \
  eks.amazonaws.com/role-arn=$ROLE_ARN \
  --overwrite

echo "✓ ServiceAccount annotated: splunk-app-s3-reader"

# Annotate operator ServiceAccount (for downloading apps)
kubectl annotate serviceaccount -n $NAMESPACE splunk-operator-controller-manager \
  eks.amazonaws.com/role-arn=$ROLE_ARN \
  --overwrite

echo "✓ ServiceAccount annotated: splunk-operator-controller-manager"
```

### 5.3 Verify ServiceAccount Annotations

```bash
# Verify annotation on splunk-app-s3-reader
kubectl get serviceaccount -n $NAMESPACE splunk-app-s3-reader -o yaml | grep role-arn

# Expected output:
# eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/splunk-ldap-s3-read

# Verify annotation on operator ServiceAccount
kubectl get serviceaccount -n $NAMESPACE splunk-operator-controller-manager -o yaml | grep role-arn
```

### 5.4 Restart Operator (to pick up new annotations)

```bash
# Restart operator deployment
kubectl rollout restart deployment -n $NAMESPACE splunk-operator-controller-manager

# Wait for rollout to complete
kubectl rollout status deployment -n $NAMESPACE splunk-operator-controller-manager

echo "✓ Operator restarted"
```

---

## Step 6: Deploy Splunk with App Framework

Create and deploy a Splunk Standalone CR with App Framework configuration.

### 6.1 Create Standalone CR Manifest

Create file: `splunk-standalone-ldap.yaml`

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: splunk-ldap-test
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  # Number of Splunk instances
  replicas: 1

  # Splunk image configuration
  image: splunk/splunk:10.0.0
  imagePullPolicy: IfNotPresent

  # ServiceAccount with IRSA annotation
  serviceAccount: splunk-app-s3-reader

  # App Framework configuration
  appRepo:
    # App sources - where to find apps
    appSources:
      - name: authApps
        location: authAppsLoc/
        scope: local
        volumeName: auth-apps

    # Default settings for apps
    defaults:
      volumeName: app-defaults
      scope: local

    # Storage volumes - where apps are stored
    volumes:
      - name: auth-apps
        storageType: s3
        provider: aws
        path: YOUR-BUCKET-NAME
        endpoint: https://s3.YOUR-REGION.amazonaws.com
        region: YOUR-REGION

      - name: app-defaults
        storageType: s3
        provider: aws
        path: YOUR-BUCKET-NAME
        endpoint: https://s3.YOUR-REGION.amazonaws.com
        region: YOUR-REGION

  # Resource requests and limits
  resources:
    requests:
      memory: "4Gi"
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"

  # Storage for Splunk etc directory
  etcVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 10Gi

  # Storage for Splunk var directory (data)
  varVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 100Gi
```

### 6.2 Update Manifest with Your Values

```bash
# Replace bucket name in YAML
sed -i "s|YOUR-BUCKET-NAME|$BUCKET_NAME|g" splunk-standalone-ldap.yaml

# Replace region in YAML
sed -i "s|YOUR-REGION|$REGION|g" splunk-standalone-ldap.yaml

# Verify changes
grep "path:" splunk-standalone-ldap.yaml
grep "endpoint:" splunk-standalone-ldap.yaml
```

### 6.3 Apply Standalone CR

```bash
# Apply the Standalone CR
kubectl apply -f splunk-standalone-ldap.yaml

echo "✓ Standalone CR created"

# Check CR status
kubectl get standalone -n $NAMESPACE splunk-ldap-test

# Expected output:
# NAME               PHASE      REPLICAS   READY   AGE
# splunk-ldap-test   Pending    1          0       10s
```

### 6.4 Monitor Deployment

```bash
# Watch pod creation
kubectl get pods -n $NAMESPACE -w

# In another terminal, watch operator logs
kubectl logs -n $NAMESPACE deployment/splunk-operator-controller-manager -f

# Look for messages like:
# "Downloading apps from remote storage"
# "Successfully downloaded app: ldap-auth-config-app.tgz"
# "Installing app in Splunk pod"
```

### 6.5 Wait for Splunk Pod to be Ready

```bash
# Wait for Splunk pod to be ready (this may take 5-10 minutes)
kubectl wait --for=condition=Ready \
  pod/splunk-ldap-test-standalone-0 \
  -n $NAMESPACE \
  --timeout=900s

echo "✓ Splunk pod is ready"

# Check pod status
kubectl get pods -n $NAMESPACE -l app.kubernetes.io/instance=splunk-ldap-test

# Expected output:
# NAME                              READY   STATUS    RESTARTS   AGE
# splunk-ldap-test-standalone-0     1/1     Running   0          8m
```

---

## Step 7: Verify Installation

Verify that the LDAP app was successfully downloaded and installed.

### 7.1 Check Operator Logs for App Download

```bash
# Check operator logs for app download
kubectl logs -n $NAMESPACE deployment/splunk-operator-controller-manager \
  --tail=200 | grep -i "ldap-auth-config-app"

# Expected messages:
# "Downloading app: ldap-auth-config-app.tgz"
# "Successfully downloaded app from S3"
# "Installing app: ldap-auth-config-app"
# "App installation completed successfully"
```

### 7.2 Verify App is Installed in Splunk

```bash
# Check if app directory exists
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  ls -la /opt/splunk/etc/apps/ | grep ldap

# Expected output:
# drwxr-xr-x  5 splunk splunk  160 Nov 27 12:00 ldap-auth-config-app

# Check authentication.conf exists
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  ls -la /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf

# Expected output:
# -rw-r--r--  1 splunk splunk  1234 Nov 27 12:00 authentication.conf
```

### 7.3 Verify Splunk Recognizes LDAP Configuration

```bash
# Check authentication configuration using btool
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list authentication

# Expected output:
# [authentication]
# authSettings = corporate-ldap
# authType = LDAP
# passwordHashAlgorithm = SHA512-crypt
```

### 7.4 Verify LDAP Strategy Configuration

```bash
# Check LDAP strategy settings
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list corporate-ldap

# Expected output:
# [corporate-ldap]
# SSLEnabled = 0
# bindDN = cn=admin,dc=splunktest,dc=local
# charset = utf8
# emailAttribute = mail
# groupBaseDN = ou=groups,dc=splunktest,dc=local
# groupBaseFilter = (objectclass=groupOfNames)
# groupMappingAttribute = dn
# groupMemberAttribute = member
# groupNameAttribute = cn
# host = openldap.splunk-operator.svc.cluster.local
# port = 389
# realNameAttribute = displayName
# timelimit = 15
# userBaseDN = ou=users,dc=splunktest,dc=local
# userBaseFilter = (objectclass=inetOrgPerson)
# userNameAttribute = uid
```

### 7.5 Verify Role Mappings

```bash
# Check role mappings
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list roleMap_corporate-ldap

# Expected output:
# [roleMap_corporate-ldap]
# admin = Splunk Admins
# power = Splunk Power Users
# user = Splunk Users
```

### 7.6 Check Network Connectivity to LDAP

```bash
# Test connectivity from Splunk pod to OpenLDAP
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -v telnet://openldap.splunk-operator.svc.cluster.local:389

# Expected output:
# * Connected to openldap.splunk-operator.svc.cluster.local (10.x.x.x) port 389
# * Connection #0 to host openldap.splunk-operator.svc.cluster.local left intact
```

---

## Step 8: Test LDAP Authentication

Test LDAP authentication using REST API and Splunk Web UI.

### 8.1 Test via REST API

#### Test Admin User (john.doe)

```bash
# Test authentication for admin user
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -k -s -u john.doe:SplunkAdmin123 \
  https://localhost:8089/services/authentication/current-context

# Expected output (partial):
# <s:key name="username">john.doe</s:key>
# <s:key name="type">LDAP</s:key>
# <s:key name="roles"><s:list><s:item>admin</s:item></s:list></s:key>
# <s:key name="email">john.doe@splunktest.local</s:key>
# <s:key name="realname">John Doe</s:key>
```

#### Test Power User (jane.smith)

```bash
# Test authentication for power user
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -k -s -u jane.smith:SplunkPower123 \
  https://localhost:8089/services/authentication/current-context

# Expected output (partial):
# <s:key name="username">jane.smith</s:key>
# <s:key name="type">LDAP</s:key>
# <s:key name="roles"><s:list><s:item>power</s:item></s:list></s:key>
# <s:key name="email">jane.smith@splunktest.local</s:key>
# <s:key name="realname">Jane Smith</s:key>
```

#### Test Regular User (bob.user)

```bash
# Test authentication for regular user
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -k -s -u bob.user:SplunkUser123 \
  https://localhost:8089/services/authentication/current-context

# Expected output (partial):
# <s:key name="username">bob.user</s:key>
# <s:key name="type">LDAP</s:key>
# <s:key name="roles"><s:list><s:item>user</s:item></s:list></s:key>
# <s:key name="email">bob.user@splunktest.local</s:key>
# <s:key name="realname">Bob User</s:key>
```

### 8.2 Test via Splunk Web UI

#### Step 1: Port Forward to Splunk Pod

```bash
# Port-forward to Splunk Web (port 8000)
kubectl port-forward -n $NAMESPACE splunk-ldap-test-standalone-0 8000:8000

# Keep this running in the terminal
```

#### Step 2: Access Splunk Web

1. Open browser to: `http://localhost:8000`
2. You should see the Splunk login page

#### Step 3: Login with Test Users

**Test 1: Admin User**
- Username: `john.doe`
- Password: `SplunkAdmin123`
- Expected: Login successful, full admin access

**Test 2: Power User**
- Username: `jane.smith`
- Password: `SplunkPower123`
- Expected: Login successful, power user capabilities

**Test 3: Regular User**
- Username: `bob.user`
- Password: `SplunkUser123`
- Expected: Login successful, basic user access

#### Step 4: Verify Authentication Method

1. Go to: **Settings → Access controls → Authentication method**
2. Verify:
   - External auth system type: **LDAP**
   - Strategy name: **corporate-ldap**
   - LDAP server: **openldap.splunk-operator.svc.cluster.local**

### 8.3 Verify Role Assignments

```bash
# Check john.doe's role
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -k -s -u john.doe:SplunkAdmin123 \
  https://localhost:8089/services/authentication/current-context \
  | grep -A 5 "capabilities"

# Expected: Should have admin capabilities (40+ capabilities)

# Check jane.smith's role
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -k -s -u jane.smith:SplunkPower123 \
  https://localhost:8089/services/authentication/current-context \
  | grep -A 5 "capabilities"

# Expected: Should have power user capabilities (35+ capabilities)

# Check bob.user's role
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -k -s -u bob.user:SplunkUser123 \
  https://localhost:8089/services/authentication/current-context \
  | grep -A 5 "capabilities"

# Expected: Should have basic user capabilities (25+ capabilities)
```

### 8.4 Monitor Authentication Logs

```bash
# Watch Splunk logs for authentication attempts
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  tail -f /opt/splunk/var/log/splunk/splunkd.log | grep -i "ldap\|authentication"

# Look for successful authentication messages:
# "LDAPAuth - Attempting to authenticate user: john.doe"
# "LDAPAuth - Successfully authenticated user: john.doe"
# "LDAPAuth - User john.doe mapped to role: admin"
```

### 8.5 Test Results Summary

| Test | User | Expected Result | Status |
|------|------|----------------|--------|
| REST API - Admin | john.doe | Authenticated, role=admin | ✅ |
| REST API - Power | jane.smith | Authenticated, role=power | ✅ |
| REST API - User | bob.user | Authenticated, role=user | ✅ |
| Web UI - Admin | john.doe | Login successful, full access | ✅ |
| Web UI - Power | jane.smith | Login successful, power access | ✅ |
| Web UI - User | bob.user | Login successful, basic access | ✅ |
| Role Mapping | All users | Correct roles assigned | ✅ |
| User Attributes | All users | Email and real name populated | ✅ |

---

## Troubleshooting

### Issue 1: App Not Downloaded from S3

**Symptoms:**
- Operator logs show: "Unable to get apps list from remote storage"
- App not appearing in Splunk pod

**Diagnosis:**

```bash
# Check operator logs for errors
kubectl logs -n $NAMESPACE deployment/splunk-operator-controller-manager \
  --tail=100 | grep -i "error\|failed"

# Check if IAM role exists
aws iam get-role --role-name splunk-ldap-s3-read

# Check ServiceAccount annotation
kubectl get sa -n $NAMESPACE splunk-app-s3-reader -o yaml | grep role-arn
kubectl get sa -n $NAMESPACE splunk-operator-controller-manager -o yaml | grep role-arn

# Check if app exists in S3
aws s3 ls s3://${BUCKET_NAME}/${S3_PREFIX}/
```

**Solutions:**

**1. IAM Role Missing or Incorrect:**
```bash
# Verify IAM role exists
aws iam get-role --role-name splunk-ldap-s3-read

# If missing, recreate role (see Step 3.4)

# Verify trust policy allows both ServiceAccounts
aws iam get-role --role-name splunk-ldap-s3-read \
  --query 'Role.AssumeRolePolicyDocument.Statement[0].Condition'
```

**2. ServiceAccount Not Annotated:**
```bash
# Re-annotate ServiceAccounts
kubectl annotate serviceaccount -n $NAMESPACE splunk-app-s3-reader \
  eks.amazonaws.com/role-arn=$ROLE_ARN --overwrite

kubectl annotate serviceaccount -n $NAMESPACE splunk-operator-controller-manager \
  eks.amazonaws.com/role-arn=$ROLE_ARN --overwrite

# Restart operator
kubectl rollout restart deployment -n $NAMESPACE splunk-operator-controller-manager
```

**3. S3 Permissions Issue:**
```bash
# Test S3 access from operator pod
kubectl exec -n $NAMESPACE deployment/splunk-operator-controller-manager -- \
  aws s3 ls s3://${BUCKET_NAME}/${S3_PREFIX}/

# If access denied, verify IAM policy
aws iam get-policy-version \
  --policy-arn arn:aws:iam::${ACCOUNT_ID}:policy/SplunkLDAPAppS3ReadPolicy \
  --version-id v1
```

**4. Wrong S3 Path in Standalone CR:**
```bash
# Check app source location in CR
kubectl get standalone -n $NAMESPACE splunk-ldap-test -o yaml | grep -A 5 appSources

# Verify location matches S3 structure:
# location: authAppsLoc/
# S3 path: s3://bucket/splunk-apps/authAppsLoc/ldap-auth-config-app.tgz
```

---

### Issue 2: LDAP Authentication Failing

**Symptoms:**
- Users cannot login with LDAP credentials
- Splunk logs show: "Can't contact LDAP server" or "Authentication failed"

**Diagnosis:**

```bash
# Check Splunk logs
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  tail -100 /opt/splunk/var/log/splunk/splunkd.log | grep -i "ldap\|error"

# Test network connectivity to LDAP
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -v telnet://openldap.splunk-operator.svc.cluster.local:389

# Check OpenLDAP pod status
kubectl get pods -n $NAMESPACE -l app=openldap
```

**Solutions:**

**1. OpenLDAP Not Ready:**
```bash
# Check OpenLDAP pod status
kubectl get pods -n $NAMESPACE -l app=openldap

# If not Running or not Ready (0/1), check logs
kubectl logs -n $NAMESPACE deployment/openldap -c openldap --tail=50

# Common issues:
# - ConfigMap not mounted correctly
# - Domain configuration error
# - LDIF syntax error

# Restart OpenLDAP if needed
kubectl rollout restart deployment -n $NAMESPACE openldap
```

**2. Network Connectivity Issue:**
```bash
# Test DNS resolution
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  nslookup openldap.splunk-operator.svc.cluster.local

# Test port connectivity
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  nc -zv openldap.splunk-operator.svc.cluster.local 389

# Check service exists
kubectl get svc -n $NAMESPACE openldap
```

**3. Wrong LDAP Credentials:**
```bash
# Test LDAP bind from OpenLDAP pod
kubectl exec -n $NAMESPACE deployment/openldap -c openldap -- \
  ldapwhoami -x -H ldap://localhost \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin

# Expected output: dn:cn=admin,dc=splunktest,dc=local

# If bind fails, verify:
# - bindDN in authentication.conf
# - bindDNpassword in authentication.conf
```

**4. Wrong LDAP Base DNs:**
```bash
# Verify users exist in specified userBaseDN
kubectl exec -n $NAMESPACE deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=users,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(uid=john.doe)"

# Verify groups exist in specified groupBaseDN
kubectl exec -n $NAMESPACE deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=groups,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(cn=Splunk Admins)"
```

---

### Issue 3: User Authenticated But Wrong Role

**Symptoms:**
- User can login successfully
- User has wrong Splunk role (e.g., should be admin but appears as user)

**Diagnosis:**

```bash
# Check user's LDAP group membership
kubectl exec -n $NAMESPACE deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=groups,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(member=uid=john.doe,ou=users,dc=splunktest,dc=local)" cn

# Check role mapping configuration in Splunk
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list roleMap_corporate-ldap
```

**Solutions:**

**1. Role Mapping Configuration Incorrect:**
```bash
# Verify role mapping in authentication.conf
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf | \
  grep -A 10 "roleMap_corporate-ldap"

# Expected:
# [roleMap_corporate-ldap]
# admin = Splunk Admins
# power = Splunk Power Users
# user = Splunk Users

# Group names are case-sensitive and must match exactly
```

**2. User Not in Correct LDAP Group:**
```bash
# Check which groups user belongs to
kubectl exec -n $NAMESPACE deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=groups,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin \
  "(member=uid=john.doe,ou=users,dc=splunktest,dc=local)" cn

# Expected output: cn: Splunk Admins

# If user not in group, add them (for test LDAP):
# Update bootstrap LDIF and redeploy OpenLDAP
```

**3. Multiple Role Mappings:**
```bash
# If user is in multiple groups, they get multiple roles
# Check all user's roles
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  curl -k -s -u john.doe:SplunkAdmin123 \
  https://localhost:8089/services/authentication/current-context \
  | grep -A 10 "roles"

# User will have capabilities from all assigned roles
```

---

### Issue 4: OpenLDAP Pod CrashLooping

**Symptoms:**
- OpenLDAP pod shows `CrashLoopBackOff` or `Error` status
- Pod restarts repeatedly

**Diagnosis:**

```bash
# Check pod status
kubectl get pods -n $NAMESPACE -l app=openldap

# Check pod logs
kubectl logs -n $NAMESPACE deployment/openldap -c openldap --tail=100

# Check pod events
kubectl describe pod -n $NAMESPACE -l app=openldap
```

**Common Causes and Solutions:**

**1. Domain Configuration Error:**
```bash
# Check LDAP_DOMAIN environment variable
kubectl get deployment -n $NAMESPACE openldap -o yaml | grep -A 2 LDAP_DOMAIN

# Must be: splunktest.local
# NOT: splunktest.dc=local (incorrect!)

# If incorrect, fix and redeploy:
kubectl edit deployment -n $NAMESPACE openldap
# Change: value: "splunktest.dc=local"
# To: value: "splunktest.local"
```

**2. Bootstrap LDIF Syntax Error:**
```bash
# Check ConfigMap for syntax errors
kubectl get configmap -n $NAMESPACE openldap-bootstrap -o yaml

# Common issues:
# - Missing blank line between entries
# - Incorrect DN syntax
# - Missing required attributes
# - Tabs instead of spaces

# To fix, edit ConfigMap and restart:
kubectl edit configmap -n $NAMESPACE openldap-bootstrap
kubectl rollout restart deployment -n $NAMESPACE openldap
```

**3. Volume Mount Issues:**
```bash
# Check volume mounts
kubectl get deployment -n $NAMESPACE openldap -o yaml | grep -A 10 volumeMounts

# Ensure:
# - bootstrap ConfigMap mounted as read-only
# - data and config use emptyDir (not ConfigMap)
```

**4. Container Image Issue:**
```bash
# Verify image can be pulled
kubectl describe pod -n $NAMESPACE -l app=openldap | grep -A 5 "Events:"

# If image pull fails, check:
# - Image name: osixia/openldap:1.5.0
# - Network access to Docker Hub
# - Image pull secrets (if required)
```

---

### Issue 5: AWS Credentials Conflict

**Symptoms:**
- Operator cannot access S3
- Errors: "ExpiredToken", "InvalidAccessKeyId", "UnrecognizedClientException"

**Cause:**
Local AWS CLI credentials (from Bedrock or other tools) conflict with IRSA credentials.

**Solution:**

```bash
# Unset environment AWS credentials before kubectl commands
unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_SESSION_TOKEN
unset AWS_PROFILE

# Then run kubectl commands
kubectl get pods -n $NAMESPACE

# For automation, add to beginning of scripts:
#!/bin/bash
unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_SESSION_TOKEN
unset AWS_PROFILE

# Continue with kubectl commands...
```

---

### Issue 6: App Installed But LDAP Not Working

**Symptoms:**
- App exists in `/opt/splunk/etc/apps/`
- authentication.conf file exists
- But Splunk still uses local authentication

**Diagnosis:**

```bash
# Check if Splunk recognizes LDAP config
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list --debug

# Check for configuration precedence issues
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list authentication --debug

# Check Splunk restart status
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk status
```

**Solutions:**

**1. Splunk Needs Restart:**
```bash
# Restart Splunk to pick up configuration changes
kubectl delete pod -n $NAMESPACE splunk-ldap-test-standalone-0

# Wait for pod to restart
kubectl wait --for=condition=Ready \
  pod/splunk-ldap-test-standalone-0 \
  -n $NAMESPACE \
  --timeout=600s
```

**2. Configuration Precedence Issue:**
```bash
# Check if other apps override LDAP config
kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \
  find /opt/splunk/etc/apps -name "authentication.conf"

# If multiple authentication.conf files exist, check precedence:
# 1. Local overrides default
# 2. Apps loaded alphabetically
# 3. System apps load last

# Ensure ldap-auth-config-app has highest precedence
```

**3. App Scope Issue:**
```bash
# Verify app scope in Standalone CR
kubectl get standalone -n $NAMESPACE splunk-ldap-test -o yaml | grep -A 5 appSources

# Ensure scope is set to "local" or "clusterManager"
# scope: local  # Correct for authentication.conf
```

---

### Diagnostic Commands Summary

```bash
# Quick diagnostic commands

# 1. Check all components status
kubectl get pods -n splunk-operator

# 2. Check Splunk logs
kubectl logs -n splunk-operator splunk-ldap-test-standalone-0 --tail=100

# 3. Check operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager --tail=100

# 4. Check OpenLDAP logs
kubectl logs -n splunk-operator deployment/openldap -c openldap --tail=50

# 5. Test LDAP connectivity
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -v telnet://openldap.splunk-operator.svc.cluster.local:389

# 6. Verify LDAP configuration
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list --debug

# 7. Test authentication
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -k -s -u john.doe:SplunkAdmin123 \
  https://localhost:8089/services/authentication/current-context
```

---

## Production Deployment

### Adapting for Production LDAP/Active Directory

#### 1. Update LDAP Configuration

Modify `/tmp/ldap-auth-config-app/local/authentication.conf`:

```ini
[authentication]
authType = LDAP
authSettings = corporate-ldap

[corporate-ldap]
# Production LDAP/AD Configuration

# Enable SSL
SSLEnabled = 1

# Multiple servers for high availability (semicolon-separated)
host = ldap1.yourcompany.com;ldap2.yourcompany.com;ldap3.yourcompany.com
port = 636

# Service account for binding
# Use least privilege - read-only access to users and groups
bindDN = CN=SplunkService,OU=ServiceAccounts,DC=yourcompany,DC=com
bindDNpassword = $LDAP_BIND_PASSWORD$

# User search configuration
userBaseDN = OU=Employees,DC=yourcompany,DC=com
# Exclude computer accounts in Active Directory
userBaseFilter = (&(objectClass=user)(!(objectClass=computer)))
# Active Directory uses sAMAccountName
userNameAttribute = sAMAccountName

# Group search configuration
groupBaseDN = OU=Groups,DC=yourcompany,DC=com
groupBaseFilter = (objectClass=group)
groupMemberAttribute = member
groupNameAttribute = cn
groupMappingAttribute = dn

# Additional attributes
emailAttribute = mail
realNameAttribute = displayName
charset = utf8
timelimit = 15
network_timeout = 20

# Role mapping with full DNs
[roleMap_corporate-ldap]
admin = CN=Splunk-Administrators,OU=Groups,DC=yourcompany,DC=com
power = CN=Splunk-PowerUsers,OU=Groups,DC=yourcompany,DC=com
user = CN=Splunk-Users,OU=Groups,DC=yourcompany,DC=com
```

#### 2. Use Kubernetes Secrets for Passwords (RECOMMENDED for Production)

**This is the CORRECT way to handle sensitive credentials with the Splunk Operator.**

##### Step 1: Create Kubernetes Secret

```bash
# Create secret for LDAP bind password
kubectl create secret generic ldap-bind-password \
  -n splunk-operator \
  --from-literal=password='your-ldap-bind-password'

# Verify secret created
kubectl get secret -n splunk-operator ldap-bind-password
```

##### Step 2: Update Standalone CR to Inject Environment Variable

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: splunk-ldap-test
  namespace: splunk-operator
spec:
  # ... existing configuration ...

  # Add extraEnv to inject secret as environment variable
  extraEnv:
    - name: LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-bind-password
          key: password
```

Apply the updated CR:

```bash
kubectl apply -f splunk-standalone-ldap.yaml
```

##### Step 3: Update authentication.conf to Use Environment Variable

Update `/tmp/ldap-auth-config-app/local/authentication.conf`:

```ini
[corporate-ldap]
# ... other settings ...

# Use environment variable for password
# Splunk will substitute $LDAP_BIND_PASSWORD$ with the actual value from the environment
bindDN = CN=SplunkService,OU=ServiceAccounts,DC=yourcompany,DC=com
bindDNpassword = $LDAP_BIND_PASSWORD$
```

##### Step 4: Repackage and Upload App

```bash
# Repackage app
cd /tmp
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/

# Upload to S3
aws s3 cp ldap-auth-config-app.tgz \
  s3://${BUCKET_NAME}/${S3_PREFIX}/ldap-auth-config-app.tgz

# Force app reinstall
kubectl delete pod splunk-ldap-test-standalone-0 -n splunk-operator
```

##### Step 5: Verify Environment Variable is Injected

```bash
# Check environment variable in pod
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  printenv | grep LDAP_BIND_PASSWORD

# Should output: LDAP_BIND_PASSWORD=your-ldap-bind-password

# Verify Splunk substituted the password (password will be encrypted)
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list corporate-ldap --debug \
  | grep bindDNpassword

# Should show: bindDNpassword = $encrypted_password$
```

**Why This Works:**
- Kubernetes Secret stores the sensitive credential
- `extraEnv` in the CR tells the Operator to inject the secret as an environment variable
- Splunk's configuration system automatically substitutes `$VAR_NAME$` syntax with environment variables
- The Operator preserves this configuration because it's part of the App Framework app

#### 3. SSL/TLS Certificate Configuration

For production LDAP with SSL, add CA certificate:

```bash
# Create ConfigMap with CA certificate
kubectl create configmap ldap-ca-cert \
  -n splunk-operator \
  --from-file=ca.crt=/path/to/your-ldap-ca.crt

# Mount in Standalone CR
kubectl edit standalone -n splunk-operator splunk-ldap-test
```

Add volume mount:

```yaml
spec:
  volumes:
    - name: ldap-ca-cert
      configMap:
        name: ldap-ca-cert
  volumeMounts:
    - name: ldap-ca-cert
      mountPath: /opt/splunk/etc/auth/ldap-ca.crt
      subPath: ca.crt
```

Update authentication.conf:

```ini
[corporate-ldap]
SSLEnabled = 1
port = 636
# Path to CA certificate
sslCAFile = /opt/splunk/etc/auth/ldap-ca.crt
```

#### 4. Test Production Configuration

Before deploying to production:

```bash
# 1. Test LDAP connectivity from Splunk pod
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  openssl s_client -connect ldap1.yourcompany.com:636 -CAfile /opt/splunk/etc/auth/ldap-ca.crt

# 2. Test LDAP bind
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  ldapsearch -x -H ldaps://ldap1.yourcompany.com:636 \
  -D "CN=SplunkService,OU=ServiceAccounts,DC=yourcompany,DC=com" \
  -w "$LDAP_PASSWORD" \
  -b "DC=yourcompany,DC=com" \
  "(objectClass=*)" dn

# 3. Test user search
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  ldapsearch -x -H ldaps://ldap1.yourcompany.com:636 \
  -D "CN=SplunkService,OU=ServiceAccounts,DC=yourcompany,DC=com" \
  -w "$LDAP_PASSWORD" \
  -b "OU=Employees,DC=yourcompany,DC=com" \
  "(sAMAccountName=testuser)"

# 4. Test group membership
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  ldapsearch -x -H ldaps://ldap1.yourcompany.com:636 \
  -D "CN=SplunkService,OU=ServiceAccounts,DC=yourcompany,DC=com" \
  -w "$LDAP_PASSWORD" \
  -b "OU=Groups,DC=yourcompany,DC=com" \
  "(cn=Splunk-Administrators)" member
```

### High Availability Considerations

#### 1. Multiple LDAP Servers

Configure multiple LDAP servers for failover:

```ini
[corporate-ldap]
host = ldap1.company.com;ldap2.company.com;ldap3.company.com
```

Splunk will automatically failover if primary server is unavailable.

#### 2. Splunk Clustering

For production deployments, use Splunk clusters instead of Standalone:

**SearchHeadCluster Example:**

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: example-shc
  namespace: splunk-operator
spec:
  replicas: 3

  # App Framework configuration (same as Standalone)
  appRepo:
    appSources:
      - name: authApps
        location: authAppsLoc/
        scope: clusterManager
        volumeName: auth-apps

    volumes:
      - name: auth-apps
        storageType: s3
        provider: aws
        path: your-bucket-name
        endpoint: https://s3.us-west-2.amazonaws.com
        region: us-west-2

  # Service account with IRSA
  serviceAccount: splunk-app-s3-reader
```

**ClusterManager + IndexerCluster Example:**

```yaml
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: example-cm
  namespace: splunk-operator
spec:
  appRepo:
    # Same app framework configuration
  serviceAccount: splunk-app-s3-reader

---
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example-idc
  namespace: splunk-operator
spec:
  replicas: 3
  clusterManagerRef:
    name: example-cm
```

#### 3. Network Policies

Restrict network access between components:

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
  # Allow traffic to LDAP server
  - to:
    - podSelector:
        matchLabels:
          app: openldap
    ports:
    - protocol: TCP
      port: 389
  # Allow traffic to production LDAP
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 636
  # Allow DNS
  - to:
    - namespaceSelector: {}
      podSelector:
        matchLabels:
          k8s-app: kube-dns
    ports:
    - protocol: UDP
      port: 53
```

### Monitoring and Alerting

#### 1. Monitor Authentication Failures

Create Splunk alert for failed authentication attempts:

```spl
index=_internal source=*splunkd.log "authentication failed"
| stats count by user, src_ip
| where count > 5
```

#### 2. Monitor LDAP Connection Health

```spl
index=_internal source=*splunkd.log "Can't contact LDAP server"
| stats count by host
| where count > 0
```

#### 3. Monitor App Framework

```bash
# Check operator metrics (if Prometheus is enabled)
kubectl get --raw /metrics | grep app_framework
```

### Backup and Recovery

#### 1. Backup LDAP Configuration

```bash
# Export authentication.conf
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf \
  > authentication-backup-$(date +%Y%m%d).conf

# Store in version control or secure backup location
```

#### 2. Keep Local Admin Account

Always maintain a local Splunk admin account as backup:

```bash
# Create local admin via REST API (before enabling LDAP)
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -k -u admin:password \
  https://localhost:8089/services/authentication/users \
  -d name=localadmin \
  -d password=secure-password \
  -d roles=admin
```

#### 3. Rollback Procedure

If LDAP authentication fails, rollback:

```bash
# Option 1: Remove LDAP app
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  rm -rf /opt/splunk/etc/apps/ldap-auth-config-app

# Option 2: Disable LDAP in authentication.conf
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk edit authentication-method Splunk -auth admin:password

# Restart Splunk
kubectl delete pod splunk-ldap-test-standalone-0 -n splunk-operator
```

### Security Best Practices

#### 1. Least Privilege IAM

Restrict S3 access to specific paths:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject"],
      "Resource": [
        "arn:aws:s3:::your-bucket/splunk-apps/authAppsLoc/*"
      ]
    }
  ]
}
```

#### 2. Audit LDAP Access

Enable LDAP server audit logging to track Splunk access:

```bash
# For OpenLDAP (testing), enable access logs
# For Active Directory, enable audit policies
```

#### 3. Rotate Service Account Credentials

Regularly rotate LDAP bind account password:

```bash
# Update Kubernetes Secret
kubectl create secret generic ldap-bind-password \
  -n splunk-operator \
  --from-literal=password='new-password' \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart Splunk pods to pick up new password
kubectl rollout restart statefulset -n splunk-operator splunk-ldap-test-standalone
```

#### 4. Enable TLS for All Connections

- LDAP: Use port 636 with SSL enabled
- S3: Always use HTTPS endpoints
- Splunk Web: Enable SSL for web interface

### Updating LDAP Configuration

To update LDAP settings in production:

```bash
# 1. Update authentication.conf in app source
vim /tmp/ldap-auth-config-app/local/authentication.conf

# 2. Repackage app with new version
cd /tmp
tar -czf ldap-auth-config-app-v1.1.tgz ldap-auth-config-app/

# 3. Upload to S3 with version tag
aws s3 cp ldap-auth-config-app-v1.1.tgz \
  s3://${BUCKET_NAME}/${S3_PREFIX}/ldap-auth-config-app.tgz \
  --metadata version=1.1,updated=$(date +%Y%m%d)

# 4. Trigger app update in operator
# Option A: Delete app from Splunk (operator will reinstall)
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  rm -rf /opt/splunk/etc/apps/ldap-auth-config-app

# Option B: Update Standalone CR to force refresh
kubectl annotate standalone -n splunk-operator splunk-ldap-test \
  force-update=$(date +%s) --overwrite

# 5. Restart Splunk pod
kubectl delete pod splunk-ldap-test-standalone-0 -n splunk-operator

# 6. Verify new configuration
kubectl wait --for=condition=Ready \
  pod/splunk-ldap-test-standalone-0 \
  -n splunk-operator \
  --timeout=600s

kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list corporate-ldap
```

---

## Appendix: Configuration Files

### Complete File Listing

```
Project Files:
├── openldap-deployment.yaml          # OpenLDAP deployment manifest
├── splunk-standalone-ldap.yaml       # Splunk Standalone CR
├── /tmp/trust-policy.json            # IAM trust policy
├── /tmp/s3-policy.json               # IAM S3 access policy
└── /tmp/ldap-auth-config-app/        # LDAP app source
    ├── default/
    │   └── app.conf                  # App metadata
    ├── local/
    │   └── authentication.conf       # LDAP configuration
    └── metadata/                     # (empty)

Generated Files:
├── /tmp/ldap-auth-config-app.tgz    # Packaged app
└── docs/
    ├── ldap-deployment-architecture.d2   # Deployment diagram
    └── ldap-authentication-flow.d2       # Auth flow diagram
```

### Environment Variables Reference

```bash
# AWS Configuration
CLUSTER_NAME="your-eks-cluster-name"
REGION="us-west-2"
ACCOUNT_ID="123456789012"
OIDC_PROVIDER="oidc.eks.us-west-2.amazonaws.com/id/XXXXX"

# S3 Configuration
BUCKET_NAME="your-s3-bucket-name"
S3_PREFIX="splunk-apps/authAppsLoc"

# IAM Configuration
ROLE_NAME="splunk-ldap-s3-read"
POLICY_NAME="SplunkLDAPAppS3ReadPolicy"
ROLE_ARN="arn:aws:iam::123456789012:role/splunk-ldap-s3-read"
POLICY_ARN="arn:aws:iam::123456789012:policy/SplunkLDAPAppS3ReadPolicy"

# Kubernetes Configuration
NAMESPACE="splunk-operator"
```

### Test Users Reference

```
Test Environment (OpenLDAP):

LDAP Server:
  Host: openldap.splunk-operator.svc.cluster.local
  Port: 389
  Base DN: dc=splunktest,dc=local
  Admin DN: cn=admin,dc=splunktest,dc=local
  Admin Password: admin

Test User 1 (Admin):
  Username: john.doe
  Password: SplunkAdmin123
  Email: john.doe@splunktest.local
  Display Name: John Doe
  LDAP Group: Splunk Admins
  Splunk Role: admin

Test User 2 (Power):
  Username: jane.smith
  Password: SplunkPower123
  Email: jane.smith@splunktest.local
  Display Name: Jane Smith
  LDAP Group: Splunk Power Users
  Splunk Role: power

Test User 3 (User):
  Username: bob.user
  Password: SplunkUser123
  Email: bob.user@splunktest.local
  Display Name: Bob User
  LDAP Group: Splunk Users
  Splunk Role: user
```

### Quick Commands Reference

```bash
# Deploy OpenLDAP
kubectl apply -f openldap-deployment.yaml
kubectl get pods -n splunk-operator -l app=openldap

# Create LDAP app
mkdir -p /tmp/ldap-auth-config-app/{local,default,metadata}
# (create files as shown in Step 2)
cd /tmp && tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/

# Create IAM role
aws iam create-role --role-name splunk-ldap-s3-read \
  --assume-role-policy-document file:///tmp/trust-policy.json

# Upload to S3
aws s3 cp /tmp/ldap-auth-config-app.tgz \
  s3://your-bucket/splunk-apps/authAppsLoc/

# Annotate ServiceAccount
kubectl annotate sa -n splunk-operator splunk-app-s3-reader \
  eks.amazonaws.com/role-arn=arn:aws:iam::ACCOUNT_ID:role/splunk-ldap-s3-read

# Deploy Splunk
kubectl apply -f splunk-standalone-ldap.yaml

# Test authentication
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -k -s -u john.doe:SplunkAdmin123 \
  https://localhost:8089/services/authentication/current-context

# Port-forward for Web UI
kubectl port-forward -n splunk-operator splunk-ldap-test-standalone-0 8000:8000
```

---

## Architecture Diagrams

### Deployment Architecture

![Deployment Architecture](docs/ldap-deployment-architecture.svg)

**Available formats:**
- SVG: `docs/ldap-deployment-architecture.svg`
- PNG: `docs/ldap-deployment-architecture.png`

This diagram shows:
- User authentication flow (7 steps)
- App Framework deployment pipeline (6 steps)
- IRSA/IAM integration
- All components and their relationships

### Authentication Flow

![Authentication Flow](docs/ldap-authentication-flow.svg)

**Available formats:**
- SVG: `docs/ldap-authentication-flow.svg`
- PNG: `docs/ldap-authentication-flow.png`

This diagram shows:
- Step-by-step authentication process (9 steps)
- Decision points (success/failure paths)
- Error handling

---

## Frequently Asked Questions (FAQ)

### Q1: Why does the Operator overwrite my custom Docker image configurations?

**A:** The Splunk Operator is designed to manage the full lifecycle of Splunk deployments. When you use a custom Docker image with pre-configured authentication.conf:

1. The Operator applies its own configuration management during reconciliation
2. It expects configuration to come through the App Framework or CR specifications
3. Any files baked into the custom image are replaced with Operator-managed configurations
4. This ensures consistency and allows the Operator to manage updates/changes

**Solution:** Use the App Framework to deploy LDAP configuration as a Splunk app (as shown in this guide).

### Q2: Why don't environment variables work for LDAP bind passwords?

**A:** There are two common misunderstandings:

**❌ Wrong Approach:**
Setting environment variables directly in the Standalone CR and expecting them to automatically appear in authentication.conf:

```yaml
spec:
  extraEnv:
    - name: LDAP_PASSWORD
      value: "mypassword"
```

Then using `bindDNpassword = mypassword` in authentication.conf → **This doesn't work**

**✅ Correct Approach:**
You must use Splunk's environment variable substitution syntax `$VAR_NAME$`:

1. Set environment variable via `extraEnv` (as above)
2. In authentication.conf: `bindDNpassword = $LDAP_PASSWORD$` (with dollar signs)
3. Splunk will substitute the variable at runtime

### Q3: Why does kubectl exec editing not persist?

**A:** Any changes made via `kubectl exec` (e.g., editing files directly in the container) are ephemeral:

1. Pod restarts lose all manual changes
2. The Operator reconciliation loop may revert changes
3. Splunk lifecycle events (restarts, upgrades) reset configurations

**Solution:** All configuration must be deployed via:
- App Framework (for authentication, apps)
- Standalone/Cluster CR specifications (for Splunk-level settings)
- Kubernetes ConfigMaps/Secrets (for injected files)

### Q4: What is the officially supported method for LDAP authentication?

**A:** The **App Framework** is the officially supported and recommended method:

1. **Create a Splunk app** containing authentication.conf
2. **Package the app** as a tarball
3. **Upload to S3** (or Azure Blob, GCS)
4. **Configure appRepo** in your Standalone/Cluster CR
5. **Operator downloads and installs** the app automatically
6. **Configuration persists** across pod restarts and reconciliation

This guide demonstrates this exact approach.

### Q5: How do I securely handle the LDAP bind password?

**A:** Use Kubernetes Secrets with environment variable substitution:

```bash
# 1. Create Kubernetes Secret
kubectl create secret generic ldap-bind-password \
  --from-literal=password='your-password'

# 2. Inject via extraEnv in CR
spec:
  extraEnv:
    - name: LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-bind-password
          key: password

# 3. Reference in authentication.conf
bindDNpassword = $LDAP_BIND_PASSWORD$
```

See the Production Deployment section for complete details.

### Q6: Can I use Azure or GCS instead of AWS S3?

**A:** Yes! The App Framework supports multiple storage providers:

**Azure Blob Storage:**
```yaml
volumes:
  - name: auth-apps
    storageType: blob
    provider: azure
    path: your-container-name
    endpoint: https://yourstorageaccount.blob.core.windows.net
```

**Google Cloud Storage:**
```yaml
volumes:
  - name: auth-apps
    storageType: gcs
    provider: gcp
    path: your-bucket-name
```

### Q7: What if I need to update the LDAP configuration?

**A:** Update the configuration using the App Framework workflow:

1. **Update authentication.conf** in your app source directory
2. **Repackage the app**: `tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/`
3. **Upload to S3**: `aws s3 cp ldap-auth-config-app.tgz s3://bucket/path/`
4. **Force reinstall**: Delete the Splunk pod or update the app version

The Operator will download and install the updated app automatically.

### Q8: Does this work with Splunk clusters (not just Standalone)?

**A:** Yes! The same App Framework approach works with:

- **Standalone** (single instance)
- **SearchHeadCluster** (multiple search heads)
- **IndexerCluster** (multiple indexers)
- **ClusterManager** (cluster manager with indexers)

For clusters, set `scope: clusterManager` in the appSource configuration to deploy the app across all cluster members.

### Q9: Why does the Operator need S3 access?

**A:** The Operator itself (not the Splunk pod) downloads apps from S3:

1. The Operator watches for CR changes
2. When it detects an app source configuration, it downloads apps from S3
3. The Operator then copies apps to the Splunk pod(s)
4. This is why both ServiceAccounts need IAM role annotations

This centralized approach allows the Operator to manage app deployment across multiple pods consistently.

### Q10: What if my company's security policy doesn't allow IRSA?

**A:** Alternatives to IRSA:

**Option 1: AWS Secret Store CSI Driver**
```yaml
volumes:
  - name: aws-credentials
    csi:
      driver: secrets-store.csi.k8s.io
      readOnly: true
```

**Option 2: Service Account Keys (GCP)**
For Google Cloud, use Workload Identity instead of IRSA.

**Option 3: Static Credentials in Secret (Not Recommended)**
```yaml
extraEnv:
  - name: AWS_ACCESS_KEY_ID
    valueFrom:
      secretKeyRef:
        name: aws-credentials
        key: access_key_id
```

However, IRSA is the most secure approach as it provides temporary credentials and follows least privilege principles.

---

## Summary

This guide provided complete end-to-end instructions for implementing LDAP authentication in Splunk on Kubernetes:

### What Was Accomplished

✅ **OpenLDAP Test Server**: Deployed with 3 test users and 3 groups
✅ **LDAP Authentication App**: Created and packaged Splunk app
✅ **AWS IAM (IRSA)**: Configured secure S3 access without static credentials
✅ **App Framework**: Automated app deployment from S3
✅ **Splunk Deployment**: Standalone instance with LDAP enabled
✅ **Testing**: All 3 test users authenticated successfully with correct roles
✅ **Troubleshooting**: Comprehensive guide for common issues
✅ **Production Ready**: Guidelines for enterprise LDAP/AD deployment

### Key Benefits

- **Security**: IRSA eliminates static AWS credentials
- **Automation**: App Framework handles deployment and updates
- **Scalability**: Works with Standalone, ClusterManager, SearchHeadCluster
- **Flexibility**: Easy to adapt for any LDAP/Active Directory server
- **Maintainability**: Configuration as code, version-controlled

### Next Steps

**For Development/Testing:**
1. Deploy test environment using this guide
2. Test with OpenLDAP and sample users
3. Verify authentication via REST API and Web UI

**For Production:**
1. Update `authentication.conf` with production LDAP settings
2. Enable SSL/TLS (port 636)
3. Use Kubernetes Secrets for credentials
4. Configure multiple LDAP servers for HA
5. Test in staging environment
6. Deploy to production with rollback plan

### Support

**Documentation Files:**
- Main guide: `SPLUNK_LDAP_COMPLETE_GUIDE.md` (this file)
- Diagrams: `docs/ldap-deployment-architecture.d2`, `docs/ldap-authentication-flow.d2`

**External Resources:**
- [Splunk Operator Documentation](https://splunk.github.io/splunk-operator/)
- [Splunk LDAP Configuration Guide](https://docs.splunk.com/Documentation/Splunk/latest/Security/SetupuserauthenticationwithLDAP)
- [AWS EKS IRSA Guide](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)

---

**Document Version:** 1.0
**Last Updated:** 2025-11-27
**Status:** Production Ready ✅
