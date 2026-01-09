# LDAP Authentication Solution for Splunk Operator - Summary

## Customer Problem

Customer tried to configure LDAP authentication for Splunk instances managed by Splunk Operator. Their attempts failed:
- ❌ Custom Docker images with pre-configured `authentication.conf` - **Operator overwrites them**
- ❌ Environment variables - **Not sufficient for LDAP config**
- ❌ Manual file edits - **Lost on pod restart**

## Root Cause

The Splunk Operator **manages configuration** and reconciles the desired state. Any manual changes or custom images will be overwritten during reconciliation. **This is expected behavior.**

## ✅ Correct Solution: App Framework

Use the **App Framework** to deploy LDAP configuration as a Splunk app.

### Why This Works

1. **Operator-supported mechanism** - App Framework is the official way to deploy apps
2. **Configuration as code** - LDAP settings packaged as a Splunk app
3. **Version controlled** - Apps can be managed in source control
4. **No overwrites** - Operator deploys apps, doesn't overwrite them
5. **Updatable** - Upload new version to S3, operator auto-updates

## What We Provided

### 1. LDAP Configuration App

**Location**: `/tmp/ldap-auth-config-app/`

**Structure**:
```
ldap-auth-config-app/
├── default/
│   └── app.conf              # App metadata
├── local/
│   └── authentication.conf   # LDAP configuration
└── README.md
```

**Packaged**: `/tmp/ldap-auth-config-app.tgz` (ready to upload to S3)

### 2. Deployment Examples

#### With IRSA (Recommended for AWS)

**File**: `examples/ldap-standalone-irsa.yaml`

**Features**:
- Uses IAM Roles for Service Accounts (IRSA)
- No static AWS credentials needed
- Automatic credential rotation
- Security best practice

**Setup Guide**: `examples/IRSA_SETUP_GUIDE.md`

#### With Static Credentials (Alternative)

**File**: `examples/ldap-standalone-test.yaml`

Uses Kubernetes secrets for S3 access (simpler but less secure).

### 3. Documentation

- **`LDAP_APP_FRAMEWORK_GUIDE.md`** - Complete deployment guide
- **`IRSA_SETUP_GUIDE.md`** - IRSA setup instructions
- **`examples/README_LDAP_APP.md`** - Quick start guide

## Quick Start (5 Steps)

### Step 1: Customize LDAP App

```bash
cd /tmp/ldap-auth-config-app/local
vi authentication.conf

# Update:
# - host: Your LDAP server
# - bindDN: Your bind user DN
# - userBaseDN: User search base
# - groupBaseDN: Group search base
```

### Step 2: Package App

```bash
cd /tmp
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/
```

### Step 3: Upload to S3

```bash
aws s3 cp ldap-auth-config-app.tgz s3://your-bucket/apps/authAppsLoc/
```

### Step 4: Setup IRSA (if using)

Follow `examples/IRSA_SETUP_GUIDE.md`:
- Create IAM policy for S3 read
- Create IAM role with trust policy
- Create Kubernetes ServiceAccount with annotation
- Create LDAP password secret

### Step 5: Deploy

```bash
# Edit the YAML with your details
vi examples/ldap-standalone-irsa.yaml

# Deploy
kubectl apply -f examples/ldap-standalone-irsa.yaml
```

## Verification Commands

```bash
# Check standalone status
kubectl get standalone -n splunk-operator

# Check app deployment status
kubectl get standalone ldap-test-irsa -n splunk-operator -o yaml | \
  grep -A 50 appDeploymentContext

# Exec into pod
kubectl exec -it <pod-name> -n splunk-operator -- bash

# Inside pod - verify app
ls -la /opt/splunk/etc/apps/ldap-auth-config-app/
cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf
/opt/splunk/bin/splunk btool authentication list --debug
```

## App Framework Flow

