---
title: Recovering PostgresDatabase Terminal Failures
parent: PostgreSQL
nav_order: 5
---

# Recovering PostgresDatabase Terminal Failures

This guide explains how to recover a `PostgresDatabase` when the operator stops reconciliation after detecting a terminal failure.

## When this applies

During `PostgresDatabase` reconciliation, the operator creates or validates role Secrets, publishes role intent for the `PostgresCluster` controller to reconcile, creates databases and ConfigMaps, and then grants privileges for each `<database>_rw` role. See [PostgresDatabase Managed Roles](postgresdatabase-managed-roles.md) for the role handoff between controllers.

Most reconciliation errors are treated as retryable and reconciliation continues automatically. Some known user-actionable errors are terminal because retrying the same spec is not expected to succeed without user intervention. When a terminal failure is detected, the operator marks the `PostgresDatabase` as `Failed`, records a failure type in status, and stops retrying that same spec generation.

## Terminal reconciliation failures

When this happens, the operator:

- sets `status.phase` to `Failed`
- records the failure type in `status.reconcileFailureType`
- stops retrying that failure for the current `metadata.generation`
- resumes after a spec change creates a new generation

`status.conditions` is the canonical status surface for users and automation. `status.reconcileFailureType` is controller state used to decide whether reconciliation should stay blocked for the current generation.

Internally, terminal errors use a generic terminal marker and the controller maps that marker to a status failure type for the phase being reconciled.

Current terminal failure types:

| `status.reconcileFailureType` | Condition | Reconciliation phase | Meaning |
| --- | --- | --- | --- |
| `Privileges` | `PrivilegesReady` | RW role privilege grants | The operator could not complete live grants for one or more `<database>_rw` roles because of a user-actionable PostgreSQL failure. |

The mechanism is generic, but `Privileges` is currently the only recorded terminal failure type for `PostgresDatabase`.

Check the resource status:

```bash
kubectl get postgresdatabase -n <namespace> <name> -o yaml
```

Relevant fields:

- `status.phase`
- `status.conditions`
- `status.reconcileFailureType`

For example, a terminal privilege grant failure looks like this:

```yaml
status:
  observedGeneration: 7
  phase: Failed
  conditions:
    - type: PrivilegesReady
      status: "False"
      reason: PrivilegesTerminalFailure
      message: Failed to grant RW role privileges. Manual intervention required: fix the PostgresDatabase spec or referenced configuration, then redeploy with a spec change.
  reconcileFailureType: Privileges
```

The value `reconcileFailureType: Privileges` means the privileges failure is terminal for the recorded generation. Retryable privilege grant errors do not set `status.reconcileFailureType`.

Some configuration problems are detected before the operator attempts live privilege grants and are outside the scope of this recovery flow. For example, a missing superuser Secret or a missing password key follows the existing earlier-phase reconciliation behavior and may not set `status.reconcileFailureType`.

## Privilege Grant Example

The current terminal failure type is `Privileges`. It applies when the operator reaches the RW role privilege grant phase and PostgreSQL returns a known user-actionable error.

Terminal privilege grant failures include PostgreSQL errors such as invalid authorization, invalid superuser password, or insufficient privileges. These are recorded as `reconcileFailureType: Privileges`.

### Terminal SQLSTATEs

The operator treats a PostgreSQL error as a terminal privileges failure when it has one of these SQLSTATE matches:

| SQLSTATE match | PostgreSQL condition name | Meaning |
| --- | --- | --- |
| `28xxx` | `invalid_authorization_specification` class | Any authorization failure in SQLSTATE class `28`. PostgreSQL currently defines `28000` (`invalid_authorization_specification`) and `28P01` (`invalid_password`) in this class. |
| `42501` | `insufficient_privilege` | The connected user is authenticated but does not have the privileges required for the grant operation. |

All other PostgreSQL SQLSTATEs are treated as retryable by this terminal PostgreSQL error classification. PostgreSQL SQLSTATE condition names are documented in [PostgreSQL Appendix A: Error Codes](https://www.postgresql.org/docs/current/errcodes-appendix.html).

## Fix the underlying issue

For a terminal `Privileges` failure, common checks include:

- the referenced `PostgresCluster` is `Ready`
- the CNPG cluster is healthy and reachable
- the superuser Secret exists and contains the expected password key
- the superuser password matches the live PostgreSQL cluster
- the operator can connect to the RW endpoint
- no external policy or manual database change prevents the required grants

After fixing the issue, redeploy the `PostgresDatabase` with a spec change. The operator uses the new `metadata.generation` to retry terminal failures, so simple annotation-only changes are not enough.

For example, update a real spec field that matches the intended configuration change:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: <name>
spec:
  clusterRef:
    name: <cluster-name>
  databases:
    - name: <database-name>
      deletionPolicy: Retain # change from Delete to Retain, or vice versa
```

Apply the manifest through your normal deployment workflow.

## What the operator does

- If the resource is in a current terminal failure, the operator does not retry the same failed operation.
- If the `PostgresDatabase` spec changes, the operator treats the stale marker as a retry signal.
- If recovery succeeds, the operator clears `status.reconcileFailureType`.
- If recovery hits another terminal error, the operator records the mapped `status.reconcileFailureType` and returns to `Failed`.
- If the retry hits an unknown or temporary error, the operator keeps the resource in `Provisioning` and retries automatically.
- For `Privileges` failures, a spec change retries RW role grants even when no new databases were added.
- If no new databases require live grants and there is no stale terminal failure to recover from, the operator marks `PrivilegesReady=True` with an "already current" message and does not emit a privileges-ready event for skipped work.

## Verify recovery

Watch the resource until it returns to `Ready`:

```bash
kubectl get postgresdatabase -n <namespace> <name> -w
```

Then inspect status:

```bash
kubectl get postgresdatabase -n <namespace> <name> \
  -o jsonpath='{.status.phase}{"\n"}{.status.reconcileFailureType}{"\n"}{.status.conditions}{"\n"}'
```

Expected result:

- `status.phase` is `Ready`
- `status.reconcileFailureType` is empty
- the relevant readiness condition is `True`; for the current `Privileges` failure type, check `PrivilegesReady`

If the resource returns to `Failed`, read the relevant condition message, fix the issue, and redeploy with another spec change. For the current `Privileges` failure type, the relevant condition is `PrivilegesReady`.
