# LDAP Authentication Configuration Guide

## Overview

This guide explains the recommended and supported approach for configuring LDAP authentication when using the Splunk Operator for Kubernetes.

## Understanding How Configuration Works

The Splunk Operator manages configuration files on behalf of the Splunk instances it creates. **This is by design** - the operator automatically maintains and reconciles configuration to ensure consistency across the deployment.

### Key Principles:

1. **Operator-Managed Configuration**: The operator controls `/opt/splunk/etc/system/local/` configurations
2. **Custom Docker Images Won't Persist**: If you build custom images with pre-configured files, the operator will overwrite them during reconciliation
3. **Use Operator-Provided Methods**: Configuration must be done through operator-supported mechanisms

## Recommended Approach: Using `default.yml` with `defaultsUrl`

The **correct and supported way** to configure LDAP authentication is through the `default.yml` configuration mechanism.

### Step 1: Create LDAP Configuration File

Create a `default.yml` file with your LDAP settings:

```yaml
splunk:
  conf:
    authentication:
      directory:
        LDAP-Strategy:
          SSLEnabled: 1
          authSettings: LDAP-Provider
          bindDN: cn=bind-user,ou=service-accounts,dc=example,dc=com
          # Note: Do NOT put the actual password here
          # bindDNpassword: <will be handled via secrets>
          charset: utf8
          emailAttribute: mail
          groupBaseDN: ou=groups,dc=example,dc=com
          groupBaseFilter: (objectclass=group)
          groupMappingAttribute: dn
          groupMemberAttribute: member
          groupNameAttribute: cn
          host: ldap.example.com
          port: 636
          realNameAttribute: displayName
          timelimit: 15
          userBaseDN: ou=users,dc=example,dc=com
          userBaseFilter: (objectclass=user)
          userNameAttribute: sAMAccountName
      authentication:
        authSettings: LDAP-Strategy
        authType: LDAP
```

### Step 2: Store LDAP Bind Password in Kubernetes Secret

The LDAP bind password must be stored securely in the global Kubernetes secret object managed by the operator.

**Important**: As of recent operator versions, password management via `defaults`/`defaultsUrl` has been disabled for security reasons. You must use the global secret object.

#### Option A: Update Global Secret Object

1. Get the current secret:
```bash
kubectl get secret splunk-<namespace>-secret -n <namespace> -o yaml > splunk-secret.yaml
```

2. Edit the secret and add your LDAP bind password:
```bash
# First, base64 encode your password
echo -n 'your-ldap-bind-password' | base64

# Edit the secret YAML
kubectl edit secret splunk-<namespace>-secret -n <namespace>
```

3. Add this field to the `data` section:
```yaml
data:
  ldap_bind_password: <base64-encoded-password>
```

#### Option B: Reference in `default.yml` (If Supported)

Some versions allow referencing Kubernetes secrets in `default.yml`:

```yaml
splunk:
  conf:
    authentication:
      directory:
        LDAP-Strategy:
          bindDNpassword: $SPLUNK_LDAP_BIND_PASSWORD
```

Then set the environment variable via the CR spec:

```yaml
spec:
  extraEnv:
    - name: SPLUNK_LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-credentials
          key: bind-password
```

### Step 3: Create Kubernetes Secret from `default.yml`

```bash
kubectl create secret generic splunk-ldap-config \
  --from-file=default.yml \
  -n <namespace>
```

### Step 4: Configure Your Custom Resource

Reference the configuration in your Splunk CR (Standalone, SearchHeadCluster, etc.):

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  # Mount the secret containing default.yml
  volumes:
    - name: ldap-config
      secret:
        secretName: splunk-ldap-config

  # Point to the mounted default.yml
  defaultsUrl: /mnt/ldap-config/default.yml

  # Optional: Add LDAP password as environment variable
  extraEnv:
    - name: SPLUNK_LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-credentials
          key: bind-password
```

## Alternative Approach: Using Inline `defaults`

For simpler configurations, you can use inline defaults:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  namespace: splunk-operator
spec:
  defaults: |-
    splunk:
      conf:
        authentication:
          directory:
            LDAP-Strategy:
              SSLEnabled: 1
              authSettings: LDAP-Provider
              host: ldap.example.com
              port: 636
              # ... other LDAP settings
```

**Note**: Inline defaults are processed **last**, after any `defaultsUrl` files.

## Why Custom Docker Images Don't Work

The customer attempted to build a custom Docker image with pre-configured LDAP settings. This approach fails because:

1. **Operator Reconciliation Loop**: The operator continuously reconciles the desired state
2. **Configuration Overwrite**: During reconciliation, the operator re-applies its managed configuration
3. **StatefulSet Updates**: When pods are recreated, the operator's configuration takes precedence

**The operator's behavior is intentional** - it ensures configuration consistency and prevents configuration drift.

## Verification Steps

After applying your configuration:

### 1. Check Pod Configuration

```bash
# Exec into a pod
kubectl exec -it <pod-name> -n <namespace> -- bash

# Check authentication.conf
cat /opt/splunk/etc/system/local/authentication.conf

# Verify LDAP settings are applied
/opt/splunk/bin/splunk list auth-config
```

### 2. Test LDAP Connectivity

```bash
# From within the pod
/opt/splunk/bin/splunk test authentication
```

### 3. Check Operator Logs

```bash
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager
```

Look for any errors related to configuration application.

## Troubleshooting

### Issue: Configuration Not Applying

**Symptoms**: LDAP settings don't appear in `authentication.conf`

**Solutions**:
1. Verify the `defaultsUrl` path is correct (matches volume mount)
2. Check that the secret exists and contains valid YAML
3. Ensure the CR has been reconciled (check operator logs)
4. Delete and recreate the pod to force reconciliation

### Issue: LDAP Bind Fails

**Symptoms**: "LDAP bind failed" errors in splunkd.log

**Solutions**:
1. Verify bind password is correctly set in the global secret
2. Check that the bind DN has proper permissions
3. Test LDAP connectivity from the pod:
   ```bash
   ldapsearch -x -H ldaps://ldap.example.com:636 \
     -D "cn=bind-user,ou=service-accounts,dc=example,dc=com" \
     -w "password" \
     -b "dc=example,dc=com"
   ```

### Issue: Configuration Keeps Getting Overwritten

**Symptoms**: Manual changes to `/opt/splunk/etc/system/local/` are lost

**Solutions**:
- **Don't make manual changes** - use the operator's mechanisms
- All configuration must go through `defaults`, `defaultsUrl`, or inline specs
- This is the operator's intended behavior

## Complete Working Example

```yaml
---
# Create secret with LDAP bind password
apiVersion: v1
kind: Secret
metadata:
  name: ldap-credentials
  namespace: splunk-operator
type: Opaque
stringData:
  bind-password: "YourSecurePasswordHere"

---
# Create secret with default.yml
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
          directory:
            LDAP-Strategy:
              SSLEnabled: 1
              authSettings: LDAP-Provider
              bindDN: cn=splunk-bind,ou=service-accounts,dc=example,dc=com
              charset: utf8
              emailAttribute: mail
              groupBaseDN: ou=groups,dc=example,dc=com
              groupBaseFilter: (objectclass=group)
              groupMappingAttribute: dn
              groupMemberAttribute: member
              groupNameAttribute: cn
              host: ldap.example.com
              port: 636
              realNameAttribute: displayName
              timelimit: 15
              userBaseDN: ou=users,dc=example,dc=com
              userBaseFilter: (objectclass=user)
              userNameAttribute: sAMAccountName
          authentication:
            authSettings: LDAP-Strategy
            authType: LDAP

---
# Standalone Splunk instance with LDAP
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: standalone-with-ldap
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

  # Point to default.yml
  defaultsUrl: /mnt/ldap-config/default.yml

  # Add LDAP bind password as environment variable
  extraEnv:
    - name: SPLUNK_LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-credentials
          key: bind-password
```

## Additional Resources

- [Splunk Operator Examples](Examples.md)
- [Password Management Guide](PasswordManagement.md)
- [Custom Resources Documentation](CustomResources.md)
- [Default Configuration Spec](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md)
- [Splunk LDAP Authentication Documentation](https://docs.splunk.com/Documentation/Splunk/latest/Security/SetupuserauthenticationwithLDAP)

## Support

If you continue to experience issues:

1. Collect operator logs: `kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager`
2. Collect pod logs: `kubectl logs <pod-name> -n <namespace>`
3. Check splunkd logs: `kubectl exec <pod-name> -- tail -100 /opt/splunk/var/log/splunk/splunkd.log`
4. Open a support case with the collected logs

## Important Notes

- ✅ **DO**: Use `defaultsUrl` or inline `defaults` for LDAP configuration
- ✅ **DO**: Store passwords in Kubernetes secrets
- ✅ **DO**: Use environment variables for sensitive data
- ❌ **DON'T**: Build custom Docker images with pre-configured files
- ❌ **DON'T**: Manually edit files in `/opt/splunk/etc/system/local/`
- ❌ **DON'T**: Expect manual changes to persist across pod restarts
