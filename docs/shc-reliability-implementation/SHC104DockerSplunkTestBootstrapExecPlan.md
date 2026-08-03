# Make the Docker-Splunk test bootstrap reproducible on current Linux builders

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The canonical Docker-Splunk SHC feature branch has deterministic Make targets
for shutdown coordination, the exact Splunk Ansible source reference, and
base-image signing-key security. Those tests pass on the Linux vWorkstation.
The aggregate `make test_setup` target then installs the repository's broader
legacy Python test requirements into the user environment. On the current
builder, dependency resolution selects PyYAML 5.4.1 through Docker Compose
1.29.2, and the isolated source build fails before the broader test framework
is installed.

SHC-104 will make that aggregate bootstrap reproducible on the supported Linux
builder without weakening or bypassing the tests. This is test-infrastructure
work. It does not identify a Docker-Splunk runtime, shutdown, Search Head,
Splunk Ansible, or Splunk Enterprise failure.

## Progress

- [x] (2026-08-03 UTC) Fast-forwarded canonical Docker-Splunk branch
  `feature/shc-kubernetes-reliability` to exact qualified source
  `6ee266c14e25a1d5849a3d5b96cdaf155b09c696` with no divergent commits.
- [x] (2026-08-03 UTC) Re-ran the repository Make targets on native Linux.
  All 15 shutdown tests, four exact-Ansible-ref tests, and one base-image-
  security test passed. The repository remained clean.
- [x] (2026-08-03 UTC) Reproduced the aggregate bootstrap failure in
  `make test_setup`: pip 26.2 on Python 3.10 selected PyYAML 5.4.1 through
  Docker Compose 1.29.2, then its isolated wheel-requirements step failed with
  `AttributeError: 'build_ext' object has no attribute 'cython_sources'`.
- [x] (2026-08-03 22:27Z) Defined a repository-owned Python 3.10 lock,
  pinned the PyYAML source-build toolchain, and retained Docker Compose 1.29.2
  compatibility on isolated Docker-Splunk branch
  `codex/shc-104-docker-test-bootstrap` at `0604eeb`.
- [x] (2026-08-03 22:27Z) Changed `make test_setup` to construct and verify
  `.test-venv`, route all broader pytest targets through it, and provide an
  exact cleanup target. Added five regression tests for the bootstrap
  contract.
- [x] (2026-08-03 22:27Z) Validated the corrected bootstrap from an empty
  environment and a second time on local Python 3.10. Both runs passed; all 91
  broader image tests collected, the Compose configuration command succeeded,
  and the 15/4/1 bounded SHC tests retained their prior counts.
- [x] (2026-08-03 22:31Z) Qualified exact commit `0604eeb` twice in a clean
  Linux/AMD64 Python 3.10.18 environment pinned by image digest
  `sha256:ee0c7d26e2dba416773cb51c042496e9cffacc872459f57726935dd6833f2d96`.
  Both aggregate runs passed 15 shutdown, four exact-Ansible-ref, one base-
  image-security, and five bootstrap tests. All 91 broader image tests
  collected, Compose configuration validated, and exact cleanup removed the
  repository virtual environment. Fast-forwarded canonical Docker-Splunk
  branch `feature/shc-kubernetes-reliability` to `0604eeb`.

## Surprises & Discoveries

- Observation: the SHC-relevant tests run before the aggregate target's own
  dependency-install recipe.
  Evidence: the Make dependency chain completed 15 shutdown, four Ansible-ref,
  and one security tests before the failing `pip install -r
  tests/requirements.txt --upgrade` command.
  Consequence: the bounded product contracts passed, while the aggregate Make
  target still correctly returned nonzero because its bootstrap is not
  reproducible.
- Observation: unconstrained current build tooling is incompatible with the
  legacy dependency graph.
  Evidence: Docker Compose 1.29.2 constrains PyYAML below version 6, no matching
  wheel was selected for the current Python environment, and the PyYAML 5.4.1
  source build failed under the isolated modern build backend.
  Consequence: rerunning pip or changing application source cannot make this a
  reliable gate; the test environment itself must be owned and locked.
- Observation: the current target installs into the workspace user's Python
  environment when system site-packages are not writable.
  Evidence: pip reported `Defaulting to user installation` before resolving
  the legacy requirements.
  Consequence: SHC-104 must isolate the bootstrap so one repository's test
  dependencies do not alter another repository's tools.
- Observation: the broader test modules invoke the `docker-compose` executable
  but do not import Compose as a Python API.
  Evidence: every Compose use in `tests/executor.py` and the two image-test
  modules is a subprocess command; the isolated environment nevertheless
  provides and validates the legacy executable.
  Consequence: the correction can preserve existing command behavior without
  coupling test source to Compose's internal Python modules.
- Observation: pytest 4.4.0 cannot start under the supported Python 3.10
  interpreter.
  Evidence: after repairing the PyYAML build, pytest 4.4.0 failed assertion
  rewriting with `TypeError: required field "lineno" missing from alias`.
  Consequence: the test-only lock uses pytest 7.4.4 and compatible xdist and
  rerun plugins; production image inputs are unchanged.

## Decision Log

