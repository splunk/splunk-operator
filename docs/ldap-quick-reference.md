# LDAP on Splunk Kubernetes - Quick Reference Card

## Test Users

| Username | Password | Group | Splunk Role |
|----------|----------|-------|-------------|
| john.doe | SplunkAdmin123 | Splunk Admins | admin |
| jane.smith | SplunkPower123 | Splunk Power Users | power |
| bob.user | SplunkUser123 | Splunk Users | user |

## OpenLDAP Connection Details

```
Host: openldap.splunk-operator.svc.cluster.local
Port: 389 (LDAP) or 636 (LDAPS)
Base DN: dc=splunktest,dc=local
Admin DN: cn=admin,dc=splunktest,dc=local
Admin Password: admin
User Base: ou=users,dc=splunktest,dc=local
Group Base: ou=groups,dc=splunktest,dc=local
```

## Quick Commands

### Deploy OpenLDAP
```bash
kubectl apply -f openldap-deployment.yaml
kubectl get pods -n splunk-operator -l app=openldap
```

### Test LDAP Query
```bash
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapsearch -x -H ldap://localhost \
  -b "ou=users,dc=splunktest,dc=local" \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin "(uid=*)"
```

### Test Authentication
```bash
# Via REST API
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -k -s -u john.doe:SplunkAdmin123 \
  https://localhost:8089/services/authentication/current-context

# Via Splunk Web (port-forward first)
kubectl port-forward -n splunk-operator splunk-ldap-test-standalone-0 8000:8000
# Open: http://localhost:8000
```

### Check Splunk LDAP Config
```bash
# View authentication config
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list authentication

# View role mappings
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  /opt/splunk/bin/splunk btool authentication list roleMap_corporate-ldap
```

### Check Logs
```bash
# Splunk logs
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  tail -100 /opt/splunk/var/log/splunk/splunkd.log | grep -i ldap

# OpenLDAP logs
kubectl logs -n splunk-operator deployment/openldap -c openldap --tail=50

# Operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f
```

## Troubleshooting Quick Checks

### LDAP Server Not Ready
```bash
kubectl get pods -n splunk-operator -l app=openldap
kubectl describe pod -n splunk-operator -l app=openldap
```

### Network Connectivity
```bash
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  curl -v telnet://openldap.splunk-operator.svc.cluster.local:389
```

### App Framework Not Working
```bash
# Check IAM role
aws iam get-role --role-name splunk-ldap-s3-read

# Check ServiceAccount annotation
kubectl get sa -n splunk-operator splunk-app-s3-reader -o yaml | grep role-arn

# Check operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager | grep -i "ldap-auth"
```

### Authentication Failing
```bash
# Check if app is installed
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  ls -la /opt/splunk/etc/apps/ | grep ldap

# Check authentication config
kubectl exec -n splunk-operator splunk-ldap-test-standalone-0 -- \
  cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf

# Test LDAP bind
kubectl exec -n splunk-operator deployment/openldap -c openldap -- \
  ldapwhoami -x -H ldap://localhost \
  -D "cn=admin,dc=splunktest,dc=local" \
  -w admin
```

## Architecture Components

```
┌─────────────┐
│    User     │
└──────┬──────┘
       │ 1. Login
       ▼
┌─────────────────────────────┐
│   Splunk Pod                │
│   - authentication.conf     │
│   - LDAP auth enabled       │
└──────┬──────────────────────┘
       │ 2. Authenticate
       ▼
┌─────────────────────────────┐
│   OpenLDAP Pod              │
│   - Users: john, jane, bob  │
│   - Groups: admin, power    │
└──────┬──────────────────────┘
       │ 3. Return groups
       ▼
┌─────────────────────────────┐
│   Role Mapping              │
│   - Splunk Admins → admin   │
│   - Splunk Power → power    │
│   - Splunk Users → user     │
└─────────────────────────────┘
```

## App Framework Flow

```
┌─────────────────┐
│  App in S3      │
└────────┬────────┘
         │
         ▼
┌─────────────────┐      ┌──────────────┐
│  Operator Pod   │─────→│  IAM Role    │
│  (uses IRSA)    │      │  (OIDC trust)│
└────────┬────────┘      └──────────────┘
         │
         │ Download
         ▼
┌─────────────────┐
│  Splunk Pod     │
│  Install app    │
└─────────────────┘
```

## Files Reference

| File | Purpose |
|------|---------|
| `openldap-deployment.yaml` | OpenLDAP server deployment |
| `ldap-auth-config-app/` | Splunk LDAP auth app |
| `splunk-standalone-ldap.yaml` | Splunk Standalone CR with App Framework |
| `trust-policy.json` | IAM trust policy for IRSA |
| `s3-policy.json` | S3 read permissions |

## Common Error Messages

| Error | Cause | Solution |
|-------|-------|----------|
| `Can't contact LDAP server` | Network issue or LDAP down | Check OpenLDAP pod status |
| `Invalid credentials` | Wrong bind DN or password | Verify bindDN and bindDNpassword |
| `User not found` | Wrong userBaseDN | Check userBaseDN matches LDAP structure |
| `Unable to get apps list` | IAM/IRSA issue | Check IAM role and ServiceAccount annotation |

## Production Checklist

- [ ] Replace OpenLDAP with production LDAP/AD server
- [ ] Enable SSL/TLS (SSLEnabled = 1, port = 636)
- [ ] Use service account credentials in Kubernetes Secret
- [ ] Update role mappings for your organization's groups
- [ ] Test failover with multiple LDAP servers
- [ ] Keep local admin account enabled as backup
- [ ] Set up monitoring for authentication failures
- [ ] Document rollback procedure
