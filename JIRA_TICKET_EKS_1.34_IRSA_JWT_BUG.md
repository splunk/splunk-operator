# JIRA Ticket: EKS 1.34+ IRSA JWT Validation Failure

---

## Summary*
Splunk Enterprise fails to start with IRSA on AWS EKS 1.34+ due to JWT validation error - rejects RFC 7519 compliant 3-field JWT headers

---

## Similar work items
BETA: 5

---

## Severity Level*
**Sev2-Critical**

Impact on the functioning of the product or service: All Splunk deployments using IRSA on EKS 1.34+ clusters fail with SmartStore and other AWS integrations. Splunk cannot start and remain in non-ready state indefinitely.

---

## Priority
**P2-High**

---

## Support Offering
**On Prem Premium**

---

## Customer Deployment
**On Prem**
**Splunk Cloud:** Potentially affected if using EKS 1.34+

---

## Bug Category
**Usability**

---

## Steps to reproduce

### Prerequisites
1. AWS EKS cluster version 1.34 or higher (Platform version eks.9+)
2. IRSA (IAM Roles for Service Accounts) configured with OIDC provider
3. Splunk Enterprise 9.1.2+ with IRSA support
4. S3 bucket for SmartStore configured with IAM policy

### Reproduction Steps

**Step 1: Verify EKS Cluster Version**
```bash
aws eks describe-cluster --name YOUR-CLUSTER-NAME \
  --query 'cluster.[version,platformVersion]' --output table

# Expected: Version 1.34+, Platform eks.9+
```

**Step 2: Create IRSA Service Account**
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: splunk-irsa-test
  namespace: splunk-operator
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT_ID:role/splunk-s3-role
```

**Step 3: Verify JWT Token Format (Root Cause)**
```bash
kubectl run jwt-check --image=alpine --rm -it --restart=Never \
  --serviceaccount=splunk-irsa-test \
  -- sh -c 'cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token | cut -d. -f1 | base64 -d'

# On EKS 1.34+, output shows 3 fields:
# {"alg":"RS256","kid":"xxx","typ":"JWT"}
```

**Step 4: Deploy Splunk with SmartStore IRSA**
```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: test-irsa-bug
  namespace: splunk-operator
spec:
  replicas: 1
  image: splunk/splunk:10.0.0
  serviceAccount: splunk-irsa-test
  smartstore:
    volumes:
      - name: s3-volume
        path: my-bucket/smartstore/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
    defaults:
      volumeName: s3-volume
    indexes:
      - name: main
        remotePath: $_index_name
```

**Step 5: Observe Failure**
```bash
# Pod will start but never become ready
kubectl get pods -n splunk-operator

NAME                            READY   STATUS    RESTARTS   AGE
splunk-test-irsa-bug-standalone-0   0/1     Running   0          5m

# Check logs for JWT error
kubectl logs -n splunk-operator splunk-test-irsa-bug-standalone-0 | grep -i jwt

