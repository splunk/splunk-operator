# LDAP App Framework - IAM Fix Required

## Current Status: ❌ IAM Permissions Issue

### Problem Identified

The operator is failing to list apps from S3 with this error:
```
ERROR initAndCheckAppInfoStatus Unable to get apps list, will retry in next reconcile...
error: unable to get apps list from remote storage list for all the apps
```

### Root Cause

The IAM role `splunk-ldap-test-s3-read` trust policy **does not include the operator's ServiceAccount** yet.

**What's configured:**
- ✅ LDAP app uploaded to S3: `s3://ai-platform-dev-vivekr/splunk-apps/authAppsLoc/ldap-auth-config-app.tgz`
- ✅ Standalone CR with App Framework configuration
- ✅ Operator ServiceAccount has IRSA annotation
- ✅ Splunk pod ServiceAccount has IRSA annotation
- ❌ IAM role trust policy only allows Splunk pod SA (not operator SA)

**Why it fails:**
The **operator pod** (not the Splunk pod) downloads apps from S3. The operator uses ServiceAccount `splunk-operator-controller-manager`, which is NOT in the IAM trust policy.

## The Fix

### Option 1: Automated Script (Recommended)

Run this script with AWS credentials for account **667741767953**:

```bash
/tmp/verify-and-fix-irsa.sh
```

The script will:
1. Check current trust policy
2. Update it to include both ServiceAccounts
3. Verify the update
4. Provide next steps

### Option 2: AWS CLI Manual Update

```bash
# Use credentials for account 667741767953
aws iam update-assume-role-policy \
  --role-name splunk-ldap-test-s3-read \
  --policy-document file:///tmp/trust-policy-both.json \
  --region us-west-2
```

### Option 3: AWS Console

1. Switch to AWS account: **667741767953**
2. Go to IAM → Roles → `splunk-ldap-test-s3-read`
3. Click "Trust relationships" tab
4. Click "Edit trust policy"
5. Update the condition to include BOTH ServiceAccounts:

```json
{
  "Condition": {
    "StringEquals": {
      "oidc.eks.us-west-2.amazonaws.com/id/A5918B756AF6A17A4223F1108B9488EE:sub": [
        "system:serviceaccount:splunk-operator:splunk-app-s3-reader",
        "system:serviceaccount:splunk-operator:splunk-operator-controller-manager"
      ],
      "oidc.eks.us-west-2.amazonaws.com/id/A5918B756AF6A17A4223F1108B9488EE:aud": "sts.amazonaws.com"
    }
  }
}
```

## After Applying the Fix

### 1. Restart Operator Pod
```bash
kubectl delete pod -n splunk-operator -l control-plane=controller-manager
```

### 2. Monitor Operator Logs (wait ~30 seconds)
```bash
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f | grep "ldap-test-irsa"
```

**Look for success indicators:**
- `INFO initAndCheckAppInfoStatus Checking status of apps on remote storage...`
- No more ERROR messages about "unable to get apps list"
- `INFO downloadPhaseManager` with actual download activity
- `INFO afwSchedulerEntry All the phase managers finished`

### 3. Verify App Installation
```bash
# Wait 2-3 minutes for app deployment pipeline to complete
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  ls -la /opt/splunk/etc/apps/ | grep ldap
```

**Expected output:**
```
drwxr-sr-x.  4 splunk splunk 4096 Nov 27 XX:XX ldap-auth-config-app
```

### 4. Verify LDAP Configuration
```bash
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf
```

Should show the LDAP configuration with corporate-ldap strategy.

## Why This Happened

The confusion arose because:

1. **Splunk pod** uses ServiceAccount: `splunk-app-s3-reader` (specified in `spec.serviceAccount`)
2. **Operator pod** downloads apps from S3 (not the Splunk pod)
3. **Operator pod** uses ServiceAccount: `splunk-operator-controller-manager`
4. **Both pods** need S3 access:
   - Operator pod: To download apps from S3
   - Splunk pod: (Potentially for other app framework operations)

## Architecture Flow

```
┌────────────────────────────────────────┐
│ Operator Pod                           │
│ SA: splunk-operator-controller-manager │ ──┐
│ Annotation: IAM role ARN               │   │
└────────────────────────────────────────┘   │
                                               │
                                               ├──> AssumeRole
                                               │
┌────────────────────────────────────────┐   │
│ Splunk Pod                             │   │
│ SA: splunk-app-s3-reader               │ ──┘
│ Annotation: IAM role ARN               │
└────────────────────────────────────────┘
                │
                ▼
┌────────────────────────────────────────┐
│ IAM Role: splunk-ldap-test-s3-read     │
│                                        │
│ Trust Policy MUST allow BOTH:          │
│ - splunk-operator-controller-manager   │ ← REQUIRED for downloads
│ - splunk-app-s3-reader                 │ ← For consistency
└────────────────────────────────────────┘
                │
                ▼
┌────────────────────────────────────────┐
│ S3: ai-platform-dev-vivekr             │
│     splunk-apps/authAppsLoc/           │
│     └─ ldap-auth-config-app.tgz        │
└────────────────────────────────────────┘
```

## Test Configuration Details

- **EKS Cluster**: vivek-ai-platform (account 667741767953)
- **S3 Bucket**: ai-platform-dev-vivekr
- **S3 Path**: splunk-apps/authAppsLoc/
- **App File**: ldap-auth-config-app.tgz (2198 bytes, uploaded Nov 26)
- **IAM Role**: splunk-ldap-test-s3-read
- **IAM Policy**: SplunkLDAPTestS3ReadPolicy (S3 read-only)
- **Namespace**: splunk-operator
- **Standalone CR**: ldap-test-irsa

## Files Reference

- `/tmp/trust-policy-both.json` - Correct trust policy with both SAs
- `/tmp/verify-and-fix-irsa.sh` - Automated fix script
- `/tmp/ldap-standalone-irsa-correct.yaml` - Working Standalone CR
- `/Users/viveredd/Projects/splunk-operator/LDAP_TEST_FINAL_STATUS.md` - Complete test documentation
- `/Users/viveredd/Projects/splunk-operator/LDAP_TEST_SUMMARY.md` - Detailed setup guide

## Next Action Required

**YOU MUST UPDATE THE IAM TRUST POLICY** using one of the three options above.

This cannot be done automatically because:
- Current AWS credentials are for Bedrock account (387769110234)
- IAM role exists in EKS cluster account (667741767953)
- Cross-account IAM modification is not possible

Once the trust policy is updated and the operator is restarted, the app framework will work immediately.
