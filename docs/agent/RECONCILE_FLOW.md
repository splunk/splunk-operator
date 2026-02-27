# Reconcile Flow

This document describes the typical reconcile flow and the most common gates.

## Control Flow
1. Controller `Reconcile` fetches the CR instance.
2. Paused annotation check may short-circuit and requeue.
3. Apply function in `pkg/splunk/enterprise/*` builds desired state.
4. Status is updated and the controller returns a requeue or completion.

## Common Gates
- Paused annotation in `api/v4/*_types.go` or `api/v3/*_types.go`.
- Phase gating constants in `api/v4/common_types.go`.
- Predicate filtering in `internal/controller/common/predicate.go`.

## Phases
`Pending`, `Ready`, `Updating`, `ScalingUp`, `ScalingDown`, `Terminating`, `Error`.

## Where To Look First
- Controller entry: `internal/controller/<kind>_controller.go`.
- Apply logic: `pkg/splunk/enterprise/<kind>.go`.
- Shared helpers: `pkg/splunk/enterprise/util.go` and `pkg/splunk/common/*`.

## Debug Checklist
- Confirm CR spec and status (`kubectl get -o yaml`).
- Check events (`kubectl get events -n <ns> --sort-by=.lastTimestamp`).
- Inspect operator logs for the CR name.
