# Testing LDAP Configuration with IRSA - Complete Instructions

## What Was Created

This solution provides a complete, production-ready approach to configure LDAP authentication for Splunk Enterprise managed by Splunk Operator using:

1. **App Framework** - The correct, supported mechanism
2. **IRSA** - IAM Roles for Service Accounts (no static credentials)
3. **Splunk App** - LDAP configuration packaged as a standard Splunk app

## Files Created

```
/tmp/
├── ldap-auth-config-app/               # LDAP app source
│   ├── default/app.conf                # App metadata
│   ├── local/authentication.conf       # LDAP configuration
│   └── README.md                       # App documentation
└── ldap-auth-config-app.tgz            # Packaged app

splunk-operator/
├── test-ldap-complete.sh               # 🚀 Automated test script
├── TEST_INSTRUCTIONS.md                # This file
├── LDAP_SOLUTION_SUMMARY.md            # Complete solution summary
├── LDAP_APP_FRAMEWORK_GUIDE.md         # Detailed guide
├── CUSTOMER_LDAP_SOLUTION.md           # Original solution
└── examples/
    ├── ldap-standalone-test.yaml       # Static credentials version
    ├── ldap-standalone-irsa.yaml       # IRSA version (recommended)
    ├── README_LDAP_APP.md              # Quick start
    └── IRSA_SETUP_GUIDE.md             # Manual IRSA setup
```

## Quick Test (Automated)

### 1. Edit Configuration

Edit `test-ldap-complete.sh` and update these variables (lines 15-21):

```bash
export CLUSTER_NAME="your-eks-cluster-name"
export AWS_REGION="us-west-2"
export S3_BUCKET="your-s3-bucket-name"
export NAMESPACE="splunk-operator"
export LDAP_BIND_PASSWORD="YourSecureLDAPPassword"
export LDAP_SERVER="ldap.example.com"
export LDAP_BIND_DN="cn=splunk-bind,ou=service-accounts,dc=example,dc=com"
```

### 2. Run the Test

```bash
cd /Users/viveredd/Projects/splunk-operator

# Run the complete test
./test-ldap-complete.sh
```

The script will:
- ✅ Enable OIDC for EKS
- ✅ Create IAM policy for S3 read
- ✅ Create IAM role with IRSA trust policy
- ✅ Customize LDAP app with your settings
- ✅ Package and upload app to S3
- ✅ Create Kubernetes ServiceAccount with IRSA annotation
- ✅ Create LDAP password secret
- ✅ Deploy Standalone with App Framework
- ✅ Wait for pod to be ready
- ✅ Verify IRSA is working
- ✅ Verify app deployment
- ✅ Verify LDAP configuration

### 3. Cleanup

```bash
./test-ldap-complete.sh cleanup
```

## Manual Test (Step by Step)

If you prefer to run steps manually, follow the [IRSA_SETUP_GUIDE.md](examples/IRSA_SETUP_GUIDE.md).

## Verification Steps

### Check Standalone Status

```bash
kubectl get standalone ldap-test-irsa -n splunk-operator
```

Expected:
```
NAME             PHASE   DESIRED   READY   AGE
ldap-test-irsa   Ready   1         1       5m
```

### Check App Deployment Status

```bash
kubectl get standalone ldap-test-irsa -n splunk-operator -o yaml | \
  grep -A 50 appDeploymentContext
```

Look for:
```yaml
appDeploymentContext:
  appSrcDeployStatus:
    authApps:
      appDeploymentInfo:
      - appName: ldap-auth-config-app.tgz
        phaseInfo:
          phase: install
          status: 303  # 303 = install complete ✅
```

### Verify IRSA

```bash
POD_NAME=$(kubectl get pods -n splunk-operator -l app.kubernetes.io/instance=ldap-test-irsa -o jsonpath='{.items[0].metadata.name}')

# Check AWS env vars (injected by IRSA)
kubectl exec -n splunk-operator $POD_NAME -- env | grep AWS

# Test S3 access
kubectl exec -n splunk-operator $POD_NAME -- \
  aws s3 ls s3://your-bucket/apps/authAppsLoc/
```

### Verify LDAP Configuration

```bash
# Exec into pod
kubectl exec -it $POD_NAME -n splunk-operator -- bash

# Inside pod - check app
ls -la /opt/splunk/etc/apps/ldap-auth-config-app/

# View authentication.conf
cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf

# Check Splunk config (btool)
/opt/splunk/bin/splunk btool authentication list --debug | grep -A 20 corporate-ldap

# Check Splunk logs
tail -100 /opt/splunk/var/log/splunk/splunkd.log | grep -i ldap
```

### Test LDAP Login

1. **Port forward to Splunk Web:**
```bash
kubectl port-forward svc/splunk-ldap-test-irsa-service 8000:8000 -n splunk-operator
```

2. **Open browser:** https://localhost:8000

3. **Login with LDAP credentials:**
   - Username: your-ldap-username
   - Password: your-ldap-password

## Expected Results

### ✅ Success Indicators

1. **Pod Status**: `Running` with `Ready 1/1`
2. **App Status**: Phase `install`, Status `303`
3. **IRSA**: AWS env vars present in pod
4. **S3 Access**: Can list bucket contents from pod
5. **App Installed**: `/opt/splunk/etc/apps/ldap-auth-config-app/` exists
6. **Config Loaded**: `btool` shows LDAP settings
7. **LDAP Login**: Can login with LDAP credentials

