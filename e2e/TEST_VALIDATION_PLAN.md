# E2E Test Validation Plan

This document tracks the validation of all E2E test specs before they go into CI/CD.

## Test Inventory

| Spec File | Tests | Tags | Complexity | Status |
|-----------|-------|------|------------|--------|
| `simple_smoke.yaml` | 3 | simple-smoke, s1 | Low | ⏳ Pending |
| `smoke_fast.yaml` | 12 | smoke, fast, s1 | Low | ⏳ Pending |
| `smoke.yaml` | 21 | smoke, s1 | Medium | ⏳ Pending |
| `ingest_search.yaml` | 22 | ingest, search, s1 | Medium | ⏳ Pending |
| `delete_cr.yaml` | 8 | deletecr, s1 | Low | ⏳ Pending |
| `smartstore.yaml` | 42 | smartstore, s1 | High | ⏳ Pending |
| `license_manager.yaml` | 39 | licensemanager | Medium | ⏳ Pending |
| `license_master.yaml` | 39 | licensemaster | Medium | ⏳ Pending |
| `secret.yaml` | 119 | secret, s1 | High | ⏳ Pending |
| `secret_advanced.yaml` | 59 | secret, license | High | ⏳ Pending |
| `appframework.yaml` | 69 | appframework, smoke | High | ⏳ Pending |
| `appframework_cloud.yaml` | 58 | appframework, s3 | High | ⏳ Pending |
| `index_and_ingestion_separation.yaml` | 61 | indingsep, smoke | High | ⏳ Pending |
| `custom_resource_crud.yaml` | 84 | crcrud | High | ⏳ Pending |
| `monitoring_console.yaml` | 149 | monitoringconsole | Very High | ⏳ Pending |
| `monitoring_console_advanced.yaml` | 60 | monitoring_console, c3 | High | ⏳ Pending |
| `resilience_and_performance.yaml` | 79 | performance, c3 | Very High | ⏳ Pending |

**Total Tests: 924 across 17 spec files**

## Validation Phases

### Phase 1: Quick Smoke Tests (Priority 1) ⏳

**Goal:** Validate basic framework functionality

**Tests to run:**
1. ✅ `simple_smoke.yaml` (3 tests) - Absolute basics
2. ⏳ `smoke_fast.yaml` (12 tests) - Fast smoke tests
3. ⏳ `smoke.yaml` (21 tests) - Full smoke suite

**Prerequisites:**
- EKS cluster running
- Splunk Operator deployed
- AWS S3 access (for some tests)

**Estimated Duration:** 30-45 minutes

**Success Criteria:**
- All 36 smoke tests pass
- No framework errors
- Artifacts generated correctly
- PlantUML diagrams created

### Phase 2: Core Functionality (Priority 2) ⏳

**Goal:** Validate core operator features

**Tests to run:**
4. ⏳ `ingest_search.yaml` (22 tests) - Data ingestion and search
5. ⏳ `delete_cr.yaml` (8 tests) - CR deletion
6. ⏳ `license_manager.yaml` (39 tests) - License Manager topology
7. ⏳ `license_master.yaml` (39 tests) - License Master topology

**Estimated Duration:** 2-3 hours

**Success Criteria:**
- Core CRUD operations work
- License management functional
- Data ingestion verified

### Phase 3: Advanced Features (Priority 3) ⏳

**Goal:** Validate complex topologies and features

**Tests to run:**
8. ⏳ `smartstore.yaml` (42 tests) - S3 remote storage
9. ⏳ `secret.yaml` (119 tests) - Secret management
10. ⏳ `appframework.yaml` (69 tests) - App deployment
11. ⏳ `monitoring_console.yaml` (149 tests) - MC topology

**Estimated Duration:** 4-6 hours

**Success Criteria:**
- SmartStore S3 integration works
- Secret rotation functional
- App Framework deploys apps correctly
- Monitoring Console peer management works

### Phase 4: Full Validation (Priority 4) ⏳

**Goal:** Validate all remaining tests

**Tests to run:**
12. ⏳ `secret_advanced.yaml` (59 tests)
13. ⏳ `appframework_cloud.yaml` (58 tests)
14. ⏳ `index_and_ingestion_separation.yaml` (61 tests)
15. ⏳ `custom_resource_crud.yaml` (84 tests)
16. ⏳ `monitoring_console_advanced.yaml` (60 tests)
17. ⏳ `resilience_and_performance.yaml` (79 tests)

**Estimated Duration:** 8-12 hours

**Success Criteria:**
- All 924 tests analyzed
- Common patterns identified
- Issues documented

## Test Execution Plan

### Step 1: Environment Setup

