# EKS 1.34+ IRSA JWT Validation Issue - Root Cause and Fix

## Executive Summary

Splunk Enterprise fails to start with IRSA (IAM Roles for Service Accounts) on AWS EKS version 1.34+ due to JWT token validation errors. The issue is caused by EKS 1.34 adding an optional `typ` field to JWT headers, which Splunk's validation code incorrectly rejects.

**Impact:** All Splunk deployments using IRSA on EKS 1.34+ clusters fail with SmartStore and other AWS integrations.

**Status:** ✅ Root cause identified | ⏳ Code fix required in splunkd

---

## Table of Contents

1. [Problem Statement](#problem-statement)
2. [Root Cause Analysis](#root-cause-analysis)
3. [EKS Version Comparison](#eks-version-comparison)
4. [Code Fix](#code-fix)
5. [Testing Guide](#testing-guide)
6. [Verification Steps](#verification-steps)

---

## Problem Statement

### Symptoms

When deploying Splunk Enterprise on EKS 1.34+ with IRSA configured for SmartStore or other AWS services, Splunk fails to start with the following error:

```
ERROR RemoteStorageManager - Cannot load IndexConfig: Unable to load remote volume "s3-volume"
ERROR AWSIAMProvider - Could not find access_key in credentials file
ERROR IAMJsonWebToken - Header size must be 4
```

### Affected Versions

- **Splunk Enterprise:** All versions with IRSA support (9.1.2+)
- **EKS Versions:** 1.34+ (Platform version eks.9+)
- **Components Affected:**
  - SmartStore (S3 remote storage)
  - SQS SmartBus
  - Any AWS service using IRSA credentials

### Working Environments

- **EKS 1.31 and earlier:** ✅ IRSA works correctly
- **Platform Version:** eks.46 and earlier

---

## Root Cause Analysis

### JWT Token Format Change

AWS EKS 1.34 introduced a change to OIDC JWT token headers:

**EKS 1.31 JWT Header (2 fields):**
```json
{
  "alg": "RS256",
  "kid": "f3cd3c89b9f7cf543038162f562f8d5bf41d03b8"
}
```

**EKS 1.34+ JWT Header (3 fields):**
```json
{
  "alg": "RS256",
  "kid": "3058b0718114ad1b1d31b08f44496b14",
  "typ": "JWT"
}
```

The addition of the optional `"typ": "JWT"` field is **RFC 7519 compliant** but breaks Splunk's strict validation.

### Splunk Code Issue

**File:** `/opt/splunk/src/framework/auth/IAMJsonWebToken.cpp`
**Line:** 29
**Function:** `_validateHeader()`

```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;

    // BUG: Strictly checks for exactly 2 header fields
    if (!rootHdr.valid() || rootHdr.numChildren() != IAM_JWT_HDR_SIZE) {  // IAM_JWT_HDR_SIZE = 2
        throw JsonWebTokenException(Str::fromFormat(
                "Header size must be %lu", (unsigned long)HEADERS_SIZE));
    }

    _validateAttribute(rootHdr, HDR_KID);    // Validates "kid" exists
    _validateAttribute(rootHdr, HDR_ALGO);   // Validates "alg" exists

    // Missing: Does not validate "typ" but rejects if present
}
```

**Problem:** The code uses `numChildren() != IAM_JWT_HDR_SIZE` which requires **exactly 2 fields**. When EKS 1.34 adds the optional `typ` field, the count becomes 3, causing validation to fail.

### Why This Violates RFC 7519

From [RFC 7519 Section 5.1](https://datatracker.ietf.org/doc/html/rfc7519#section-5.1):

> **typ (Type) Header Parameter:**
> The "typ" (type) header parameter is used by JWT applications to declare the media type of this complete JWT. This is intended for use by the application when more than one kind of object could be present in an application data structure. **The "typ" value is OPTIONAL.**

The `typ` field is explicitly optional and should be accepted if present.

---

## EKS Version Comparison

### Test Results

| Cluster | EKS Version | Platform | JWT Fields | IRSA Status |
|---------|-------------|----------|------------|-------------|
| vivek-ai-platform | 1.31 | eks.46 | 2 (alg, kid) | ✅ Working |
| kkoziol-irsa-test-cluster | 1.34 | eks.9 | 3 (alg, kid, typ) | ❌ Failed |

### Cluster Details

**Working Cluster:**
```bash
Cluster: vivek-ai-platform
EKS Version: 1.31
Platform: eks.46
OIDC Issuer: https://oidc.eks.us-west-2.amazonaws.com/id/A5918B756AF6A17A4223F1108B9488EE

JWT Token Header:
{
  "alg": "RS256",
  "kid": "f3cd3c89b9f7cf543038162f562f8d5bf41d03b8"
}

Splunk Result: ✅ SmartStore uploads buckets to S3 successfully
```

**Non-Working Cluster:**
```bash
Cluster: kkoziol-irsa-test-cluster
EKS Version: 1.34
Platform: eks.9
OIDC Issuer: https://oidc.eks.us-west-2.amazonaws.com/id/3058B0718114AD1B1D31B08F44496B14

JWT Token Header:
{
  "alg": "RS256",
  "kid": "3058b0718114ad1b1d31b08f44496b14",
  "typ": "JWT"
}

Splunk Result: ❌ JWT validation fails - "Header size must be 4"
```

---

## Code Fix

### Proposed Fix

**File:** `src/framework/auth/IAMJsonWebToken.cpp`
**Lines:** 27-36

#### Current Code (Broken)

```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;

    // BROKEN: Strictly requires exactly 2 fields
    if (!rootHdr.valid() || rootHdr.numChildren() != IAM_JWT_HDR_SIZE) {
        throw JsonWebTokenException(Str::fromFormat(
                "Header size must be %lu", (unsigned long)HEADERS_SIZE));
    }

    _validateAttribute(rootHdr, HDR_KID);
    _validateAttribute(rootHdr, HDR_ALGO);
}
```

#### Fixed Code (RFC 7519 Compliant)

```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;

    // FIXED: Validate header exists and has at least required fields
    if (!rootHdr.valid()) {
        throw JsonWebTokenException("Invalid JWT header format");
    }

    // Validate required fields exist (alg and kid are required)
    _validateAttribute(rootHdr, HDR_KID);
    _validateAttribute(rootHdr, HDR_ALGO);

    // OPTIONAL: Validate 'typ' field if present (RFC 7519 compliant)
    if (rootHdr.hasChild(HDR_TYP)) {
        std::string typ = rootHdr.getChild(HDR_TYP).getValue();
        if (typ != "JWT") {
            throw JsonWebTokenException(Str::fromFormat(
                    "Invalid JWT type: %s (expected 'JWT')", typ.c_str()));
        }
    }

    // Ensure no unexpected extra fields beyond alg, kid, and typ
    size_t numChildren = rootHdr.numChildren();
    if (numChildren < IAM_JWT_HDR_SIZE || numChildren > IAM_JWT_HDR_SIZE + 1) {
        throw JsonWebTokenException(Str::fromFormat(
                "JWT header must have 2-3 fields, found %lu",
                (unsigned long)numChildren));
    }
}
```

### Header Constants Update

**File:** `src/framework/auth/IAMJsonWebToken.h`

Add the `typ` field constant:

```cpp
// JWT Header field names
static const char* HDR_KID = "kid";
static const char* HDR_ALGO = "alg";
static const char* HDR_TYP = "typ";  // ADD THIS LINE

// Minimum required header size
static const size_t IAM_JWT_HDR_SIZE = 2;  // alg and kid are required
```

### Alternative Simpler Fix

If you want a minimal change without full `typ` validation:

```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;

    if (!rootHdr.valid()) {
        throw JsonWebTokenException("Invalid JWT header format");
    }

    // Only validate that REQUIRED fields exist, ignore optional fields
    _validateAttribute(rootHdr, HDR_KID);
    _validateAttribute(rootHdr, HDR_ALGO);

    // Accept any number of fields >= 2 (allows optional typ and future fields)
}
```

---

## Testing Guide

### Prerequisites

1. **EKS Clusters:**
   - EKS 1.31 cluster (baseline - should work)
   - EKS 1.34+ cluster (reproduction - should fail before fix)

2. **Tools:**
   - `kubectl` configured for both clusters
   - `aws` CLI with appropriate credentials
   - Splunk Operator for Kubernetes installed

3. **IAM Setup:**
   - IRSA-enabled service account with IAM role
   - S3 bucket with read/write permissions
   - IAM policy allowing S3 operations

### Test Setup

#### 1. Create IRSA Service Account

```yaml
# irsa-service-account.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: splunk-irsa-test
  namespace: splunk-operator
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT_ID:role/splunk-irsa-role
```

Apply:
```bash
kubectl apply -f irsa-service-account.yaml
```

#### 2. Verify JWT Token Format

**On EKS 1.31 (should have 2 fields):**
```bash
kubectl run jwt-test --image=alpine --rm -it --restart=Never \
  --serviceaccount=splunk-irsa-test \
  -- sh -c 'cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token | cut -d. -f1 | base64 -d'
```

Expected output:
```json
{"alg":"RS256","kid":"..."}
```

**On EKS 1.34+ (should have 3 fields):**
```bash
# Same command
kubectl run jwt-test --image=alpine --rm -it --restart=Never \
  --serviceaccount=splunk-irsa-test \
  -- sh -c 'cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token | cut -d. -f1 | base64 -d'
```

Expected output:
```json
{"alg":"RS256","kid":"...","typ":"JWT"}
```

#### 3. Deploy Splunk Standalone with SmartStore

```yaml
# splunk-standalone-irsa.yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: test-irsa-smartstore
  namespace: splunk-operator
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  image: splunk/splunk:10.0.0  # or 9.4.3
  serviceAccount: splunk-irsa-test
  smartstore:
    volumes:
      - name: s3-volume
        path: YOUR-BUCKET-NAME/smartstore/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
    defaults:
      volumeName: s3-volume
    indexes:
      - name: main
        remotePath: $_index_name
```

Apply:
```bash
kubectl apply -f splunk-standalone-irsa.yaml
```

### Test Scenarios

#### Scenario 1: Reproduce Issue on EKS 1.34+

**Steps:**
1. Deploy Splunk with IRSA on EKS 1.34+ cluster
2. Wait for pod to start
3. Check logs for JWT errors

**Expected Result (Before Fix):**
```bash
kubectl logs -n splunk-operator splunk-test-irsa-smartstore-standalone-0 | grep -i jwt

# Output:
ERROR IAMJsonWebToken - Header size must be 4
ERROR AWSIAMProvider - Could not find access_key in credentials file
ERROR RemoteStorageManager - Cannot load IndexConfig: Unable to load remote volume "s3-volume"
```

**Pod Status:**
```bash
kubectl get pods -n splunk-operator

# Output:
NAME                                      READY   STATUS    RESTARTS   AGE
splunk-test-irsa-smartstore-standalone-0   0/1     Running   0          5m
# Pod stuck in non-ready state
```

#### Scenario 2: Verify Fix Works

**Steps:**
1. Rebuild Splunk with the fixed JWT validation code
2. Deploy the fixed Splunk image with same IRSA configuration
3. Verify pod starts successfully
4. Test SmartStore S3 upload

**Expected Result (After Fix):**
```bash
kubectl get pods -n splunk-operator

# Output:
NAME                                      READY   STATUS    RESTARTS   AGE
splunk-test-irsa-smartstore-standalone-0   1/1     Running   0          3m
# Pod is READY
```

Check logs - should have NO JWT errors:
```bash
kubectl logs -n splunk-operator splunk-test-irsa-smartstore-standalone-0 | grep -i jwt
# No output (no JWT errors)

kubectl logs -n splunk-operator splunk-test-irsa-smartstore-standalone-0 | grep "remoteVolume=s3-volume"
# Output shows indexes initialized with SmartStore:
INFO  IndexWriter - idx=main, Initializing, params='[...remoteVolume=s3-volume...]'
```

#### Scenario 3: Test SmartStore Functionality

**Generate test data:**
```bash
kubectl exec -n splunk-operator splunk-test-irsa-smartstore-standalone-0 -- bash -c '
PASSWORD=$(cat /mnt/splunk-secrets/password)
for i in {1..1000}; do
  curl -k -s -u admin:$PASSWORD \
    "https://localhost:8089/services/receivers/simple?index=main" \
    --data-binary "Test event $i timestamp=$(date +%s)" > /dev/null
done
echo "Generated 1000 events"
'
```

**Roll buckets to trigger S3 upload:**
```bash
kubectl exec -n splunk-operator splunk-test-irsa-smartstore-standalone-0 -- bash -c '
PASSWORD=$(cat /mnt/splunk-secrets/password)
/opt/splunk/bin/splunk _internal call /data/indexes/main/roll-hot-buckets \
  -auth admin:$PASSWORD
'
```

**Verify S3 upload:**
```bash
# Wait 30 seconds for SmartStore to upload
sleep 30

# Check S3 bucket
aws s3 ls s3://YOUR-BUCKET-NAME/smartstore/main/ --recursive

# Expected output: Splunk bucket files uploaded
# Example:
# 2025-12-12 00:02:21  19160 smartstore/main/db/.../1765526531-1765526506.tsidx
# 2025-12-12 00:02:21   3423 smartstore/main/db/.../rawdata/journal.zst
# 2025-12-12 00:02:21   1609 smartstore/main/db/.../receipt.json
```

---

## Verification Steps

### 1. Check EKS Cluster Version

```bash
aws eks describe-cluster --name YOUR-CLUSTER-NAME \
  --query 'cluster.[version,platformVersion]' --output table
```

### 2. Inspect JWT Token Header

```bash
# Get JWT token from pod
kubectl exec -n splunk-operator POD-NAME -- \
  cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token | \
  cut -d. -f1 | base64 -d | jq

# Count header fields
kubectl exec -n splunk-operator POD-NAME -- \
  cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token | \
  cut -d. -f1 | base64 -d | jq 'keys | length'

# Expected output:
# EKS 1.31: 2
# EKS 1.34: 3
```

### 3. Verify IRSA Environment Variables

```bash
kubectl exec -n splunk-operator POD-NAME -- env | grep AWS

# Expected output:
# AWS_ROLE_ARN=arn:aws:iam::ACCOUNT_ID:role/splunk-irsa-role
# AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token
# AWS_REGION=us-west-2
```

### 4. Check Splunk Logs for JWT Errors

```bash
# Check for JWT validation errors
kubectl exec -n splunk-operator POD-NAME -- \
  grep -i "jwt\|header.*size" /opt/splunk/var/log/splunk/splunkd.log

# If fixed: No output
# If broken: ERROR IAMJsonWebToken - Header size must be 4
```

### 5. Verify SmartStore Initialization

```bash
kubectl exec -n splunk-operator POD-NAME -- \
  grep "remoteVolume=s3-volume" /opt/splunk/var/log/splunk/splunkd.log | head -5

# Expected: Multiple indexes showing remoteVolume=s3-volume
```

### 6. Test S3 Access with IRSA

```bash
# Test S3 access from within Splunk pod using IRSA
kubectl exec -n splunk-operator POD-NAME -- /opt/splunk/bin/splunk cmd python3 -c "
import boto3
s3 = boto3.client('s3', region_name='us-west-2')
response = s3.list_objects_v2(Bucket='YOUR-BUCKET', Prefix='smartstore/', MaxKeys=5)
print('S3 Access Success!' if response else 'S3 Access Failed')
"

# Expected: S3 Access Success!
```

---

## Workarounds (Before Code Fix)

### Option 1: Use EKS 1.31 or Earlier

Downgrade or create new cluster with EKS 1.31:
```bash
eksctl create cluster \
  --name splunk-cluster \
  --version 1.31 \
  --region us-west-2 \
  --nodegroup-name standard-workers \
  --node-type m5.xlarge \
  --nodes 3
```

### Option 2: Use Static IAM Credentials (Not Recommended)

Create AWS credentials secret:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aws-s3-credentials
  namespace: splunk-operator
type: Opaque
stringData:
  access_key_id: AKIAIOSFODNN7EXAMPLE
  secret_access_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
```

Update Splunk CR:
```yaml
spec:
  smartstore:
    volumes:
      - name: s3-volume
        path: YOUR-BUCKET/smartstore/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: aws-s3-credentials  # Use static credentials
```

**⚠️ Warning:** Static credentials are less secure than IRSA. Use IRSA when the fix is available.

---

## Build and Release Process

### 1. Apply Code Fix

Update the following files in splunkd source:
- `src/framework/auth/IAMJsonWebToken.cpp` (validation logic)
- `src/framework/auth/IAMJsonWebToken.h` (add HDR_TYP constant)

### 2. Build Splunk Enterprise

```bash
cd /path/to/splunkd/source
make clean
make
```

### 3. Create Test Build

Package the fixed binary:
```bash
make package
```

### 4. Test with Docker

Create custom Splunk image:
```dockerfile
FROM splunk/splunk:10.0.0

# Copy fixed binary
COPY splunkd /opt/splunk/bin/splunkd
RUN chmod +x /opt/splunk/bin/splunkd
```

Build and push:
```bash
docker build -t YOUR-REGISTRY/splunk:10.0.0-jwt-fix .
docker push YOUR-REGISTRY/splunk:10.0.0-jwt-fix
```

### 5. Deploy to Test Cluster

Update Splunk CR with fixed image:
```yaml
spec:
  image: YOUR-REGISTRY/splunk:10.0.0-jwt-fix
```

### 6. Run Test Suite

Execute all test scenarios from [Testing Guide](#testing-guide).

---

## Rollout Plan

### Phase 1: Internal Testing (Week 1)
- Build fix in development branch
- Test on EKS 1.31 (baseline)
- Test on EKS 1.34+ (fix validation)
- Verify SmartStore functionality
- Performance testing

### Phase 2: Beta Release (Week 2-3)
- Release as beta/patch version
- Document in release notes
- Provide to early adopters on EKS 1.34+
- Gather feedback

### Phase 3: General Availability (Week 4)
- Include in next Splunk Enterprise release
- Update documentation
- Publish KB article
- Notify customers on EKS 1.34+

---

## Success Criteria

✅ **Fix is successful when:**

1. Splunk starts successfully on EKS 1.34+ with IRSA
2. No JWT validation errors in logs
3. SmartStore successfully uploads buckets to S3 using IRSA
4. SQS SmartBus works with IRSA credentials
5. All AWS integrations function correctly
6. Performance is not degraded
7. Backward compatible with EKS 1.31 (2-field JWT headers)

---

## References

### Documentation
- [RFC 7519 - JSON Web Token (JWT)](https://datatracker.ietf.org/doc/html/rfc7519)
- [EKS IRSA Documentation](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- [Splunk SmartStore Documentation](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/AboutSmartStore)
- [Splunk Operator for Kubernetes](https://github.com/splunk/splunk-operator)

### Related Issues
- AWS EKS Platform Version Release Notes: eks.9 (EKS 1.34)
- Splunk IRSA Support: Added in 9.1.2

### Test Clusters
- Working: `vivek-ai-platform` (EKS 1.31, eks.46)
- Failing: `kkoziol-irsa-test-cluster` (EKS 1.34, eks.9)

---

## Contact

For questions or issues related to this fix:
- **Splunk Engineering:** File JIRA ticket in CSPL project
- **Kubernetes Team:** Slack #splunk-kubernetes
- **AWS Support:** For EKS-specific questions

---

**Document Version:** 1.0
**Last Updated:** 2025-12-12
**Author:** Splunk Platform Engineering
**Status:** Pending Code Fix
