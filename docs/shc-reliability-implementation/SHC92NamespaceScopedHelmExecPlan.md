# Make namespace-scoped Helm placement and watch scope agree

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

A customer can install the Splunk Operator Helm chart in namespace-scoped mode
by setting `splunkOperator.clusterWideAccess=false`. The chart also accepts a
global `namespaceOverride` value that is intended to place chart resources in a
namespace different from the Helm release namespace. Today those controls do
not compose: the Deployment and service account move to the override, but the
Operator watches the release namespace and multiple Roles, RoleBindings, and a
Service stay there. The service account therefore lacks the permissions needed
where the Pod runs, while the Namespace reader is restricted to a different
namespace from the effective installation.

After SHC-92, `namespaceOverride` has one documented meaning: it is the
effective namespace for every namespaced resource rendered by the Operator
chart. In namespace-scoped mode, that same effective namespace is the one and
only watched namespace and the one named by the narrowly scoped Namespace
reader. A user can render, install, upgrade, and uninstall this configuration
without manually moving RBAC or editing `WATCH_NAMESPACE`.

Cluster-wide mode retains its existing independent watch contract:
`splunkOperator.watchNamespaces` controls which namespaces are watched, with
an empty value meaning all namespaces, while `namespaceOverride` only changes
where the Operator's namespaced installation resources live.

## Progress

- [x] (2026-08-01 23:46Z) Created isolated worktree
  `/Users/viveredd/Projects/splunk-operator-shc92-namespace-scoped-helm` and
  branch `codex/shc-92-namespace-scoped-helm` from qualified SHC-91
  documentation tip `88e3b423a`.
- [x] Audited current Helm rendering for namespace-scoped mode with release
  namespace `release-namespace` and override `watched-namespace`.
- [x] Selected the effective-namespace contract described above.
- [x] (2026-08-01 23:49Z) Added test-first Helm assertions for watch scope,
  critical namespaced object placement, Namespace-reader restriction/naming,
  and representative user Roles.
- [x] Recorded the unchanged-source result: 10 tests failed and 38 passed
  across 12 suites. The failures were exactly the namespace mismatch; existing
  cluster-wide and already-correct effective-namespace tests remained green.
- [x] (2026-08-01 23:53Z) Implemented the effective-namespace contract across
  values, watch environment, Namespace-reader scope/name, every namespaced
  Role/RoleBinding/Service, and user-facing Helm documentation.
- [x] The corrected focused gate passes 16 suites and 52 tests. A complete
  render contains no mismatched namespaced object or namespace on a
  cluster-scoped object; two effective namespaces produce distinct reader
  names; cluster-wide placement and `watchNamespaces` remain independent.
- [x] (2026-08-01 23:57Z) Passed the full macOS source gate: 42 Ginkgo
  suites, including 185 enterprise-controller specs, completed with zero
  failures and 78.3% composite statement coverage.
- [x] (2026-08-02 00:00Z) Passed macOS `make build` and final `make
  helm-check`; the latter passed both chart lints and all 52 Operator plus 85
  Universal Forwarder Helm tests.
- [x] (2026-08-02 00:12Z) Passed the same Linux source, build, and chart
  gates. Packaged the exact 10,369-byte Operator chart with SHA-256
  `23258a699126ae318fee287a5734d939521f3d32ef8741f936ff44b31ef9b5b8`.
- [x] (2026-08-02 00:20Z) Qualified fresh default and overridden
  namespace-scoped installations,
  cross-namespace isolation, two-install non-collision, upgrade from the prior
  inconsistent rendering, uninstall cleanup, and retained-workload health on
  EKS.
- [x] Committed and pushed source separately as `91f742b52`; qualification
  documentation remains the final separate commit.
- [x] Prepared the qualification record and project-index updates as the
  separate final documentation commit for official GitLab remote `sok`.

## Surprises & Discoveries

- Observation: the mismatch is broader than `WATCH_NAMESPACE`.
  Evidence: an unchanged-source `helm template` placed the Deployment, service
  account, telemetry ConfigMap, and metrics Service in `watched-namespace`, but
  emitted the manager, leader-election, proxy, editor, and viewer Roles plus
  manager and leader-election RoleBindings and the controller-manager Service
  without explicit namespaces. Helm therefore installs them in
  `release-namespace`.