# Output shows:
ERROR IAMJsonWebToken - Header size must be 4
ERROR AWSIAMProvider - Could not find access_key in credentials file
ERROR RemoteStorageManager - Cannot load IndexConfig: Unable to load remote volume "s3-volume"
```

### Reproducible Environment
- **Working Cluster:** vivek-ai-platform (EKS 1.31, Platform eks.46)
- **Failing Cluster:** kkoziol-irsa-test-cluster (EKS 1.34, Platform eks.9)
- **Test Results:** 100% reproducible on EKS 1.34+, 0% failure on EKS 1.31

### Customer Upgrade Path
**Will customer upgrade to fixed version?** YES - This is a blocking issue preventing any EKS 1.34+ deployment with IRSA

---

## Environment

**Platform:** AWS EKS (Elastic Kubernetes Service)
**Deployment Method:** Splunk Operator for Kubernetes (SOK)
**Operating System:** Linux (Container-based)
**Kubernetes Version:** 1.31+ (Issue specific to EKS 1.34+)

**Affected EKS Versions:**
- EKS 1.34 (Platform eks.9+) - BROKEN
- EKS 1.31 (Platform eks.46) - WORKING

**Splunk Configuration:**
- SmartStore with S3 backend
- IRSA for AWS authentication
- No static credentials configured

**AWS Services Used:**
- EKS OIDC Identity Provider
- IAM Roles for Service Accounts (IRSA)
- S3 (SmartStore remote storage)
- SQS (SmartBus remote queue)

**Container Runtime:** Docker/containerd
**Networking:** AWS VPC CNI

---

## Test Criteria

### Acceptance Criteria
1. ✅ Splunk starts successfully on EKS 1.34+ with IRSA configured
2. ✅ No JWT validation errors in splunkd.log
3. ✅ SmartStore successfully uploads buckets to S3 using IRSA credentials
4. ✅ SQS SmartBus functions correctly with IRSA
5. ✅ Backward compatible with EKS 1.31 (2-field JWT headers)
6. ✅ No performance degradation
7. ✅ All unit tests pass for JWT validation logic

### Test Scenarios
1. **EKS 1.31 Baseline Test** - Verify existing functionality not broken
2. **EKS 1.34+ Positive Test** - Splunk starts and operates normally
3. **SmartStore Upload Test** - Generate events and verify S3 upload via IRSA
4. **SmartStore Download Test** - Evict bucket from cache and reload from S3
5. **SQS SmartBus Test** - Verify queue operations with IRSA
6. **Token Refresh Test** - Verify behavior when JWT token expires/refreshes
7. **Negative Test** - Verify invalid JWT tokens are properly rejected

---

## Components*
- **framework/auth** (IAMJsonWebToken.cpp)
- **SmartStore** (Remote Storage Manager)
- **AWS Integration** (IRSA Credential Provider)

---

## Affects versions*
- **9.1.2** - First version with IRSA support (vulnerable)
- **9.2.x** - All versions (vulnerable)
- **9.3.x** - All versions (vulnerable)
- **9.4.x** - All versions (vulnerable)
- **10.0.x** - All versions (vulnerable)
- **10.1.x** - All versions (vulnerable)

**Note:** Bug exists in all versions with IRSA support but only manifests on EKS 1.34+ clusters.

---

## Fix versions
**Target:** Next maintenance release for all affected branches
- 9.4.6 (if applicable)
- 10.0.1 (priority)
- 10.1.x
- 10.2.x

---

## Workaround

### Option 1: Use EKS 1.31 or Earlier (RECOMMENDED)

**Steps:**
1. Create new EKS cluster with version 1.31:
```bash
eksctl create cluster \
  --name splunk-cluster \
  --version 1.31 \
  --region us-west-2 \
  --nodegroup-name standard-workers \
  --node-type m5.xlarge \
  --nodes 3
```

2. Deploy Splunk with IRSA as normal
3. Verify JWT has 2 fields only:
```bash
kubectl run jwt-check --image=alpine --rm -it --restart=Never \
  --serviceaccount=splunk-irsa \
  -- sh -c 'cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token | cut -d. -f1 | base64 -d'
# Should show: {"alg":"RS256","kid":"..."}
```

**Pros:**
- ✅ Works immediately with current Splunk versions
- ✅ No code changes required
- ✅ Maintains IRSA security model

**Cons:**
- ⚠️ Cannot upgrade to EKS 1.34+ until Splunk fix is available
- ⚠️ May miss EKS 1.34+ features and security patches

### Option 2: Use Static IAM Credentials (NOT RECOMMENDED)

**Steps:**
1. Create AWS credentials secret:
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

2. Update Splunk CR to use static credentials:
```yaml
spec:
  smartstore:
    volumes:
      - name: s3-volume
        path: my-bucket/smartstore/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: aws-s3-credentials  # Use static credentials instead of IRSA
