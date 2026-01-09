# SBOM and Dependency Management Implementation

This document describes the implementation of Software Bill of Materials (SBOM) generation and dependency management for the Splunk Operator, based on best practices from the splunk-ai-operator project.

## Overview

We've added comprehensive supply chain security features to the splunk-operator release process:
1. **Bill of Materials (BOM)** generation for tracking all container images
2. **Software Bill of Materials (SBOM)** generation for vulnerability scanning and compliance
3. **Centralized dependency management** via `.env` file
4. **Enhanced release workflow** with attestation and signing

## Files Added/Modified

### 1. `.env` - Centralized Dependency Management
**Location**: `/Users/viveredd/Projects/splunk-operator/.env`

Added the following dependencies:
```bash
# Additional Splunk versions for compatibility matrix
RELATED_IMAGE_SPLUNK_ENTERPRISE=splunk/splunk:10.0.2
SPLUNK_ENTERPRISE_9_4_IMAGE=splunk/splunk:9.4.5
SPLUNK_ENTERPRISE_9_3_IMAGE=splunk/splunk:9.3.7

# Helm and Kustomize versions
HELM_VERSION=v3.14.0
KUSTOMIZE_VERSION=v5.0.1
```

**Benefits**:
- Single source of truth for all version dependencies
- Easy to update versions across the project
- Enables automated dependency tracking
- Facilitates compatibility matrix generation

### 2. `scripts/generate-bom.sh` - BOM Generation Script
**Location**: `/Users/viveredd/Projects/splunk-operator/scripts/generate-bom.sh`

**Features**:
- Generates BOM in CycloneDX format (industry standard, machine-readable)
- Creates human-readable text format for documentation
- Tracks all container images used by the operator
- Includes build dependencies (Go, Kubernetes, tools)
- Records supported platforms and versions

**Output Files**:
- `dist/bom-v{VERSION}.json` - CycloneDX format (JSON)
- `dist/bom-v{VERSION}.txt` - Human-readable text

**Usage**:
```bash
# Via Makefile
make generate-bom VERSION=3.0.1

# Direct script invocation
./scripts/generate-bom.sh 3.0.1 dist/
```

### 3. Makefile Target - `generate-bom`
**Location**: `/Users/viveredd/Projects/splunk-operator/Makefile` (lines 190-195)

Added new Makefile target:
```makefile
.PHONY: generate-bom
generate-bom: ## Generate Bill of Materials (BOM) for release
	@echo "Generating Bill of Materials..."
	@mkdir -p dist
	@./scripts/generate-bom.sh $(VERSION) dist
	@echo "✅ BOM generated in dist/ directory"
```

### 4. Enhanced Release Workflow
**Location**: `/Users/viveredd/Projects/splunk-operator/.github/workflows/release-with-sbom.yml`

**New Capabilities**:

#### A. BOM Generation
- Generates Bill of Materials during release
- Tracks all managed images and dependencies
- Includes in GitHub release assets

#### B. SBOM Generation with Syft
- Uses Anchore Syft for SBOM generation
- Generates SBOMs in both CycloneDX and SPDX formats
- Creates separate SBOMs for standard and distroless images
- Supports vulnerability scanning with Grype/Trivy

**SBOM Formats Generated**:
```
dist/sbom-operator-v{VERSION}.cyclonedx.json           # Standard image, CycloneDX
dist/sbom-operator-v{VERSION}.spdx.json                # Standard image, SPDX
dist/sbom-operator-distroless-v{VERSION}.cyclonedx.json # Distroless, CycloneDX
dist/sbom-operator-distroless-v{VERSION}.spdx.json     # Distroless, SPDX
```

#### C. Multi-Registry Support
Pushes images to both:
- **Docker Hub**: `splunk/splunk-operator:{TAG}`
- **GitHub Container Registry**: `ghcr.io/splunk/splunk-operator:{TAG}`

#### D. Image Signing and Attestation
- Signs all images with cosign
- Provides verification instructions in release notes
- Supports supply chain security best practices

#### E. Enhanced Release Assets
Every release now includes:
- Kubernetes manifests (cluster, namespace, CRDs)
- BOM files (JSON + text)
- SBOM files (CycloneDX + SPDX for both image variants)
- Release notes
- Verification instructions

## Benefits

### 1. Security & Compliance
- **Vulnerability Scanning**: SBOMs enable automated scanning with Grype, Trivy, and other tools
- **Supply Chain Transparency**: Complete visibility into dependencies and components
- **Compliance**: Meets requirements for SLSA, SSDF, and executive orders on software supply chain security
- **Image Signing**: Cryptographic verification of image authenticity

