# LDAP Configuration via App Framework - Complete Guide

This guide shows how to configure LDAP authentication for Splunk Enterprise using the Splunk Operator's App Framework.

## Overview

Instead of using `defaultsUrl` or inline configuration, this approach packages the LDAP configuration as a Splunk app and deploys it via App Framework. This is the **recommended approach for production environments** because:

✅ Apps are version-controlled and can be managed in source control
✅ Changes are tracked and can be rolled back
✅ Configuration is portable across environments
✅ Supports automatic updates when the app is modified

## Prerequisites

1. **Splunk Operator installed** in your Kubernetes cluster
2. **Remote storage** (S3, Azure Blob, or GCS) configured and accessible
3. **LDAP server** accessible from your Kubernetes cluster
4. **kubectl** configured to access your cluster

## Step 1: Create the LDAP Configuration App

The app has been created at `/tmp/ldap-auth-config-app/` with the following structure:

```
ldap-auth-config-app/
├── default/
│   └── app.conf              # App metadata
├── local/
│   └── authentication.conf   # LDAP configuration
└── README.md                 # Documentation
```

### Customize the Configuration

Edit `/tmp/ldap-auth-config-app/local/authentication.conf` to match your environment:

```bash
# Edit the authentication.conf file
vi /tmp/ldap-auth-config-app/local/authentication.conf
```

**Key settings to update:**
- `host`: Your LDAP server hostname (e.g., `ldap.company.com`)
- `port`: LDAP port (636 for LDAPS, 389 for plain LDAP)
- `bindDN`: LDAP bind user DN
- `userBaseDN`: Base DN for user searches
- `groupBaseDN`: Base DN for group searches
- Role mappings in `[roleMap_corporate-ldap]` section

## Step 2: Package the App

```bash
cd /tmp
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/

# Verify the package
tar -tzf ldap-auth-config-app.tgz
```

## Step 3: Upload to Remote Storage

### For AWS S3:

```bash
# Create the folder structure in S3
aws s3 mb s3://your-bucket-name/apps/ldap-config/

# Upload the app
aws s3 cp ldap-auth-config-app.tgz s3://your-bucket-name/apps/ldap-config/

# Verify upload
aws s3 ls s3://your-bucket-name/apps/ldap-config/
```

### For Azure Blob:

```bash
# Create container and upload
az storage blob upload \
  --account-name mystorageaccount \
  --container-name apps \
  --name ldap-config/ldap-auth-config-app.tgz \
  --file ldap-auth-config-app.tgz
```

### For GCS:

```bash
# Upload to GCS bucket
gsutil cp ldap-auth-config-app.tgz gs://your-bucket-name/apps/ldap-config/

# Verify upload
gsutil ls gs://your-bucket-name/apps/ldap-config/
```

## Step 4: Create Kubernetes Secrets

### LDAP Credentials Secret

```bash
# Create secret with LDAP bind password
kubectl create secret generic ldap-credentials \
  --from-literal=bind-password='YourActualLDAPBindPassword' \
  -n splunk-operator
```

### Storage Credentials Secret

**For AWS S3:**
```bash
kubectl create secret generic s3-secret \
  --from-literal=s3_access_key='AKIAIOSFODNN7EXAMPLE' \
  --from-literal=s3_secret_key='wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY' \
  -n splunk-operator
```

**For Azure Blob:**
```bash
kubectl create secret generic azureblob-secret \
  --from-literal=azure_sa_name='mystorageaccount' \
  --from-literal=azure_sa_secret_key='YourAzureStorageKey' \
  -n splunk-operator
```

**For GCS:**
```bash
kubectl create secret generic gcs-secret \
  --from-file=key.json=/path/to/service-account-key.json \
  -n splunk-operator
```

## Step 5: Deploy Standalone with LDAP App

### Update the Deployment YAML

Edit `test-ldap-app-deployment.yaml` and update:

1. **Storage configuration** (lines 44-51):
   - `path`: Your S3 bucket path (e.g., `my-company-bucket/apps/`)
   - `endpoint`: Your S3 endpoint
   - `region`: Your AWS region
   - `secretRef`: Match your secret name

2. **App source location** (line 36):
   - `location`: Folder in S3 containing the app (e.g., `ldap-config/`)

