# LDAP Authentication Configuration via App Framework

## Summary for Customer

This example demonstrates how to configure LDAP authentication for Splunk Enterprise managed by the Splunk Operator using the **App Framework** approach.

### Why App Framework?

The App Framework is the **correct and supported way** to deploy LDAP configuration because:
- ✅ The operator manages apps via App Framework
- ✅ Configuration is packaged as a Splunk app
- ✅ Apps can be updated by uploading new versions to remote storage
- ✅ The operator won't overwrite app-deployed configuration

### What We Created

1. **LDAP Configuration App** (`/tmp/ldap-auth-config-app.tgz`):
   - Standard Splunk app structure with `authentication.conf`
   - Configures LDAP authentication settings
   - Can be customized for your environment

2. **Test Deployment** (`examples/ldap-standalone-test.yaml`):
   - Standalone Splunk instance
   - App Framework configured to deploy the LDAP app
   - Environment variable for LDAP bind password

## Quick Start

### 1. Customize the LDAP App

Edit the authentication.conf in the app:

```bash
cd /tmp/ldap-auth-config-app/local
vi authentication.conf

# Update these settings for your environment:
# - host: Your LDAP server hostname
# - bindDN: Your LDAP bind user DN
# - userBaseDN: Base DN for users
# - groupBaseDN: Base DN for groups
```

### 2. Package the App

```bash
cd /tmp
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/
```

### 3. Upload to S3 (or Azure/GCS)

```bash
# Example for S3:
aws s3 cp ldap-auth-config-app.tgz s3://your-bucket/Standalone-us/authAppsLoc/
```

### 4. Create Secrets

```bash
# LDAP bind password
kubectl create secret generic ldap-credentials \
  --from-literal=bind-password='YourLDAPPassword' \
  -n splunk-operator

# S3 credentials (if not using IAM)
kubectl create secret generic s3-secret \
  --from-literal=s3_access_key='YOUR_KEY' \
  --from-literal=s3_secret_key='YOUR_SECRET' \
  -n splunk-operator
```

### 5. Update and Deploy

Edit `examples/ldap-standalone-test.yaml` to match your S3 configuration:
- Update `path`, `endpoint`, and `region` in the volumes section

Then deploy:

```bash
kubectl apply -f examples/ldap-standalone-test.yaml
```

### 6. Verify

```bash
# Check pod status
kubectl get standalone ldap-test -n splunk-operator
kubectl get pods -n splunk-operator

# Check app deployment status
kubectl get standalone ldap-test -n splunk-operator -o yaml | \
  grep -A 30 appDeploymentContext

# Exec into pod and verify
kubectl exec -it ldap-test-standalone-0 -n splunk-operator -- bash
ls -la /opt/splunk/etc/apps/ldap-auth-config-app/
cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf
```

## App Structure

The LDAP app created at `/tmp/ldap-auth-config-app/`:

```
ldap-auth-config-app/
├── default/
│   └── app.conf              # App metadata
├── local/
│   └── authentication.conf   # LDAP settings
└── README.md
```

## App Framework Flow

1. **Upload**: App tarball uploaded to S3/Azure/GCS
2. **Download**: Operator downloads app to operator pod
3. **Copy**: Operator copies app to Splunk pod(s)
4. **Install**: App installed to `/opt/splunk/etc/apps/`
5. **Load**: Splunk loads the authentication.conf settings

## Status Codes

Monitor app deployment via CR status:

- **Download**: 103 = complete
- **Copy**: 203 = complete
- **Install**: 303 = complete ✅

```bash
kubectl get standalone ldap-test -n splunk-operator -o jsonpath='{.status.appDeploymentContext.appSrcDeployStatus}'
```

## Key Points for Customer

1. **DO NOT use custom Docker images** - The operator will overwrite configuration
2. **DO use App Framework** - This is the supported mechanism
3. **DO package as Splunk app** - Standard app structure with authentication.conf
4. **DO use secrets for passwords** - Pass via extraEnv with secretKeyRef
5. **DO follow exact S3 path structure** - App must be in the location specified in appSources

## Files Provided

- **`/tmp/ldap-auth-config-app/`** - LDAP configuration app (ready to customize)
- **`/tmp/ldap-auth-config-app.tgz`** - Packaged app (ready to upload)
- **`examples/ldap-standalone-test.yaml`** - Test deployment manifest
- **`LDAP_APP_FRAMEWORK_GUIDE.md`** - Complete deployment guide

## Next Steps

1. Customize the LDAP app for your environment
2. Upload to your S3/Azure/GCS bucket
3. Update the deployment YAML with your storage details
4. Deploy and test

This is the **correct, supported approach** for LDAP configuration with Splunk Operator.