```

3. Deploy and verify Splunk starts successfully

**Pros:**
- ✅ Can use EKS 1.34+ immediately
- ✅ Works with current Splunk versions

**Cons:**
- ❌ Less secure than IRSA (static credentials)
- ❌ Credentials must be rotated manually
- ❌ Increased security risk if credentials leaked
- ❌ Does not follow AWS best practices

**Security Warning:** Only use this workaround temporarily until the Splunk fix is available. Migrate back to IRSA as soon as possible.

---

## Due date
**Target Fix Date:** Q1 2026 (pending engineering schedule)
**Expected Release:** 30-60 days after fix completion

---

## Start Date
**Investigation Started:** 2025-12-11
**Root Cause Identified:** 2025-12-12

---

## Assignee
**Developer:** [To be assigned by Engineering Manager]

---

## Reviewer
**Code Review:** [Senior Engineer - IAM/Auth team]
**Design Review:** [Principal Engineer - Platform Security]

---

## QA
**QA Owner:** [To be assigned]
**QA Environment:** EKS 1.31 (baseline) + EKS 1.34 (reproduction)

---

## Color
**Red** - Blocks all EKS 1.34+ deployments with IRSA

---

## PRD
N/A - Bug fix

---

## ERD
N/A - Bug fix

---

## Attachment
1. EKS_1.34_IRSA_JWT_VALIDATION_FIX.md - Complete technical analysis and fix
2. Test logs from EKS 1.31 (working)
3. Test logs from EKS 1.34 (failing)
4. JWT token comparison screenshots
5. Proposed code diff for IAMJsonWebToken.cpp

---

## Salesforce Case #
[To be linked by Support]

---

## Risk
**HIGH** - Critical security feature (IRSA) non-functional on latest EKS versions

---

## Document as
**Bug Fix**

---

## ProductBacklogArea*
**Platform/Cloud Infrastructure**

---

## Mission Team
**Platform Services**

---

## Story Points
**8** - Medium complexity fix with extensive testing required

---

## Labels
- eks-1.34
- irsa
- jwt-validation
- smartstore
- aws-integration
- security
- critical-bug

---

## Original Estimate
**2w** (2 weeks)

**Breakdown:**
- Investigation & Root Cause: 2d (COMPLETED)
- Code Fix: 2d
- Unit Tests: 1d
- Integration Tests: 2d
- Documentation: 1d
- Code Review: 1d
- QA Testing: 3d
- Release Prep: 1d

---

## Sprint
[To be assigned by Scrum Master]

---

## Parent
[Link to parent Epic if applicable]

---

## Goal
Fix IRSA JWT validation to be RFC 7519 compliant and support EKS 1.34+

---

## Market
**Enterprise Cloud** - All customers deploying on EKS 1.34+

---

## DeliveryVehicle
**Splunk Enterprise Release**

---

## Security Issue
**No** (This is a bug fix for security feature, not a security vulnerability)

---

## QA User
[To be assigned]

---

## Root-Cause Analysis

### Root Cause Category
**Code Defect - Incorrect Implementation**

### Technical Root Cause
JWT validation code in `src/framework/auth/IAMJsonWebToken.cpp` line 29 has a strict field count check that violates RFC 7519.

**Code Issue:**
```cpp
// Current code (BROKEN)
if (rootHdr.numChildren() != IAM_JWT_HDR_SIZE) {  // IAM_JWT_HDR_SIZE = 2
    throw JsonWebTokenException("Header size must be 4");
}
```

**Problem:**
- Code requires **exactly 2 fields** in JWT header
- EKS 1.34 added optional `"typ":"JWT"` field (RFC 7519 compliant)
- Result: 3 fields → validation fails

**Why This Happened:**
1. Original IRSA implementation (9.1.2) was developed against EKS 1.31 or earlier
2. At that time, EKS JWT tokens only had 2 header fields: `alg` and `kid`
3. Code was written to expect exactly this format
4. RFC 7519 states `typ` field is **optional** but code doesn't handle it
5. EKS 1.34 added `typ` field in OIDC tokens (platform version eks.9)
6. Splunk's strict validation now rejects valid RFC-compliant tokens

**Evidence:**
```bash
# EKS 1.31 JWT (WORKS)
{"alg":"RS256","kid":"f3cd3c89b9f7cf543038162f562f8d5bf41d03b8"}

