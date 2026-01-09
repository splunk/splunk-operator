# Helm Chart Publishing and Distribution Guide

This document describes the multi-registry Helm chart publishing strategy for the Splunk Operator, based on best practices from splunk-ai-operator.

## Overview

The Splunk Operator Helm charts are now published to multiple registries for maximum accessibility and flexibility:

1. **GitHub Container Registry (GHCR)** - OCI format (Recommended)
2. **GitHub Releases** - Traditional tgz format
3. **Artifact Hub** - Discovery and metadata

## Architecture

### Publishing Flow

```
┌─────────────────────┐
│  Release Trigger    │
│ (Manual/Automated)  │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  Update Chart.yaml  │
│  (version sync)     │
└──────────┬──────────┘
           │
           ├──────────────────────────┬──────────────────────┐
           ▼                          ▼                      ▼
┌──────────────────┐      ┌──────────────────┐   ┌─────────────────┐
│ Package Charts   │      │  Generate BOM    │   │  Generate SBOM  │
│ (helm package)   │      │  (dependencies)  │   │  (security)     │
└──────────┬───────┘      └──────────────────┘   └─────────────────┘
           │
           ├────────────────────┬──────────────────────┐
           ▼                    ▼                      ▼
┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
│  Push to GHCR    │  │ GitHub Release   │  │  Artifact Hub    │
│  (OCI format)    │  │ (with assets)    │  │  (discovery)     │
└──────────────────┘  └──────────────────┘  └──────────────────┘
```

## Files Added/Modified

### 1. Enhanced Chart.yaml
**Location**: `/Users/viveredd/Projects/splunk-operator/helm-chart/splunk-operator/Chart.yaml`

**New Features Added**:

#### A. Metadata Enhancement
```yaml
icon: https://www.splunk.com/content/dam/splunk2/images/icons/favicons/favicon-32x32.png
keywords:
  - splunk
  - operator
  - kubernetes
  - enterprise
  - observability
  - data-platform
home: https://github.com/splunk/splunk-operator
sources:
  - https://github.com/splunk/splunk-operator
```

#### B. Artifact Hub Annotations
Complete metadata for Artifact Hub integration:

- **Category**: `monitoring-logging`
- **License**: `Apache-2.0`
- **Operator Capabilities**: `Seamless Upgrades`
- **Links**: Documentation, installation guides, examples
- **CRDs**: All 6 custom resources with descriptions
- **Security**: Cosign signing information
- **Changes**: Release changelog

**Benefits**:
- Enhanced discoverability on Artifact Hub
- Rich metadata for users
- Automatic security updates notification
- CRD documentation

### 2. artifacthub-repo.yml
**Location**: `/Users/viveredd/Projects/splunk-operator/artifacthub-repo.yml`

Repository-level metadata for Artifact Hub:
- Repository identification
- Owner information
- Multiple installation sources (OCI, GitHub, Docker Hub)
- Documentation links

### 3. Helm Release Workflow
**Location**: `/Users/viveredd/Projects/splunk-operator/.github/workflows/release-helm-charts.yml`

**Features**:

#### A. Version Management
- Automatic Chart.yaml version updates
- Version validation
- Synchronization between chart version and appVersion

#### B. Multi-Registry Publishing
- **GHCR OCI Registry**: Primary distribution method
- **GitHub Releases**: Traditional tgz format with index.yaml
- **Artifact Hub**: Automatic discovery (for public repos)

#### C. Comprehensive Release Assets
- Helm chart packages (.tgz)
- Repository index (index.yaml)
- Artifact Hub metadata
- Installation instructions
- Multiple installation methods

## Publishing Destinations

### 1. GitHub Container Registry (GHCR) - OCI Format

**Advantages**:
- Native OCI support (Helm 3.8+)
- Integrated with GitHub authentication
- Fast and reliable
- Version immutability
- Built-in vulnerability scanning

**Published Locations**:
```
oci://ghcr.io/splunk/charts/splunk-operator:{VERSION}
oci://ghcr.io/splunk/charts/splunk-enterprise:{VERSION}
```

**Installation**:
```bash
helm install splunk-operator \
  oci://ghcr.io/splunk/charts/splunk-operator \
  --version 3.0.1
```

### 2. GitHub Releases

**Advantages**:
- Compatible with all Helm versions
- Easy to download directly
- Includes comprehensive release notes
- Multiple installation options
- Permanent URLs

**Assets Included**:
- Chart packages (.tgz files)
- Repository index (index.yaml)
- Artifact Hub metadata
- Installation documentation

