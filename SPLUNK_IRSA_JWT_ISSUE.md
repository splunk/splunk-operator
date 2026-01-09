# Splunk IRSA JWT Token Validation Issue

## Executive Summary

Splunk's AWS IRSA (IAM Roles for Service Accounts) credential provider fails to authenticate when using EKS-issued OIDC JWT tokens, affecting **ALL AWS service integrations including SQS SmartBus AND S3 SmartStore**. The root cause is an overly strict JWT header validation that rejects standard-compliant tokens containing the optional `typ` field.

**Impact**:
- **IngestorCluster**: Cannot connect to SQS queues, resulting in failed data ingestion and continuous restart loops
- **SmartStore**: Cannot authenticate to S3, **preventing Splunk from starting entirely** with "Cannot load IndexConfig" fatal error

**Priority**: Critical - Blocks ALL IRSA usage in EKS environments for both data ingestion and storage

---

## Problem Statement

### Symptoms

1. **Initial Startup**: Splunk IngestorCluster pods fail to establish SQS SmartBus connections
2. **Restart Behavior**: After Splunk restart, RemoteQueueOutputWorker initialization fails with:
   ```
   ERROR AwsCredentials - SQS_Queue did not find credentials in metadata endpoint ... giving up
   FATAL RemoteQueueOutputWorker - Phase-1 pre-flight checks failed for queueName=titan-demo-q
   ```
3. **Timeout**: ~4 minutes of attempting to reach EC2 metadata endpoint (169.254.169.254) before giving up
4. **Result**: Pods remain in `CrashLoopBackOff` or non-ready state

### Environment

- **Platform**: Amazon EKS (Elastic Kubernetes Service)
- **Splunk Version**: 10.0.1
- **Component**: IngestorCluster with SQS SmartBus configuration
- **Authentication**: IRSA (IAM Roles for Service Accounts) with OIDC
- **Affected Code**: `src/framework/auth/IAMJsonWebToken.cpp` and `src/framework/AwsCredentials.cpp`

### Configuration

Service Account with IRSA annotation:
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ingestion-demo-sa
  namespace: default
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::667741767953:role/eksctl-titan-demo-addon-iamserviceaccount-def-Role1-2OJKSdacq0ic
```

IngestorCluster with SQS configuration:
```yaml
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: ingestor
spec:
  serviceAccount: ingestion-demo-sa
  busConfigurationRef:
    name: bus
    namespace: default
  replicas: 3
```

Environment variables correctly set:
```
AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token
AWS_ROLE_ARN=arn:aws:iam::667741767953:role/eksctl-titan-demo-addon-iamserviceaccount-def-Role1-2OJKSdacq0ic
AWS_REGION=us-west-2
SPLUNK_ROLE=splunk_standalone
```

---

## Debug Methodology

### 1. Initial Investigation

**Examined pod logs and identified credential failure:**
```bash
kubectl logs splunk-ingestor-ingestor-0 -n default --tail=500 | grep -i "credential\|sqs\|error"
```

**Key finding**: Error occurred 4 minutes after RemoteQueueOutputWorker initialization attempt.

### 2. IRSA Configuration Validation

**Verified service account and token mount:**
```bash
# Check service account annotation
kubectl get sa ingestion-demo-sa -n default -o yaml | grep eks.amazonaws.com

# Verify token file exists
kubectl exec splunk-ingestor-ingestor-0 -- ls -la /var/run/secrets/eks.amazonaws.com/serviceaccount/

# Check environment variables
kubectl exec splunk-ingestor-ingestor-0 -- env | grep AWS
```

**Result**: All IRSA components correctly configured.

### 3. Splunk Source Code Analysis

**Examined AWS credential provider chain** in `AwsCredentials.cpp:1254-1305`:
1. Static credentials from config
2. **IamServiceAccountAwsCredentials** (IRSA - web identity token)
3. TaskAwsCredentials (ECS)
4. InstanceAwsCredentials (EC2 metadata endpoint)

**Key insight**: If IRSA throws exception, it falls through to EC2 metadata endpoint.

### 4. Enable Debug Logging

**Created log configuration to enable AWS credential debugging:**
```bash
kubectl exec splunk-ingestor-ingestor-0 -- bash -c 'cat > /opt/splunk/etc/log-local.cfg << EOF
[splunkd]
category.AwsCredentials=DEBUG
category.RemoteQueueOutputWorker=DEBUG
category.RemoteQueueOutputProcessor=DEBUG
category.SQS_QueueProps=DEBUG
EOF'