- Observation: the SHC-90 Namespace reader follows the old watch target rather
  than the existing placement helper.
  Evidence: its `resourceNames` contains `.Release.Namespace`, and its
  cluster-scoped name hashes `.Release.Namespace`, even though its binding
  subject correctly refers to the service account in `namespaceOverride`.
- Observation: the global `namespaceOverride` value is consumed by templates
  and tested but is not declared or documented in the Operator chart's
  `values.yaml`.
  Evidence: the helper reads `.Values.namespaceOverride`, while `rg` finds no
  value definition in that file.
- Observation: two releases using the same release namespace and different
  effective namespaces would generate the same Namespace-reader ClusterRole
  and ClusterRoleBinding names.
  Evidence: the current name hashes only `.Release.Namespace`.
- Observation: the first test-first run produced a bounded failure set rather
  than chart-wide noise.
  Evidence: `helm unittest helm-chart/splunk-operator` reported 10 failures and
  38 passes. Missing `metadata.namespace` accounted for seven failures;
  namespace-scoped `WATCH_NAMESPACE` accounted for one; and reader name/scope
  accounted for two.
- Observation: no new watch setting is needed.
  Evidence: after applying the existing `splunk-operator.namespace` helper at
  each boundary, the complete adversarial render passed one aggregate object
  placement assertion and preserved `installation-namespace ns1,ns2` for a
  cluster-wide render.
- Observation: the chart-only change did not perturb the Go source baseline.
  Evidence: the full macOS `make test` gate passed 42 suites, including all
  185 enterprise-controller specs, with the same 78.3% composite coverage as
  the qualified parent; `make build` also passed.
- Observation: the old split rendering can look healthy to Kubernetes while
  no controller is running.
  Evidence: the pre-fix EKS Pod was `1/1 Running` and its Deployment was
  Available because the health endpoint responded, but the manager repeatedly
  received Forbidden for its leader Lease in the override namespace. It never
  acquired leadership or started controllers.
- Observation: changing the Namespace-reader hash during upgrade is naturally
  handled by Helm.
  Evidence: revision 2 removed release-derived reader `b0af0495`, created
  effective-namespace reader `5a5312bf`, and recovered the existing Deployment
  and service account without a manual patch.
- Observation: status on a shared EKS cluster cannot identify which of two
  overlapping Operators performed a write.
  Evidence: the retained cluster-wide Operator and disposable namespace-scoped
  Operators could both observe effective-namespace paused probes, producing
  expected status-update conflicts. SHC-92 therefore uses each instance's
  environment, leader Lease, startup logs, and negative service-account
  authorization checks as the authoritative scope evidence.

## Decision Log

- Decision: Define `effective namespace` as
  `namespaceOverride` when non-empty, otherwise `.Release.Namespace`.
  Rationale: this matches the existing helper and the ordinary meaning of a
  chart-wide namespace override.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: In namespace-scoped mode, watch exactly the effective namespace.
  Rationale: a namespaced Role grants permissions only in its own namespace;
  splitting Pod placement, RBAC placement, and watch target would require a
  second explicit product concept and a more complex cross-namespace binding
  contract that the chart does not expose.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: In cluster-wide mode, preserve `watchNamespaces` independently of
  installation placement.
  Rationale: existing users rely on empty-for-all or a comma-separated watch
  list, and cluster-wide RBAC already permits that behavior.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Derive Namespace-reader names and `resourceNames` from the
  effective namespace.
  Rationale: the permission must match the watched Namespace, and independent
  namespace-scoped installations need distinct cluster-scoped object names.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Treat this as a chart-only runtime change and reuse the exact
  already-qualified SHC-91 Operator image digest for live tests.
  Rationale: rebuilding an unchanged manager binary would add a new artifact
  without testing a different executable behavior. The SHC-92 immutable
  artifact is the packaged Helm chart plus its digest.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Do not scale down or mutate the retained cluster-wide Operator to
  manufacture exclusive status-writer evidence.
  Rationale: `WATCH_NAMESPACE`, leader-election namespace, controller startup,
  and Kubernetes RBAC denials prove the bounded instance contract without
  interrupting the retained installation. Overlapping watch scopes remain an
  explicit unsupported operational topology rather than an SHC-92 claim.
  Date/Author: 2026-08-02, Codex with Vivek Reddy.

