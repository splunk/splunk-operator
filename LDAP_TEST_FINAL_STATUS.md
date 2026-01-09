# LDAP App Framework Test - Final Status

## Test Completed: 2025-11-27

### Summary

Successfully configured and tested LDAP authentication deployment using Splunk Operator App Framework with IRSA (IAM Roles for Service Accounts).

## What Was Accomplished

### ✅ 1. LDAP App Creation
- Created standard Splunk app structure at `/tmp/ldap-auth-config-app/`
- Configured `authentication.conf` with LDAP settings
- Packaged as `.tgz` file
- Uploaded to S3: `s3://ai-platform-dev-vivekr/splunk-apps/authAppsLoc/ldap-auth-config-app.tgz`

### ✅ 2. App Framework Configuration
- Configured Standalone CR with correct app framework spec
- Path format: `ai-platform-dev-vivekr/splunk-apps/` (bucket + prefix)
- Location: `authAppsLoc/` (relative to path)
- Poll interval: 60 seconds (for testing)
- Successfully validated by operator

### ✅ 3. IRSA Setup
- Created IAM policy: `SplunkLDAPTestS3ReadPolicy` with S3 read permissions
- Created IAM role: `splunk-ldap-test-s3-read`
- Updated trust policy to allow BOTH ServiceAccounts:
  - `splunk-operator-controller-manager` (operator pod)
  - `splunk-app-s3-reader` (Splunk pod)
- Annotated both ServiceAccounts with IAM role ARN

### ✅ 4. Operator Behavior Verified
- Operator successfully:
  - Detects app framework configuration
  - Initializes AWS S3 client with IRSA credentials
  - Executes download, copy, and install phases
  - Updates CR status

## Current Status

### Standalone CR
- **Name**: `ldap-test-irsa`
- **Namespace**: `splunk-operator`
- **Phase**: `Ready`
- **Replicas**: 1/1
- **ServiceAccount**: `splunk-app-s3-reader`

### Operator Logs Showed
```
INFO downloadPhaseManager    # Download phase executed
INFO podCopyPhaseManager     # Copy phase executed
INFO installWorkerHandler    # Install phase executed
INFO afwSchedulerEntry All the phase managers finished
```

## Key Learning: IAM Trust Policy

**Critical Discovery**: The operator pod (not the Splunk pod) downloads apps from S3.

Therefore, the IAM role trust policy must allow **BOTH** ServiceAccounts:
1. `splunk-operator-controller-manager` - Operator needs S3 access to download apps
2. `splunk-app-s3-reader` - Splunk pod (less critical for S3, but used for consistency)

## Configuration Files Created

### 1. Trust Policy (`/tmp/trust-policy-both.json`)
```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Federated": "arn:aws:iam::667741767953:oidc-provider/oidc.eks.us-west-2.amazonaws.com/id/A5918B756AF6A17A4223F1108B9488EE"
    },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "oidc.eks.us-west-2.amazonaws.com/id/A5918B756AF6A17A4223F1108B9488EE:sub": [
          "system:serviceaccount:splunk-operator:splunk-app-s3-reader",
          "system:serviceaccount:splunk-operator:splunk-operator-controller-manager"
        ],
        "oidc.eks.us-west-2.amazonaws.com/id/A5918B756AF6A17A4223F1108B9488EE:aud": "sts.amazonaws.com"
      }
    }
  }]
}
```

### 2. Standalone CR (`/tmp/ldap-standalone-irsa-correct.yaml`)
```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: ldap-test-irsa
  namespace: splunk-operator
spec:
  replicas: 1
  serviceAccount: splunk-app-s3-reader
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
  extraEnv:
    - name: LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-credentials
          key: bind-password
```

## Verification Commands

### Check Standalone Status
```bash
kubectl get standalone ldap-test-irsa -n splunk-operator
```

### Check App Deployment Context
```bash
kubectl get standalone ldap-test-irsa -n splunk-operator -o yaml | grep -A 50 appDeploymentContext
```

Look for status codes:
- **Download**: 101 (pending), 102 (in progress), 103 (complete)
- **Copy**: 201 (pending), 202 (in progress), 203 (complete)
- **Install**: 301 (pending), 302 (in progress), 303 (complete)

### Check Operator Logs
```bash
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f | grep ldap-test-irsa
```

### Verify App Installation
```bash
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  ls -la /opt/splunk/etc/apps/ | grep ldap
```

### Verify LDAP Configuration
```bash
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf

kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list --debug | grep -A 10 corporate-ldap
```

## Test Deployment Details

### EKS Cluster
- **Name**: `vivek-ai-platform`
- **Region**: `us-west-2`
- **Account**: `667741767953`
- **OIDC Provider**: `oidc.eks.us-west-2.amazonaws.com/id/A5918B756AF6A17A4223F1108B9488EE`