```bash
# 1. Ensure you have an EKS cluster
aws eks update-kubeconfig --name <cluster-name> --region us-west-2

# 2. Verify cluster access
kubectl get nodes

# 3. Deploy Splunk Operator
kubectl create namespace splunk-operator
helm install splunk-operator splunk-operator/splunk-operator \
  --namespace splunk-operator

# 4. Verify operator is running
kubectl get pods -n splunk-operator

# 5. Set up environment variables
export E2E_KUBECONFIG=$HOME/.kube/config
export E2E_CLUSTER_PROVIDER=eks
export E2E_OPERATOR_NAMESPACE=splunk-operator
export E2E_SPLUNK_IMAGE="splunk/splunk:9.2.1"
export E2E_TEST_BUCKET="<your-s3-bucket>"
export E2E_S3_REGION="us-west-2"
export E2E_ARTIFACTS_DIR="./e2e-artifacts"
export E2E_LOG_LEVEL="debug"

# 6. Build E2E runner
make e2e-build
```

### Step 2: Run Tests Systematically

```bash
# Create results directory
mkdir -p test-validation-results

# Phase 1: Smoke Tests
echo "=== Phase 1: Simple Smoke ===" | tee -a validation.log
./bin/e2e-runner \
  -cluster-provider eks \
  -operator-namespace splunk-operator \
  -artifact-dir test-validation-results/simple-smoke \
  -log-level debug \
  e2e/specs/operator/simple_smoke.yaml 2>&1 | tee -a validation.log

# Check results
cat test-validation-results/simple-smoke/summary.json

# If successful, continue to smoke_fast
echo "=== Phase 1: Smoke Fast ===" | tee -a validation.log
./bin/e2e-runner \
  -cluster-provider eks \
  -operator-namespace splunk-operator \
  -artifact-dir test-validation-results/smoke-fast \
  -log-level debug \
  e2e/specs/operator/smoke_fast.yaml 2>&1 | tee -a validation.log

# Check results
cat test-validation-results/smoke-fast/summary.json

# Continue pattern for all specs...
```

### Step 3: Automated Validation Script

Create `e2e/scripts/validate-all-tests.sh`:

```bash
#!/bin/bash
set -e

SPECS=(
  "simple_smoke.yaml:simple-smoke"
  "smoke_fast.yaml:smoke-fast"
  "smoke.yaml:smoke"
  "ingest_search.yaml:ingest-search"
  "delete_cr.yaml:delete-cr"
  "license_manager.yaml:license-manager"
  "license_master.yaml:license-master"
  "smartstore.yaml:smartstore"
  "secret.yaml:secret"
  "appframework.yaml:appframework"
  "monitoring_console.yaml:monitoring-console"
)

RESULTS_DIR="test-validation-results"
mkdir -p ${RESULTS_DIR}

echo "Starting E2E Test Validation"
echo "=============================="
echo ""

for spec_entry in "${SPECS[@]}"; do
  IFS=':' read -r spec_file spec_name <<< "$spec_entry"

  echo "Running: $spec_name"
  echo "Spec: e2e/specs/operator/$spec_file"
  echo ""

  ./bin/e2e-runner \
    -cluster-provider eks \
    -operator-namespace splunk-operator \
    -artifact-dir ${RESULTS_DIR}/${spec_name} \
    -log-level info \
    e2e/specs/operator/${spec_file} 2>&1 | tee ${RESULTS_DIR}/${spec_name}.log

  # Check results
  if [ -f "${RESULTS_DIR}/${spec_name}/summary.json" ]; then
    PASSED=$(jq -r '.passed // 0' ${RESULTS_DIR}/${spec_name}/summary.json)
    FAILED=$(jq -r '.failed // 0' ${RESULTS_DIR}/${spec_name}/summary.json)
    TOTAL=$(jq -r '.total // 0' ${RESULTS_DIR}/${spec_name}/summary.json)

    echo "Results: $PASSED passed, $FAILED failed out of $TOTAL"

    if [ "$FAILED" != "0" ]; then
      echo "⚠️  FAILURES DETECTED in $spec_name"
      echo "Failed tests:"
      if [ -f "${RESULTS_DIR}/${spec_name}/results.json" ]; then
        jq -r '.tests[] | select(.status=="failed") | "  - \(.name): \(.error)"' \
          ${RESULTS_DIR}/${spec_name}/results.json
      else
        echo "  - No results.json available"
      fi
    else
      echo "✅ All tests passed for $spec_name"
    fi
  else
    echo "❌ No summary.json found for $spec_name"
  fi

  echo ""
  echo "---"
  echo ""

  # Brief pause between specs
  sleep 5
done

echo "Validation Complete!"
echo ""
echo "Generating summary report..."

# Generate summary
cat > ${RESULTS_DIR}/SUMMARY.md <<EOF
# E2E Test Validation Summary

Generated: $(date)

## Results by Spec

| Spec | Total | Passed | Failed | Duration |
|------|-------|--------|--------|----------|
EOF

for spec_entry in "${SPECS[@]}"; do
  IFS=':' read -r spec_file spec_name <<< "$spec_entry"

  if [ -f "${RESULTS_DIR}/${spec_name}/summary.json" ]; then
    TOTAL=$(jq -r '.total // 0' ${RESULTS_DIR}/${spec_name}/summary.json)
    PASSED=$(jq -r '.passed // 0' ${RESULTS_DIR}/${spec_name}/summary.json)
    FAILED=$(jq -r '.failed // 0' ${RESULTS_DIR}/${spec_name}/summary.json)
    if [ -f "${RESULTS_DIR}/${spec_name}/results.json" ]; then
      DURATION=$(jq -r '.duration // "N/A"' ${RESULTS_DIR}/${spec_name}/results.json)
    else
      DURATION="N/A"
    fi

    STATUS="✅"
    if [ "$FAILED" != "0" ]; then
      STATUS="❌"
    fi

    echo "| $STATUS $spec_name | $TOTAL | $PASSED | $FAILED | $DURATION |" >> ${RESULTS_DIR}/SUMMARY.md
  fi
done

cat ${RESULTS_DIR}/SUMMARY.md
```

