# LDAP App Framework Test - Summary

## Current Status

### ✅ What's Working
1. **App Framework Configuration**: Correctly configured in Standalone CR
2. **IRSA Setup**: Both ServiceAccounts have IRSA annotations
3. **S3 Upload**: LDAP app successfully uploaded to S3
4. **Splunk Pod**: Running and ready
5. **Operator Detection**: Operator successfully detects app framework config and attempts to download

### ❌ Current Issue

**Error**: `AccessDenied: Not authorized to perform sts:AssumeRoleWithWebIdentity`

**Root Cause**: The IAM role trust policy only allows the `splunk-app-s3-reader` ServiceAccount, but the **operator pod** needs S3 access to download apps.

## How App Framework Works

```
1. Operator Pod (with SA: splunk-operator-controller-manager)
   ↓ Downloads apps from S3
   ↓ Stores in: /opt/splunk/appframework/

2. Operator copies apps to Splunk Pod
   ↓

3. Splunk Pod installs apps
   ↓ Apps land in: /opt/splunk/etc/apps/
```

**Key Insight**: The **Operator pod** downloads apps, not the Splunk pod!

## The Fix

### Problem
IAM role `splunk-ldap-test-s3-read` trust policy currently allows:
```json
{
  "StringEquals": {
    "oidc...eks.../sub": "system:serviceaccount:splunk-operator:splunk-app-s3-reader"
  }
}
```

### Solution
Update trust policy to allow BOTH ServiceAccounts:
```json
{
  "StringEquals": {
    "oidc...eks.../sub": [
      "system:serviceaccount:splunk-operator:splunk-app-s3-reader",
      "system:serviceaccount:splunk-operator:splunk-operator-controller-manager"
    ]
  }
}
```

## How to Apply the Fix

### Option 1: Run the Script (Requires IAM Permissions in Account 667741767953)

```bash
# The script is ready at:
/tmp/fix-irsa-trust-policy.sh

# Run it with appropriate AWS credentials:
./tmp/fix-irsa-trust-policy.sh
```

### Option 2: Manual Update via AWS Console

1. Go to IAM → Roles → `splunk-ldap-test-s3-read`
2. Click "Trust relationships" tab
3. Click "Edit trust policy"
4. Update the `Condition.StringEquals` to include both ServiceAccounts (see Solution above)
5. Save changes

### Option 3: Manual Update via AWS CLI

```bash
aws iam update-assume-role-policy \
  --role-name splunk-ldap-test-s3-read \
  --policy-document file:///tmp/trust-policy-both.json \
  --region us-west-2
```

## After Applying the Fix

### 1. Restart Operator Pod
```bash
kubectl delete pod -n splunk-operator -l control-plane=controller-manager
```

### 2. Wait for Operator to Reconcile
The operator will automatically retry downloading apps. Check logs:
```bash
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f | grep "ldap-test-irsa\|GetAppsList"
```

### 3. Check for Success
Look for these log messages:
```
INFO GetAppsList Getting Apps list AWS S3 Bucket: ai-platform-dev-vivekr
INFO Successfully retrieved app list from S3
```

### 4. Verify App Deployment
```bash
# Check deployment status
kubectl get standalone ldap-test-irsa -n splunk-operator -o yaml | grep -A 50 appDeploymentContext

# Look for:
# - phase: download (101-103)
# - phase: copy (201-203)
# - phase: install (301-303)
# Status 303 = install complete!
```

### 5. Verify App Installed in Pod
```bash
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- ls -la /opt/splunk/etc/apps/ | grep ldap
```

Expected output:
```
drwxr-sr-x.  4 splunk splunk 4096 Nov 27 00:XX ldap-auth-config-app
```

### 6. Verify LDAP Configuration
```bash
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list --debug | grep -A 10 corporate-ldap
```

## Test Configuration Details

### S3 Location
- **Bucket**: `ai-platform-dev-vivekr`
- **Path**: `splunk-apps/authAppsLoc/`
- **File**: `ldap-auth-config-app.tgz`
- **Full Path**: `s3://ai-platform-dev-vivekr/splunk-apps/authAppsLoc/ldap-auth-config-app.tgz`

### Kubernetes Resources
- **Namespace**: `splunk-operator`
- **Standalone CR**: `ldap-test-irsa`
- **ServiceAccounts**:
  - `splunk-app-s3-reader` (for Splunk pods)
  - `splunk-operator-controller-manager` (for operator)