**Installation**:
```bash
# Direct installation
helm install splunk-operator \
  https://github.com/splunk/splunk-operator/releases/download/v3.0.1/splunk-operator-3.0.1.tgz

# Via Helm repo
helm repo add splunk https://github.com/splunk/splunk-operator/releases/latest/download
helm install splunk-operator splunk/splunk-operator --version 3.0.1
```

### 3. Artifact Hub

**Advantages**:
- Central discovery for Kubernetes packages
- Rich metadata display
- Security scanning integration
- Automated updates
- Community visibility

**Features**:
- Automatic chart discovery (for public repos)
- CRD documentation
- Installation instructions
- Change logs
- Security updates notifications

**Access**:
- URL: `https://artifacthub.io/packages/helm/splunk/splunk-operator`
- Search: "Splunk Operator" on Artifact Hub

## Usage Guide

### For Release Managers

#### Triggering a Helm Chart Release

1. **Via GitHub Actions UI**:
   - Go to Actions tab
   - Select "Release Helm Charts with Multi-Registry Publishing"
   - Click "Run workflow"
   - Enter release version (e.g., `3.0.1`)
   - Choose whether to create GitHub release

2. **Via gh CLI**:
   ```bash
   gh workflow run release-helm-charts.yml \
     -f release_version=3.0.1 \
     -f create_github_release=true
   ```

#### Post-Release Checklist

- [ ] Verify GHCR OCI push succeeded
- [ ] Check GitHub release created with all assets
- [ ] Test installation from GHCR
- [ ] Test installation from GitHub release
- [ ] Wait for Artifact Hub to discover (24-48 hours for public repos)
- [ ] Update documentation with new version
- [ ] Announce release

### For End Users

#### Installation Methods

**Method 1: GHCR OCI (Recommended)**
```bash
helm install splunk-operator \
  oci://ghcr.io/splunk/charts/splunk-operator \
  --version 3.0.1 \
  --namespace splunk-operator \
  --create-namespace
```

**Method 2: GitHub Releases**
```bash
helm install splunk-operator \
  https://github.com/splunk/splunk-operator/releases/download/v3.0.1/splunk-operator-3.0.1.tgz \
  --namespace splunk-operator \
  --create-namespace
```

**Method 3: Helm Repository**
```bash
helm repo add splunk https://github.com/splunk/splunk-operator/releases/latest/download
helm repo update
helm install splunk-operator splunk/splunk-operator \
  --version 3.0.1 \
  --namespace splunk-operator \
  --create-namespace
```

**Method 4: Artifact Hub**
```bash
# Find and copy installation command from Artifact Hub page
# https://artifacthub.io/packages/helm/splunk/splunk-operator
```

#### Viewing Chart Information

**Show Values**:
```bash
# From GHCR
helm show values oci://ghcr.io/splunk/charts/splunk-operator --version 3.0.1

# From GitHub
helm show values https://github.com/splunk/splunk-operator/releases/download/v3.0.1/splunk-operator-3.0.1.tgz
```

**Show Chart Metadata**:
```bash
helm show chart oci://ghcr.io/splunk/charts/splunk-operator --version 3.0.1
```

**Show README**:
```bash
helm show readme oci://ghcr.io/splunk/charts/splunk-operator --version 3.0.1
```

#### Upgrading Charts

```bash
# Update repo (if using helm repo)
helm repo update

# Upgrade with new version
helm upgrade splunk-operator \
  oci://ghcr.io/splunk/charts/splunk-operator \
  --version 3.0.2 \
  --namespace splunk-operator
```

## Artifact Hub Integration

### Automatic Discovery

For **public repositories**, Artifact Hub will automatically discover charts if:
1. `artifacthub-repo.yml` exists in repository root
2. Chart packages are published to GitHub Releases
3. `index.yaml` is included in releases

### Manual Registration

For **private repositories** or to expedite discovery:

1. Go to https://artifacthub.io
2. Sign in with GitHub
3. Click "Control Panel"
4. Select "Add Repository"
5. Fill in:
   - **Name**: `splunk-operator`
   - **Display Name**: `Splunk Operator`
   - **URL**: `https://github.com/splunk/splunk-operator/releases`
   - **Repository Type**: `Helm charts`
6. Click "Add"

### Updating Metadata

When you want to update Artifact Hub metadata without a release:

1. Edit `helm-chart/splunk-operator/Chart.yaml` annotations
2. Edit `artifacthub-repo.yml`
3. Commit and push
4. Artifact Hub will sync within 24 hours

