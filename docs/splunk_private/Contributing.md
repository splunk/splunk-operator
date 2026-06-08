---
title: Contributing
parent: Internal Onboarding
nav_order: 5
---

# Contributing

This page describes how SOK developers should contribute to this repo.

## Internal developer contribution path 

When you add behavior to SOK, own the whole path from API input to operational support.

1. API contract
    - Add or change CRD fields intentionally.
    - Keep JSON field names stable after release.
    - Document defaults, validation rules, feature gates, maturity, and unsupported combinations.
    - Preserve backward compatibility or provide an explicit migration path.

2. Reconciliation behavior
    - Keep the controller thin and place business behavior according to [pkg/splunk/README.md](https://cd.splunkdev.com/sok/splunk-operator/-/blob/develop/pkg/splunk/README.md).
    - Make reconcile steps idempotent.
    - Watch all dependency resources needed to converge after upstream changes.
    - Avoid one-time imperative actions that cannot be retried safely.

3. Kubernetes resources
    - Define which Services, StatefulSets, Secrets, ConfigMaps, PVCs, Jobs, or RBAC objects SOK creates.
    - Set owner references only where SOK should own lifecycle and cleanup.
    - Preserve user-owned configuration and unrelated cluster state.
    - Describe any resources that users must create before reconciliation can succeed.

4. Splunk-side behavior
    - Explain what SOK configures inside Splunk Enterprise.
    - Explain what SOK waits for before marking the CR ready.
    - Account for scale, restart, upgrade, password rotation, bundle push, and deletion behavior where relevant.
    - Identify manual recovery steps when Splunk-side state cannot be repaired by reconciliation alone.

5. Status and events
    - Update `status.phase`, `status.conditions`, `status.observedGeneration`, selectors, replica counts, and messages where applicable.
    - Emit Kubernetes events for user-visible milestones and failures.
    - Keep event messages actionable. Log detailed errors, but avoid raw `err.Error()` in events.
    - Use existing event reason constants instead of adding string literals.

6. Tests
    - Add unit coverage for validation, defaulting, resource builders, and edge cases.
    - Add controller or integration coverage when behavior spans multiple resources.
    - Cover upgrade and deletion paths when the change affects persisted resources.
    - Include negative tests for unsupported specs, missing dependencies, and feature-gated behavior.

7. Documentation and support
    - Update user docs, examples, runbooks, and troubleshooting guidance in the same change or linked follow-up.
    - Tell users what they configure, what SOK reconciles, and what signals indicate progress or failure.
    - Provide a support handoff: owning team, escalation path, common failure modes, and known limitations.
    - Keep docs aligned with the current API group and controller behavior.

## Code review expectations

Before opening an MR, the following must be true:
- The CRD API is stable, validated, documented, and covered by tests.
- The reconcile path is idempotent and safe to retry.
- Dependency watches are complete.
- User-owned resources are not overwritten or deleted unexpectedly.
- Status, conditions, and events explain progress and failure clearly.
- Upgrade, rollback, scale, pause, and deletion behavior are understood.
- Secrets are not logged, exposed in events, or copied into user-visible messages.
- Runbooks and troubleshooting docs name the owning team and escalation path.

Before requesting reviews for an MR, the following must be true:
- All MR pipelines are green.
- All Codex MR review comments are resolved.

## Where to propose docs changes 

### Public Facing Documentation

Documentation updated in the [docs/](https://cd.splunkdev.com/sok/splunk-operator/-/tree/develop/docs?ref_type=heads) folder, outside of the [docs/splunk_private/](https://cd.splunkdev.com/sok/splunk-operator/-/tree/develop/docs/splunk_private?ref_type=heads) folder, gets published to the [GitHub repo](https://github.com/splunk/splunk-operator) and [GitHub pages](https://splunk.github.io/splunk-operator/) for customers to view.

The majority of documentation updates should be made there, for both internal developers, and customers and external contributors to see.

### Internal Splunk Documentation

Documentation updated in the [docs/splunk_private/](https://cd.splunkdev.com/sok/splunk-operator/-/tree/develop/docs/splunk_private?ref_type=heads) folder does NOT get published to any external site for customers to view. This documentation may contain setup for Splunk employees, and troublshooting steps specific to internal Splunk setup. Use this folder only if the information would be useless to customers without specific setups, such as GitLab and Kraken.