## Outcomes & Retrospective

SHC-92 is source-, chart-, Linux-, and EKS-qualified for the bounded
effective-namespace contract. The test-first boundary was exact, both host
gate sets passed, and the packaged chart recovered a real pre-fix false-Ready
Operator through an ordinary Helm upgrade. Fresh default and overridden
installations acquired leadership in the correct namespace. Two releases
stored in the same Helm release namespace coexisted with independent effective
namespaces and reader names. Natural uninstall and namespace deletion left no
fixture or storage claim while the retained Operator and SHC stayed healthy.

The most important diagnostic finding is that the old Deployment's readiness
probe did not prove leader election or controller startup. That broader probe
semantics question is not changed by this chart-only work, but the exact old
failure and its visible log signature are retained in the qualification
record. The work also made the overlapping-watch boundary explicit: SHC-92
supports multiple disjoint namespace-scoped releases, not independent
controllers racing over the same custom resources.

## Context and Orientation

Helm stores a release record in the namespace passed with `helm install -n`;
this is the release namespace. A rendered namespaced Kubernetes object may
explicitly set a different `metadata.namespace`; if it omits the field, Helm
installs it in the release namespace. The Operator chart's helper
`splunk-operator.namespace` already returns `namespaceOverride` when set and
the release namespace otherwise. This plan calls that result the effective
namespace.

The chart is under `helm-chart/splunk-operator`. `values.yaml` defines
`splunkOperator.clusterWideAccess` and `splunkOperator.watchNamespaces`.
`templates/deployment.yaml` renders the `WATCH_NAMESPACE` environment
variable. `templates/_helpers.tpl` defines namespace and naming helpers.
Namespaced access is rendered by `templates/rbac/role.yaml`, the editor/viewer
Role templates, the leader-election Role and RoleBinding, and related Services.
SHC-90 added `templates/rbac/namespace_reader_clusterrole.yaml` and its binding
because Namespace is cluster-scoped even when the Operator watches only one
namespace.

The existing chart behavior is:

- Deployment and service account use the effective namespace;
- namespace-scoped `WATCH_NAMESPACE` uses the release namespace;
- manager and leader-election RBAC without explicit namespace lands in the
  release namespace;
- several other namespaced Role and Service objects also land in the release
  namespace;
- Namespace-reader `resourceNames` and name hashing use the release namespace;
  and
- the reader binding subject uses the effective-namespace service account.

These fields cannot describe one working namespace-scoped installation when
the override differs from the release namespace.

## Plan of Work

Add Helm unit tests before template edits. Extend `tests/deployment_test.yaml`
so namespace-scoped `WATCH_NAMESPACE` must equal the effective namespace.
Change the Namespace-reader tests so both the permission and cluster-scoped
object name derive from the override. Add a focused namespace-scope suite that
asserts the manager Role/RoleBinding, leader-election Role/RoleBinding,
controller-manager Service, and representative editor/viewer Roles carry the
effective namespace. Retain tests proving cluster-wide `watchNamespaces`
behavior is unchanged.

Run those focused suites on unchanged source and record their failures. Also
render the complete chart with `helm template` and parse every object. The
unchanged manifest must demonstrate that some namespaced objects still default
to the release namespace and that the reader points there.

Implement the contract in `values.yaml`, `_helpers.tpl`, `deployment.yaml`, and
every namespaced template. Use the existing namespace helper; do not introduce
a second watch-namespace value. Add explicit `metadata.namespace` only to
objects whose rendered kind is namespaced. Conditional editor/viewer templates
must add it only in their `kind: Role` branch, never to a ClusterRole.

Update `docs/deploy/Helm.md` with the contract and a namespace-scoped override
example. Explain that the release record remains in the Helm release namespace
while all namespaced chart resources and the watch target move to the effective
namespace. Explain that `watchNamespaces` applies only when cluster-wide access
is enabled.

Run `make helm-check`, full `make test`, and `make build` on macOS. Commit the
chart and user documentation as one source change. Push it, fetch the exact
commit on the Linux vWorkstation, and repeat all three Make gates. Package the
chart with the repository Make target or canonical `helm package` command and
record its SHA-256 digest.

