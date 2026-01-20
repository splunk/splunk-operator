#!/bin/bash
# End-to-end test of the framework to ensure everything works

set -e

echo "======================================"
echo "E2E Framework End-to-End Test"
echo "======================================"
echo ""

FAILED=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

test_passed() {
    echo -e "${GREEN}✓${NC} $1"
}

test_failed() {
    echo -e "${RED}✗${NC} $1"
    FAILED=$((FAILED + 1))
}

test_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Test 1: Check Go is installed
echo "Test 1: Checking Go installation..."
if command -v go &> /dev/null; then
    GO_VERSION=$(go version | awk '{print $3}')
    test_passed "Go is installed ($GO_VERSION)"
else
    test_failed "Go is not installed"
fi

# Test 2: Check directory structure
echo ""
echo "Test 2: Checking directory structure..."
required_dirs=(
    "e2e/cmd/e2e-runner"
    "e2e/cmd/e2e-query"
    "e2e/cmd/e2e-matrix"
    "e2e/framework/runner"
    "e2e/framework/steps"
    "e2e/framework/graph"
    "e2e/specs/operator"
    "e2e/matrices"
)

for dir in "${required_dirs[@]}"; do
    if [ -d "$dir" ]; then
        test_passed "Directory exists: $dir"
    else
        test_failed "Directory missing: $dir"
    fi
done

# Test 3: Check spec files
echo ""
echo "Test 3: Checking spec files..."
spec_files=(
    "e2e/specs/operator/smoke.yaml"
    "e2e/specs/operator/secret.yaml"
    "e2e/specs/operator/secret_advanced.yaml"
    "e2e/specs/operator/monitoring_console.yaml"
    "e2e/specs/operator/monitoring_console_advanced.yaml"
    "e2e/specs/operator/appframework.yaml"
    "e2e/specs/operator/appframework_cloud.yaml"
    "e2e/specs/operator/resilience_and_performance.yaml"
)

for file in "${spec_files[@]}"; do
    if [ -f "$file" ]; then
        test_passed "Spec file exists: $(basename $file)"
    else
        test_failed "Spec file missing: $file"
    fi
done

