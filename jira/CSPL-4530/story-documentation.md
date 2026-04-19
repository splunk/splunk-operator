# Documentation

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `docs/`, `config/samples/`, design documents

---

## Background

The changes introduced in this EPIC represent a significant shift in how the operator behaves. Operators who have tuned their deployments around the old behaviour (OnDelete strategy, manual pod cycling, no PDBs) need clear documentation of what has changed, what they need to know about the new behaviour, and how to configure it.

Additionally, the three new CRDs (IngestorCluster, Queue, ObjectStorage) are net-new surface area that requires complete reference documentation.

---

## What Needs to Be Built

### 1. Architecture design document (`docs/per-pod-rolling-restart-design.md`)

A technical design document explaining the architecture for engineering and advanced operators. This is a design-intent document, not a how-to guide. It must explain:

**Why:** The problems with the old operator-driven approach (blocking reconcile loops, serial operations, no PDB, no intent awareness, crash recovery failures).

**What changed:**
- StatefulSet strategy: OnDelete → RollingUpdate
- Decommission/detention API calls: controller → pod preStop hook
- PodDisruptionBudgets: none → per-cluster PDB with `minAvailable = replicas - 1`
- Pod lifecycle: no finalizer → `splunk.com/pod-cleanup` finalizer for post-termination cleanup
- Intent communication: not possible → `splunk.com/pod-intent` annotation via Downward API volume

**The three-phase lifecycle:**
- Phase 1 (Operator): Detects desired state change. Sets intent annotation on pods being removed. Updates StatefulSet.
- Phase 2 (Pod / preStop hook): Pod receives termination signal. Hook reads intent from `/etc/podinfo/intent`. Calls role-appropriate Splunk API (decommission or detention) with parameters determined by intent. Calls `splunk stop`.
- Phase 3 (Finalizer / Operator): After container exit, operator finalizer handler runs. For indexers on scale-down: calls `remove_peers` on cluster manager, deletes PVCs. Removes finalizer.

**Timeout budget model:** How `terminationGracePeriodSeconds`, `DECOMMISSION_MAX_WAIT`, `STOP_MAX_WAIT`, and `TOTAL_BUDGET` relate to each other. Include a timeline diagram (PlantUML) showing the budget split for an indexer.

**Intent-aware decommission:** Why `enforce_counts=0` is correct for restart and `enforce_counts=1` is correct for scale-down.

**Why a Downward API volume, not an env var:** Env vars are frozen at pod start. Downward API volume files update live when annotations change.

Include PlantUML C4 and sequence diagrams (generated as PNG) for:
- Overall architecture (operator, StatefulSet, PDB, pod, finalizer handler, cluster manager)
- Scale-down sequence (all three phases, showing remove_peers and PVC deletion)
- Restart sequence (preStop with enforce_counts=0, no peer removal, no PVC deletion)
- Timeout budget allocation

### 2. User guide (`docs/` or `per-pod-rolling-restart-user-guide.md`)

A practical guide for operators deploying and operating this feature. Must be written for an audience that understands Kubernetes but may not have read the design document.

Sections:
- **Overview:** What this feature does and why it exists. One paragraph.
- **Default behaviour:** What happens out of the box with no configuration. Default `maxUnavailable: 1`, default PDB.
- **Configuring rolling updates:** How to set `rollingUpdateConfig.maxPodsUnavailable` and `rollingUpdateConfig.partition`. Include YAML examples for both percentage and absolute values. Explain canary deployments using partition.
- **Scale-down behaviour:** What happens when replicas are reduced. Timeline of events: annotation set → StatefulSet scaled → preStop runs decommission with enforce_counts=1 → finalizer removes peer and PVCs.
- **Restart behaviour:** What happens on config change (image update, env var change). Timeline: StatefulSet template updated → pods replaced serially → preStop runs decommission with enforce_counts=0 (fast) → pod rejoins with same PVCs.
- **Secret rotation:** How secret changes trigger rolling restarts via `restart_required` detection. What secrets trigger restarts. How to verify restart progress via `status.restartStatus`.
- **Monitoring restart progress:** How to read `status.restartStatus` from the CR. Example `kubectl get ingestorcluster -o yaml` output.
- **Tuning timeouts:** When to increase `terminationGracePeriodSeconds`. How to override `PRESTOP_DECOMMISSION_WAIT` and `PRESTOP_STOP_WAIT` via pod env vars.
- **Troubleshooting:** Common failure modes with diagnostic commands.

