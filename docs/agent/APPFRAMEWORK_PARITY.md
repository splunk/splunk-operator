# App Framework Parity Governance

This gate ensures appframework-related implementation changes include parity evidence.

## Trigger Paths
- `pkg/splunk/enterprise/afwscheduler.go`
- `pkg/splunk/enterprise/afwscheduler_test.go`
- `test/testenv/appframework_utils.go`
- `test/appframework_*/**`
- `config/samples/*appframework*`
- `docs/AppFramework.md`

## Required Evidence
When trigger paths change in a PR:
- Update at least one parity doc in the same diff:
  - `docs/AppFramework.md` or
  - `docs/agent/APPFRAMEWORK_PARITY.md`
- Include appframework test updates in the same diff.
- Run and report:
  - `scripts/dev/appframework_parity_check.sh`

## Gate
- `scripts/dev/appframework_parity_check.sh`