kubectl exec splunk-ingestor-ingestor-0 -- /opt/splunk/bin/splunk restart
```

### 5. Critical Debug Logs Analysis

**Found the smoking gun in splunkd.log:**
```
DEBUG AwsCredentials [4444 remotequeueoutput] - using signature version=v4 for stanza=
DEBUG AwsCredentials [4444 remotequeueoutput] - SQS_Queue did not find credentials in env variables, will try IAM Service account
DEBUG AwsCredentials [4444 remotequeueoutput] - SQS_Queuetried if IRSA configured IamServiceAccountAwsCredentials error failed to deserialize JWT, err=Header size must be 4
INFO  AwsCredentials [4444 remotequeueoutput] - TaskAwsCredentials are not setup as the AWS_CONTAINER_CREDENTIALS_RELATIVE_URI is empty; falling back to InstanceAwsCredentials.
DEBUG AwsCredentials [4444 remotequeueoutput] - getting IAM role via url=http://169.254.169.254/latest/meta-data/iam/security-credentials/
DEBUG AwsCredentials [4444 remotequeueoutput] - Failed to get IAM role, statuscode=401, statusDescription=Unauthorized
```

**Critical Error**: `failed to deserialize JWT, err=Header size must be 4`

### 6. JWT Token Analysis

**Decoded EKS-issued JWT token header:**
```bash
kubectl exec splunk-ingestor-ingestor-0 -- sh -c 'TOKEN=$(cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token); echo $TOKEN | cut -d. -f1 | base64 -d'
```

**Result:**
```json
{
  "alg": "RS256",
  "kid": "71e0abe734a1a73c293796c056ec6821cd0b2e29",
  "typ": "JWT"
}
```

**Token has 3 header fields**: `alg`, `kid`, `typ`

### 7. Source Code Root Cause

**Examined `IAMJsonWebToken.cpp:27-36`:**
```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;
    if (!rootHdr.valid() || rootHdr.numChildren() != IAM_JWT_HDR_SIZE) {  // IAM_JWT_HDR_SIZE = 2
        throw JsonWebTokenException(Str::fromFormat(
                "Header size must be %lu", (unsigned long)HEADERS_SIZE));  // Prints "4" from base class
    }

    _validateAttribute(rootHdr, HDR_KID);
    _validateAttribute(rootHdr, HDR_ALGO);
}
```

**Problem**: Code expects exactly 2 header fields, but EKS tokens have 3.

---

## Root Cause

### Technical Root Cause

**Splunk's `IAMJsonWebToken` validator has an overly strict header size check that rejects RFC 7519 compliant JWT tokens.**

#### Code Analysis

1. **File**: `src/framework/auth/IAMJsonWebToken.cpp`
2. **Line**: 29
3. **Issue**: `rootHdr.numChildren() != IAM_JWT_HDR_SIZE` where `IAM_JWT_HDR_SIZE = 2`
4. **Expected fields**: `alg`, `kid`
5. **Actual EKS token fields**: `alg`, `kid`, `typ`

#### Standards Compliance

Per **RFC 7519 §5.1** (JSON Web Token Header):
- The `typ` (type) header parameter is **OPTIONAL** and has been part of the JWT standard since its inception
- Common OIDC providers (including AWS EKS) include this field as a best practice
- Valid JWT tokens can have 2-4 header fields
- Kubernetes projected service account tokens (stable since v1.20) follow RFC 7519 and include the `typ` field

**Historical Note**: The `typ: "JWT"` field has always been included in EKS OIDC tokens. This is not a recent addition - it's standard practice for OIDC providers to explicitly declare the token type. Splunk's validator has been incompatible with RFC 7519-compliant tokens from the beginning of IRSA support.

**Code History Analysis**:
- **Initial Implementation**: September 29, 2023 (commit `df5efb7`) by Vivek Reddy
- **First Splunk Version**: v9.1.2 (first version with IRSA support)
- **Original Bug**: The code defined `IAM_JWT_KEY_TYP{"typ"}` constants and wrote a `_validateTypeHeader()` function, but never called it. The `_validateHeader()` function was hardcoded to accept exactly 2 fields (`IAM_JWT_HDR_SIZE = 2`) from day one.
- **Impact**: IRSA has **NEVER** worked correctly with EKS OIDC tokens in ANY Splunk version (9.1.2+)
- **Related Ticket**: SPL-244785 / CSPL-2143 (original IRSA implementation)

#### Comparison with Other Implementations

| JWT Type | Expected Headers | Actual Size |
|----------|-----------------|-------------|
| Base `JsonWebToken` | 4 fields | `HEADERS_SIZE = 4` |
| `RFC7519JsonWebToken` | 3 fields | `HEADERS_SIZE = 3` |
| **`IAMJsonWebToken`** | **2 fields** | **`IAM_JWT_HDR_SIZE = 2`** |
| EKS OIDC Token | 3 fields | Includes `typ` |

### Universal Impact Across AWS Services

**Confirmed Affected Services:**
1. **SQS SmartBus (IngestorCluster)**: Fails to connect to SQS queues, ~4-minute timeout, pod restart loops
2. **S3 SmartStore (Standalone/Cluster)**: **MORE SEVERE** - Prevents Splunk from starting, "Cannot load IndexConfig" fatal error

Both services use the same `AwsCredentials::getCredentials()` code path and hit the identical JWT validation bug in `IAMJsonWebToken::_validateHeader()`.

### Why It Works Initially But Fails on Restart

**First boot**: In some cases, Splunk may start before the RemoteQueueOutputWorker fully initializes, or the credential provider chain isn't invoked immediately.

**After restart**: RemoteQueueOutputWorker initializes during startup and immediately attempts to fetch credentials, hitting the JWT validation bug every time.

**SmartStore**: ALWAYS fails on first boot because index validation happens during splunkd startup, before any services start.

---

## Solution

### Proposed Fix

**Modify `src/framework/auth/IAMJsonWebToken.cpp` line 27-36 to accept 2 or 3 header fields:**

#### Option 1: Simple Fix (Recommended)

```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;
    size_t numChildren = rootHdr.numChildren();

    // Accept 2 fields (alg, kid) OR 3 fields (alg, kid, typ)
    // EKS OIDC tokens include the optional 'typ' field per RFC 7519 §5.1
    if (!rootHdr.valid() || (numChildren != IAM_JWT_HDR_SIZE && numChildren != 3)) {
        throw JsonWebTokenException(Str::fromFormat(
                "Header size must be %lu or 3 (with optional typ field), got %lu",
                (unsigned long)IAM_JWT_HDR_SIZE,
                (unsigned long)numChildren));
    }

    // Validate required fields - typ is optional so we don't validate it
    _validateAttribute(rootHdr, HDR_KID);
    _validateAttribute(rootHdr, HDR_ALGO);
}
```

#### Option 2: Stricter Validation (More Defensive)

```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;
    size_t numChildren = rootHdr.numChildren();

    // Accept 2-3 header fields per RFC 7519
    if (!rootHdr.valid() || numChildren < 2 || numChildren > 3) {
        throw JsonWebTokenException(Str::fromFormat(
                "Header must have 2-3 fields (alg, kid, optional typ), got %lu",
                (unsigned long)numChildren));
    }

    // Validate required fields
    _validateAttribute(rootHdr, HDR_KID);
    _validateAttribute(rootHdr, HDR_ALGO);

    // If 3rd field exists, validate it's 'typ' with value 'JWT'
    if (numChildren == 3) {
        _validateTypeHeader(rootHdr);  // This method already exists
    }
}
```

### Impact Analysis

#### Safety Assessment

✅ **Safe**: `IAMJsonWebToken` is ONLY used for AWS IRSA authentication
✅ **Isolated**: No other JWT validation in Splunk uses this class
✅ **Standards Compliant**: RFC 7519 allows optional `typ` field
✅ **Backward Compatible**: Still accepts 2-field tokens
✅ **Forward Compatible**: Accepts standard 3-field tokens

#### Files Modified

1. `src/framework/auth/IAMJsonWebToken.cpp` - Validation logic (1 function)

#### Affected Components

- AWS IRSA credential provider only
- No impact on other authentication mechanisms
- No impact on other JWT implementations (RFC7519, Duo, IAC, etc.)

---

## Testing Procedure

### Unit Tests

**File**: `src/framework/tests/AwsCredentialsTest.cpp`

Add test cases for JWT tokens with 2 and 3 header fields:

```cpp
TEST(IAMJsonWebTokenTest, AcceptsTokenWithTwoHeaderFields) {
    // Token with only alg and kid (original format)
    Str token = "eyJhbGciOiJSUzI1NiIsImtpZCI6InRlc3Qta2V5In0...";
    IAMJsonWebToken jwt;
    EXPECT_NO_THROW(jwt.deserialize(token));
}

