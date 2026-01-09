# Solution: LDAP Authentication Configuration for Splunk Operator

## Customer Issue Summary

Customer is trying to configure LDAP authentication for Splunk instances managed by Splunk Operator for Kubernetes. They've tried:
- ❌ Environment variables
- ❌ Custom Docker images with pre-configured authentication.conf
- ❌ Direct file modifications

**All attempts failed** because the Operator overwrites configuration during reconciliation.

## Root Cause

The Splunk Operator manages Splunk configuration and **intentionally** overwrites manual changes to maintain desired state. This is expected Kubernetes operator behavior.

## Correct Solution

### Understanding Splunk LDAP Configuration

In standard Splunk, LDAP is configured via `/opt/splunk/etc/system/local/authentication.conf`:

```ini
[authentication]
authType = LDAP
authSettings = ldapstrategy

[ldapstrategy]
SSLEnabled = 1
bindDN = cn=splunk-bind,ou=service-accounts,dc=company,dc=com
bindDNpassword = <password>
charset = utf8
emailAttribute = mail
groupBaseDN = ou=groups,dc=company,dc=com
groupBaseFilter = (objectclass=group)
groupMemberAttribute = member
groupNameAttribute = cn
host = ldap.company.com
port = 636
realNameAttribute = displayName
timelimit = 15
userBaseDN = ou=users,dc=company,dc=com
userBaseFilter = (objectclass=user)
userNameAttribute = sAMAccountName
```

### Solution 1: Using `defaults` or `defaultsUrl` (Recommended)

