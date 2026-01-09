#!/bin/bash
#
# Complete LDAP Configuration Test Script
# This script sets up IRSA and deploys Splunk with LDAP via App Framework
#
# Prerequisites:
# - AWS CLI configured with appropriate permissions
# - kubectl configured for your EKS cluster
# - eksctl installed (for OIDC setup)
#

set -e  # Exit on error

# Configuration variables - UPDATE THESE
export CLUSTER_NAME="your-eks-cluster-name"
export AWS_REGION="us-west-2"
export S3_BUCKET="your-s3-bucket-name"
export NAMESPACE="splunk-operator"
export LDAP_BIND_PASSWORD="YourSecureLDAPPassword"
export LDAP_SERVER="ldap.example.com"
export LDAP_BIND_DN="cn=splunk-bind,ou=service-accounts,dc=example,dc=com"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_prerequisites() {
    log_info "Checking prerequisites..."

    command -v aws >/dev/null 2>&1 || { log_error "aws CLI not found"; exit 1; }
    command -v kubectl >/dev/null 2>&1 || { log_error "kubectl not found"; exit 1; }
    command -v eksctl >/dev/null 2>&1 || { log_error "eksctl not found"; exit 1; }

    log_info "✓ All prerequisites met"
}

setup_variables() {
    log_info "Setting up variables..."

    export ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
    export OIDC_PROVIDER=$(aws eks describe-cluster \
        --name $CLUSTER_NAME \
        --region $AWS_REGION \
        --query "cluster.identity.oidc.issuer" \
        --output text | sed -e "s/^https:\/\///")

    export SERVICE_ACCOUNT="splunk-app-s3-reader"
    export ROLE_NAME="splunk-app-framework-s3-read"
    export POLICY_NAME="SplunkAppFrameworkS3ReadPolicy"

    log_info "Account ID: $ACCOUNT_ID"
    log_info "OIDC Provider: $OIDC_PROVIDER"
}

enable_oidc() {
    log_info "Enabling OIDC provider for EKS cluster..."

    eksctl utils associate-iam-oidc-provider \
        --region=$AWS_REGION \
        --cluster=$CLUSTER_NAME \
        --approve || log_warn "OIDC provider may already be associated"

    log_info "✓ OIDC provider enabled"
}

create_iam_policy() {
    log_info "Creating IAM policy for S3 read access..."

    cat > /tmp/splunk-app-s3-read-policy.json <<EOF
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
        "arn:aws:s3:::${S3_BUCKET}",
        "arn:aws:s3:::${S3_BUCKET}/*"
      ]
    }
  ]
}
EOF

    # Check if policy already exists
    POLICY_ARN="arn:aws:iam::${ACCOUNT_ID}:policy/${POLICY_NAME}"
    if aws iam get-policy --policy-arn $POLICY_ARN >/dev/null 2>&1; then
        log_warn "Policy $POLICY_NAME already exists"
    else
        aws iam create-policy \
            --policy-name $POLICY_NAME \
            --policy-document file:///tmp/splunk-app-s3-read-policy.json
        log_info "✓ IAM policy created: $POLICY_ARN"
    fi
}

create_iam_role() {
    log_info "Creating IAM role for IRSA..."

    cat > /tmp/trust-policy.json <<EOF
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

    # Check if role already exists
    if aws iam get-role --role-name $ROLE_NAME >/dev/null 2>&1; then
        log_warn "Role $ROLE_NAME already exists"
    else
        aws iam create-role \
            --role-name $ROLE_NAME \
            --assume-role-policy-document file:///tmp/trust-policy.json \
            --description "IAM role for Splunk Operator App Framework S3 access"
        log_info "✓ IAM role created"
    fi

    # Attach policy to role
    POLICY_ARN="arn:aws:iam::${ACCOUNT_ID}:policy/${POLICY_NAME}"
    aws iam attach-role-policy \
        --role-name $ROLE_NAME \
        --policy-arn $POLICY_ARN || log_warn "Policy may already be attached"

    export ROLE_ARN="arn:aws:iam::${ACCOUNT_ID}:role/${ROLE_NAME}"
    log_info "✓ IAM role ARN: $ROLE_ARN"
}