On EKS, leave the existing cluster-wide Operator and
`shc85-lifecycle-hold` read-only. Use disposable release and effective
namespaces. First install the preceding SHC-91 chart in namespace-scoped mode
with a differing override and record the expected false-Ready
leader-election/RBAC failure. Upgrade
that same Helm release to the SHC-92 chart and prove the same Deployment and
service account identities recover without manual RBAC or environment edits.

Install a second corrected release using a different effective namespace.
Prove both reader names are distinct and each is restricted to its own
Namespace. Create paused v4 resources in the watched namespaces without
attributing shared status writes to one controller when a retained
cluster-wide Operator overlaps the fixture. Use the scoped manager's
environment, leader Lease, controller-start logs, and service-account
authorization matrix as instance-specific evidence. Uninstall both releases,
delete the disposable namespaces, and verify no namespaced resource,
cluster-scoped reader, PVC, PV claim reference, or Helm release remains.

## Concrete Steps

Work from:

    cd /Users/viveredd/Projects/splunk-operator-shc92-namespace-scoped-helm

Run focused Helm tests before and after the correction with:

    helm unittest helm-chart/splunk-operator \
      -f 'tests/deployment_test.yaml' \
      -f 'tests/namespace_reader*_test.yaml' \
      -f 'tests/*role*_test.yaml' \
      -f 'tests/service*_test.yaml'

Render the complete adversarial configuration with:

    helm template shc92 helm-chart/splunk-operator \
      --namespace release-namespace \
      --set splunkOperator.clusterWideAccess=false \
      --set namespaceOverride=watched-namespace

Final local and Linux gates are:

    make helm-check
    make test
    make build
    git diff --check

Package from the exact committed Linux checkout with:

    make helm-package

If the repository target packages more than the Operator chart or requires
release preparation, use `helm package helm-chart/splunk-operator` and record
the exact command and digest in this plan.

## Validation and Acceptance

Source acceptance requires the complete namespace-scoped render to satisfy:

- every namespaced object explicitly names the effective namespace;
- no namespaced object is left to Helm's release-namespace default;
- `WATCH_NAMESPACE` equals the effective namespace;
- manager, leader-election, proxy, editor, and viewer Roles are in the
  effective namespace when rendered as Roles;
- RoleBindings live in the effective namespace and reference the service
  account there;
- Namespace-reader `resourceNames` contains only the effective namespace;
- Namespace-reader names differ for distinct effective namespaces;
- ClusterRoles never contain `metadata.namespace`; and
- cluster-wide mode still uses `watchNamespaces` and does not render the
  namespace-scoped reader.

Live acceptance requires an old-to-new Helm upgrade to recover the expected
pre-fix RBAC failure without manual changes. The corrected Deployment must be
Available, its environment and service account must match the effective
namespace, and `kubectl auth can-i` for that service account must allow managed
resource and Namespace access only where intended. Two releases must coexist
without a cluster-scoped object-name collision. Each namespace-scoped service
account must be authorized only in its own effective namespace, and each
manager must acquire leadership and start controllers with that namespace as
its watch target. Uninstall must remove all chart-owned namespaced and
cluster-scoped objects. The existing SHC-91 Operator and retained SHC-85
workloads must remain Ready with zero restarts.

Rendering with Kubernetes version flags can prove manifest compatibility at
the documented 1.27 floor and the latest version accepted by the installed
Helm client, but live EKS evidence covers only the cluster's actual version.
The result must state that boundary rather than claiming untested providers or
versions.

## Idempotence and Recovery

Helm rendering and unit tests are read-only and repeatable. EKS fixtures use
unique disposable namespaces and release names. Do not install a second
cluster-wide Operator and do not modify the existing `splunk-operator` release
or `shc85-lifecycle-hold` resources.

If the old chart fixture cannot start, preserve Pod Events and authorization
evidence before upgrade; that failure is an expected baseline, not a reason to
patch RBAC manually. If a corrected release fails, collect the rendered
manifest, Helm status, Roles, RoleBindings, service-account authorization,
Deployment Events, and manager logs before cleanup. `helm uninstall` is the
normal rollback. Remove cluster-scoped reader objects manually only if Helm
itself demonstrably fails to clean an owned object, and record that as a failed
acceptance result rather than hiding it.

