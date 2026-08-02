# SHC-92 Namespace-Scoped Helm Qualification

## Result

SHC-92 defines and qualifies one effective-namespace contract for the Splunk
Operator Helm chart. The effective namespace is the top-level
`namespaceOverride` value when it is non-empty, and the Helm release namespace
otherwise. In namespace-scoped mode, every chart-owned namespaced resource,
the service-account identity, the leader-election lease, the narrowly scoped
Namespace reader, and `WATCH_NAMESPACE` now agree on that namespace.

Exact source `91f742b52b0e3483ff8a156189e64b1914e38ecd` passed the full macOS
and Linux source, build, lint, and Helm gates. The exact packaged chart used on
EKS was:

```text
file: splunk-operator-3.1.0.tgz
size: 10369 bytes
sha256: 23258a699126ae318fee287a5734d939521f3d32ef8741f936ff44b31ef9b5b8
```

The EKS campaign reproduced the previous false-Ready failure, upgraded that
same Helm release without manual RBAC edits, qualified fresh default and
overridden installations, ran two releases from one Helm release namespace
with distinct effective namespaces, and completed normal Helm and Kubernetes
cleanup. This is bounded chart qualification; it is not a claim for untested
providers, Kubernetes versions, arbitrary changes to `namespaceOverride`
during an upgrade, or overlapping Operator watch scopes.

## Contract

The supported chart behavior is:

- an empty `namespaceOverride` makes the release namespace the effective
  namespace;
- a non-empty `namespaceOverride` makes that value the effective namespace;
- when `splunkOperator.clusterWideAccess=false`, the Operator watches exactly
  the effective namespace;
- all chart-owned namespaced resources explicitly use the effective namespace;
- the namespace-scoped service account receives manager and leader-election
  permissions only in the effective namespace;
- the Namespace-reader ClusterRole allows only `get` for the effective
  Namespace through one `resourceNames` entry;
- the Namespace-reader ClusterRole and ClusterRoleBinding names hash the
  effective namespace, allowing independent releases with different effective
  namespaces to coexist; and
- when `splunkOperator.clusterWideAccess=true`, `namespaceOverride` controls
  namespaced installation placement while `splunkOperator.watchNamespaces`
  independently controls watch scope.

Helm still stores the release record in the namespace passed through `-n`.
The override namespace must already exist because Helm's
`--create-namespace` option creates only the release namespace.

## Source qualification

The test-first run used the unchanged preceding chart and produced the exact
bounded failure set: 10 tests failed and 38 passed across 12 suites. The
failures covered namespace-scoped `WATCH_NAMESPACE`, seven critical
namespaced-object placements, and Namespace-reader scope and naming.

After the correction, the focused Operator chart gate passed 16 suites and 52
tests. It covers the Deployment, service account, telemetry ConfigMap, App
Framework PVC, controller and proxy Services, manager and leader Roles and
RoleBindings, Namespace reader, and representative editor/viewer Roles. A
complete adversarial render also confirmed that every rendered namespaced
object names the effective namespace and that cluster-scoped objects do not
carry `metadata.namespace`.

Both macOS and Linux passed:

- 42 Ginkgo suites;
- all 185 enterprise-controller specs;
- zero failures;
- 78.3 percent composite statement coverage;
- `make build` including generation, formatting, vet, and manager compilation;
- both Helm lints; and
- `make helm-check` with 52 Operator and 85 Universal Forwarder tests.

The Linux host emitted a warning that its cached Ginkgo CLI was older than the
package version. The complete suite still executed successfully with no flag
or test failure; this warning is a host-tool-cache issue rather than SHC-92
product evidence.

## Previous live failure

The preceding SHC-91 chart at source `a76c30e0c2395506cbfbb8d9e2643c186df0a3ef`
was installed as release `shc92-upgrade` in `shc92-old-release`, with
`namespaceOverride=shc92-old-watch` and namespace-scoped access.

The rendered state was internally inconsistent:

- the Deployment, service account, and App Framework PVC were in
  `shc92-old-watch`;
- `WATCH_NAMESPACE` was `shc92-old-release`;
- all 25 Roles and both RoleBindings were in `shc92-old-release`;
- the controller Service was also in the release namespace; and
- Namespace-reader `splunk-operator-namespace-reader-b0af0495` allowed only
  `get` for `shc92-old-release`.

Kubernetes reported the Pod `1/1 Running` and the Deployment Available because
the health probe endpoint was alive. The manager was not operational: it
repeatedly failed to read its leader Lease in `shc92-old-watch` with a
Forbidden response, never acquired leadership, and never started its
controllers. This demonstrates why Pod readiness alone was misleading for
this chart defect.

The pre-upgrade service-account authorization matrix was:

```text
                                      shc92-old-watch   shc92-old-release
get leader Lease                              no                 yes
get enterprise.splunk.com Standalone          no                 yes
get Namespace                                 no                 yes
```

No RBAC object or Deployment environment value was manually patched.

## In-place recovery