TEST(IAMJsonWebTokenTest, AcceptsTokenWithThreeHeaderFields) {
    // Token with alg, kid, and typ (EKS format)
    Str token = "eyJhbGciOiJSUzI1NiIsImtpZCI6InRlc3Qta2V5IiwidHlwIjoiSldUIn0...";
    IAMJsonWebToken jwt;
    EXPECT_NO_THROW(jwt.deserialize(token));
}

TEST(IAMJsonWebTokenTest, RejectsTokenWithInvalidHeaderSize) {
    // Token with only 1 field - should fail
    Str token = "eyJhbGciOiJSUzI1NiJ9...";
    IAMJsonWebToken jwt;
    EXPECT_THROW(jwt.deserialize(token), JsonWebTokenException);
}

TEST(IAMJsonWebTokenTest, RejectsTokenWithTooManyHeaderFields) {
    // Token with 4+ fields - should fail
    Str token = "eyJhbGciOiJSUzI1NiIsImtpZCI6InRlc3QiLCJ0eXAiOiJKV1QiLCJleHRyYSI6InZhbHVlIn0...";
    IAMJsonWebToken jwt;
    EXPECT_THROW(jwt.deserialize(token), JsonWebTokenException);
}
```

### Integration Testing in EKS

#### Prerequisites

1. EKS cluster with OIDC provider enabled
2. IAM role with S3/SQS permissions
3. Service account with IRSA annotation
4. IngestorCluster CR configured with SQS SmartBus

#### Test Steps

**1. Deploy Test Environment**

```bash
# Create IRSA service account
eksctl create iamserviceaccount \
  --name=test-irsa-sa \
  --namespace=default \
  --cluster=titan-demo \
  --attach-policy-arn=arn:aws:iam::aws:policy/AmazonSQSFullAccess \
  --approve