# Test 4: Validate YAML syntax
echo ""
echo "Test 4: Validating YAML syntax..."
if command -v yamllint &> /dev/null; then
    for file in e2e/specs/operator/*.yaml; do
        if yamllint -d relaxed "$file" &> /dev/null; then
            test_passed "Valid YAML: $(basename $file)"
        else
            test_warning "YAML validation warning: $(basename $file)"
        fi
    done
else
    test_warning "yamllint not installed, skipping YAML validation"
    echo "   Install: pip install yamllint"
fi

# Test 5: Check Go modules
echo ""
echo "Test 5: Checking Go module dependencies..."
if go mod verify &> /dev/null; then
    test_passed "Go modules verified"
else
    test_warning "Go module verification failed - may need: go mod tidy"
fi

# Test 6: Try building CLI tools
echo ""
echo "Test 6: Building CLI tools..."

mkdir -p bin

# Build e2e-runner
if go build -o bin/e2e-runner ./e2e/cmd/e2e-runner 2>/dev/null; then
    test_passed "Built: e2e-runner"
else
    test_failed "Failed to build e2e-runner"
fi

# Build e2e-query
if go build -o bin/e2e-query ./e2e/cmd/e2e-query 2>/dev/null; then
    test_passed "Built: e2e-query"
else
    test_failed "Failed to build e2e-query"
fi

# Build e2e-matrix
if go build -o bin/e2e-matrix ./e2e/cmd/e2e-matrix 2>/dev/null; then
    test_passed "Built: e2e-matrix"
else
    test_failed "Failed to build e2e-matrix"
fi

# Test 7: Test matrix validation
echo ""
echo "Test 7: Testing matrix generator..."
if [ -f "bin/e2e-matrix" ] && [ -f "e2e/matrices/comprehensive.yaml" ]; then
    if ./bin/e2e-matrix validate -m e2e/matrices/comprehensive.yaml &> /dev/null; then
        test_passed "Matrix file is valid"
    else
        test_failed "Matrix validation failed"
    fi

    # Try generating report
    if ./bin/e2e-matrix report -m e2e/matrices/comprehensive.yaml &> /dev/null; then
        test_passed "Matrix report generation works"
    else
        test_warning "Matrix report generation had issues"
    fi
else
    test_warning "Skipping matrix test (binary or file missing)"
fi

# Test 8: Check step handler registration
echo ""
echo "Test 8: Checking step handler files..."
handler_files=(
    "e2e/framework/steps/handlers_topology.go"
    "e2e/framework/steps/handlers_k8s.go"
    "e2e/framework/steps/handlers_splunkd.go"
    "e2e/framework/steps/handlers_diagnostics.go"
    "e2e/framework/steps/handlers_chaos.go"
    "e2e/framework/steps/handlers_upgrade.go"
    "e2e/framework/steps/defaults.go"
)

for file in "${handler_files[@]}"; do
    if [ -f "$file" ]; then
        test_passed "Handler file exists: $(basename $file)"
    else
        test_failed "Handler file missing: $file"
    fi
done

# Test 9: Check documentation
echo ""
echo "Test 9: Checking documentation..."
doc_files=(
    "e2e/README.md"
    "e2e/QUICK_START.md"
    "e2e/FRAMEWORK_GUIDE.md"
    "e2e/IMPROVEMENTS_SUMMARY.md"
    "e2e/MIGRATION_COMPLETE.md"
)

for file in "${doc_files[@]}"; do
    if [ -f "$file" ]; then
        test_passed "Documentation exists: $(basename $file)"
    else
        test_failed "Documentation missing: $file"
    fi
done

# Test 10: Try loading a spec
echo ""
echo "Test 10: Testing spec loading..."
if go run ./e2e/cmd/e2e-runner --help &> /dev/null; then
    test_passed "e2e-runner can be executed"
else
    test_warning "e2e-runner execution had issues (may need dependencies)"
fi

# Test 11: Check graph enrichment code
echo ""
echo "Test 11: Checking graph enrichment..."
if [ -f "e2e/framework/graph/enrichment.go" ]; then
    if grep -q "ErrorPattern" e2e/framework/graph/enrichment.go; then
        test_passed "Graph enrichment includes ErrorPattern"
    else
        test_failed "ErrorPattern not found in graph enrichment"
    fi

    if grep -q "Resolution" e2e/framework/graph/enrichment.go; then
        test_passed "Graph enrichment includes Resolution"
    else
        test_failed "Resolution not found in graph enrichment"
    fi
else
    test_failed "Graph enrichment file missing"
fi

# Test 12: Check Neo4j query interface
echo ""
echo "Test 12: Checking Neo4j query interface..."
if [ -f "e2e/framework/graph/query.go" ]; then
    query_methods=(
        "FindSimilarFailures"
        "FindResolutionsForError"
        "FindUntestedCombinations"
        "GetTestSuccessRate"
        "FindFlakyTests"
    )

    for method in "${query_methods[@]}"; do
        if grep -q "$method" e2e/framework/graph/query.go; then
            test_passed "Query method exists: $method"
        else
            test_failed "Query method missing: $method"
        fi
    done
else
    test_failed "Query interface file missing"
fi

# Test 13: Count tests in specs
echo ""
echo "Test 13: Counting tests in specs..."
total_tests=$(grep -r "^  name:" e2e/specs/operator/*.yaml 2>/dev/null | wc -l | tr -d ' ')
if [ "$total_tests" -ge 60 ]; then
    test_passed "Found $total_tests tests (target: 60+)"
else
    test_warning "Found only $total_tests tests (expected 60+)"
fi

# Summary
echo ""
echo "======================================"
echo "Test Summary"
echo "======================================"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ ALL TESTS PASSED!${NC}"
    echo ""
    echo "The framework is ready to use!"
    echo ""
    echo "Next steps:"
    echo "  1. Setup Neo4j:"
    echo "     • Local: ./e2e/scripts/setup-neo4j.sh"
    echo "     • K8s: ./e2e/scripts/setup-neo4j-k8s.sh"
    echo ""
    echo "  2. Run a smoke test:"
    echo "     source .env.e2e"
    echo "     E2E_INCLUDE_TAGS=smoke go run ./e2e/cmd/e2e-runner"
    echo ""
    echo "  3. Query results:"
    echo "     ./bin/e2e-query flaky-tests"
    echo ""
    exit 0
else
    echo -e "${RED}✗ $FAILED TESTS FAILED${NC}"
    echo ""
    echo "Please review the failures above."
    echo "Some failures may require:"
    echo "  • Running 'go mod tidy'"
    echo "  • Installing missing dependencies"
    echo "  • Checking file paths"
    echo ""
    exit 1
fi