The Splunk Operator uses [splunk-ansible](https://github.com/splunk/splunk-ansible) to configure Splunk instances. Configuration should be provided via `default.yml` format.

#### Step 1: Create authentication configuration file

Create `ldap-config.yml`:

```yaml
splunk:
  conf:
    authentication:
      directory: /opt/splunk/etc/system/local
      content:
        authentication:
          authType: LDAP
          authSettings: ldapstrategy
        ldapstrategy:
          SSLEnabled: 1
          bindDN: cn=splunk-bind,ou=service-accounts,dc=company,dc=com
          bindDNpassword: $LDAP_BIND_PASSWORD
          charset: utf8
          emailAttribute: mail
          groupBaseDN: ou=groups,dc=company,dc=com
          groupBaseFilter: (objectclass=group)
          groupMemberAttribute: member
          groupNameAttribute: cn
          host: ldap.company.com
          port: 636
          realNameAttribute: displayName
          timelimit: 15
          userBaseDN: ou=users,dc=company,dc=com
          userBaseFilter: (objectclass=user)
          userNameAttribute: sAMAccountName
```

#### Step 2: Create Kubernetes Secrets

```bash
# Create secret for LDAP bind password
kubectl create secret generic ldap-bind-password \
  --from-literal=password='YourSecurePassword' \
  -n splunk-operator

# Create secret for LDAP configuration
kubectl create secret generic splunk-ldap-config \
  --from-file=default.yml=ldap-config.yml \
  -n splunk-operator
```

#### Step 3: Apply Configuration to Splunk CR

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: standalone-ldap
  namespace: splunk-operator
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1

  # Mount the LDAP configuration
  volumes:
    - name: ldap-config
      secret:
        secretName: splunk-ldap-config

  # Point to the configuration file
  defaultsUrl: /mnt/ldap-config/default.yml

  # Pass LDAP bind password as environment variable
  extraEnv:
    - name: LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-bind-password
          key: password
```

### Solution 2: Using Inline `defaults`

For simpler setups, use inline configuration:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: standalone-ldap
  namespace: splunk-operator
spec:
  # Inline LDAP configuration
  defaults: |-
    splunk:
      conf:
        authentication:
          directory: /opt/splunk/etc/system/local
          content:
            authentication:
              authType: LDAP
              authSettings: ldapstrategy
            ldapstrategy:
              SSLEnabled: 1
              bindDN: cn=splunk-bind,ou=service-accounts,dc=company,dc=com
              bindDNpassword: $LDAP_BIND_PASSWORD
              host: ldap.company.com
              port: 636
              userBaseDN: ou=users,dc=company,dc=com
              groupBaseDN: ou=groups,dc=company,dc=com
              userNameAttribute: sAMAccountName

  # Pass LDAP bind password as environment variable
  extraEnv:
    - name: LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-bind-password
          key: password
```

### Solution 3: Using Splunk App Framework (For Complex Deployments)

If you need more control or have multiple authentication configurations, create a Splunk app:

#### Step 1: Create LDAP Configuration App

Create directory structure:
```
ldap-auth-app/
├── default/
│   └── app.conf
└── local/
    └── authentication.conf
```

`default/app.conf`:
```ini
[install]
is_configured = true

[ui]
is_visible = false
is_manageable = false

[launcher]
author = Your Company
description = LDAP Authentication Configuration
version = 1.0.0
```

`local/authentication.conf`:
```ini
[authentication]
authType = LDAP
authSettings = ldapstrategy

[ldapstrategy]
SSLEnabled = 1
bindDN = cn=splunk-bind,ou=service-accounts,dc=company,dc=com
bindDNpassword = <use-environment-variable-or-secret>
host = ldap.company.com
port = 636
# ... rest of LDAP settings
```

#### Step 2: Package the App

```bash
tar -czf ldap-auth-app.tgz ldap-auth-app/
```

#### Step 3: Upload to Remote Storage

Upload `ldap-auth-app.tgz` to your S3/Azure/GCS bucket configured in App Framework.

#### Step 4: Configure App Framework

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: standalone-ldap
  namespace: splunk-operator
spec:
  appRepo:
    appsRepoPollIntervalSeconds: 900
    defaults:
      volumeName: appframework-volume
      scope: local
    appSources:
      - name: ldap-apps
        location: ldap-auth-config/
        volumeName: appframework-volume
        scope: local

  volumes:
    - name: appframework-volume
      storageType: s3
      provider: aws
      path: your-bucket-name/
      endpoint: https://s3.region.amazonaws.com
      secretRef: s3-secret

  extraEnv:
    - name: LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-bind-password
          key: password
```

## Important Notes

### Why Custom Docker Images Don't Work

1. **Operator Reconciliation**: The operator continuously reconciles actual state with desired state
2. **Configuration Management**: The operator manages `/opt/splunk/etc/system/local/` configurations
3. **Pod Recreation**: When pods restart, operator-managed configuration is reapplied

**This is intentional design** - operators maintain control over configuration to prevent drift.

### Password Security

❌ **Don't** put passwords directly in YAML files
✅ **Do** use Kubernetes Secrets
✅ **Do** reference secrets via environment variables
✅ **Do** use `extraEnv` with `secretKeyRef`

### Configuration Precedence

When using multiple methods:
1. `defaultsUrl` files (processed first)
2. Inline `defaults` (processed last, overrides URL files)
3. Environment variables (available to both)

## Verification Steps

### 1. Check Pod Logs

```bash
kubectl logs standalone-ldap-0 -n splunk-operator | grep -i ldap
```

### 2. Exec into Pod and Verify

```bash
# Get shell access
kubectl exec -it standalone-ldap-0 -n splunk-operator -- /bin/bash

# Check authentication.conf
cat /opt/splunk/etc/system/local/authentication.conf

# Test LDAP connectivity
/opt/splunk/bin/splunk btool authentication list

# Check Splunk logs
tail -f /opt/splunk/var/log/splunk/splunkd.log | grep -i ldap
```

### 3. Test Authentication

1. Access Splunk Web UI
2. Try logging in with LDAP credentials
3. Check `/opt/splunk/var/log/splunk/splunkd.log` for authentication attempts

## Troubleshooting

### Configuration Not Applied

**Symptom**: `authentication.conf` doesn't contain LDAP settings

**Solutions**:
1. Check operator logs:
   ```bash
   kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager
   ```
2. Verify `defaultsUrl` path matches volume mount
3. Check secret exists and is properly mounted
4. Force pod restart:
   ```bash
   kubectl delete pod standalone-ldap-0 -n splunk-operator
   ```

### LDAP Bind Fails

**Symptom**: "LDAP bind failed" in splunkd.log

**Solutions**:
1. Verify bind DN and password:
   ```bash
   # From within pod
   ldapsearch -x -H ldaps://ldap.company.com:636 \
     -D "cn=splunk-bind,ou=service-accounts,dc=company,dc=com" \
     -w "$LDAP_BIND_PASSWORD" \
     -b "dc=company,dc=com" "(uid=testuser)"
   ```
2. Check network connectivity to LDAP server
3. Verify SSL certificates if using LDAPS
4. Ensure bind user has proper permissions

### Environment Variable Not Set

**Symptom**: `$LDAP_BIND_PASSWORD` appears literally in config

**Solutions**:
1. Verify secret exists:
   ```bash
   kubectl get secret ldap-bind-password -n splunk-operator
   ```
2. Check `extraEnv` is properly configured in CR
3. Verify environment variable name matches in both places

## Complete Working Example

```yaml
---
# LDAP bind password secret
apiVersion: v1
kind: Secret
metadata:
  name: ldap-bind-password
  namespace: splunk-operator
type: Opaque
stringData:
  password: "YourSecureBindPassword"

---
# LDAP configuration
apiVersion: v1
kind: Secret
metadata:
  name: splunk-ldap-config
  namespace: splunk-operator
type: Opaque
stringData:
  default.yml: |
    splunk:
      conf:
        authentication:
          directory: /opt/splunk/etc/system/local
          content:
            authentication:
              authType: LDAP
              authSettings: corporateldap
            corporateldap:
              SSLEnabled: 1
              bindDN: cn=splunk-svc,ou=service-accounts,dc=example,dc=com
              bindDNpassword: $LDAP_BIND_PASSWORD
              charset: utf8
              emailAttribute: mail
              groupBaseDN: ou=groups,dc=example,dc=com
              groupBaseFilter: (objectclass=group)
              groupMemberAttribute: member
              groupNameAttribute: cn
              host: ldap.example.com
              port: 636
              realNameAttribute: displayName
              timelimit: 15
              userBaseDN: ou=users,dc=example,dc=com
              userBaseFilter: (objectclass=user)
              userNameAttribute: sAMAccountName

---
# Standalone Splunk with LDAP
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: standalone-ldap
  namespace: splunk-operator
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1

  # Mount LDAP configuration
  volumes:
    - name: ldap-config
      secret:
        secretName: splunk-ldap-config

  # Point to configuration file
  defaultsUrl: /mnt/ldap-config/default.yml

  # Pass LDAP password as environment variable
  extraEnv:
    - name: LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-bind-password
          key: password
```

## Apply the Configuration

```bash
# Apply all resources
kubectl apply -f ldap-standalone.yaml

# Watch pod startup
kubectl get pods -n splunk-operator -w

# Check logs
kubectl logs standalone-ldap-0 -n splunk-operator -f
```

## Additional Resources

- [Splunk Operator Examples](https://github.com/splunk/splunk-operator/blob/main/docs/Examples.md)
- [Custom Resources Guide](https://github.com/splunk/splunk-operator/blob/main/docs/CustomResources.md)
- [splunk-ansible default.yml Spec](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md)
- [Splunk authentication.conf Spec](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Authenticationconf)

## Summary for Customer

✅ **Use `defaultsUrl` or inline `defaults`** to provide authentication.conf settings
✅ **Store bind password in Kubernetes Secret** and reference via `extraEnv`
✅ **Let the operator manage configuration** - don't use custom images
❌ **Don't manually edit files** in `/opt/splunk/etc/system/local/`
❌ **Don't build custom Docker images** with pre-configured files