customize_ldap_app() {
    log_info "Customizing LDAP app with your settings..."

    # Update authentication.conf with actual values
    cat > /tmp/ldap-auth-config-app/local/authentication.conf <<EOF
# LDAP Authentication Configuration
# Deployed via Splunk Operator App Framework

[authentication]
authType = LDAP
authSettings = corporate-ldap

[corporate-ldap]
# LDAP Server Configuration
SSLEnabled = 1
host = ${LDAP_SERVER}
port = 636

# Bind Configuration
bindDN = ${LDAP_BIND_DN}
bindDNpassword = \$LDAP_BIND_PASSWORD\$

# User Configuration
userBaseDN = ou=users,dc=example,dc=com
userBaseFilter = (objectclass=user)
userNameAttribute = sAMAccountName

# Group Configuration
groupBaseDN = ou=groups,dc=example,dc=com
groupBaseFilter = (objectclass=group)
groupMemberAttribute = member
groupNameAttribute = cn
groupMappingAttribute = dn

# Additional Attributes
emailAttribute = mail
realNameAttribute = displayName
charset = utf8
timelimit = 15

[roleMap_corporate-ldap]
admin = Splunk Admins
power = Splunk Power Users
user = Splunk Users
EOF

    log_info "✓ LDAP app customized"
}

package_ldap_app() {
    log_info "Packaging LDAP app..."

    cd /tmp
    tar -czf ldap-auth-config-app.tgz ldap-auth-config-app/

    log_info "✓ App packaged: /tmp/ldap-auth-config-app.tgz"
}

upload_to_s3() {
    log_info "Uploading LDAP app to S3..."

    aws s3 cp /tmp/ldap-auth-config-app.tgz \
        s3://${S3_BUCKET}/apps/authAppsLoc/ \
        --region $AWS_REGION

    log_info "✓ App uploaded to s3://${S3_BUCKET}/apps/authAppsLoc/"

    # Verify upload
    aws s3 ls s3://${S3_BUCKET}/apps/authAppsLoc/
}

create_k8s_resources() {
    log_info "Creating Kubernetes resources..."

    # Create namespace if it doesn't exist
    kubectl create namespace $NAMESPACE 2>/dev/null || log_warn "Namespace $NAMESPACE already exists"

    # Create service account with IRSA annotation
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${SERVICE_ACCOUNT}
  namespace: ${NAMESPACE}
  annotations:
    eks.amazonaws.com/role-arn: ${ROLE_ARN}
EOF

    log_info "✓ Service account created"

    # Create LDAP credentials secret
    kubectl create secret generic ldap-credentials \
        --from-literal=bind-password="${LDAP_BIND_PASSWORD}" \
        -n $NAMESPACE \
        --dry-run=client -o yaml | kubectl apply -f -

    log_info "✓ LDAP credentials secret created"
}

deploy_standalone() {
    log_info "Deploying Standalone with LDAP app..."

    cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: ldap-test-irsa
  namespace: ${NAMESPACE}
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  serviceAccount: ${SERVICE_ACCOUNT}

  appRepo:
    appsRepoPollIntervalSeconds: 600
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
        path: ${S3_BUCKET}/apps/
        endpoint: https://s3-${AWS_REGION}.amazonaws.com
        region: ${AWS_REGION}

  extraEnv:
    - name: LDAP_BIND_PASSWORD
      valueFrom:
        secretKeyRef:
          name: ldap-credentials
          key: bind-password
EOF

    log_info "✓ Standalone deployed"
}

wait_for_pod() {
    log_info "Waiting for pod to be ready..."

    kubectl wait --for=condition=ready pod \
        -l app.kubernetes.io/instance=ldap-test-irsa \
        -n $NAMESPACE \
        --timeout=600s || {
            log_error "Pod failed to become ready"
            kubectl get pods -n $NAMESPACE
            kubectl logs -n $NAMESPACE -l app.kubernetes.io/instance=ldap-test-irsa --tail=50
            exit 1
        }

    export POD_NAME=$(kubectl get pods -n $NAMESPACE \
        -l app.kubernetes.io/instance=ldap-test-irsa \
        -o jsonpath='{.items[0].metadata.name}')

    log_info "✓ Pod ready: $POD_NAME"
}

verify_irsa() {
    log_info "Verifying IRSA configuration..."

    # Check SA annotation
    kubectl get sa $SERVICE_ACCOUNT -n $NAMESPACE -o yaml | grep eks.amazonaws.com

    # Check AWS env vars in pod
    log_info "AWS environment variables in pod:"
    kubectl exec -n $NAMESPACE $POD_NAME -- env | grep AWS || log_warn "No AWS env vars found"

    # Test S3 access from pod
    log_info "Testing S3 access from pod..."
    kubectl exec -n $NAMESPACE $POD_NAME -- \
        aws s3 ls s3://${S3_BUCKET}/apps/authAppsLoc/ || {
            log_error "S3 access failed from pod"
            exit 1
        }

    log_info "✓ IRSA verified - S3 access working"
}

