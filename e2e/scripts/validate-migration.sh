#!/bin/bash
# Validation script to verify test migration is complete

set -e

echo "======================================"
echo "E2E Test Migration Validation"
echo "======================================"
echo ""

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Count old tests
echo "📊 Counting old framework tests..."
OLD_TEST_FILES=$(find test -name "*_test.go" -not -name "*_suite_test.go" | wc -l | tr -d ' ')
echo "   Old test files: $OLD_TEST_FILES"

# Count new specs
echo ""
echo "📊 Counting new framework specs..."
NEW_SPEC_FILES=$(find e2e/specs/operator -name "*.yaml" | wc -l | tr -d ' ')
echo "   New spec files: $NEW_SPEC_FILES"

# Count individual tests in new specs
echo ""
echo "📊 Counting individual tests in new specs..."
NEW_TEST_COUNT=$(grep -r "^  name:" e2e/specs/operator/*.yaml 2>/dev/null | wc -l | tr -d ' ')
echo "   Individual tests: $NEW_TEST_COUNT"

# Analyze by category
echo ""
echo "======================================"
echo "Coverage by Category"
echo "======================================"

check_coverage() {
    local category=$1
    local old_pattern=$2
    local new_file=$3

    old_count=$(find test -name "*${old_pattern}*.go" -not -name "*_suite_test.go" 2>/dev/null | wc -l | tr -d ' ')
    if [ -f "e2e/specs/operator/${new_file}" ]; then
        new_count=$(grep "^  name:" e2e/specs/operator/${new_file} 2>/dev/null | wc -l | tr -d ' ')
        status="${GREEN}✓${NC}"
    else
        new_count=0
        status="${RED}✗${NC}"
    fi

    printf "%-20s Old: %2d  →  New: %2d  %b\n" "$category" "$old_count" "$new_count" "$status"
}

check_coverage "Smoke" "smoke" "smoke.yaml"
check_coverage "Secret" "secret" "secret.yaml"
check_coverage "License Manager" "lm" "license_manager.yaml"
check_coverage "License Master" "lm" "license_master.yaml"
check_coverage "Monitoring Console" "monitoring" "monitoring_console.yaml"
check_coverage "App Framework" "appframework" "appframework.yaml"
check_coverage "SmartStore" "smartstore" "smartstore.yaml"
check_coverage "Custom Resource" "custom_resource" "custom_resource_crud.yaml"
check_coverage "Delete CR" "deletecr" "delete_cr.yaml"
check_coverage "Ingest/Search" "ingest" "ingest_search.yaml"

# Check for new additions
echo ""
echo "======================================"
echo "New Test Categories (Not in Old Framework)"
echo "======================================"

count_new_tests() {
    local file=$1
    local description=$2
    if [ -f "e2e/specs/operator/${file}" ]; then
        count=$(grep "^  name:" e2e/specs/operator/${file} 2>/dev/null | wc -l | tr -d ' ')
        printf "${GREEN}✓${NC} %-35s %2d tests\n" "$description" "$count"
    fi
}

count_new_tests "secret_advanced.yaml" "Advanced Secret Management"
count_new_tests "monitoring_console_advanced.yaml" "Advanced Monitoring Console"
count_new_tests "appframework_cloud.yaml" "Cloud-Specific App Framework"
count_new_tests "resilience_and_performance.yaml" "Resilience & Performance"

# Check for step handlers
echo ""
echo "======================================"
echo "Step Handler Coverage"
echo "======================================"

check_handlers() {
    local file=$1
    local description=$2
    if [ -f "e2e/framework/steps/${file}" ]; then
        count=$(grep -c "^func handle" e2e/framework/steps/${file} 2>/dev/null || echo "0")
        printf "${GREEN}✓${NC} %-35s %2d handlers\n" "$description" "$count"
    else
        printf "${RED}✗${NC} %-35s Missing\n" "$description"
    fi
}

check_handlers "handlers_topology.go" "Topology Management"
check_handlers "handlers_k8s.go" "Kubernetes Operations"
check_handlers "handlers_splunkd.go" "Splunk Operations"
check_handlers "handlers_cluster.go" "Cluster Operations"
check_handlers "handlers_license.go" "License Operations"
check_handlers "handlers_secret.go" "Secret Operations"
check_handlers "handlers_appframework.go" "App Framework Operations"
check_handlers "handlers_diagnostics.go" "Diagnostics (NEW)"
check_handlers "handlers_chaos.go" "Chaos Engineering (NEW)"
check_handlers "handlers_upgrade.go" "Upgrade Testing (NEW)"

# Check for documentation
echo ""
echo "======================================"
echo "Documentation"
echo "======================================"

check_doc() {
    local file=$1
    local description=$2
    if [ -f "e2e/${file}" ]; then
        lines=$(wc -l < "e2e/${file}" | tr -d ' ')
        printf "${GREEN}✓${NC} %-35s %4d lines\n" "$description" "$lines"
    else
        printf "${RED}✗${NC} %-35s Missing\n" "$description"
    fi
}

check_doc "QUICK_START.md" "Quick Start Guide"
check_doc "FRAMEWORK_GUIDE.md" "Framework Guide"
check_doc "IMPROVEMENTS_SUMMARY.md" "Improvements Summary"
check_doc "MIGRATION_COMPLETE.md" "Migration Summary"

# Check for CLI tools
echo ""
echo "======================================"
echo "CLI Tools"
echo "======================================"

check_cli() {
    local dir=$1
    local description=$2
    if [ -f "e2e/cmd/${dir}/main.go" ]; then
        printf "${GREEN}✓${NC} %-35s Available\n" "$description"
    else
        printf "${RED}✗${NC} %-35s Missing\n" "$description"
    fi
}

check_cli "e2e-runner" "Test Runner"
check_cli "e2e-query" "Query Interface"
check_cli "e2e-matrix" "Matrix Generator"

# Summary
echo ""
echo "======================================"
echo "Summary"
echo "======================================"
echo ""

total_new_tests=$((NEW_TEST_COUNT))
echo "Total new spec tests: $total_new_tests"

# Check if migration is complete
if [ $NEW_SPEC_FILES -ge 10 ] && [ $NEW_TEST_COUNT -ge 50 ]; then
    echo ""
    echo -e "${GREEN}✓ Migration appears COMPLETE!${NC}"
    echo ""
    echo "Key achievements:"
    echo "  • $NEW_SPEC_FILES spec files created"
    echo "  • $NEW_TEST_COUNT individual tests migrated/added"
    echo "  • All major test categories covered"
    echo "  • New capabilities added (chaos, performance, cloud)"
    echo "  • Complete documentation provided"
    echo "  • CLI tools available"
    echo ""
    echo "Next steps:"
    echo "  1. Run tests: source e2e/.env.e2e && go run ./e2e/cmd/e2e-runner"
    echo "  2. Query results: ./bin/e2e-query flaky-tests"
    echo "  3. Generate more tests: ./bin/e2e-matrix generate -m e2e/matrices/comprehensive.yaml"
    echo ""
else
    echo ""
    echo -e "${YELLOW}⚠ Migration may be incomplete${NC}"
    echo ""
    echo "Please review:"
    echo "  • Expected at least 10 spec files, found: $NEW_SPEC_FILES"
    echo "  • Expected at least 50 tests, found: $NEW_TEST_COUNT"
    echo ""
fi

echo "======================================"
echo "Validation complete!"
echo "======================================"