## Comparison with Other Registries

| Registry | Format | Auth Required | Helm Version | Best For |
|----------|--------|---------------|--------------|----------|
| **GHCR** | OCI | Optional (public) | 3.8+ | Modern deployments, CI/CD |
| **GitHub Releases** | Traditional | No | All | Maximum compatibility |
| **Artifact Hub** | Discovery | No | All | Chart discovery, metadata |
| **Docker Hub** | N/A | Optional | N/A | Container images only |

## Benefits

### 1. Multi-Registry Strategy

**Redundancy**:
- If one registry is down, others are available
- Geographic distribution
- Multiple authentication methods

**Flexibility**:
- Users choose their preferred method
- Enterprise environments may prefer certain registries
- Air-gapped installations via GitHub releases

### 2. Enhanced Discoverability

**Artifact Hub**:
- Centralized chart discovery
- Rich metadata and documentation
- Security scanning integration
- Community ratings and feedback

**SEO and Documentation**:
- Better Google discovery
- Link to comprehensive docs
- Installation examples
- CRD documentation

### 3. Modern Best Practices

**OCI Registry (GHCR)**:
- Industry standard for container artifacts
- Native Helm 3 support
- Efficient storage and bandwidth
- Image signing integration

**Metadata Richness**:
- Comprehensive annotations
- Security information
- Compatibility matrices
- Change logs

## Security Considerations

### Chart Signing

Future enhancement to add:
```bash
# Sign chart
helm package --sign --key mykey --keyring ~/.gnupg/secring.gpg helm-chart/splunk-operator

# Verify chart
helm verify splunk-operator-3.0.1.tgz --keyring ~/.gnupg/pubring.gpg
```

### Vulnerability Scanning

Charts in GHCR are automatically scanned. View results:
```bash
# Via GitHub UI
https://github.com/splunk/splunk-operator/security/dependabot

# Via API
gh api /orgs/splunk/packages/container/charts%2Fsplunk-operator/versions
```

## Troubleshooting

### GHCR Push Fails

**Error**: `unauthorized: authentication required`

**Solution**:
```bash
echo $GITHUB_TOKEN | helm registry login ghcr.io --username $GITHUB_ACTOR --password-stdin
```

### Chart Not Found on Artifact Hub

**Possible Reasons**:
1. Repository is private
2. First release (wait 24-48 hours)
3. Missing `artifacthub-repo.yml`
4. Invalid metadata format

**Solution**:
- Verify `artifacthub-repo.yml` is in repository root
- Check GitHub release includes `index.yaml`
- Manually register repository on Artifact Hub

### Version Mismatch

**Error**: Downloaded chart version doesn't match requested

**Solution**:
```bash
# Clear Helm cache
rm -rf ~/.cache/helm

# Update repo
helm repo update

# Verify version
helm search repo splunk/splunk-operator --versions
```

## Maintenance

### Regular Tasks

**Monthly**:
- Review Artifact Hub feedback
- Update chart dependencies
- Security scanning review

**Per Release**:
- Update Chart.yaml `artifacthub.io/changes` annotation
- Verify all registry publishes
- Test installation from each source

### Monitoring

Track chart downloads and usage:
```bash
# GitHub API - Release downloads
gh api repos/splunk/splunk-operator/releases | jq '.[].assets[].download_count'

# GHCR - Package views
gh api /orgs/splunk/packages/container/charts%2Fsplunk-operator
```

## References

- **Splunk AI Operator**: Implementation reference
- **Helm OCI**: https://helm.sh/docs/topics/registries/
- **Artifact Hub**: https://artifacthub.io/docs/
- **GHCR**: https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry
- **Chart Best Practices**: https://helm.sh/docs/chart_best_practices/

## Future Enhancements

### Short Term
1. Automated chart testing with chart-testing
2. Chart provenance generation
3. Automated version bumping
4. Release notes automation

### Long Term
1. Chart museum hosting
2. Multi-arch chart support
3. Signed chart releases
4. Automated dependency updates

---

## Summary

The Splunk Operator now supports modern Helm chart distribution with:
- ✅ Multi-registry publishing (GHCR + GitHub Releases)
- ✅ OCI registry support for modern deployments
- ✅ Artifact Hub integration for discovery
- ✅ Rich metadata and documentation
- ✅ Multiple installation methods
- ✅ Enterprise-friendly distribution

This implementation provides maximum flexibility, accessibility, and follows cloud-native best practices for Helm chart distribution.