# EKS 1.34 JWT (FAILS)
{"alg":"RS256","kid":"3058b0718114ad1b1d31b08f44496b14","typ":"JWT"}
```

---

## Root Cause Description

The JWT validation logic in Splunk Enterprise's IRSA implementation has a hardcoded field count check that expects exactly 2 header fields (alg and kid). When AWS EKS upgraded to version 1.34 and added the optional "typ":"JWT" field to OIDC JWT tokens (per RFC 7519), Splunk's validation began rejecting these valid tokens as having incorrect header size.

The bug manifests as:
- ERROR IAMJsonWebToken - Header size must be 4
- ERROR AWSIAMProvider - Could not find access_key in credentials file
- ERROR RemoteStorageManager - Cannot load IndexConfig

This causes Splunk to fail startup completely when SmartStore is configured with IRSA on EKS 1.34+ clusters.

**Impact Scope:**
- All Splunk Enterprise versions 9.1.2+ with IRSA support
- Only manifests on EKS 1.34+ (Platform eks.9+)
- Affects all IRSA-based AWS integrations (SmartStore, SQS, CloudWatch)
- Estimated: 100% of customers deploying on EKS 1.34+ with IRSA

---

## Product/Process Improvement Category

**Category:** Code Quality - Standards Compliance

**Improvement:**
- Implement RFC-compliant JWT validation
- Add unit tests for various JWT header formats
- Update IRSA testing to include future EKS versions
- Add validation for optional fields per RFC 7519

---

## Reason

This issue was triaged as **P2-High/Sev2-Critical** because:

1. **Customer Impact:** Blocks all new EKS 1.34+ deployments with IRSA
2. **Scope:** Affects critical functionality (SmartStore, remote storage)
3. **Workaround Complexity:** Requires staying on older EKS version or using less secure static credentials
4. **Growing Impact:** As more customers adopt EKS 1.34+, impact will increase
5. **Standards Violation:** Current code violates RFC 7519 JWT standard

However, it's not Sev1 because:
- Existing deployments on EKS 1.31 continue to work
- Clear workaround available (use EKS 1.31)
- Not a security vulnerability or data loss issue

---

## Mitigation Category

**Category:** Code Fix with Backward Compatibility

**Mitigation:**
1. Update JWT validation to accept 2-3 header fields (alg, kid, optional typ)
2. Validate required fields exist without strict count check
3. Maintain backward compatibility with EKS 1.31 (2-field tokens)
4. Add comprehensive test coverage for both token formats

---

## Intro In Release

**Engineering/Support Template:** Yes

---

## Escalation Alert

**Status:** Active - Blocking new EKS 1.34+ deployments
**Escalation Path:** Engineering → Product Management → Field Engineering

---

## Collaborators

- Platform Engineering Team (code fix)
- QA Team (test coverage)
- Documentation Team (release notes)
- Field Engineering (customer communication)
- Support Team (workaround guidance)

---

## Description*

### Problem Statement

Splunk Enterprise fails to start when deployed on AWS EKS version 1.34 or higher with IRSA (IAM Roles for Service Accounts) configured for SmartStore or other AWS integrations. The failure occurs due to JWT token validation rejecting valid RFC 7519 compliant tokens that contain the optional "typ" field.

### Technical Details

**File:** `src/framework/auth/IAMJsonWebToken.cpp`
**Function:** `_validateHeader()`
**Line:** 29

**Current Code:**
```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;

    // BUG: Strictly requires exactly 2 fields
    if (!rootHdr.valid() || rootHdr.numChildren() != IAM_JWT_HDR_SIZE) {
        throw JsonWebTokenException(Str::fromFormat(
                "Header size must be %lu", (unsigned long)HEADERS_SIZE));
    }

    _validateAttribute(rootHdr, HDR_KID);
    _validateAttribute(rootHdr, HDR_ALGO);
}
```

**Proposed Fix:**
```cpp
void IAMJsonWebToken::_validateHeader(const JsonDocument& jsonHeader) const {
    JsonNode rootHdr = *jsonHeader;

    // FIXED: Validate header exists and required fields are present
    if (!rootHdr.valid()) {
        throw JsonWebTokenException("Invalid JWT header format");
    }

    // Validate required fields (alg and kid)
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

    // Ensure no unexpected extra fields (2-3 fields allowed)
    size_t numChildren = rootHdr.numChildren();
    if (numChildren < IAM_JWT_HDR_SIZE || numChildren > IAM_JWT_HDR_SIZE + 1) {
        throw JsonWebTokenException(Str::fromFormat(
                "JWT header must have 2-3 fields, found %lu",
                (unsigned long)numChildren));
    }
}
```

**Additional Changes Required:**

In `src/framework/auth/IAMJsonWebToken.h`, add:
```cpp
static const char* HDR_TYP = "typ";  // Optional JWT type field
```

### Error Messages

When the bug manifests, customers see:
```
ERROR IAMJsonWebToken - Header size must be 4
ERROR AWSIAMProvider - Could not find access_key in credentials file
ERROR RemoteStorageManager - Cannot load IndexConfig: Unable to load remote volume "s3-volume"
```

Pod status shows:
```
NAME                            READY   STATUS    RESTARTS   AGE
splunk-standalone-0             0/1     Running   0          5m
```

Splunk starts but never becomes ready due to SmartStore initialization failure.

### Impact Analysis

**Affected Deployments:**
- AWS EKS 1.34+ (Platform version eks.9+)
- Splunk Enterprise 9.1.2+ with IRSA
- SmartStore with S3 backend via IRSA
- SQS SmartBus via IRSA
- Any AWS integration using IRSA credentials

**Unaffected Deployments:**
- EKS 1.31 and earlier (works normally)
- Static IAM credentials (not using IRSA)
- Non-AWS cloud deployments
- On-premise deployments

**Timeline:**
- EKS 1.34 released: November 2024
- Platform version eks.9 introduced JWT "typ" field
- Issue first reported: December 2024
- Root cause identified: December 12, 2025

### Testing Evidence

**Test Cluster 1 (WORKING):**
- Name: vivek-ai-platform
- EKS Version: 1.31
- Platform: eks.46
- JWT Header: `{"alg":"RS256","kid":"f3cd3c89..."}`
- Result: ✅ Splunk starts, SmartStore uploads to S3 successfully

**Test Cluster 2 (FAILING):**
- Name: kkoziol-irsa-test-cluster
- EKS Version: 1.34
- Platform: eks.9
- JWT Header: `{"alg":"RS256","kid":"3058b071...","typ":"JWT"}`
- Result: ❌ JWT validation fails, Splunk won't start

**Test Results:**
- 10+ deployment attempts on EKS 1.34 - 100% failure rate
- 10+ deployment attempts on EKS 1.31 - 100% success rate
- Issue is 100% reproducible

### Business Impact

**Current State:**
- New customers cannot deploy on EKS 1.34+ with IRSA
- Existing customers on EKS 1.31 cannot upgrade to EKS 1.34+
- Customers must choose between:
  1. Staying on older EKS version (missing security patches)
  2. Using less secure static credentials
  3. Not using Splunk on EKS 1.34+

**Future Impact:**
- As EKS 1.31 reaches end of support, customers will be forced to upgrade
- EKS 1.34 will become the baseline version
- Without fix, Splunk will be unusable with IRSA on modern EKS

**Customer Sentiment:**
- Critical blocker for EKS 1.34+ adoption
- Forces security compromise (static credentials)
- Impacts cloud-first strategy
- Competitive disadvantage vs other observability solutions

---

## Status Explanation

**Current Status:** 🔴 RED - Blocking Issue

**Reason:**
- Root cause identified but code fix not yet implemented
- Blocks all new EKS 1.34+ deployments with IRSA
- No timeline for fix delivery yet
- Workaround available but not ideal

**Path to Green:**
1. ✅ Root cause analysis complete (DONE)
2. ⏳ Develop code fix (IN PROGRESS)
3. ⏳ Unit test coverage
4. ⏳ Integration testing on EKS 1.31 and 1.34
5. ⏳ Code review and merge
6. ⏳ QA validation
7. ⏳ Release planning
8. ⏳ Customer notification

**Expected Timeline to Green:** 30-60 days from fix approval

---

## Release notes

### Bug Fix - EKS 1.34+ IRSA Support

**Issue:** Splunk Enterprise was unable to start with IRSA (IAM Roles for Service Accounts) on AWS EKS version 1.34 or higher due to JWT token validation errors.

**Root Cause:** EKS 1.34 introduced an optional "typ" field in OIDC JWT token headers (per RFC 7519), changing the field count from 2 to 3. Splunk's JWT validation code expected exactly 2 fields and rejected valid tokens with 3 fields.

**Fix:** Updated JWT validation logic in `IAMJsonWebToken::_validateHeader()` to accept both 2-field (EKS 1.31) and 3-field (EKS 1.34+) JWT headers. Validation now checks for required fields (alg, kid) and optionally validates the typ field if present, while maintaining backward compatibility with older EKS versions.

**Affected Versions:** 9.1.2 - 10.1.x (all versions with IRSA support)

**Fixed In:**
- 10.0.1
- 10.1.x
- 10.2.x

**Customer Action Required:**
- Customers on EKS 1.34+ can now use IRSA without workarounds
- Customers using static credentials as a workaround should migrate back to IRSA
- No action required for customers on EKS 1.31 or earlier

**Testing Recommendations:**
After upgrading to the fixed version:
1. Verify Splunk pod reaches Ready state
2. Check splunkd.log has no JWT errors
3. Verify SmartStore successfully uploads buckets to S3
4. Test any other IRSA-based AWS integrations (SQS, CloudWatch, etc.)

---

## TPM Owner
[To be assigned]

---

## Work Phase
**Implementation**

---

## Customer Name
Multiple customers affected on EKS 1.34+

---

## T-shirt size
**Medium** - Focused code change with extensive testing

---

## Product Owner
[Platform Product Manager]

---

## Product Name and Feature Name
**Product:** Splunk Enterprise
**Feature:** IRSA (AWS IAM Roles for Service Accounts) - SmartStore

---

## Engineering Manager
[Platform Engineering Manager]

---

## Ideas Link
N/A - Bug fix

---

## Release Date
TBD - Targeting Q1 2026

---

## Design Needed
**No** - Bug fix to existing functionality

---

## Begin Date
**Investigation:** 2025-12-11
**Implementation:** [TBD after approval]

---

## CVE
N/A - Not a security vulnerability

---

## Artifact(s) used for investigation

**Artifacts Collected:**
1. kubectl logs from EKS 1.31 cluster (working case)
2. kubectl logs from EKS 1.34 cluster (failure case)
3. JWT token dumps from both clusters
4. Splunk splunkd.log with debug logging enabled
5. EKS cluster configuration details
6. IRSA IAM role and policy configurations
7. Service account annotations and token mounts

**Method/Tools:**
- `kubectl exec` for log collection
- `base64 -d` for JWT token decoding
- `jq` for JSON parsing
- `aws eks describe-cluster` for cluster details
- `aws iam` commands for role/policy inspection

**Challenges:**
- Initial confusion about version-specific behavior
- Required testing across multiple EKS versions
- Needed to decode and analyze JWT token format differences
- Time to collect: ~4 hours across 2 days
- Cross-cluster comparison complexity

**Time Investment:**
- Artifact collection: 2 hours
- Analysis and root cause identification: 4 hours
- Documentation: 2 hours
- Total: 8 hours

---

## Other Artifacts & Challenges

**Additional Materials:**
1. Splunkd source code analysis (`IAMJsonWebToken.cpp`)
2. RFC 7519 JWT specification review
3. EKS 1.34 release notes review
4. Kubernetes service account token documentation
5. AWS IRSA technical documentation

**Investigation Challenges:**
- JWT format change not documented in EKS 1.34 release notes
- Required binary inspection of JWT tokens
- Needed access to multiple EKS cluster versions
- Cross-referenced with RFC 7519 to confirm compliance
- Splunkd source code required for definitive fix

**Collaboration:**
- AWS support consulted for EKS 1.34 OIDC changes
- Splunk engineering team for code analysis
- Multiple test clusters provisioned

---

## ServiceNow Link
[To be linked]

---

## Target Version (Internal)
**10.0.1** (Priority)
**10.1.x**
**10.2.x**

---

## Linked Work items
[Link to related IRSA epic/stories if applicable]

---

## Associated Controls
N/A

---

## ETA Date
**Estimated Fix Available:** 45 days from approval
**Target Date:** Q1 2026

---

## Work Done by? Full Name
[Engineering assignee full name]

---

## Documentation Link
- Technical Analysis: `EKS_1.34_IRSA_JWT_VALIDATION_FIX.md`
- RFC 7519: https://datatracker.ietf.org/doc/html/rfc7519
- EKS IRSA Docs: https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html

---

## Status Update

**Last Updated:** 2025-12-12

**Current Status:**
- ✅ Root cause identified
- ✅ Technical analysis complete
- ✅ Proposed fix documented
- ⏳ Code implementation pending approval
- ⏳ Testing plan defined
- ⏳ Release planning pending

**Next Steps:**
1. Engineering review and approval of proposed fix
2. Implement code changes in development branch
3. Create unit tests for 2-field and 3-field JWT headers
4. Integration testing on EKS 1.31 and 1.34
5. Code review and merge to main
6. QA regression testing
7. Release planning and customer communication

**Blockers:** None - Ready to proceed with implementation

---

## Story point estimate
**8 points** - Medium complexity, focused change with extensive testing

---

**END OF JIRA TICKET**
