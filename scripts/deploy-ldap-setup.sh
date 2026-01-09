#!/bin/bash
#
# Splunk LDAP Authentication Setup Script
# This script automates the deployment of LDAP authentication for Splunk on Kubernetes
#
# Usage: ./deploy-ldap-setup.sh [OPTIONS]
#
# Options:
#   --cluster-name CLUSTER_NAME    EKS cluster name (required)
#   --region REGION                AWS region (default: us-west-2)
#   --bucket BUCKET                S3 bucket name (required)
#   --namespace NAMESPACE          Kubernetes namespace (default: splunk-operator)
#   --skip-openldap               Skip OpenLDAP deployment (use existing LDAP)
#   --help                        Show this help message
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
REGION="us-west-2"
NAMESPACE="splunk-operator"
SKIP_OPENLDAP=false
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Functions
print_header() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

usage() {
    grep '^#' "$0" | tail -n +3 | head -n -1 | cut -c 3-
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --cluster-name)
            CLUSTER_NAME="$2"
            shift 2
            ;;
        --region)
            REGION="$2"
            shift 2
            ;;
        --bucket)
            BUCKET="$2"
            shift 2
            ;;
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --skip-openldap)
            SKIP_OPENLDAP=true
            shift
            ;;
        --help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Validate required arguments
if [ -z "$CLUSTER_NAME" ]; then
    print_error "Cluster name is required"
    usage
fi

if [ -z "$BUCKET" ]; then
    print_error "S3 bucket name is required"
    usage
fi

print_header "Splunk LDAP Authentication Setup"
echo "Cluster: $CLUSTER_NAME"
echo "Region: $REGION"
echo "Bucket: $BUCKET"
echo "Namespace: $NAMESPACE"
echo ""

# Step 1: Get AWS account and OIDC provider
print_header "Step 1: Getting AWS Configuration"

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
print_success "AWS Account ID: $ACCOUNT_ID"

OIDC_PROVIDER=$(aws eks describe-cluster \
  --name "$CLUSTER_NAME" \
  --region "$REGION" \
  --query "cluster.identity.oidc.issuer" \
  --output text | sed -e "s/^https:\/\///")
print_success "OIDC Provider: $OIDC_PROVIDER"

# Step 2: Deploy OpenLDAP (optional)
if [ "$SKIP_OPENLDAP" = false ]; then
    print_header "Step 2: Deploying OpenLDAP Test Server"

    if [ -f "$PROJECT_DIR/openldap-deployment.yaml" ]; then
        kubectl apply -f "$PROJECT_DIR/openldap-deployment.yaml"
        print_success "OpenLDAP deployment created"

        print_info "Waiting for OpenLDAP to be ready..."
        kubectl wait --for=condition=Ready \
          pod -l app=openldap \
          -n "$NAMESPACE" \
          --timeout=300s
        print_success "OpenLDAP is ready"
    else
        print_error "openldap-deployment.yaml not found"
        exit 1
    fi
else
    print_info "Skipping OpenLDAP deployment"
fi

# Step 3: Create LDAP app
print_header "Step 3: Creating LDAP Authentication App"

APP_DIR="/tmp/ldap-auth-config-app"
rm -rf "$APP_DIR"
mkdir -p "$APP_DIR/local"
mkdir -p "$APP_DIR/default"
mkdir -p "$APP_DIR/metadata"

# Create app.conf
cat > "$APP_DIR/default/app.conf" << 'EOF'
[install]
is_configured = true
state = enabled

[ui]
is_visible = false
label = LDAP Authentication Configuration

[launcher]
author = Splunk Administrator
description = LDAP authentication configuration for Splunk Enterprise
version = 1.0.0
EOF

# Create authentication.conf
cat > "$APP_DIR/local/authentication.conf" << 'EOF'
[authentication]
authType = LDAP
authSettings = corporate-ldap

[corporate-ldap]
SSLEnabled = 0
host = openldap.splunk-operator.svc.cluster.local
port = 389

bindDN = cn=admin,dc=splunktest,dc=local
bindDNpassword = admin

userBaseDN = ou=users,dc=splunktest,dc=local
userBaseFilter = (objectclass=inetOrgPerson)
userNameAttribute = uid

groupBaseDN = ou=groups,dc=splunktest,dc=local
groupBaseFilter = (objectclass=groupOfNames)
groupMemberAttribute = member
groupNameAttribute = cn
groupMappingAttribute = dn

emailAttribute = mail
realNameAttribute = displayName
charset = utf8
timelimit = 15

[roleMap_corporate-ldap]
admin = Splunk Admins
power = Splunk Power Users
user = Splunk Users
EOF

print_success "LDAP app created"

# Package app
cd /tmp
tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/
print_success "LDAP app packaged"

# Step 4: Create IAM role
print_header "Step 4: Creating IAM Role for IRSA"

ROLE_NAME="splunk-ldap-s3-read"
POLICY_NAME="SplunkLDAPAppS3ReadPolicy"

# Create trust policy
TRUST_POLICY=$(cat <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Federated": "arn:aws:iam::${ACCOUNT_ID}:oidc-provider/${OIDC_PROVIDER}"
    },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "${OIDC_PROVIDER}:sub": [
          "system:serviceaccount:${NAMESPACE}:splunk-app-s3-reader",
          "system:serviceaccount:${NAMESPACE}:splunk-operator-controller-manager"
        ],
        "${OIDC_PROVIDER}:aud": "sts.amazonaws.com"
      }
    }
  }]
}
EOF
)

# Check if role exists
if aws iam get-role --role-name "$ROLE_NAME" &>/dev/null; then
    print_info "IAM role $ROLE_NAME already exists"
