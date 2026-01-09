#!/usr/bin/env bash

# Generate Bill of Materials (BOM) for Splunk Operator
# This script extracts all Docker images used by the operator and its dependencies
# Usage: ./scripts/generate-bom.sh [VERSION] [OUTPUT_DIR]

set -euo pipefail

VERSION="${1:-unknown}"
OUTPUT_DIR="${2:-.}"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Output files
BOM_JSON="${OUTPUT_DIR}/bom-v${VERSION}.json"
BOM_TXT="${OUTPUT_DIR}/bom-v${VERSION}.txt"

echo "Generating Bill of Materials for Splunk Operator v${VERSION}"
echo "Output directory: ${OUTPUT_DIR}"

# Source .env file to get image versions
if [ -f .env ]; then
    # shellcheck disable=SC1091
    set -a
    source .env
    set +a
else
    echo "Warning: .env file not found, using defaults"
fi

# Operator image (from kustomization.yaml or parameter)
OPERATOR_IMAGE="ghcr.io/splunk/splunk-operator:${VERSION}"
OPERATOR_IMAGE_DOCKERHUB="splunk/splunk-operator:${VERSION}"

# Extract images from environment variables
declare -A IMAGES=(
    ["operator-ghcr"]="${OPERATOR_IMAGE}"
    ["operator-dockerhub"]="${OPERATOR_IMAGE_DOCKERHUB}"
    ["splunk-enterprise-10.0"]="${RELATED_IMAGE_SPLUNK_ENTERPRISE:-splunk/splunk:10.0.2}"
    ["splunk-enterprise-9.4"]="${SPLUNK_ENTERPRISE_9_4_IMAGE:-splunk/splunk:9.4.5}"
    ["splunk-enterprise-9.3"]="${SPLUNK_ENTERPRISE_9_3_IMAGE:-splunk/splunk:9.3.7}"
)

# Additional metadata
K8S_VERSION="${EKS_CLUSTER_K8_VERSION:-1.29}"
GO_VERSION="${GO_VERSION:-1.23.0}"
OPERATOR_SDK_VERSION="${OPERATOR_SDK_VERSION:-v1.39.0}"
KUBECTL_VERSION="${KUBECTL_VERSION:-v1.29.1}"
HELM_VERSION="${HELM_VERSION:-v3.14.0}"
KUSTOMIZE_VERSION="${KUSTOMIZE_VERSION:-v5.0.1}"

# Generate JSON BOM (CycloneDX format)
cat > "${BOM_JSON}" <<EOF
{
  "bomFormat": "CycloneDX",
  "specVersion": "1.4",
  "version": 1,
  "metadata": {
    "timestamp": "${TIMESTAMP}",
    "component": {
      "type": "application",
      "name": "splunk-operator",
      "version": "${VERSION}",
      "description": "Splunk Operator for Kubernetes - Manages Splunk Enterprise deployments"
    },
    "properties": [
      {
        "name": "kubernetes_version",
        "value": "${K8S_VERSION}+"
      },
      {
        "name": "go_version",
        "value": "${GO_VERSION}"
      },
      {
        "name": "operator_sdk_version",
        "value": "${OPERATOR_SDK_VERSION}"
      },
      {
        "name": "kubectl_version",
        "value": "${KUBECTL_VERSION}"
      },
      {
        "name": "helm_version",
        "value": "${HELM_VERSION}"
      },
      {
        "name": "kustomize_version",
        "value": "${KUSTOMIZE_VERSION}"
      }
    ]
  },
  "components": [
EOF

# Add components to JSON
FIRST=true
for name in "${!IMAGES[@]}"; do
    image="${IMAGES[$name]}"
    if [ "$FIRST" = true ]; then
        FIRST=false
    else
        echo "," >> "${BOM_JSON}"
    fi

    # Extract image parts
    if [[ "$image" == *":"* ]]; then
        image_name="${image%:*}"
        image_tag="${image##*:}"
    else
        image_name="$image"
        image_tag="latest"
    fi

    cat >> "${BOM_JSON}" <<COMPONENT
    {
      "type": "container",
      "name": "${name}",
      "version": "${image_tag}",
      "purl": "pkg:docker/${image_name}@${image_tag}",
      "properties": [
        {
          "name": "image",
          "value": "${image}"
        }
      ]
    }
COMPONENT
done

cat >> "${BOM_JSON}" <<EOF

  ]
}
EOF

# Generate human-readable text BOM
cat > "${BOM_TXT}" <<EOF
================================================================================
Bill of Materials (BOM)
Splunk Operator for Kubernetes v${VERSION}
Generated: ${TIMESTAMP}
================================================================================

OPERATOR IMAGES
---------------
GHCR:      ${OPERATOR_IMAGE}
DockerHub: ${OPERATOR_IMAGE_DOCKERHUB}

MANAGED SPLUNK ENTERPRISE IMAGES
---------------------------------
EOF

for name in "${!IMAGES[@]}"; do
    if [[ "$name" != operator-* ]]; then
        printf "%-30s %s\n" "${name}:" "${IMAGES[$name]}" >> "${BOM_TXT}"
    fi
done

cat >> "${BOM_TXT}" <<EOF

BUILD DEPENDENCIES
------------------
Kubernetes Version:           ${K8S_VERSION}+
Go Version:                   ${GO_VERSION}
Operator SDK:                 ${OPERATOR_SDK_VERSION}
kubectl:                      ${KUBECTL_VERSION}
Helm:                         ${HELM_VERSION}
Kustomize:                    ${KUSTOMIZE_VERSION}

SUPPORTED PLATFORMS
-------------------
- Kubernetes 1.25+
- OpenShift 4.12+
- EKS, GKE, AKS

VERIFICATION
------------
To verify image digests:
  docker pull <image> --platform linux/amd64
  docker inspect <image> --format='{{.RepoDigests}}'

SECURITY SCANNING
-----------------
Scan operator image for vulnerabilities:
  grype ${OPERATOR_IMAGE}
  trivy image ${OPERATOR_IMAGE}

Scan Splunk Enterprise images:
  grype splunk/splunk:10.0.2
  trivy image splunk/splunk:10.0.2

================================================================================
EOF

echo "✅ Generated BOM files:"
echo "   - ${BOM_JSON} (CycloneDX format - machine-readable)"
echo "   - ${BOM_TXT} (Human-readable text)"

# Print summary
echo ""
echo "Summary of images included in v${VERSION}:"
echo "----------------------------------------"
for name in "${!IMAGES[@]}"; do
    printf "%-30s %s\n" "${name}:" "${IMAGES[$name]}"
done
echo "----------------------------------------"
echo "Total images: ${#IMAGES[@]}"
