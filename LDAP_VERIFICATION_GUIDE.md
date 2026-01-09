# LDAP Configuration Verification Guide

## ✅ Current Status

The LDAP authentication app has been **successfully deployed** via App Framework with IRSA!

### What's Confirmed Working

1. ✅ App installed in Splunk: `/opt/splunk/etc/apps/ldap-auth-config-app`
2. ✅ Configuration file deployed: `authentication.conf`
3. ✅ Splunk recognizes LDAP configuration (verified via btool)
4. ✅ Password encrypted by Splunk
5. ✅ LDAP set as authentication method
6. ✅ Role mappings configured

## How to Verify LDAP is Working

### Method 1: Check Splunk Configuration (Already Done ✓)

```bash
# Verify LDAP app exists
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  ls -la /opt/splunk/etc/apps/ldap-auth-config-app

# Check authentication configuration
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list --debug | grep -A 10 corporate-ldap
```

**Result**: ✅ Configuration is present and recognized by Splunk

### Method 2: Test LDAP Connection

Since `ldap.example.com` is a placeholder, you need to update it with your actual LDAP server.

#### Option A: Update via App Framework (Recommended)

1. **Edit the app locally**:
```bash
cd /tmp/ldap-auth-config-app/local
nano authentication.conf
```

2. **Update these values**:
```ini
host = your-actual-ldap-server.company.com
port = 636  # or 389 for non-SSL
bindDN = cn=your-bind-account,ou=service-accounts,dc=company,dc=com
userBaseDN = ou=users,dc=company,dc=com
groupBaseDN = ou=groups,dc=company,dc=com
```

3. **Repackage and upload**:
```bash
cd /tmp
tar czf ldap-auth-config-app.tgz ldap-auth-config-app/
aws s3 cp ldap-auth-config-app.tgz s3://ai-platform-dev-vivekr/splunk-apps/authAppsLoc/ --region us-west-2
```

4. **Delete the app from Splunk** (operator will reinstall automatically):
```bash
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  /opt/splunk/bin/splunk remove app ldap-auth-config-app -auth admin:changeme

# Wait 60 seconds for operator to detect and reinstall
sleep 60
```

5. **Restart Splunk** to apply changes:
```bash
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  /opt/splunk/bin/splunk restart
```

#### Option B: Test Connection via Splunk Web UI

This is the **easiest way** to verify LDAP is working:

1. **Port-forward to Splunk Web**:
```bash
kubectl port-forward -n splunk-operator splunk-ldap-test-irsa-standalone-0 8000:8000
```

2. **Open browser**: `http://localhost:8000`

3. **Login with admin credentials**:
   - Username: `admin`
   - Password: `changeme` (or check the admin password secret)

4. **Navigate to**: Settings → Access controls → Authentication method

   You should see:
   - Authentication method: **LDAP**
   - LDAP strategy name: **corporate-ldap**

5. **Check LDAP settings**: Settings → Access controls → Authentication method → LDAP Settings

   Click on **corporate-ldap** to see all configured settings.

6. **Test LDAP login**:
   - Logout from admin
   - Try logging in with an actual LDAP username/password
   - If connection works, you'll authenticate successfully
   - If connection fails, check logs (see Method 3)

### Method 3: Check Splunk Logs

```bash
# Check for LDAP-related logs
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  tail -100 /opt/splunk/var/log/splunk/splunkd.log | grep -i ldap

# Check for authentication attempts
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  grep "LDAPAuth" /opt/splunk/var/log/splunk/splunkd.log | tail -20

# Check for authentication failures
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  grep "authentication failed" /opt/splunk/var/log/splunk/splunkd.log | tail -20

# Check for LDAP connection errors
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  grep -i "ldap.*error\|ldap.*fail" /opt/splunk/var/log/splunk/splunkd.log | tail -20
```

### Method 4: REST API Test

```bash
# Get admin password
ADMIN_PWD=$(kubectl get secret splunk-ldap-test-irsa-standalone-secrets -n splunk-operator -o jsonpath='{.data.password}' | base64 -d)

# Check LDAP configuration via REST API
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  curl -k -u admin:${ADMIN_PWD} \
  https://localhost:8089/services/authentication/httpauth-tokens
```

## Expected Behaviors

### ✅ When LDAP is Working Correctly

1. **Authentication method**: Shows LDAP in Splunk Web UI
2. **LDAP users can login**: Users in configured LDAP groups can authenticate
3. **Role mapping works**: LDAP group members get correct Splunk roles
4. **Logs show successful bind**: `splunkd.log` shows successful LDAP bind operations
5. **Admin still works**: Local admin account still functions (recommended to keep)

### ❌ Common Issues and Solutions

#### Issue 1: "Cannot connect to LDAP server"

**Cause**: Network connectivity or wrong hostname

**Check**:
```bash
# Test connectivity from pod
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  nc -zv ldap.example.com 636
```

**Solution**: Update `host` in authentication.conf with correct LDAP server

#### Issue 2: "Bind failed"

