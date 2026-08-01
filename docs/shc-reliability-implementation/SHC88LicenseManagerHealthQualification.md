# SHC-88 LicenseManager Health Endpoint Qualification

## Purpose

This record explains and proves the bounded SHC-88 correction. The Splunk
Operator checks a LicenseManager's license inventory so it can report an
expired license. Before this work, that check addressed the LicenseManager Pod
through a Kubernetes DNS name that depended on a headless Service which the
LicenseManager reconciler did not create. The check therefore failed before it
reached Splunk, logged a DNS error, skipped license evaluation, and allowed the
normal workload phase calculation to continue.

After this correction, the StatefulSet's declared network identity exists, the
Operator waits for the exact Pod to be Kubernetes Ready, and a successful REST
response is the only input allowed to produce a `LicenseExpired` Event. A
temporary DNS or REST failure remains retryable and appears as one aggregating
Kubernetes Warning Event series rather than a false expired-license result or
a terminal condition.

## Result

Bounded OPS-013/SHC-88 passes source and EKS qualification for a healthy
LicenseManager and for retryable endpoint failure. Exact Operator source
`241ea3d91901748c5bf60247ae8fd67e33b60653` created the previously absent
headless Service, resolved the exact Pod FQDN, and received HTTP 200 responses
from `/services/licenser/licenses?output_mode=json`.

A clean Operator restart then proved stable reconciliation: the headless
Service UID and final LicenseManager Pod UID remained unchanged, the new
controller completed three successful HTTP 200 checks, the failure Event count
did not increase, and every managed Splunk tier remained Ready. The
expired-license branch passed unit coverage but was not exercised by installing
an intentionally expired license on EKS.

## Baseline facts

The observed LicenseManager StatefulSet declared:

    spec.serviceName: splunk-shc85-license-manager-headless

Only this regular Service existed:

    splunk-shc85-license-manager-service

The existing health implementation constructed this exact Pod address:

    splunk-shc85-license-manager-0.splunk-shc85-license-manager-headless.shc85-lifecycle-hold.svc.cluster.local

Resolution from the Operator Pod failed. The accepted pre-fix log at
`2026-08-01T02:57:57Z` reported `no such host` for that address. Resolution of
the regular Service succeeded, and resolution from inside the LicenseManager
Pod itself was not accepted as proof because a Pod can resolve its own hostname
locally. The cross-Pod controller lookup was the relevant network path.

This was an Operator resource-contract mismatch. The Splunk management handler
was not reached, so no Docker-Splunk or Splunk Enterprise change was justified
for this item.

## Accepted contract

The accepted design has five parts.

First, the LicenseManager reconciles both Services. The regular Service remains
the stable load-balanced address used by other managed tiers. The headless
Service is the stable per-Pod identity named by the StatefulSet.

Second, the headless Service is reconciled before the StatefulSet and before
the health request. It has `clusterIP: None`, selects the LicenseManager Pod,
and is controlled by the LicenseManager custom resource.

Third, `Running` is not sufficient to authorize a management request. The Pod
must have the Kubernetes `PodReady=True` condition. This prevents expected
startup and replacement intervals from being reported as license-health
transport faults.

Fourth, a transport or HTTP failure logs the detailed error and emits the
stable reason `LicenseHealthCheckFailed` with a stable message. Kubernetes
aggregates repeated occurrences on one Event object. The check returns without
creating a terminal error or a false `LicenseExpired` Event, and normal
reconciliation can retry.

Fifth, a successful response is parsed. Only a returned license whose Splunk
status is `EXPIRED` emits `LicenseExpired`.

## Source qualification

The isolated branch is `codex/shc-88-license-health`. The source commit is
`241ea3d91901748c5bf60247ae8fd67e33b60653`.

Focused tests first failed because the LicenseManager Apply path did not create
the named headless Service and because no health-failure Event reason existed.
After the correction, focused Apply and health-check tests passed. They cover:

- creation and update of both LicenseManager Services;
- skipping a Pending Pod and a Running-but-not-Ready Pod;
- errors for an absent namespace Secret and an empty admin password once the
  Pod is Ready;