verify_app_deployment() {
    log_info "Verifying app deployment..."

    # Check app framework status
    kubectl get standalone ldap-test-irsa -n $NAMESPACE -o yaml | \
        grep -A 30 appDeploymentContext || log_warn "No app deployment context yet"

    # Wait a bit for app to deploy
    sleep 30

    # Check if app is installed
    kubectl exec -n $NAMESPACE $POD_NAME -- \
        ls -la /opt/splunk/etc/apps/ | grep ldap || {
            log_warn "LDAP app not yet installed, checking status..."
            kubectl get standalone ldap-test-irsa -n $NAMESPACE -o jsonpath='{.status.appDeploymentContext}'
        }

    log_info "Checking app files..."
    kubectl exec -n $NAMESPACE $POD_NAME -- \
        cat /opt/splunk/etc/apps/ldap-auth-config-app/local/authentication.conf 2>/dev/null || \
        log_warn "authentication.conf not found yet"
}

verify_ldap_config() {
    log_info "Verifying LDAP configuration in Splunk..."

    # Check btool output
    log_info "Splunk btool authentication list:"
    kubectl exec -n $NAMESPACE $POD_NAME -- \
        /opt/splunk/bin/splunk btool authentication list --debug | grep -A 10 "corporate-ldap" || \
        log_warn "LDAP configuration not loaded yet"

    # Check for LDAP in logs
    kubectl exec -n $NAMESPACE $POD_NAME -- \
        grep -i ldap /opt/splunk/var/log/splunk/splunkd.log | tail -20 || \
        log_warn "No LDAP entries in splunkd.log yet"
}

display_summary() {
    log_info "================================================"
    log_info "LDAP Configuration Test - Summary"
    log_info "================================================"
    echo ""
    echo "✓ IRSA IAM Role: $ROLE_ARN"
    echo "✓ S3 Bucket: s3://${S3_BUCKET}/apps/authAppsLoc/"
    echo "✓ Namespace: $NAMESPACE"
    echo "✓ Pod: $POD_NAME"
    echo ""
    log_info "To check app deployment status:"
    echo "kubectl get standalone ldap-test-irsa -n $NAMESPACE -o yaml | grep -A 50 appDeploymentContext"
    echo ""
    log_info "To exec into pod:"
    echo "kubectl exec -it $POD_NAME -n $NAMESPACE -- bash"
    echo ""
    log_info "To view operator logs:"
    echo "kubectl logs -n $NAMESPACE deployment/splunk-operator-controller-manager -f"
    echo ""
    log_info "To access Splunk Web:"
    echo "kubectl port-forward svc/splunk-ldap-test-irsa-service 8000:8000 -n $NAMESPACE"
    echo "Then visit: https://localhost:8000"
}

cleanup() {
    log_warn "Cleaning up resources..."

    read -p "Are you sure you want to cleanup? (yes/no): " confirm
    if [ "$confirm" != "yes" ]; then
        log_info "Cleanup cancelled"
        return
    fi

    # Delete standalone
    kubectl delete standalone ldap-test-irsa -n $NAMESPACE || true

    # Delete service account
    kubectl delete sa $SERVICE_ACCOUNT -n $NAMESPACE || true

    # Delete secret
    kubectl delete secret ldap-credentials -n $NAMESPACE || true

    # Delete from S3
    aws s3 rm s3://${S3_BUCKET}/apps/authAppsLoc/ldap-auth-config-app.tgz || true

    log_info "✓ Cleanup complete"
}

main() {
    log_info "Starting LDAP Configuration Test"
    log_info "=================================="

    check_prerequisites
    setup_variables
    enable_oidc
    create_iam_policy
    create_iam_role
    customize_ldap_app
    package_ldap_app
    upload_to_s3
    create_k8s_resources
    deploy_standalone
    wait_for_pod
    verify_irsa
    verify_app_deployment
    verify_ldap_config
    display_summary

    log_info ""
    log_info "Test complete! Review the output above."
    log_info "To cleanup, run: $0 cleanup"
}

# Handle command line arguments
if [ "$1" == "cleanup" ]; then
    setup_variables
    cleanup
else
    main
fi
