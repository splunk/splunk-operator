# Using IRSA for App Framework S3 Access

## Overview

This guide shows how to use **IAM Roles for Service Accounts (IRSA)** to provide S3 access to the Splunk Operator for App Framework, instead of using static AWS credentials.

### Benefits of IRSA

✅ **No static credentials** - No need to manage access keys
✅ **Automatic credential rotation** - AWS handles token refresh
✅ **Fine-grained permissions** - IAM policies control access
✅ **Audit trail** - CloudTrail logs show which pod accessed S3
✅ **Security best practice** - Follows AWS recommendations

## Prerequisites

- EKS cluster with OIDC provider enabled
- AWS CLI configured
- kubectl access to your cluster
- Permissions to create IAM roles and policies

## Step 1: Enable OIDC Provider for EKS (if not already enabled)

```bash
# Get your cluster name
CLUSTER_NAME=your-eks-cluster

# Check if OIDC provider exists
eksctl utils associate-iam-oidc-provider \
  --region=us-west-2 \
  --cluster=$CLUSTER_NAME \
  --approve
```

Or manually:

```bash
# Get OIDC issuer URL
aws eks describe-cluster \
  --name $CLUSTER_NAME \
  --query "cluster.identity.oidc.issuer" \
  --output text

# Create OIDC provider if it doesn't exist
eksctl utils associate-iam-oidc-provider \
  --cluster $CLUSTER_NAME \
  --approve
```

## Step 2: Create IAM Policy for S3 Read Access

Create a policy that grants read-only access to your S3 bucket:

```bash
# Create policy file
cat > splunk-app-s3-read-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::YOUR_BUCKET_NAME",
        "arn:aws:s3:::YOUR_BUCKET_NAME/*"
      ]
    }
  ]
}
EOF

# Create the IAM policy
aws iam create-policy \
  --policy-name SplunkAppFrameworkS3ReadPolicy \
  --policy-document file://splunk-app-s3-read-policy.json

# Note the Policy ARN from the output
```

## Step 3: Create IAM Role for IRSA

```bash
# Set variables
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
OIDC_PROVIDER=$(aws eks describe-cluster \
  --name $CLUSTER_NAME \
  --query "cluster.identity.oidc.issuer" \
  --output text | sed -e "s/^https:\/\///")

NAMESPACE=splunk-operator
SERVICE_ACCOUNT=splunk-app-s3-reader

# Create trust policy
cat > trust-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::${ACCOUNT_ID}:oidc-provider/${OIDC_PROVIDER}"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "${OIDC_PROVIDER}:sub": "system:serviceaccount:${NAMESPACE}:${SERVICE_ACCOUNT}",
          "${OIDC_PROVIDER}:aud": "sts.amazonaws.com"
        }
      }
    }
  ]
}
EOF

# Create the IAM role
aws iam create-role \
  --role-name splunk-app-framework-s3-read \
  --assume-role-policy-document file://trust-policy.json \
  --description "IAM role for Splunk Operator App Framework S3 access"

# Attach the policy to the role
aws iam attach-role-policy \
  --role-name splunk-app-framework-s3-read \
  --policy-arn arn:aws:iam::${ACCOUNT_ID}:policy/SplunkAppFrameworkS3ReadPolicy

# Get the role ARN (you'll need this)
aws iam get-role \
  --role-name splunk-app-framework-s3-read \
  --query Role.Arn \
  --output text
```

## Step 4: Create Kubernetes Service Account

```bash
# Create service account with IRSA annotation
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: splunk-app-s3-reader
  namespace: splunk-operator
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::${ACCOUNT_ID}:role/splunk-app-framework-s3-read
EOF
```

## Step 5: Upload LDAP App to S3

```bash
# Upload the LDAP configuration app
aws s3 cp /tmp/ldap-auth-config-app.tgz \
  s3://YOUR_BUCKET_NAME/apps/authAppsLoc/

# Verify upload
aws s3 ls s3://YOUR_BUCKET_NAME/apps/authAppsLoc/
```

## Step 6: Create LDAP Credentials Secret

```bash
kubectl create secret generic ldap-credentials \
  --from-literal=bind-password='YourActualLDAPBindPassword' \
  -n splunk-operator
```

## Step 7: Update and Deploy Standalone

Edit `examples/ldap-standalone-irsa.yaml`:

1. Replace `YOUR_ACCOUNT_ID` with your AWS account ID
2. Replace `YOUR_BUCKET_NAME` with your S3 bucket name
3. Update `region` and `endpoint` if needed
4. Customize LDAP settings in the uploaded app

Apply the manifest:

```bash
kubectl apply -f examples/ldap-standalone-irsa.yaml
```

## Step 8: Verify IRSA is Working

### Check Service Account

```bash
# Verify SA has IRSA annotation
kubectl get sa splunk-app-s3-reader -n splunk-operator -o yaml | grep eks.amazonaws.com

# Expected output:
#   eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/splunk-app-framework-s3-read
```

### Check Pod Environment

```bash
# Get pod name
POD_NAME=$(kubectl get pods -n splunk-operator -l app.kubernetes.io/instance=ldap-test-irsa -o jsonpath='{.items[0].metadata.name}')

# Check AWS environment variables (injected by IRSA)
kubectl exec -it $POD_NAME -n splunk-operator -- env | grep AWS

# Expected output includes:
#   AWS_ROLE_ARN=arn:aws:iam::123456789012:role/splunk-app-framework-s3-read
#   AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token
```