else
    echo "$TRUST_POLICY" > /tmp/trust-policy.json
    aws iam create-role \
      --role-name "$ROLE_NAME" \
      --assume-role-policy-document file:///tmp/trust-policy.json
    print_success "IAM role created: $ROLE_NAME"
fi

# Create S3 policy
S3_POLICY=$(cat <<EOF
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
        "arn:aws:s3:::${BUCKET}",
        "arn:aws:s3:::${BUCKET}/*"
      ]
    }
  ]
}
EOF
)

# Check if policy exists
POLICY_ARN="arn:aws:iam::${ACCOUNT_ID}:policy/${POLICY_NAME}"
if aws iam get-policy --policy-arn "$POLICY_ARN" &>/dev/null; then
    print_info "Policy $POLICY_NAME already exists"
else
    echo "$S3_POLICY" > /tmp/s3-policy.json
    aws iam create-policy \
      --policy-name "$POLICY_NAME" \
      --policy-document file:///tmp/s3-policy.json
    print_success "S3 policy created: $POLICY_NAME"
fi

# Attach policy to role
aws iam attach-role-policy \
  --role-name "$ROLE_NAME" \
  --policy-arn "$POLICY_ARN" &>/dev/null || true
print_success "Policy attached to role"

ROLE_ARN="arn:aws:iam::${ACCOUNT_ID}:role/${ROLE_NAME}"
print_success "IAM Role ARN: $ROLE_ARN"

# Step 5: Upload app to S3
print_header "Step 5: Uploading LDAP App to S3"

S3_PREFIX="splunk-apps/authAppsLoc"
aws s3 cp /tmp/ldap-auth-config-app.tgz "s3://${BUCKET}/${S3_PREFIX}/ldap-auth-config-app.tgz"
print_success "App uploaded to s3://${BUCKET}/${S3_PREFIX}/"

# Step 6: Annotate ServiceAccount
print_header "Step 6: Configuring ServiceAccount"

kubectl annotate serviceaccount -n "$NAMESPACE" splunk-app-s3-reader \
  eks.amazonaws.com/role-arn="$ROLE_ARN" --overwrite
print_success "ServiceAccount annotated with IAM role"

# Step 7: Create or update Standalone CR
print_header "Step 7: Creating Splunk Standalone CR"

STANDALONE_CR="splunk-ldap-test-standalone.yaml"

cat > "/tmp/$STANDALONE_CR" <<EOF
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: splunk-ldap-test
  namespace: $NAMESPACE
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  image: splunk/splunk:10.0.0
  imagePullPolicy: IfNotPresent
  serviceAccount: splunk-app-s3-reader

  appRepo:
    appSources:
      - name: authApps
        location: authAppsLoc/
        scope: local
        volumeName: auth-apps

    defaults:
      volumeName: app-defaults
      scope: local

    volumes:
      - name: auth-apps
        storageType: s3
        provider: aws
        path: $BUCKET
        endpoint: https://s3.${REGION}.amazonaws.com
        region: $REGION

      - name: app-defaults
        storageType: s3
        provider: aws
        path: $BUCKET
        endpoint: https://s3.${REGION}.amazonaws.com
        region: $REGION

  resources:
    requests:
      memory: "4Gi"
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"

  etcVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 10Gi

  varVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 100Gi
EOF

kubectl apply -f "/tmp/$STANDALONE_CR"
print_success "Standalone CR created"

print_info "Waiting for Splunk pod to be ready (this may take 5-10 minutes)..."
kubectl wait --for=condition=Ready \
  pod/splunk-ldap-test-standalone-0 \
  -n "$NAMESPACE" \
  --timeout=900s || true

# Step 8: Verify setup
print_header "Step 8: Verifying Setup"

print_info "Checking if app was downloaded..."
sleep 30

if kubectl exec -n "$NAMESPACE" splunk-ldap-test-standalone-0 -- \
   ls /opt/splunk/etc/apps/ldap-auth-config-app &>/dev/null; then
    print_success "LDAP app is installed"
else
    print_error "LDAP app not found - check operator logs"
fi

print_info "Checking LDAP configuration..."
if kubectl exec -n "$NAMESPACE" splunk-ldap-test-standalone-0 -- \
   /opt/splunk/bin/splunk btool authentication list authentication 2>/dev/null | grep -q "authType = LDAP"; then
    print_success "LDAP authentication configured"
else
    print_error "LDAP configuration not found"
fi

# Summary
print_header "Setup Complete!"
echo ""
echo -e "${GREEN}Next Steps:${NC}"
echo ""
echo "1. Test LDAP authentication via REST API:"
echo "   kubectl exec -n $NAMESPACE splunk-ldap-test-standalone-0 -- \\"
echo "     curl -k -s -u john.doe:SplunkAdmin123 \\"
echo "     https://localhost:8089/services/authentication/current-context"
echo ""
echo "2. Test via Splunk Web UI:"
echo "   kubectl port-forward -n $NAMESPACE splunk-ldap-test-standalone-0 8000:8000"
echo "   Open: http://localhost:8000"
echo "   Login: john.doe / SplunkAdmin123"
echo ""
echo "3. View logs:"
echo "   kubectl logs -n $NAMESPACE splunk-ldap-test-standalone-0"
echo ""
echo -e "${YELLOW}Test Users:${NC}"
echo "  john.doe / SplunkAdmin123 (admin role)"
echo "  jane.smith / SplunkPower123 (power role)"
echo "  bob.user / SplunkUser123 (user role)"
echo ""
echo -e "${BLUE}For full documentation, see: LDAP_REFERENCE_ARCHITECTURE.md${NC}"