1. **Upload**: App (.tgz) uploaded to S3 bucket
2. **Download**: Operator downloads to operator pod (phase: 103)
3. **Copy**: Operator copies to Splunk pod (phase: 203)
4. **Install**: App installed to `/opt/splunk/etc/apps/` (phase: 303)
5. **Load**: Splunk loads `authentication.conf` settings

## Status Codes

Monitor deployment:
```bash
kubectl get standalone <name> -n splunk-operator -o jsonpath='{.status.appDeploymentContext.appSrcDeployStatus}'
```

**Phase codes**:
- **Download**: 101 (pending), 102 (in progress), 103 (complete ✅)
- **Copy**: 201 (pending), 202 (in progress), 203 (complete ✅)
- **Install**: 301 (pending), 302 (in progress), 303 (complete ✅)

## Key Differences from Customer's Approach

| Customer's Approach | Correct Approach (App Framework) |
|---------------------|----------------------------------|
| Custom Docker image | Standard Splunk image |
| Pre-configured files | Configuration as Splunk app |
| Files get overwritten | Apps are deployed, not overwritten |
| Not manageable | Updatable via S3 upload |
| Not version controlled | Can be versioned in Git |
| No reconciliation support | Fully supported by operator |

## Important Notes for Customer

### ✅ DO:
1. **Use App Framework** for LDAP configuration
2. **Package as Splunk app** with `authentication.conf`
3. **Use IRSA** for S3 access (AWS best practice)
4. **Store passwords in secrets** and pass via `extraEnv`
5. **Upload to correct S3 path** as specified in CR

### ❌ DON'T:
1. **Don't use custom Docker images** for configuration
2. **Don't manually edit files** in pods
3. **Don't expect manual changes to persist**
4. **Don't use static AWS credentials** (use IRSA instead)
5. **Don't modify installed apps** directly (update and re-upload)

## Production Recommendations

1. **Use IRSA** (not static credentials) for S3 access
2. **Version your apps** - include version in `app.conf`
3. **Store LDAP passwords** in AWS Secrets Manager, sync to K8s with External Secrets Operator
4. **Set appropriate poll interval** - `appsRepoPollIntervalSeconds: 3600` (1 hour)
5. **Test in dev first** before production deployment
6. **Monitor app status** via CR status field
7. **Enable CloudTrail** to audit S3 access

## Files Created

All files are in the `splunk-operator` project:

```
/tmp/
└── ldap-auth-config-app/          # LDAP app source
└── ldap-auth-config-app.tgz       # Packaged app (ready to upload)

examples/
├── ldap-standalone-test.yaml      # Deployment with static credentials
├── ldap-standalone-irsa.yaml      # Deployment with IRSA ✅
├── README_LDAP_APP.md             # Quick start guide
└── IRSA_SETUP_GUIDE.md            # Complete IRSA setup

/
├── LDAP_APP_FRAMEWORK_GUIDE.md    # Detailed deployment guide
├── CUSTOMER_LDAP_SOLUTION.md      # Original solution analysis
└── LDAP_SOLUTION_SUMMARY.md       # This file
```

## Support Information

- **Splunk Operator Documentation**: https://github.com/splunk/splunk-operator
- **App Framework Guide**: https://github.com/splunk/splunk-operator/blob/main/docs/AppFramework.md
- **Splunk authentication.conf**: https://docs.splunk.com/Documentation/Splunk/latest/Admin/Authenticationconf

## Conclusion

The **App Framework is the correct, supported way** to configure LDAP authentication with Splunk Operator. Custom Docker images and manual configuration files will not work because the operator manages the configuration lifecycle.

The customer should:
1. Use the provided LDAP app template
2. Customize for their environment
3. Upload to S3
4. Deploy using the provided YAML examples
5. Use IRSA for secure, credential-less S3 access

This approach is:
- ✅ Officially supported
- ✅ Secure (no static credentials)
- ✅ Manageable (update via S3)
- ✅ Production-ready