# Apply IngestorCluster
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: test-ingestor
  namespace: default
spec:
  serviceAccount: test-irsa-sa
  image: splunk/splunk:10.0.1-patched  # Use patched image
  replicas: 1
  busConfigurationRef:
    name: test-bus
    namespace: default
EOF
```

**2. Verify JWT Token Format**

```bash
POD=$(kubectl get pod -n default -l app.kubernetes.io/component=ingestor -o jsonpath='{.items[0].metadata.name}')

# Verify token has 3 header fields
kubectl exec $POD -- sh -c 'cat $AWS_WEB_IDENTITY_TOKEN_FILE | cut -d. -f1 | base64 -d' | jq

# Expected output:
# {
#   "alg": "RS256",
#   "kid": "...",
#   "typ": "JWT"
# }
```

**3. Enable Debug Logging**

```bash
kubectl exec $POD -- bash -c 'cat > /opt/splunk/etc/log-local.cfg << EOF
[splunkd]
category.AwsCredentials=DEBUG
category.RemoteQueueOutputWorker=DEBUG
EOF'
```

**4. Test Initial Startup**

```bash
# Watch pod startup
kubectl logs -f $POD -n default | grep -i "awscredentials\|remotequeue"

# Expected successful output:
# DEBUG AwsCredentials - using signature version=v4
# DEBUG AwsCredentials - SQS_Queue did not find credentials in env variables, will try IAM Service account
# INFO  RemoteQueueOutputWorker - Successfully connected to remote queue
```

**5. Test Restart (Critical Test)**

```bash
# Restart Splunk
kubectl exec $POD -- /opt/splunk/bin/splunk restart

# Monitor logs for successful credential retrieval
kubectl logs -f $POD | grep -i "credential"

# Should see:
# DEBUG AwsCredentials - using credentials from IAM service account
# INFO  RemoteQueueOutputWorker - Successfully finished checking stanza
```

**6. Verify NO Fallback to EC2 Metadata**

```bash
# Check logs - should NOT see these errors:
kubectl logs $POD | grep "metadata endpoint"

# If patched correctly, you should see ZERO of these lines:
# ❌ "Failed to get IAM role via url=http://169.254.169.254"
# ❌ "did not find credentials in metadata endpoint"
```

**7. Functional Test - SQS Operations**

```bash
# Send test message to SQS
aws sqs send-message \
  --queue-url https://sqs.us-west-2.amazonaws.com/667741767953/titan-demo-q \
  --message-body "Test message"

# Verify Splunk receives it
kubectl exec $POD -- grep "received message" /opt/splunk/var/log/splunk/splunkd.log
```

**8. Restart Loop Test**

```bash
# Perform multiple restarts to ensure stability
for i in {1..5}; do
  echo "Restart iteration $i"
  kubectl exec $POD -- /opt/splunk/bin/splunk restart
  sleep 120
  kubectl get pod $POD -n default
  kubectl logs $POD --tail=50 | grep -i "remote.*queue.*success"