Helm upgraded `shc92-upgrade` from revision 1 to revision 2 using the exact
packaged SHC-92 chart and `--reuse-values`. The manager image remained the
already qualified SHC-91 digest:

```text
sha256:4903f70a95b150c0a29bcd3ac70e063b5c55b6a030399a4636297586dea85cea
```

The upgrade preserved the existing installation identities:

```text
Deployment UID:     8532104a-a239-477d-bc4c-b291bcd2cbd3
ServiceAccount UID: e15f25e4-2a93-4017-a9b5-8c0294db905b
```

It then produced the intended state:

- `WATCH_NAMESPACE=shc92-old-watch`;
- all 25 Roles and both RoleBindings in `shc92-old-watch`;
- zero Roles or RoleBindings left in `shc92-old-release`;
- reader `splunk-operator-namespace-reader-5a5312bf` restricted to
  `shc92-old-watch`;
- the old release-derived reader absent;
- the leader Lease acquired in `shc92-old-watch`; and
- every enterprise controller started.

The authorization matrix was exactly reversed: Lease, Splunk custom-resource,
and Namespace access were allowed for `shc92-old-watch` and denied for
`shc92-old-release`. The recovery required only the Helm upgrade.

## Fresh installs and non-collision

A fresh default release in `shc92-default` left `namespaceOverride` empty. Its
Deployment watched `shc92-default`, acquired the Lease there, and used reader
`splunk-operator-namespace-reader-700f8bca`, restricted to that Namespace.

A fresh overridden release placed all namespaced resources in
`shc92-new-watch`, watched that namespace, acquired its Lease there, and used
reader `splunk-operator-namespace-reader-98ad5634`. Its release record was
first installed in a separate release namespace to qualify fresh install and
normal uninstall cleanup.

The second release was then installed again as `shc92-peer` in the same Helm
release namespace as `shc92-upgrade`. Both releases coexisted in
`shc92-old-release` while their effective namespaces and readers remained
independent:

```text
release          effective namespace    Namespace reader suffix
shc92-upgrade    shc92-old-watch         5a5312bf
shc92-peer       shc92-new-watch         98ad5634
```

Each service account could get Splunk custom resources and its Namespace only
in its own effective namespace. Cross-access to the other effective namespace
and to the Helm release namespace was denied.

Paused Standalone probes in each effective namespace reached current-generation
`Pending/Paused` status without creating a Splunk workload. A pre-existing
cluster-wide Operator also watched the disposable namespaces, so status writer
ownership was intentionally not used as proof that one particular Operator
instance performed the update. The instance-specific evidence is instead the
rendered and live `WATCH_NAMESPACE`, successful effective-namespace leader
election, controller startup, and Kubernetes authorization denials outside the
effective namespace. Production deployments should not configure overlapping
watch scopes because independent controllers can race on the same object.

## Kubernetes-version boundary

The exact packaged chart rendered successfully with Helm v3.18.4
`--kube-version`
values `1.27.0` and `1.31.14`. The cluster-wide render placed the Deployment in
`shc92-installation` while preserving
`WATCH_NAMESPACE="team-a,team-b"`, confirming that cluster-wide watch scope
remains independent of installation placement.

Live qualification used EKS server
`v1.31.14-eks-8f14419` on context
`arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`. Kubernetes 1.27
evidence is render-only. No other provider or live Kubernetes version is
claimed by this record.

## Cleanup and retained-workload invariant

Normal `helm uninstall --wait` removed the fresh, peer, upgraded, and default
releases. It removed every chart-owned namespaced object and all three
Namespace-reader ClusterRoles and ClusterRoleBindings. The upgraded release's
bound 10 GiB App Framework PVC was deleted and its delete-reclaim PV
`pvc-cbccc585-93e3-444f-9797-cbf1bfa49c0d` disappeared naturally. No manual
Kubernetes finalizer or RBAC cleanup was used.

The five disposable namespaces were deleted. Final checks found no SHC-92
Helm release, Namespace, Standalone probe, Namespace reader, PVC, PV, or PV
claim reference. The retained cluster-wide Operator remained one of one
Available on the exact SHC-91 image digest with zero Pod restarts. The retained
SHC-85 LicenseManager, ClusterManager, four indexers, deployer, and three
Search Heads all remained Running and Ready with zero restarts; its workload
Job remained successfully completed.

## Remaining boundaries

SHC-92 does not:

- change a Go API, CRD, controller, StatefulSet, Splunk Enterprise, or
  Docker-Splunk runtime interface;
- build or qualify a new manager image, because the executable did not change;
- define migration behavior for changing a non-empty `namespaceOverride` to a
  different namespace during an ordinary upgrade;
- alter Helm's release-record namespace;
- control the namespace of user-supplied `extraManifests`;
- make overlapping Operator watch scopes safe; or
- claim live provider/version coverage beyond the recorded EKS 1.31 cluster.

Ordinary upgrades should retain the existing effective namespace. Moving an
installation to a different effective namespace needs a separately designed
migration and rollback contract.