### S3 Configuration
- **Bucket**: `ai-platform-dev-vivekr`
- **Path**: `splunk-apps/authAppsLoc/`
- **App File**: `ldap-auth-config-app.tgz`
- **Full S3 URI**: `s3://ai-platform-dev-vivekr/splunk-apps/authAppsLoc/ldap-auth-config-app.tgz`

### IAM Resources
- **Policy**: `SplunkLDAPTestS3ReadPolicy` (arn:aws:iam::667741767953:policy/SplunkLDAPTestS3ReadPolicy)
- **Role**: `splunk-ldap-test-s3-read` (arn:aws:iam::667741767953:role/splunk-ldap-test-s3-read)

### Kubernetes Resources
- **Namespace**: `splunk-operator`
- **Standalone CR**: `ldap-test-irsa`
- **ServiceAccounts**:
  - `splunk-app-s3-reader` (for Splunk pods)
  - `splunk-operator-controller-manager` (for operator - pre-existing)
- **Secret**: `ldap-credentials` (contains LDAP bind password)

## Lessons Learned

### 1. App Framework Download Architecture
The **operator pod** downloads apps from S3, not the Splunk pod:
```
Operator Pod (SA: splunk-operator-controller-manager)
  ↓ Downloads from S3 to /opt/splunk/appframework/
  ↓ Copies to Splunk pod
Splunk Pod (SA: splunk-app-s3-reader)
  ↓ Installs to /opt/splunk/etc/apps/
```

### 2. Path Configuration for S3
The `path` field in volumes must include:
- Bucket name: `ai-platform-dev-vivekr`
- Path prefix: `splunk-apps/`
- Combined: `ai-platform-dev-vivekr/splunk-apps/`

The `location` in appSources is relative to this path:
- Location: `authAppsLoc/`
- Final path: `s3://ai-platform-dev-vivekr/splunk-apps/authAppsLoc/`

### 3. IRSA Requirements
For app framework with IRSA:
1. IAM policy with S3 read permissions (GetObject, ListBucket)
2. IAM role with OIDC trust policy
3. Trust policy must include operator's ServiceAccount
4. ServiceAccount annotation: `eks.amazonaws.com/role-arn`
5. No `secretRef` needed in volumes spec

### 4. Troubleshooting Steps
1. Check operator logs for "AccessDenied" errors
2. Verify ServiceAccount annotations
3. Verify IAM role trust policy includes correct ServiceAccount
4. Check app deployment status in CR status field
5. Monitor operator logs for download/copy/install phases

## Production Recommendations

1. **IRSA is Required**: Always use IRSA instead of static credentials for security
2. **Poll Interval**: Use longer intervals in production (600+ seconds)
3. **IAM Least Privilege**: Grant read-only S3 access, no write
4. **App Versioning**: Version apps in S3, use semantic versioning
5. **Monitoring**: Set up alerts on app deployment failures
6. **Testing**: Always test in dev/staging before production
7. **CloudTrail**: Enable CloudTrail to audit S3 access
8. **Secrets Management**: Use AWS Secrets Manager for LDAP passwords with External Secrets Operator

## Documentation References

- [Splunk Operator App Framework](https://github.com/splunk/splunk-operator/blob/main/docs/AppFramework.md)
- [AWS IRSA Documentation](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- [Splunk authentication.conf](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Authenticationconf)

## Files Created During Testing

- `/tmp/ldap-auth-config-app/` - LDAP app source
- `/tmp/ldap-auth-config-app.tgz` - Packaged app
- `/tmp/trust-policy-both.json` - IAM trust policy
- `/tmp/ldap-standalone-irsa-correct.yaml` - Working Standalone CR
- `/tmp/fix-irsa-trust-policy.sh` - IAM update script
- `/Users/viveredd/Projects/splunk-operator/LDAP_TEST_SUMMARY.md` - Detailed guide
- `/Users/viveredd/Projects/splunk-operator/LDAP_TEST_FINAL_STATUS.md` - This file

## Next Steps for Customer

1. **Review Configuration**: The LDAP app template at `/tmp/ldap-auth-config-app/`
2. **Customize for Environment**: Update LDAP server, bind DN, search bases, etc.
3. **Test in Dev**: Deploy to development environment first
4. **Verify LDAP Login**: Test authentication with actual LDAP credentials
5. **Production Deployment**: Roll out to production with appropriate poll intervals
6. **Monitor**: Set up monitoring for app deployment status

## Test Conclusion

The LDAP app framework with IRSA configuration has been successfully implemented and tested. The operator can download apps from S3 using IRSA, and the app framework pipeline (download → copy → install) executes successfully.

The key breakthrough was understanding that the operator pod (not the Splunk pod) downloads apps, requiring the IAM trust policy to include the operator's ServiceAccount.

This provides a production-ready, secure method for deploying LDAP configuration (or any Splunk app) without custom Docker images or manual configuration.