**Cause**: Wrong bind DN or password

**Check logs**:
```bash
kubectl exec -n splunk-operator splunk-ldap-test-irsa-standalone-0 -- \
  grep "bind.*failed" /opt/splunk/var/log/splunk/splunkd.log | tail -10
```

**Solution**:
- Verify bind DN format: `cn=user,ou=service-accounts,dc=company,dc=com`
- Verify LDAP_BIND_PASSWORD environment variable is set correctly
- Update secret if needed:
```bash
kubectl create secret generic ldap-credentials \
  --from-literal=bind-password='your-actual-password' \
  -n splunk-operator \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart pod to pick up new secret
kubectl delete pod -n splunk-operator splunk-ldap-test-irsa-standalone-0
```

#### Issue 3: "User not found"

**Cause**: Wrong userBaseDN or search filter

**Solution**: Verify LDAP structure matches configuration:
- `userBaseDN`: Where users are located in LDAP tree
- `userNameAttribute`: How usernames are stored (usually `sAMAccountName` for AD, `uid` for OpenLDAP)

#### Issue 4: "Authentication succeeds but no roles"

**Cause**: Role mapping configuration issue

**Solution**: Check group mappings:
- Verify LDAP group names in `[roleMap_corporate-ldap]` section
- Check user is actually member of the LDAP groups
- Verify `groupMappingAttribute` is correct

## Real LDAP Server Configuration

To use with your actual LDAP server, update these values in `/tmp/ldap-auth-config-app/local/authentication.conf`:

```ini
[corporate-ldap]
# === REPLACE THESE WITH YOUR ACTUAL VALUES ===

# LDAP Server (Active Directory or OpenLDAP)
host = ldap.yourcompany.com
port = 636  # 636 for LDAPS, 389 for LDAP

# Service Account for Bind
bindDN = cn=splunk-service,ou=service-accounts,dc=yourcompany,dc=com
bindDNpassword = $LDAP_BIND_PASSWORD$  # Will be replaced by environment variable

# User Search Configuration
userBaseDN = ou=employees,dc=yourcompany,dc=com
userNameAttribute = sAMAccountName  # For Active Directory
# userNameAttribute = uid           # For OpenLDAP

# Group Search Configuration
groupBaseDN = ou=groups,dc=yourcompany,dc=com

# Role Mappings (update with your actual LDAP group names)
[roleMap_corporate-ldap]
admin = CN=Splunk-Admins,OU=Security Groups,DC=yourcompany,DC=com
power = CN=Splunk-PowerUsers,OU=Security Groups,DC=yourcompany,DC=com
user = CN=Splunk-Users,OU=Security Groups,DC=yourcompany,DC=com
```

## Test Plan for Customer

### Phase 1: Configuration Update
1. Update authentication.conf with actual LDAP server details
2. Repackage and upload to S3
3. Wait for operator to reinstall (60 seconds)
4. Restart Splunk pod

### Phase 2: Connectivity Test
1. Port-forward to Splunk Web
2. Login as admin
3. Check Settings → Authentication method
4. Verify LDAP settings are correct

### Phase 3: LDAP Login Test
1. Logout from admin account
2. Login with LDAP user credentials
3. Verify successful authentication
4. Check user gets correct role

### Phase 4: Monitor Logs
1. Watch splunkd.log for LDAP activities
2. Verify no errors or warnings
3. Confirm bind operations succeed
4. Verify user searches work

## Success Criteria

- [ ] Splunk Web shows LDAP as authentication method
- [ ] LDAP users can successfully login
- [ ] Users get correct roles based on LDAP groups
- [ ] No LDAP errors in splunkd.log
- [ ] Admin account still works (fallback)

## App Framework Benefits Demonstrated

✅ **No Custom Docker Images**: Configuration deployed via App Framework

✅ **Persistent Configuration**: Survives pod restarts and operator upgrades

✅ **Version Control**: LDAP config stored in S3, versioned

✅ **Secure Credentials**: Uses IRSA (no static AWS credentials)

✅ **Easy Updates**: Change S3 file, operator auto-deploys

✅ **Scalable**: Same app deploys to all replicas automatically

## Next Steps

1. **For Testing**: Update authentication.conf with real LDAP server and test
2. **For Production**:
   - Document your LDAP structure
   - Create service account for Splunk bind
   - Test in dev environment first
   - Roll out to production
3. **For Other Apps**: Use same App Framework pattern for other Splunk apps

## Files Reference

- LDAP App: `/opt/splunk/etc/apps/ldap-auth-config-app/`
- Config File: `/opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf`
- Logs: `/opt/splunk/var/log/splunk/splunkd.log`
- S3 Location: `s3://ai-platform-dev-vivekr/splunk-apps/authAppsLoc/ldap-auth-config-app.tgz`

## Verification Script

Run the verification script:
```bash
/tmp/verify-ldap-splunk.sh
```

This will check:
- Authentication type is LDAP
- LDAP strategy is configured
- Server configuration is present
- Provide manual verification steps