### ❌ Troubleshooting

#### App Not Downloading (Status 199)

**Check:**
```bash
# Check operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager | grep -i download

# Verify S3 bucket/path
aws s3 ls s3://your-bucket/apps/authAppsLoc/
```

**Common causes:**
- Wrong S3 bucket name
- Wrong S3 path
- IRSA not configured correctly
- IAM policy doesn't allow s3:GetObject

#### IRSA Not Working

**Check:**
```bash
# Verify OIDC provider
aws eks describe-cluster --name your-cluster | grep oidc

# Check IAM role trust policy
aws iam get-role --role-name splunk-app-framework-s3-read | jq .Role.AssumeRolePolicyDocument

# Check ServiceAccount annotation
kubectl get sa splunk-app-s3-reader -n splunk-operator -o yaml | grep eks.amazonaws.com
```

#### LDAP Bind Fails

**Check:**
```bash
# Verify password secret
kubectl get secret ldap-credentials -n splunk-operator -o yaml

# Check env var in pod
kubectl exec $POD_NAME -n splunk-operator -- env | grep LDAP_BIND_PASSWORD

# Test LDAP connectivity
kubectl exec $POD_NAME -n splunk-operator -- \
  ldapsearch -x -H ldaps://ldap.example.com:636 \
  -D "cn=splunk-bind,ou=service-accounts,dc=example,dc=com" \
  -w "$LDAP_BIND_PASSWORD" \
  -b "dc=example,dc=com" "(uid=testuser)"
```

## Monitoring App Deployment

Watch the app deployment in real-time:

```bash
# Watch CR status
watch 'kubectl get standalone ldap-test-irsa -n splunk-operator -o yaml | grep -A 30 appDeploymentContext'

# Follow operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f | \
  grep -E "(ldap|download|copy|install)"
```

## CloudTrail Verification

Verify IRSA is working by checking CloudTrail:

```bash
aws cloudtrail lookup-events \
  --lookup-attributes AttributeKey=Username,AttributeValue=splunk-app-framework-s3-read \
  --region us-west-2 \
  --max-items 10
```

You should see S3 GetObject events from the IAM role.

## Performance Testing

Test LDAP authentication performance:

```bash
# Inside Splunk pod
time /opt/splunk/bin/splunk login -auth ldap-user:ldap-password

# Check authentication logs
grep "authentication" /opt/splunk/var/log/splunk/splunkd.log | tail -50
```

## Production Deployment Checklist

Before deploying to production:

- [ ] Customize LDAP app for your environment
- [ ] Test LDAP connectivity from EKS to LDAP server
- [ ] Verify SSL certificates for LDAPS
- [ ] Set appropriate role mappings
- [ ] Use AWS Secrets Manager for LDAP password
- [ ] Set `appsRepoPollIntervalSeconds` to appropriate value (e.g., 3600)
- [ ] Enable CloudTrail logging
- [ ] Set up monitoring/alerts for app deployment failures
- [ ] Test in dev/staging first
- [ ] Document runbook for updates
- [ ] Version the LDAP app (update app.conf version field)

## Updating LDAP Configuration

To update the LDAP configuration after initial deployment:

1. **Modify app locally:**
```bash
cd /tmp/ldap-auth-config-app/local
vi authentication.conf
# Make your changes
```

2. **Re-package (SAME filename):**
```bash
cd /tmp
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/
```

3. **Upload to S3 (overwrite):**
```bash
aws s3 cp ldap-auth-config-app.tgz s3://your-bucket/apps/authAppsLoc/
```

4. **Wait for auto-update** or **trigger manually:**
```bash
kubectl patch cm/splunk-splunk-operator-ldap-test-irsa-configmap \
  -n splunk-operator \
  --type merge \
  -p '{"data":{"manualUpdate":"true"}}'
```

## Support

For issues:

1. Check operator logs
2. Check CR status (appDeploymentContext)
3. Check Splunk pod logs
4. Verify IRSA configuration
5. Test S3 access from pod
6. Check IAM policies/roles

## References

- [LDAP_SOLUTION_SUMMARY.md](LDAP_SOLUTION_SUMMARY.md) - Complete solution overview
- [IRSA_SETUP_GUIDE.md](examples/IRSA_SETUP_GUIDE.md) - Detailed IRSA setup
- [LDAP_APP_FRAMEWORK_GUIDE.md](LDAP_APP_FRAMEWORK_GUIDE.md) - Full deployment guide
- [Splunk Operator Docs](https://github.com/splunk/splunk-operator)
- [App Framework Docs](https://github.com/splunk/splunk-operator/blob/main/docs/AppFramework.md)

## Summary

This solution demonstrates the **correct, supported way** to configure LDAP authentication with Splunk Operator:

✅ Uses App Framework (not custom images)
✅ Uses IRSA (not static credentials)
✅ Configuration as code (Splunk app)
✅ Secure (secrets for passwords)
✅ Manageable (update via S3)
✅ Production-ready

The automated test script handles all the complexity - just update the variables and run it!