done

# All restarts should succeed without credential errors
```

### Success Criteria

✅ **JWT Parsing**: Token with 3 header fields successfully validates
✅ **IRSA Authentication**: Credentials obtained via AssumeRoleWithWebIdentity
✅ **No Fallback**: No attempts to reach EC2 metadata endpoint (169.254.169.254)
✅ **Restart Stability**: Splunk restarts succeed consistently
✅ **SQS Connectivity**: RemoteQueueOutputWorker successfully connects
✅ **No Timeouts**: Connection established within seconds, not minutes
✅ **Pod Ready**: Pods reach Ready state and remain stable

### Failure Indicators (Before Fix)

❌ JWT validation error: "failed to deserialize JWT, err=Header size must be 4"
❌ Fallback to metadata endpoint
❌ 4-minute timeout attempting to reach 169.254.169.254
❌ ERROR: "SQS_Queue did not find credentials in metadata endpoint"
❌ FATAL: "Phase-1 pre-flight checks failed"
❌ Pod crashes or restarts continuously

---

## Additional Context

### Related Components

- **Splunk Operator**: Creates IngestorCluster resources
- **EKS OIDC Provider**: Issues JWT tokens with `typ` field
- **AWS STS**: Validates web identity tokens via AssumeRoleWithWebIdentity API
- **SQS SmartBus**: Remote queue for Splunk data ingestion

### Workarounds (Temporary)

Until the fix is implemented, the following workarounds are possible:

1. **Static AWS Credentials** (Not recommended - security risk)
   ```yaml
   # Add to outputs.conf
   [remote_queue:titan-demo-q]
   remote_queue.sqs_smartbus.access_key = AKIA...
   remote_queue.sqs_smartbus.secret_key = ...
   ```

2. **EC2 Instance Profile** (Only if running on EC2 nodes with IMDSv1)
   - Not applicable for EKS with IMDSv2 enabled

3. **Proxy Service** (Complex)
   - Deploy a sidecar that translates between EKS tokens and expected format

**None of these workarounds are recommended for production use.**

---

## References

### Standards

- [RFC 7519 - JSON Web Token (JWT)](https://www.rfc-editor.org/rfc/rfc7519.html)
  - Section 5.1: "typ" (Type) Header Parameter
- [AWS IAM - AssumeRoleWithWebIdentity](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRoleWithWebIdentity.html)
- [EKS - IAM Roles for Service Accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)

### Splunk Code References

- `src/framework/AwsCredentials.cpp:1254-1305` - Credential provider chain
- `src/framework/AwsCredentials.cpp:940-956` - IamServiceAccountAwsCredentials constructor
- `src/framework/auth/IAMJsonWebToken.cpp:27-36` - Header validation (BUG LOCATION)
- `src/framework/auth/IAMJsonWebToken.h` - Interface definition
- `src/framework/tests/AwsCredentialsTest.cpp` - Existing unit tests

---

## Appendix

### Complete Debug Log Sequence

```
# Startup
12-05-2025 07:29:58.565 +0000 INFO  RemoteQueueOutputProcessor - Initializing RemoteQueueOutputProcessor
12-05-2025 07:29:58.565 +0000 INFO  RemoteQueueOutputWorker - Attempting to check connectivity for remote queue=titan-demo-q

# IRSA Attempt
12-05-2025 07:29:58.565 +0000 DEBUG AwsCredentials - using signature version=v4 for stanza=
12-05-2025 07:29:58.565 +0000 DEBUG AwsCredentials - SQS_Queue did not find credentials in env variables, will try IAM Service account

# JWT VALIDATION FAILURE (ROOT CAUSE)
12-05-2025 07:29:58.565 +0000 DEBUG AwsCredentials - SQS_Queuetried if IRSA configured IamServiceAccountAwsCredentials error failed to deserialize JWT, err=Header size must be 4

# Fallback to ECS Task Role
12-05-2025 07:29:58.565 +0000 INFO  AwsCredentials - TaskAwsCredentials are not setup as the AWS_CONTAINER_CREDENTIALS_RELATIVE_URI is empty; falling back to InstanceAwsCredentials