### Apply the Configuration

```bash
# Apply all resources
kubectl apply -f test-ldap-app-deployment.yaml

# Watch the pod creation
kubectl get pods -n splunk-operator -w
```

## Step 6: Verify Deployment

### Check Pod Status

```bash
# Check if standalone pod is running
kubectl get standalone -n splunk-operator
kubectl get pods -n splunk-operator
```

Expected output:
```
NAME                   PHASE   DESIRED   READY   AGE
standalone-ldap-test   Ready   1         1       5m

NAME                         READY   STATUS    RESTARTS   AGE
standalone-ldap-test-0       1/1     Running   0          5m
```

### Check App Framework Status

```bash
# Get detailed CR status
kubectl get standalone standalone-ldap-test -n splunk-operator -o yaml | grep -A 50 appDeploymentContext
```

Look for:
- `phase: install`
- `status: 303` (install complete)
- `bundlePushStage: 3` (if applicable)

Expected output:
```yaml
appDeploymentContext:
  appSrcDeployStatus:
    ldap-auth-apps:
      appDeploymentInfo:
      - appName: ldap-auth-config-app.tgz
        phaseInfo:
          phase: install
          status: 303  # 303 = install complete
```

### Check Operator Logs

```bash
# View operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f | grep -i "ldap-auth"
```

Look for messages like:
- `INFO initAndCheckAppInfoStatus Apps List retrieved from remote storage`
- `App Package download is complete`
- `App Package install is complete`

## Step 7: Verify LDAP Configuration

### Exec into Splunk Pod

```bash
kubectl exec -it standalone-ldap-test-0 -n splunk-operator -- bash
```

### Inside the Pod - Check Configuration

```bash
# 1. Verify the app is installed
ls -la /opt/splunk/etc/apps/ | grep ldap

# 2. Check authentication.conf
cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf

# 3. View effective authentication configuration
/opt/splunk/bin/splunk btool authentication list --debug

# 4. Check if LDAP strategy is configured
/opt/splunk/bin/splunk btool authentication list authentication
/opt/splunk/bin/splunk btool authentication list corporate-ldap

# 5. Test LDAP connectivity (this will show connection errors if any)
/opt/splunk/bin/splunk test authentication
```

### Check Splunk Logs for LDAP

```bash
# Check splunkd logs for authentication attempts
tail -100 /opt/splunk/var/log/splunk/splunkd.log | grep -i ldap

# Check for errors
grep -i "ldap.*error" /opt/splunk/var/log/splunk/splunkd.log
```

## Step 8: Test LDAP Authentication

### From Web Browser

1. Get the Splunk URL:
```bash
kubectl get service -n splunk-operator | grep standalone-ldap-test
```

2. Port-forward if needed:
```bash
kubectl port-forward svc/splunk-standalone-ldap-test-service 8000:8000 -n splunk-operator
```

3. Access Splunk Web: `https://localhost:8000`

4. Try logging in with:
   - **Username**: Your LDAP username (e.g., `john.doe`)
   - **Password**: Your LDAP password

### From CLI

```bash
# Inside the Splunk pod
/opt/splunk/bin/splunk login -auth ldap-user:ldap-password
```

## Troubleshooting

### App Not Downloaded

**Check:**
```bash
# View operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager | grep -i download

# Check app framework status
kubectl get standalone standalone-ldap-test -n splunk-operator -o jsonpath='{.status.appDeploymentContext}'
```

**Common issues:**
- S3 credentials incorrect
- S3 bucket path wrong
- Network connectivity to S3

**Fix:**
```bash
# Verify S3 access
kubectl exec -it standalone-ldap-test-0 -n splunk-operator -- \
  aws s3 ls s3://your-bucket-name/apps/ldap-config/

# Force manual app update
kubectl patch cm/splunk-splunk-operator-standalone-ldap-test-configmap \
  -n splunk-operator \
  --type merge \
  -p '{"data":{"manualUpdate":"true"}}'
```

### App Downloaded But Not Installed

**Check install status:**
```bash
kubectl get standalone standalone-ldap-test -n splunk-operator -o yaml | \
  grep -A 20 "appDeploymentInfo"
```