### 2. Dependency Management
- **Version Control**: Centralized tracking of all dependencies
- **Compatibility Testing**: Easy to test against multiple Splunk Enterprise versions
- **Reproducibility**: Can reproduce exact build environments
- **Automation**: Enables automated dependency updates (e.g., Dependabot)

### 3. Release Management
- **Transparency**: Users can see exactly what's in each release
- **Security Posture**: Easy to assess security status of releases
- **Audit Trail**: Complete record of all components and versions
- **Multi-Registry**: Flexibility for users in different environments

## Usage Examples

### For Release Managers

#### Generate BOM for testing:
```bash
make generate-bom VERSION=3.0.1
cat dist/bom-v3.0.1.txt
```

#### Trigger enhanced release:
1. Go to Actions tab in GitHub
2. Select "Release with SBOM and BOM" workflow
3. Click "Run workflow"
4. Fill in:
   - Release version: `3.0.1`
   - Operator image tag: `3.0.1`
   - Enterprise version: `10.0.2`

### For End Users

#### Verify image signatures:
```bash
cosign verify --key cosign.pub splunk/splunk-operator:3.0.1
```

#### Scan for vulnerabilities:
```bash
# Using Grype
grype splunk/splunk-operator:3.0.1

# Using Trivy
trivy image splunk/splunk-operator:3.0.1

# Scan SBOM file directly
grype sbom:./sbom-operator-v3.0.1.cyclonedx.json
```

#### Review Bill of Materials:
```bash
# Download from release
curl -L https://github.com/splunk/splunk-operator/releases/download/3.0.1/bom-v3.0.1.txt

# Or view in browser
cat bom-v3.0.1.txt
```

## Integration with Existing Workflows

This implementation is designed to:
- **Complement** existing release workflows (doesn't replace them)
- **Extend** current processes with security and compliance features
- **Maintain** backward compatibility with current release procedures
- **Enable** gradual adoption (can be used alongside existing workflows)

## Standards Compliance

### CycloneDX
- **Version**: 1.4 (current stable)
- **Format**: JSON
- **Use Case**: Vulnerability management, license compliance, security analysis
- **Tools**: Grype, Trivy, Dependency-Track, OWASP Dependency-Check

### SPDX
- **Version**: 2.3
- **Format**: JSON
- **Use Case**: License compliance, software composition analysis
- **Tools**: FOSSology, Black Duck, Snyk

### Supply Chain Levels for Software Artifacts (SLSA)
- Image signing with cosign supports SLSA Level 2+
- Build provenance can be added for SLSA Level 3
- Attestation framework in place

## Future Enhancements

### Short Term
1. Add SBOM generation to CI builds (not just releases)
2. Automate dependency updates with Dependabot
3. Add SBOM diff reports between releases
4. Integrate with vulnerability databases

### Long Term
1. Implement SLSA Level 3 with build provenance
2. Add SBOMs for Helm charts
3. Create compatibility matrix automation
4. Implement automated security advisories

## References

- **Splunk AI Operator**: `/Users/viveredd/Projects/splunk-ai-operator/.github/workflows/release-package-helm.yml`
- **CycloneDX**: https://cyclonedx.org/
- **SPDX**: https://spdx.dev/
- **Syft**: https://github.com/anchore/syft
- **Cosign**: https://github.com/sigstore/cosign
- **SLSA**: https://slsa.dev/

## Maintenance

### Updating Dependencies
Edit `.env` file and update version numbers:
```bash
# Edit .env
vim .env

# Test BOM generation
make generate-bom VERSION=test

# Review output
cat dist/bom-vtest.txt
```

### Testing SBOM Generation Locally
```bash
# Install Syft
brew install syft

# Generate SBOM for local image
syft splunk/splunk-operator:3.0.1 -o cyclonedx-json=test-sbom.json

# Verify content
jq '.components | length' test-sbom.json
```

---

## Summary

This implementation brings enterprise-grade supply chain security practices to the Splunk Operator project, enabling:
- ✅ Complete transparency of dependencies
- ✅ Automated vulnerability scanning
- ✅ Compliance with security frameworks
- ✅ Multi-registry distribution
- ✅ Cryptographic verification
- ✅ Reproducible builds

The implementation follows industry best practices from splunk-ai-operator and aligns with modern software supply chain security requirements.
