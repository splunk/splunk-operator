# Spec Kit Workspace

This directory stores Spec Kit artifacts that precede implementation.

## Workflow
1. Bootstrap artifacts:
   - `scripts/dev/speckit_bridge.sh bootstrap --change-id CSPL-XXXX --title "Your change"`
2. Fill and review Spec Kit docs:
   - `speckit/specs/<change-id>-<slug>/spec.md`
   - `speckit/specs/<change-id>-<slug>/plan.md`
   - `speckit/specs/<change-id>-<slug>/tasks.md`
3. Drive generated KEP in `docs/specs/` to `Status: Approved`.
4. Implement through harness manifest policy and PR checks.

## Notes
- Spec Kit artifacts are planning inputs.
- KEP in `docs/specs/` is the governance system of record.
- Harness manifest in `harness/manifests/` is the machine-readable execution contract.