Look for status codes:
- `201`: Pending copy
- `202`: Copy in progress
- `203`: Copy complete
- `301`: Pending install
- `302`: Install in progress
- `303`: Install complete ✅
- `399`: Install failed ❌

### LDAP Bind Fails

**Verify environment variable:**
```bash
kubectl exec -it standalone-ldap-test-0 -n splunk-operator -- \
  env | grep LDAP_BIND_PASSWORD
```

**Test LDAP connection manually:**
```bash
kubectl exec -it standalone-ldap-test-0 -n splunk-operator -- bash

# Inside pod - test LDAP bind
ldapsearch -x -H ldaps://ldap.example.com:636 \
  -D "cn=splunk-bind,ou=service-accounts,dc=example,dc=com" \
  -w "$LDAP_BIND_PASSWORD" \
  -b "dc=example,dc=com" \
  "(uid=testuser)"
```

### Configuration Not Applied

**Force Splunk restart:**
```bash
# Delete the pod to force recreation
kubectl delete pod standalone-ldap-test-0 -n splunk-operator

# Wait for pod to come back
kubectl wait --for=condition=ready pod/standalone-ldap-test-0 -n splunk-operator --timeout=300s
```

### Check App Framework Phase

```bash
# Get detailed phase information
kubectl get standalone standalone-ldap-test -n splunk-operator -o json | \
  jq '.status.appDeploymentContext.appSrcDeployStatus'
```

Phase status codes:
- **Download Phase**: 101-199
  - 101: Pending download
  - 102: Download in progress
  - 103: Download complete
  - 199: Download failed

- **Copy Phase**: 201-299
  - 201: Pending copy
  - 202: Copy in progress
  - 203: Copy complete
  - 299: Copy failed

- **Install Phase**: 301-399
  - 301: Pending install
  - 302: Install in progress
  - 303: Install complete ✅
  - 399: Install failed

## Updating the LDAP Configuration

To update the LDAP configuration:

1. **Modify the app locally:**
```bash
cd /tmp/ldap-auth-config-app/local
vi authentication.conf
# Make your changes
```

2. **Re-package with the SAME filename:**
```bash
cd /tmp
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/
```

3. **Upload to S3 (overwrite existing):**
```bash
aws s3 cp ldap-auth-config-app.tgz s3://your-bucket-name/apps/ldap-config/ --force
```

4. **Wait for automatic update** (based on `appsRepoPollIntervalSeconds`)

Or **manually trigger update:**
```bash
kubectl patch cm/splunk-splunk-operator-standalone-ldap-test-configmap \
  -n splunk-operator \
  --type merge \
  -p '{"data":{"manualUpdate":"true"}}'
```

## Monitoring App Updates

```bash
# Watch app deployment status
watch 'kubectl get standalone standalone-ldap-test -n splunk-operator -o yaml | grep -A 30 appDeploymentContext'

# Follow operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f | grep -E "(ldap|appframework)"
```

## Cleanup

To remove the test deployment:

```bash
# Delete the standalone instance
kubectl delete standalone standalone-ldap-test -n splunk-operator

# Delete secrets
kubectl delete secret ldap-credentials s3-secret -n splunk-operator

# Delete from S3
aws s3 rm s3://your-bucket-name/apps/ldap-config/ldap-auth-config-app.tgz
```

## Production Recommendations

1. **Use IAM Roles instead of static credentials** for S3/Azure/GCS access
2. **Store LDAP passwords in a proper secrets manager** (AWS Secrets Manager, Azure Key Vault, etc.)
3. **Set `appsRepoPollIntervalSeconds` appropriately** (3600 = 1 hour for production)
4. **Version your apps** - include version numbers in app.conf
5. **Test in a dev environment first** before deploying to production
6. **Monitor app deployment status** via Splunk Operator metrics
7. **Set up alerts** for app deployment failures

## Additional Resources

- [Splunk Operator App Framework Documentation](https://github.com/splunk/splunk-operator/blob/main/docs/AppFramework.md)
- [Splunk authentication.conf Reference](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Authenticationconf)
- [LDAP Configuration Best Practices](https://docs.splunk.com/Documentation/Splunk/latest/Security/SetupuserauthenticationwithLDAP)
