#!/bin/bash
# Validation script for Helm chart tgz files
# This script ensures that splunk-operator chart tgz files contain only the operator chart,
# not the full splunk-enterprise chart (which would cause Helm to load a stale subchart).

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CHARTS_DIR="${REPO_ROOT}/helm-chart/splunk-enterprise/charts"

echo "Validating Helm chart tgz files in ${CHARTS_DIR}"
echo ""

EXIT_CODE=0

# Expected size ranges for operator charts (in KB)
MIN_OPERATOR_SIZE_KB=5     # 3.x charts are ~6-7KB (no CRDs)
MAX_OPERATOR_SIZE_2X_KB=450 # 2.x charts are ~400-430KB (with CRDs)
MAX_OPERATOR_SIZE_3X_KB=10  # 3.x charts should be under 10KB

for TGZ_FILE in "${CHARTS_DIR}"/splunk-operator-*.tgz; do
    if [ ! -f "${TGZ_FILE}" ]; then
        continue
    fi

    FILENAME=$(basename "${TGZ_FILE}")
    VERSION=$(echo "${FILENAME}" | sed 's/splunk-operator-\(.*\)\.tgz/\1/')

    # Get file size in KB
    SIZE_BYTES=$(stat -f%z "${TGZ_FILE}" 2>/dev/null || stat -c%s "${TGZ_FILE}" 2>/dev/null)
    SIZE_KB=$((SIZE_BYTES / 1024))

    echo "Checking ${FILENAME} (${SIZE_KB}KB)..."

    # Check contents
    FIRST_DIR=$(tar -tzf "${TGZ_FILE}" | head -1 | cut -d'/' -f1)

    if [ "${FIRST_DIR}" != "splunk-operator" ]; then
        echo -e "${RED}ERROR: ${FILENAME} does not start with 'splunk-operator/' directory${NC}"
        echo "  Expected: splunk-operator/..."
        echo "  Got: ${FIRST_DIR}/..."
        EXIT_CODE=1
        continue
    fi

    # Check for splunk-enterprise content (should NOT be present)
    if tar -tzf "${TGZ_FILE}" | grep -q "splunk-enterprise/Chart.yaml"; then
        echo -e "${RED}ERROR: ${FILENAME} contains splunk-enterprise chart content${NC}"
        echo "  This file appears to be a full splunk-enterprise chart package instead of just the operator chart."
        echo "  Expected: Only splunk-operator chart files"
        echo "  Found: splunk-enterprise/Chart.yaml (and likely other splunk-enterprise files)"
        EXIT_CODE=1
        continue
    fi

    # Check size is reasonable based on version
    MAJOR_VERSION=$(echo "${VERSION}" | cut -d'.' -f1)

    if [ "${MAJOR_VERSION}" = "3" ]; then
        # 3.x charts removed CRDs, should be small
        if [ ${SIZE_KB} -gt ${MAX_OPERATOR_SIZE_3X_KB} ]; then
            echo -e "${YELLOW}WARNING: ${FILENAME} is larger than expected for 3.x (${SIZE_KB}KB > ${MAX_OPERATOR_SIZE_3X_KB}KB)${NC}"
            echo "  3.x operator charts should not include CRDs and should be under 10KB"
        fi
    elif [ "${MAJOR_VERSION}" = "2" ]; then
        # 2.x charts included CRDs, larger but still not huge
        if [ ${SIZE_KB} -gt ${MAX_OPERATOR_SIZE_2X_KB} ]; then
            echo -e "${YELLOW}WARNING: ${FILENAME} is larger than expected for 2.x (${SIZE_KB}KB > ${MAX_OPERATOR_SIZE_2X_KB}KB)${NC}"
        fi
    fi

    # Size sanity check - anything over 1MB is definitely wrong (4.5MB was the corrupted file)
    if [ ${SIZE_KB} -gt 1024 ]; then
        echo -e "${RED}ERROR: ${FILENAME} is suspiciously large (${SIZE_KB}KB)${NC}"
        echo "  This likely contains the full splunk-enterprise chart instead of just the operator chart"
        EXIT_CODE=1
        continue
    fi

    echo -e "${GREEN}✓ ${FILENAME} validated successfully${NC}"
    echo ""
done

if [ ${EXIT_CODE} -eq 0 ]; then
    echo -e "${GREEN}All Helm chart tgz files validated successfully!${NC}"
else
    echo -e "${RED}Validation failed! Please fix the issues above.${NC}"
fi

exit ${EXIT_CODE}
