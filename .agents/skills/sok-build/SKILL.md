---
name: sok-build
description: Build Splunk Operator artifacts with the repository-standard workflow. Use for local compile checks, CRD generation, and image build prep.
---

# SOK Build

## Overview
Run deterministic build steps for Splunk Operator using repo-native Make targets.

## Preconditions
- Run `sok-prerequisites` first.
- Any API or marker changes should be committed or staged before generation checks.

## Workflow
1. If API types changed, run generation first: `make generate manifests`.
2. Run formatting and static checks: `make fmt vet`.
3. Build operator binary: `make build`.
4. If image validation is needed, build image with explicit tag: `make docker-build IMG=<image:tag>`.
5. For CRD-sensitive changes, run `scripts/verify_crd.sh`.

## Pass / Fail Criteria
- Pass: build commands exit 0 and generated artifacts are in sync.
- Fail: any build/generation command exits non-zero.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