## Analysis Checklist

For each test spec, verify:

### ✅ Test Structure
- [ ] Metadata is complete (name, description, tags)
- [ ] Topology is correctly specified
- [ ] Tests are properly named and organized

### ✅ Action Correctness
- [ ] All actions exist in registry
- [ ] Parameters are valid
- [ ] Output/input chaining works
- [ ] Assertions are correct

### ✅ Timing and Waits
- [ ] Appropriate timeouts set
- [ ] Wait conditions are correct
- [ ] No race conditions

### ✅ Resource Management
- [ ] Resources are created properly
- [ ] Cleanup happens (implicit via topology)
- [ ] No resource leaks

### ✅ Data Validation
- [ ] Search results are validated
- [ ] Data integrity checks pass
- [ ] Expected outputs match

## Common Issues to Look For

### Issue 1: Missing Actions

**Symptom:** `unknown action: xyz`

**Solution:** Check `e2e/framework/steps/` for action implementation

### Issue 2: Timeout Issues

**Symptom:** Tests fail with "context deadline exceeded"

**Solution:** Increase timeouts in spec or action

### Issue 3: Variable Resolution

**Symptom:** `${variable}` not resolved

**Solution:** Ensure output name matches reference

### Issue 4: Topology Not Ready

**Symptom:** Tests fail immediately with "pod not found"

**Solution:** Ensure `topology.wait_ready` is used

### Issue 5: S3 Permissions

**Symptom:** AppFramework or SmartStore tests fail

**Solution:** Verify AWS credentials and S3 bucket access

## Test Results Template

For each test run, record:

```markdown
## Test: <spec-name>

**Date:** 2026-01-20
**Cluster:** eks-test-cluster
**Operator Version:** 3.0.0
**Splunk Version:** 9.2.1

### Results
- Total Tests: X
- Passed: X
- Failed: X
- Skipped: X
- Duration: Xm Xs

### Issues Found
1. Issue description
   - Impact: High/Medium/Low
   - Fix: Description

### Artifacts
- summary.json: [link]
- results.json: [link]
- PlantUML diagrams: [link]
- Pod logs: [link]

### Notes
- Any observations
- Performance notes
- Recommendations
```

## Priority Order

Run tests in this order to catch issues early:

1. **simple_smoke** - Validate basic framework (5 min)
2. **smoke_fast** - Validate common actions (15 min)
3. **smoke** - Full smoke validation (30 min)
4. **ingest_search** - Validate data flow (20 min)
5. **delete_cr** - Validate cleanup (10 min)

Then proceed with remaining tests based on priority.

## Success Criteria

Before moving to CI/CD:

- [ ] All Phase 1 tests pass (100% success rate)
- [ ] Phase 2 tests have >95% pass rate
- [ ] Phase 3 tests have >90% pass rate
- [ ] All critical issues fixed
- [ ] Documentation updated with known issues
- [ ] Performance is acceptable (<10 min per test on average)

## Next Steps

After validation:
1. Update pipeline with validated test suites
2. Document any test-specific requirements
3. Create issue tickets for any failures
4. Update ARCHITECTURE.md with findings
5. Prepare PR with test framework

---

*Last Updated: January 2026*