**Troubleshooting scenarios:**

| Symptom | Likely Cause | Diagnostic | Resolution |
|---|---|---|---|
| Pod stuck in Terminating for >20 minutes | preStop hook hanging or timeout too short | `kubectl logs <pod> -c splunk` before deletion | Check Splunk API reachability from within pod; verify `SPLUNK_CLUSTER_MANAGER_URL` is correct |
| PDB blocking rolling update | `minAvailable` too high relative to replicas | `kubectl describe pdb <name>` | Check if any pod is in a non-Ready state consuming the disruption budget |
| Peer appears as "Down" in cluster manager permanently | `remove_peers` call failed or finalizer never ran | `kubectl describe pod <pod>` check for finalizer; CM API | Check operator logs for `removeIndexerFromClusterManager` error |
| Rolling update stalled (no progress for 10+ minutes) | preStop hook not completing or PDB blocking all pods | `kubectl get sts <name>` check `updatedReplicas`; check PDB | Check pod logs during termination; check if PDB has `disruptedPods` |
| IngestorCluster stuck in Pending | Referenced Queue or ObjectStorage does not exist | `kubectl describe ingestorcluster <name>` | Create the missing Queue or ObjectStorage CR |
| Secret rotation not triggering restart | Secret version unchanged (content changed but no new Secret object) | Check `status.queueBucketAccessSecretVersion` | Trigger a touch of the Secret to update its ResourceVersion |

### 3. Custom Resources reference (`docs/CustomResources.md`)

Update the existing Custom Resources reference document to include:

- **IngestorCluster:** Full spec reference with all fields, types, defaults, and constraints. Note immutability of `queueRef` and `objectStorageRef`. Note `rollingUpdateConfig` inheritance from `CommonSplunkSpec`.
- **Queue:** Full spec reference. Note all immutable fields. Document `authRegion` pattern.
- **ObjectStorage:** Full spec reference. Note immutable `s3` block. Document `path` pattern (`s3://...`).
- **CommonSplunkSpec.rollingUpdateConfig:** New field documentation with examples.

### 4. Sample CRs (`config/samples/`)

Complete, working sample YAML files for every new and modified resource:

- `enterprise_v4_ingestorcluster.yaml` — IngestorCluster with Queue and ObjectStorage references, commented fields
- `enterprise_v4_queue.yaml` — Queue CR with all optional fields shown
- `enterprise_v4_objectstorage.yaml` — ObjectStorage CR
- `enterprise_v4_indexercluster.yaml` — Updated to show `rollingUpdateConfig` usage
- `enterprise_v4_c3_sva.yaml` — Updated C3 SVA sample showing ClusterManager + 3 IndexerClusters + SearchHeadCluster, demonstrating PDB and rolling update config

All samples must be valid against the CRD schema and deployable on a real cluster with appropriate secrets in place.

---

## Acceptance Criteria

1. `docs/per-pod-rolling-restart-design.md` exists, covers all three phases, explains the Downward API volume rationale, and includes PlantUML PNG diagrams.
2. The user guide exists and includes a troubleshooting table with at least 5 symptom/cause/resolution rows.
3. `docs/CustomResources.md` documents IngestorCluster, Queue, ObjectStorage, and `rollingUpdateConfig` with all fields, types, and constraints.
4. All sample YAML files are valid against their CRD schemas (`kubectl apply --dry-run=server` passes).
5. The `enterprise_v4_c3_sva.yaml` sample demonstrates use of `rollingUpdateConfig`.
6. Documentation does not use first-person ("we implemented", "we added"). It describes the system behaviour in present tense.
7. Troubleshooting commands in the user guide are accurate and tested against a real cluster.

---

## Definition of Done

- [ ] `docs/per-pod-rolling-restart-design.md` written with PlantUML diagrams.
- [ ] User guide written with all sections and troubleshooting table.
- [ ] `docs/CustomResources.md` updated for all new and modified types.
- [ ] All sample CRs written and validated.
- [ ] `config/samples/kustomization.yaml` updated to include new samples.
- [ ] Peer review by at least one person who was not involved in writing the feature code.
