---
name: sok-release-checklist
description: Prepare or verify a Splunk Operator release checklist, including compatibility, manifests, bundles, images, docs, and upgrade notes. Use when asked about release readiness, compatibility matrices, or release process steps.
---

# SOK Release Checklist

## Overview
Produce a release readiness checklist tailored to this repo's build, bundle, and documentation flow.

## Workflow
1. Gather release context (version, target Splunk Enterprise versions, Kubernetes support).
2. Verify CRD and bundle artifacts.
3. Verify docs and upgrade guidance.
4. Verify image tags and helm chart outputs.
5. Summarize risks and required follow-ups.

## Details

### 1) Compatibility and support
- Review `docs/README.md` for compatibility notes and pointers to release notes.
- Review `docs/SplunkOperatorUpgrade.md` for upgrade constraints and breaking changes.
- Review `docs/ChangeLog.md` for release changes.
- If a public release is being prepared, confirm compatibility in the GitHub release notes.

### 2) Manifests and bundle
- For CRD changes, ensure `make manifests` and `make bundle` are run.
- Confirm generated CRDs in `config/crd/bases/` and `bundle/manifests/`.
- Confirm helm chart CRDs updated in `helm-chart/splunk-operator/crds`.
- Confirm CSV and manifest bases in `bundle/manifests/splunk-operator.clusterserviceversion.yaml` and `config/manifests/`.
- Use `make verify` or `./scripts/verify_crd.sh` and `./scripts/verify_bundle.sh` to confirm outputs are in sync.
- Use `docs/agent/RELEASE_FLOW.md` for the canonical release flow.

### 3) Images and tags
- Confirm operator image tag and any distroless tag if used.
- Verify `bundle.Dockerfile` or `Dockerfile` changes if applicable.

### 4) Docs and examples
- Update install or upgrade docs if defaults or requirements changed.
- Update examples when spec fields or defaults changed.

## Output Contract
- Use `assets/release-checklist.md` for the final checklist.
- Call out any missing inputs needed to finish the checklist.

## Key Paths
- Release docs: `docs/README.md`, `docs/ChangeLog.md`, `docs/SplunkOperatorUpgrade.md`, `docs/Install.md`
- CRDs: `config/crd/bases/`, `bundle/manifests/`, `helm-chart/splunk-operator/crds`
- Manifests: `config/manifests/`
- CSV: `bundle/manifests/splunk-operator.clusterserviceversion.yaml`
- Build: `Makefile`, `Dockerfile`, `bundle.Dockerfile`
- Project mapping: `PROJECT`
- Agent docs: `docs/agent/TEST_MATRIX.md`
- Release flow: `docs/agent/RELEASE_FLOW.md`
