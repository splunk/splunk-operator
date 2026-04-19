# Engineering Requirements Document

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `docs/`

---

## Background

This EPIC introduces significant changes to how the Splunk Operator manages pod lifecycle: replacing the OnDelete update strategy with RollingUpdate, moving Splunk shutdown logic into preStop hooks, introducing PodDisruptionBudgets, adding a pod cleanup finalizer, and adding per-pod restart detection via the Eviction API. Before implementation begins, a single Engineering Requirements Document (ERD) must be written that captures the complete requirements for the entire EPIC — functional requirements, non-functional requirements, API changes, constraints, and explicit out-of-scope boundaries.

The ERD is the authoritative reference that all implementation stories derive their acceptance criteria from. It ensures the team has a shared, reviewed understanding of what the system must do before code is written.

---

## What Needs to Be Built

A single ERD document covering the full scope of this EPIC, including:

- **Problem statement:** Why the current operator-driven pod lifecycle approach is insufficient. Each problem must be stated precisely with its operational impact (e.g., how long a 100-indexer rolling update takes under the current model).
- **Goals:** What this EPIC will achieve, stated as outcomes rather than implementation steps.
- **Non-goals:** What is explicitly not in scope, to prevent scope creep during implementation.
- **Functional requirements:** Numbered, testable requirements for every capability being built — StatefulSet RollingUpdate strategy, PodDisruptionBudgets, pod intent annotation and Downward API volume, preStop lifecycle hook behaviour, pod cleanup finalizer, per-pod restart detection and Eviction API, RBAC, and CRD schema changes.
- **Non-functional requirements:** Availability, crash recovery, disruption budget enforcement, pod termination time ceiling, finalizer non-blocking guarantee, no spurious restarts, and test coverage floor.
- **API changes summary:** A table of every new or modified field across all CRDs, with the type of change (added, modified, removed) and whether it is backwards-compatible.
- **Constraints:** External constraints that bound the implementation — Kubernetes minimum version, Splunk API version, StatefulSet PVC naming conventions, Downward API sync latency.
- **Out of scope:** Explicit list of related work that is not part of this EPIC.

---

## Acceptance Criteria

1. The ERD document exists and covers all eight areas listed above.
2. Every functional requirement is numbered and testable — it can be verified as met or not met without ambiguity.
3. Non-functional requirements include measurable thresholds where applicable (e.g., test coverage floor, `terminationGracePeriodSeconds` ceiling, maximum pods simultaneously unavailable).
4. The API changes table accounts for every field added or modified across all CRDs in this EPIC.
5. The constraints section calls out the Kubernetes minimum version required for `policy/v1` PodDisruptionBudget and Eviction API.
6. The document has been reviewed by at least one engineer who will implement a story in this EPIC and one who will not.
7. No implementation story in this EPIC proceeds to development until the ERD has been reviewed and approved.

---

## Definition of Done

- [ ] ERD document written covering all sections listed above.
- [ ] All functional requirements are numbered and traceable to at least one implementation story.
- [ ] Reviewed and approved by engineering lead.
- [ ] Stored in the repository alongside the EPIC and story files.
