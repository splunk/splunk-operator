# Release Flow

This is a concise, repo-specific release flow intended for humans and Codex skills.

## Inputs
- Release version (update `VERSION` in `Makefile` or set `VERSION=<x.y.z>` in the environment).
- Target Splunk Enterprise compatibility (update docs and release notes accordingly).

## Core Steps
1. Update `docs/ChangeLog.md` and any release notes.
2. Update compatibility notes in `docs/README.md` and `docs/SplunkOperatorUpgrade.md` as needed.
3. If CRDs changed, run `make generate` and `make manifests` (or `./scripts/verify_crd.sh`).
4. If bundle/CSV outputs are needed, run `make bundle` (or `./scripts/verify_bundle.sh`).
5. Run `make verify VERIFY_BUNDLE=1` to ensure generated outputs are consistent.
6. Run unit tests (`make test`) and any required integration tests.
7. Build/push images and bundle artifacts as required by release packaging.

## Artifacts to Inspect
- CRDs: `config/crd/bases/`
- RBAC: `config/rbac/role.yaml`
- Bundle/CSV: `bundle/manifests/`, `bundle/manifests/splunk-operator.clusterserviceversion.yaml`
- Helm CRDs: `helm-chart/splunk-operator/crds`

## Common Commands
- `make verify VERIFY_BUNDLE=1`
- `make bundle`
- `make bundle-build` and `make bundle-push`
- `make catalog-build` and `make catalog-push`

## Docs to Update
- `docs/ChangeLog.md`
- `docs/README.md`
- `docs/SplunkOperatorUpgrade.md`
- `docs/Install.md` (if install defaults or requirements changed)