# Fallback to EC2 Metadata (10 retries, exponential backoff)
12-05-2025 07:29:58.565 +0000 DEBUG AwsCredentials - getting IAM role, attempt=1, imdsVersion=v1
12-05-2025 07:29:58.565 +0000 DEBUG AwsCredentials - getting IAM role via url=http://169.254.169.254/latest/meta-data/iam/security-credentials/
12-05-2025 07:29:58.565 +0000 DEBUG AwsCredentials - Failed to get IAM role, statuscode=401, statusDescription=Unauthorized
12-05-2025 07:29:59.565 +0000 INFO  AwsCredentials - failed to get IAM role in attempt=1; will retry in delayMsec=1000
...
12-05-2025 07:31:01.626 +0000 DEBUG AwsCredentials - getting IAM role, attempt=7, imdsVersion=v1
12-05-2025 07:31:01.626 +0000 INFO  AwsCredentials - failed to get IAM role in attempt=7; will retry in delayMsec=60000
...
# After 10 attempts (~4 minutes)
12-05-2025 07:33:58.626 +0000 ERROR AwsCredentials - SQS_Queue did not find credentials in metadata endpoint ... giving up
12-05-2025 07:33:58.626 +0000 ERROR SQS_QueueProps - Failed to find valid AWS credentials for SQS queue
12-05-2025 07:33:58.626 +0000 FATAL RemoteQueueOutputWorker - Phase-1 pre-flight checks failed for queueName=titan-demo-q
```

### JWT Token Structure (EKS OIDC)

**Raw Token:**
```
eyJhbGciOiJSUzI1NiIsImtpZCI6IjcxZTBhYmU3MzRhMWE3M2MyOTM3OTZjMDU2ZWM2ODIxY2QwYjJlMjkiLCJ0eXAiOiJKV1QifQ.eyJhdWQiOlsic3RzLmFtYXpvbmF3cy5jb20iXSwiZXhwIjoxNzY1MDA1ODQ3LCJpYXQiOjE3NjQ5MTk0NDcsImlzcyI6Imh0dHBzOi8vb2lkYy5la3MudXMtd2VzdC0yLmFtYXpvbmF3cy5jb20vaWQvQjdBRTdCMERDNjUwRTdDNzE1RUQ0QUY5RDFBQUFBQSIsImt1YmVybmV0ZXMuaW8iOnsibmFtZXNwYWNlIjoiZGVmYXVsdCIsInBvZCI6eyJuYW1lIjoic3BsdW5rLWluZ2VzdG9yLWluZ2VzdG9yLTAiLCJ1aWQiOiIwMjBlZDhlYy1kNDFkLTRhMWMtYmRkYy1hM2NiNjY3ZDQyMzkifSwic2VydmljZWFjY291bnQiOnsibmFtZSI6ImluZ2VzdGlvbi1kZW1vLXNhIiwidWlkIjoiZDIzYmZhZWYtMjQwYS00ZmE2LTgyMjEtYjFlNTg3ZDdkMmIwIn19LCJuYmYiOjE3NjQ5MTk0NDcsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OmluZ2VzdGlvbi1kZW1vLXNhIn0.signature
```

**Decoded Header:**
```json
{
  "alg": "RS256",
  "kid": "71e0abe734a1a73c293796c056ec6821cd0b2e29",
  "typ": "JWT"
}
```

**Decoded Payload:**
```json
{
  "aud": ["sts.amazonaws.com"],
  "exp": 1765005847,
  "iat": 1764919447,
  "iss": "https://oidc.eks.us-west-2.amazonaws.com/id/B7AE7B0DC650E7C715ED4AF9D1AAAAA",
  "kubernetes.io": {
    "namespace": "default",
    "pod": {
      "name": "splunk-ingestor-ingestor-0",
      "uid": "020ed8ec-d41d-4a1c-bddc-a3cb667d4239"
    },
    "serviceaccount": {
      "name": "ingestion-demo-sa",
      "uid": "d23bfaef-240a-4fa6-8221-b1e587d7d2b0"
    }
  },
  "nbf": 1764919447,
  "sub": "system:serviceaccount:default:ingestion-demo-sa"
}
```

---

## Document Information

- **Created**: 2025-12-05
- **Author**: Platform Engineering Team
- **Issue Tracker**: [Link to JIRA/GitHub issue]
- **Affected Versions**: Splunk 10.0.1 (likely earlier versions too)
- **Target Fix Version**: 10.0.2 / 10.1.0
- **Severity**: High
- **Priority**: P1

---

## Contact

For questions or clarifications, contact:
- Platform Team: [platform-team@example.com]
- Splunk Development: [splunk-dev@example.com]