## Artifacts and Notes

Starting history:

    88e3b423a docs: record SHC-91 qualification
    a76c30e0c fix: finalize tier deletion before normal apply
    86a0bc80a fix: let paused resources complete deletion

Unchanged-source render summary for release namespace `release-namespace` and
override `watched-namespace`:

    Deployment, ServiceAccount, telemetry ConfigMap: watched-namespace
    manager/leader/editor/viewer Roles: release namespace by default
    manager/leader RoleBindings: release namespace by default
    controller-manager Service: release namespace by default
    WATCH_NAMESPACE: release-namespace
    Namespace-reader resourceNames: release-namespace
    Namespace-reader binding subject: watched-namespace ServiceAccount

Test-first unit result:

    Test Suites: 9 failed, 3 passed, 12 total
    Tests:       10 failed, 38 passed, 48 total

Corrected focused result:

    Test Suites: 16 passed, 16 total
    Tests:       52 passed, 52 total
    complete namespace-scoped render invariant: true
    target-one reader: splunk-operator-namespace-reader-73ada1eb
    target-two reader: splunk-operator-namespace-reader-fe9cbf12
    cluster-wide Deployment/watch: installation-namespace ns1,ns2

Final source and chart result:

    source commit: 91f742b52b0e3483ff8a156189e64b1914e38ecd
    macOS and Linux: 42 Ginkgo suites, 185 enterprise specs, 0 failures
    composite coverage: 78.3 percent
    Operator Helm tests: 52
    Universal Forwarder Helm tests: 85
    chart: splunk-operator-3.1.0.tgz, 10369 bytes
    chart SHA-256: 23258a699126ae318fee287a5734d939521f3d32ef8741f936ff44b31ef9b5b8

EKS result:

    server: v1.31.14-eks-8f14419
    old reader: b0af0495 -> shc92-old-release
    upgraded reader: 5a5312bf -> shc92-old-watch
    default reader: 700f8bca -> shc92-default
    peer reader: 98ad5634 -> shc92-new-watch
    same-release-namespace releases: shc92-upgrade, shc92-peer
    render-only versions: 1.27.0, 1.31.14
    cleanup: no release, namespace, reader, probe, PVC, PV, or PV claim

## Interfaces and Dependencies

No Go API, CRD, manager command-line flag, finalizer, StatefulSet, Splunk
Enterprise, Docker-Splunk, or persistent-data interface changes. The public
chart contract adds and documents top-level `namespaceOverride: ""` and
clarifies its relationship to existing
`splunkOperator.clusterWideAccess` and `splunkOperator.watchNamespaces`.

The implementation must continue to use the helper
`splunk-operator.namespace` as the single effective-namespace calculation.
The helper `splunk-operator.namespaceReaderName` must hash that effective
namespace. No broad Namespace list/watch permission is allowed; the
namespace-scoped reader remains `get`-only and restricted by one
`resourceNames` entry.

Revision note, 2026-08-01 23:46Z: created SHC-92 after reconnecting to the
Linux vWorkstation, verifying the qualified SHC-91/EKS baseline, rendering the
current chart, and selecting one effective-namespace contract for placement,
watch scope, RBAC, and reader naming.

Revision note, 2026-08-01 23:49Z: recorded the 10-failure/38-pass test-first
baseline and the exact affected namespace boundaries before template changes.

Revision note, 2026-08-01 23:53Z: recorded the effective-namespace chart and
documentation correction, 52 passing focused tests, complete render invariant,
reader uniqueness, and preserved cluster-wide behavior.

Revision note, 2026-08-02 00:12Z: recorded clean macOS and Linux gates, exact
source `91f742b52`, packaged chart digest `23258a699126`, and the pre-fix EKS
false-Ready leader-election failure followed by unpatched Helm recovery.

Revision note, 2026-08-02 00:20Z: recorded fresh default/override installs,
same-release-namespace non-collision, authoritative RBAC isolation evidence,
normal uninstall/PV cleanup, and the unchanged retained Operator and SHC
health invariant.
