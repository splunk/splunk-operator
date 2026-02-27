---
name: sok-testcase-builder
description: Create new Splunk Operator integration (Ginkgo) or KUTTL tests from a CR spec and expected results. Use when a developer asks to add a new test case that validates CR status phase Ready and required resources.
---

# SOK Testcase Builder

## Overview
Generate scaffolds for new integration or KUTTL tests based on a CR spec and expected results.

## Workflow
1. Determine test type: integration (Ginkgo) or KUTTL.
2. Identify the SVA architecture (S1, C3, M4, M1), features (smartstore, appframework), and any SVA validations.
3. Collect CR manifest path(s) and expected results.
4. Create or update a testcase spec file from `docs/agent/TESTCASE_SPEC.yaml`.
5. Run the generator script to scaffold the test.
6. Fill in TODOs (spec struct, resource checks, extra asserts).
7. Run the appropriate test command.

## Test Types

### KUTTL
- Inputs: CR manifest, expected phase, and resource assertions.
- Output: `kuttl/tests/<suite>/<name>/` with deploy and assert steps.
- Recommended when validating CRD behavior with simple YAML assertions.
 - Supports optional operator upgrade steps using the `upgrade` spec block.

### Integration (Ginkgo)
- Inputs: CR spec and expected behaviors.
- Output: `test/<suite>/<name>_test.go` with a suite file if missing.
- Recommended for multi-step flows or API-based verification.
 - Use `docs/agent/TESTCASE_PATTERNS.md` to map SVA patterns to helpers.
 - For C3 SVA, set `validations.sva: C3` to include Monitoring Console + License Manager readiness checks.

## Generator Script
Use `scripts/generate_testcase.py` with a spec file:

`python3 scripts/generate_testcase.py --spec docs/agent/TESTCASE_SPEC.yaml`

Options:
- `--force` overwrite existing files
- `--dry-run` print actions without writing
Note: YAML specs require `pyyaml` (`python3 -m pip install pyyaml`).

## Expected Results
- Always validate `status.phase` is `Ready` (or the specified phase).
- Add asserts for key resources (StatefulSet, Service, Secret, ConfigMap) as needed.

## Output Contract
- List created/edited files
- Provide the test command to run
- Call out any TODOs left in the scaffold

## Key References
- Spec template: `docs/agent/TESTCASE_SPEC.yaml`
- Patterns: `docs/agent/TESTCASE_PATTERNS.md`
- Test matrix: `docs/agent/TEST_MATRIX.md`
- CRD map: `docs/agent/CRD_MAP.md`
- Test helpers: `test/testenv/verificationutils.go`