- Decision: keep SHC-104 separate from Docker-Splunk runtime qualification.
  Rationale: all directly relevant repository-owned tests passed, and the
  failure occurred while constructing the broader test environment rather
  than while executing product behavior.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: do not silently bypass Docker Compose/PyYAML constraints or report
  the aggregate target as passing.
  Rationale: an ad hoc host workaround would not be reproducible and could
  invalidate older tests that import the Compose 1.x Python package.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: require a clean-host acceptance gate.
  Rationale: a test bootstrap that succeeds only because a developer already
  has compatible packages installed does not establish repository
  reproducibility.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: retain Docker Compose 1.29.2 while pinning its complete dependency
  graph and PyYAML build toolchain.
  Rationale: existing tests execute the Compose 1.x command contract. Replacing
  it with Compose v2 would broaden SHC-104 into a test-behavior migration.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: update only the incompatible pytest toolchain to supported pinned
  versions.
  Rationale: pytest 4.4 cannot run on Python 3.10, whereas pytest 7.4.4
  successfully collected all 91 existing image tests without source changes.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

SHC-104 is complete at canonical Docker-Splunk commit `0604eeb`. Clean and
idempotent Python 3.10 runs passed locally and on Linux/AMD64. The Linux gate
passed the unchanged 15/4/1 bounded contracts plus five bootstrap regressions,
collected all 91 broader image tests, validated Docker Compose 1.29.2
configuration, and removed the disposable repository environment. Production
runtime source remains unchanged from `6ee266c1`; this commit changes only test
infrastructure.

## Context and Orientation

Docker-Splunk's `Makefile` defines `test_shutdown`, `test_ansible_ref`, and
`test_base_image_security` as prerequisites of `test_setup`. The `test_setup`
recipe then upgrades pip and installs `tests/requirements.txt`.

The current dependency graph includes pytest 4.4.0 and Docker Compose 1.29.2.
The latter constrains PyYAML below version 6. The current Linux workspace uses
Python 3.10 and pip 26.2.

The canonical repository and branch are
`~/splunk-complete/docker-splunk` and
`feature/shc-kubernetes-reliability`. The exact clean source is
`6ee266c14e25a1d5849a3d5b96cdaf155b09c696`.

## Plan of Work

First inventory which broader tests import the Docker Compose Python package
and which only invoke the Docker Compose CLI. Establish the supported Python
and Linux-builder matrix from repository and CI configuration. Select a
compatible locked dependency set or a deliberate migration away from Compose
1.x, preserving every existing test contract.

Change the Make target to create a repository-local isolated environment and
install from the locked inputs. Do not install into the user's site-packages.
Add a bootstrap regression that begins without preinstalled test packages and
fails clearly when the supported toolchain is unavailable.

Run the target twice on a clean native-Linux checkout. The second run must be
idempotent. Then rerun the three bounded SHC Make targets separately to prove
the bootstrap correction did not change their behavior.

## Validation and Acceptance

Acceptance requires:

- `make test_setup` succeeds from a clean supported Linux checkout;
- the environment is repository-local and does not write user site-packages;
- dependency versions are locked or otherwise resolved reproducibly;
- all 15 shutdown, four Ansible-ref, and one base-image-security tests pass;
- existing broader tests can import or invoke the Compose functionality they
  require;
- a second run is idempotent; and
- the Git worktree is clean after the gate.

## Idempotence and Recovery

The test environment must be disposable and recreated through a Make cleanup
target. It must not modify production source, image contents, or the user's
global Python environment. Until SHC-104 is complete, use the three bounded
Make targets for the already-qualified SHC contracts and report the aggregate
bootstrap limitation explicitly.

## Artifacts and Notes

- Docker-Splunk canonical branch:
  `feature/shc-kubernetes-reliability`.
- Qualified runtime source before SHC-104:
  `6ee266c14e25a1d5849a3d5b96cdaf155b09c696`.
- Canonical Docker-Splunk source after test-only SHC-104:
  `0604eeb293832f9c0c19a24d7de8706b63c65031`.
- Passing bounded gate: 15 shutdown, four exact-Ansible-ref, and one
  base-image-security tests.
- Passing local bootstrap gate: clean and repeated `make test_setup`, five
  bootstrap regressions, 91 broader tests collected, and Compose 1.29.2
  configuration validation.
- Linux/AMD64 qualification image:
  `python@sha256:ee0c7d26e2dba416773cb51c042496e9cffacc872459f57726935dd6833f2d96`.
- Final repository state: clean, `.test-venv` absent, canonical branch pushed.
- Current builder: Python 3.10, pip 26.2, Linux AMD64.
- Repository status after reproduction: clean.

## Interfaces and Dependencies

This work concerns only the Docker-Splunk test toolchain: Make, Python, pip,
pytest, Docker Compose's Python package, PyYAML, and their build dependencies.
It must not change the Docker-Splunk runtime entrypoint, shutdown helper,
Splunk Ansible reference, Splunk Enterprise package, or Kubernetes manifests.

Revision note (2026-08-03 20:45Z): Registered SHC-104 after the canonical
branch's bounded SHC Make tests passed but the aggregate target reproduced a
legacy dependency-bootstrap failure on the current Linux builder.