### Test S3 Access

```bash
# Exec into pod
kubectl exec -it $POD_NAME -n splunk-operator -- bash

# Inside pod - test S3 access
aws s3 ls s3://YOUR_BUCKET_NAME/apps/authAppsLoc/

# Should list your ldap-auth-config-app.tgz file
```

### Check App Framework Status

```bash
# Check app deployment status
kubectl get standalone ldap-test-irsa -n splunk-operator -o yaml | \
  grep -A 50 appDeploymentContext

# Look for status codes:
# Download: 103 (complete)
# Copy: 203 (complete)
# Install: 303 (complete)
```

### Check Operator Logs

```bash
kubectl logs -n splunk-operator \
  deployment/splunk-operator-controller-manager -f | \
  grep -E "(ldap-auth|S3|download)"
```

## Step 9: Verify LDAP Configuration

```bash
# Exec into Splunk pod
kubectl exec -it $POD_NAME -n splunk-operator -- bash

# Check app is installed
ls -la /opt/splunk/etc/apps/ldap-auth-config-app/

# View authentication.conf
cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf

# Test LDAP settings
/opt/splunk/bin/splunk btool authentication list --debug
```

## Troubleshooting

### IRSA Not Working

**Symptom**: Pod can't access S3

**Check**:
```bash
# 1. Verify OIDC provider exists
aws eks describe-cluster --name $CLUSTER_NAME | grep oidc

# 2. Verify IAM role trust policy
aws iam get-role --role-name splunk-app-framework-s3-read | jq .Role.AssumeRolePolicyDocument

# 3. Check pod's service account
kubectl get pod $POD_NAME -n splunk-operator -o yaml | grep serviceAccountName

# 4. Verify AWS env vars in pod
kubectl exec $POD_NAME -n splunk-operator -- env | grep AWS
```

### S3 Access Denied

**Check IAM policy**:
```bash
# Verify policy is attached to role
aws iam list-attached-role-policies --role-name splunk-app-framework-s3-read

# View policy permissions
aws iam get-policy-version \
  --policy-arn arn:aws:iam::${ACCOUNT_ID}:policy/SplunkAppFrameworkS3ReadPolicy \
  --version-id v1
```

### App Not Downloading

**Check operator logs**:
```bash
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager | \
  grep -i "error\|failed\|s3"
```

**Check S3 path**:
```bash
# Verify app exists in S3
aws s3 ls s3://YOUR_BUCKET_NAME/apps/authAppsLoc/

# Should show: ldap-auth-config-app.tgz
```

## CloudTrail Verification

To verify IRSA is working correctly, check CloudTrail logs:

```bash
# Search for S3 access from the pod
aws cloudtrail lookup-events \
  --lookup-attributes AttributeKey=Username,AttributeValue=splunk-app-framework-s3-read \
  --region us-west-2
```

You should see S3 GetObject and ListBucket events.

## Updating the LDAP App

When using IRSA, updates are simple:

1. **Modify the app locally**
2. **Re-package**: `tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/`
3. **Upload to S3** (same location, overwrites):
   ```bash
   aws s3 cp ldap-auth-config-app.tgz s3://YOUR_BUCKET_NAME/apps/authAppsLoc/
   ```
4. **Wait for auto-update** or trigger manually:
   ```bash
   kubectl patch cm/splunk-splunk-operator-ldap-test-irsa-configmap \
     -n splunk-operator \
     --type merge \
     -p '{"data":{"manualUpdate":"true"}}'
   ```

## Security Best Practices

1. **Least Privilege**: Only grant S3 read access to the specific bucket/path
2. **No Write Access**: App Framework only needs read access
3. **Condition Keys**: Use IAM condition keys to restrict access further
4. **Audit**: Enable CloudTrail to monitor S3 access
5. **Rotate**: No need! IRSA tokens are automatically rotated

## Example IAM Policy with Stricter Conditions

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::YOUR_BUCKET_NAME",
        "arn:aws:s3:::YOUR_BUCKET_NAME/apps/*"
      ],
      "Condition": {
        "StringEquals": {
          "s3:ExistingObjectTag/Environment": "production"
        }
      }
    }
  ]
}
```

## Cleanup

To remove the test deployment:

```bash
# Delete standalone instance
kubectl delete standalone ldap-test-irsa -n splunk-operator

# Delete service account
kubectl delete sa splunk-app-s3-reader -n splunk-operator

# Delete secret
kubectl delete secret ldap-credentials -n splunk-operator

# (Optional) Delete IAM role and policy
aws iam detach-role-policy \
  --role-name splunk-app-framework-s3-read \
  --policy-arn arn:aws:iam::${ACCOUNT_ID}:policy/SplunkAppFrameworkS3ReadPolicy

aws iam delete-role --role-name splunk-app-framework-s3-read

aws iam delete-policy \
  --policy-arn arn:aws:iam::${ACCOUNT_ID}:policy/SplunkAppFrameworkS3ReadPolicy
```

## References

- [AWS IRSA Documentation](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- [Splunk Operator App Framework](https://github.com/splunk/splunk-operator/blob/main/docs/AppFramework.md)
- [AWS S3 IAM Best Practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/security-iam.html)