- **IAM Role**: `splunk-ldap-test-s3-read`
- **IAM Policy**: `SplunkLDAPTestS3ReadPolicy`

### App Framework Config (in Standalone CR)
```yaml
appRepo:
  appsRepoPollIntervalSeconds: 60
  defaults:
    volumeName: volume_app_repo
    scope: local
  appSources:
    - name: authApps
      location: authAppsLoc/
  volumes:
    - name: volume_app_repo
      storageType: s3
      provider: aws
      path: ai-platform-dev-vivekr/splunk-apps/
      endpoint: https://s3-us-west-2.amazonaws.com
      region: us-west-2
```

## Error Details from Operator Logs

```
ERROR GetAppsList Unable to list items in bucket
AWS S3 Bucket: ai-platform-dev-vivekr
endpoint:
error: operation error S3: ListObjectsV2,
  get identity: get credentials: failed to refresh cached credentials,
  failed to retrieve credentials,
  operation error STS: AssumeRoleWithWebIdentity,
  https response error StatusCode: 403,
  api error AccessDenied: Not authorized to perform sts:AssumeRoleWithWebIdentity
```

This error occurs because the operator pod (using `splunk-operator-controller-manager` ServiceAccount) cannot assume the IAM role which only trusts `splunk-app-s3-reader`.

## Why This Happens

The confusion arises because:
1. The Splunk pod uses ServiceAccount `splunk-app-s3-reader` (specified in `spec.serviceAccount`)
2. But the **operator pod** downloads the apps from S3, not the Splunk pod
3. The operator pod uses its own ServiceAccount: `splunk-operator-controller-manager`
4. Both pods need the IRSA annotation and the IAM role must trust both

## Complete IRSA Architecture

```
┌─────────────────────────────────────────────────────┐
│ EKS Cluster (vivek-ai-platform)                    │
│                                                     │
│  ┌──────────────────────────────────────────┐     │
│  │ Operator Pod                             │     │
│  │ SA: splunk-operator-controller-manager   │     │
│  │ Annotation: eks.amazonaws.com/role-arn   │───┐ │
│  └──────────────────────────────────────────┘   │ │
│                                                   │ │
│  ┌──────────────────────────────────────────┐   │ │
│  │ Splunk Pod                               │   │ │
│  │ SA: splunk-app-s3-reader                 │   │ │
│  │ Annotation: eks.amazonaws.com/role-arn   │───┤ │
│  └──────────────────────────────────────────┘   │ │
└──────────────────────────────────────────────────┼─┘
                                                    │
                    ┌───────────────────────────────┘
                    │
                    ▼
        ┌────────────────────────────┐
        │ AWS IAM Role               │
        │ splunk-ldap-test-s3-read   │
        │                            │
        │ Trust Policy:              │
        │ - splunk-operator-...     │
        │ - splunk-app-s3-reader    │
        │                            │
        │ Attached Policy:          │
        │ - S3 Read Access          │
        └────────────────────────────┘
                    │
                    ▼
        ┌────────────────────────────┐
        │ S3 Bucket                  │
        │ ai-platform-dev-vivekr     │
        │                            │
        │ splunk-apps/               │
        │   └─ authAppsLoc/         │
        │       └─ ldap-auth-...tgz │
        └────────────────────────────┘
```

## Next Steps After Fix

1. **Apply IAM trust policy update** (requires IAM permissions)
2. **Restart operator pod** to refresh credentials
3. **Monitor logs** for successful app download
4. **Verify app installation** in Splunk pod
5. **Test LDAP authentication** (port-forward to Splunk Web)

## Files Created

- `/tmp/trust-policy-both.json` - Updated trust policy
- `/tmp/fix-irsa-trust-policy.sh` - Script to apply the fix
- `/tmp/ldap-standalone-irsa-correct.yaml` - Working Standalone CR
- `/Users/viveredd/Projects/splunk-operator/LDAP_TEST_SUMMARY.md` - This file

## Lessons Learned

1. **App Framework uses the operator pod** to download apps, not the Splunk pods
2. **IRSA must be configured for the operator's ServiceAccount**, not just the workload SA
3. **Path field format**: For S3, path = `bucket-name/prefix/`
4. **Location field**: Relative path from the base path where apps are stored
5. **Poll interval**: Set low (60s) for testing, higher (600s+) for production