- retryable REST failure with `LicenseHealthCheckFailed` and no returned error;
- a successful expired response with `LicenseExpired`; and
- a successful valid response with no Warning Event.

The exact source then passed on the Linux vWorkstation:

- `make test`: 41 suites, 156 specs, zero failures, 78.6 percent composite
  coverage;
- `make build`;
- generated manifests, deepcopy generation, formatting, and `go vet` through
  those Make targets; and
- `git diff --exit-code` after generation and build.

The same focused tests and `make test-unit`, `make fmt`, `make vet`, and
`make build` also passed in the isolated local worktree.

## Immutable EKS inputs

- Date: 2026-08-01 UTC.
- EKS cluster: `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`.
- Namespace: `shc85-lifecycle-hold`.
- Operator source: `241ea3d91901748c5bf60247ae8fd67e33b60653`.
- Operator image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-88-241ea3d91`.
- Operator image digest:
  `sha256:545910a6b769ad399fea42fdb31ddb79af11d38b5e5691ed3a59786a7606180e`.
- Splunk runtime digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.
- LicenseManager custom resource: `shc85`.
- LicenseManager StatefulSet and Pod:
  `splunk-shc85-license-manager` and
  `splunk-shc85-license-manager-0`.

The image was built and pushed with the repository's `make docker-buildx`
target for `linux/amd64`. Qualification updated only the existing Operator
Deployment and pinned the digest. It did not use `make deploy`, because that
target's uninstall dependency deletes installed Splunk custom-resource
definitions and is not an acceptable live-fixture upgrade action.

## EKS evidence

### Service creation and first successful request

At `2026-08-01T04:14:39Z`, the new Operator created
`splunk-shc85-license-manager-headless`. The Service had:

- UID `42512aa1-ba9d-4919-88bd-9dee4909fc92`;
- `clusterIP: None`;
- owner `LicenseManager/shc85`; and
- a ready EndpointSlice entry for the then-current LicenseManager Pod.

The existing LicenseManager Pod retained UID
`18935400-c5f8-4b2c-9396-8d4b8931ed08` and zero container restarts when the
Service appeared. All Search Head, indexer, Cluster Manager, Deployer, and
LicenseManager Pods were Ready and retained zero restarts.

The first per-Pod lookup raced Kubernetes DNS publication and returned
`no such host`. This produced the expected retryable
`LicenseHealthCheckFailed` Warning. Subsequent reconciles reached Splunk from
the same Operator Pod, and `splunkd_access.log` recorded HTTP 200 responses at
`04:14:39Z`.

This race is why Service creation alone must not make a failed lookup terminal.
The accepted behavior is eventual resolution plus a bounded, aggregating
diagnostic during propagation.

### Readiness behavior during a qualification-induced replacement

A proposed no-op test used a temporary annotation on the LicenseManager custom
resource to generate repeated reconciles. That assumption was wrong: parent
annotations are copied into the StatefulSet Pod template. Changing and then
removing the annotation created StatefulSet revisions and one same-version
LicenseManager replacement.

This replacement is not attributed to SHC-88. It is retained in the evidence
because it exercised the new readiness boundary. The five retryable lookup
failures between `04:14:39Z` and `04:15:58Z` were aggregated on one
`LicenseHealthCheckFailed` Event object with count five. Once Kubernetes made
the replacement unready, the Operator logged `pod not ready, skipping license
check` and stopped management REST attempts until readiness recovered.

The replacement converged as:

- final Pod UID `60ba6aef-10da-41a1-a947-9e75efaf36bf`;
- Pod IP `10.0.75.167`;
- `PodReady=True` and ready EndpointSlice publication;
- zero Kubernetes container restarts;
- Ansible recap `ok=95`, `failed=0`; and
- HTTP 200 license response at `04:17:37Z`.

The test annotation was removed. Future replay must not mutate parent custom-
resource metadata merely to trigger reconciliation.

### Stable controller restart

The Operator Deployment was restarted after the LicenseManager and DNS path
were stable. The replacement controller used Pod IP `10.0.87.202` and the
same immutable Operator digest.

The restart produced three HTTP 200 license requests at `04:18:48Z`. Its logs
contained no `no such host`, `failed to get license information`, or
`LicenseHealthCheckFailed` message. The prior Event object remained at count
five with last occurrence `04:15:58Z`.

The following identities remained stable across this clean reconciliation:

- headless Service UID `42512aa1-ba9d-4919-88bd-9dee4909fc92` and resource
  version `10728486`; and
- LicenseManager Pod UID `60ba6aef-10da-41a1-a947-9e75efaf36bf`, Ready, with
  zero restarts.

The Cluster Manager, all four indexers, the Deployer, and all three Search
Heads retained their pre-test UIDs and zero restarts. The LicenseManager,
ClusterManager, IndexerCluster, and SearchHeadCluster custom resources all
reported Ready.

## Acceptance assessment

| Assertion | Evidence | Result |
|---|---|---|
| StatefulSet's named Service exists | Headless Service created with `clusterIP: None` and LicenseManager owner | Pass |
| Exact Pod DNS resolves from controller | Controller `getent hosts` returned the ready LicenseManager Pod IP | Pass |
| Splunk endpoint is reached | `splunkd_access.log` recorded HTTP 200 management requests | Pass |
| Normal startup is not called a health fault | Live replacement logged PodReady skip until EndpointSlice readiness | Pass |
| Transport failure is visible and retryable | One Event object aggregated five occurrences; no terminal error | Pass |
| Stable reconcile is idempotent | Operator restart retained Service and Pod UIDs and added no failure | Pass |
| Valid license does not emit expiration | Healthy EKS response emitted no `LicenseExpired` | Pass |
| Expired response emits expiration | Focused unit response emitted `LicenseExpired` | Source pass; not run with expired EKS license |
| Other Splunk tiers remain stable | All non-LicenseManager UIDs unchanged, Ready, zero restarts | Pass |

## Safe replay

Run replay against a disposable or explicitly approved namespace. First prove
the StatefulSet and Services agree:

    kubectl -n <namespace> get statefulset <license-manager-statefulset> \
      -o jsonpath='{.spec.serviceName}{"\n"}'
    kubectl -n <namespace> get service \
      <license-manager-headless-service> \
      <license-manager-regular-service>

From the Operator Pod, resolve the exact Pod FQDN. Do not use a lookup from
inside the target LicenseManager Pod as the only evidence:

    kubectl -n <operator-namespace> exec <operator-pod> -- \
      getent hosts <pod>.<headless-service>.<namespace>.svc.cluster.local

Verify the EndpointSlice target UID and readiness agree with the current Pod:

    kubectl -n <namespace> get endpointslice \
      -l kubernetes.io/service-name=<headless-service> -o yaml

Inspect the Operator logs and LicenseManager access log for the same time
window:

    kubectl -n <operator-namespace> logs <operator-pod>
    kubectl -n <namespace> exec <license-manager-pod> -- \
      grep 'services/licenser/licenses' \
      /opt/splunk/var/log/splunk/splunkd_access.log

For an idempotent reconcile test, restart only the Operator Deployment and
verify its rollout. Do not annotate the LicenseManager custom resource because
parent metadata can change the StatefulSet Pod template.

## Remaining boundaries

This result itself did not close SHC-86 namespace-first LicenseManager
finalization; that separate requirement was later qualified and is recorded in
[SHC86LicenseManagerNamespaceFinalizationQualification.md](SHC86LicenseManagerNamespaceFinalizationQualification.md).
It does not close SHC-87 referenced-tier dependency status or qualify an expired
production license on EKS, dual-stack DNS, service-mesh interception, custom
management TLS, a network partition, or repeated LicenseManager Pod failure.
Those are independent test cases.

No Docker-Splunk or Splunk Enterprise source change was needed for the
demonstrated DNS mismatch. If a future run reaches Splunk successfully but the
license endpoint is slow, unavailable, or semantically incorrect, that is a
different boundary and must be attributed from the HTTP response and Splunk
logs rather than folded into this Kubernetes Service correction.
