// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package testenv

import "time"

// Per-test-case NodeTimeout tiers derived from observed JUnit durations.
// Each value is ≈1.5× the observed p95 maximum for that tier.
//
// Usage in test specs:
//
//	It("test name", NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) { ... })
const (
	// ShortTimeout for quick tests:
	// smartstore, indingsep, s1 appfw, deletecr s1,
	// crcrud s1, lmanager s1, smoke s1.
	ShortTimeout = 15 * time.Minute

	// MediumTimeout for moderate tests:
	// mc s1/m4, crcrud shc/PVC, lmanager c3,
	// secret s1, deletecr c3, most c3/m4 appfw, smoke m4,
	// indingsep resource-default opt-out (setup + 3-pod rolling restart).
	MediumTimeout = 45 * time.Minute

	// MediumLongTimeout for heavier tests:
	// m4appfw scale-up, crcrud c3, mc c3,
	// m4appfw install-local, crcrud m4, lmanager m4, smoke c3.
	MediumLongTimeout = 70 * time.Minute

	// LongTimeout for heavy tests:
	// secret m4, c3appfw image-upgrade variants.
	LongTimeout = 100 * time.Minute
)

// defaultTestTimeout is the max timeout in seconds before async test failed.
// Used as the default for the -test-timeout flag and SpecifiedTestTimeout.
const defaultTestTimeout = 5400

// DefaultTimeout is a backstop for infrastructure-level polls (namespace creation,
// operator deployment readiness, CR existence after create). These operations should
// complete in seconds; this value is a generous safety net.
const DefaultTimeout = 30 * time.Minute

// CertTimeout bounds the Eventually polls in certutil.go (pod running, secret
// populated, mount checks, cert-rev annotation, TLS handshake, rotation).
// These are all in-cluster ops that settle within a couple minutes even under
// load, so a dedicated tighter budget lets a genuine regression fail fast
// instead of consuming the full 30-minute DefaultTimeout meant for slower
// infrastructure-level polls.
const CertTimeout = 30 * time.Minute

// ReadinessPollTimeout is the per-attempt timeout for individual CR readiness
// polls inside Verify*Ready helpers. It must be shorter than DefaultTimeout so
// that an outer Eventually wrapper has room to retry on transient failures.
const ReadinessPollTimeout = 5 * time.Minute

// IndexerClusterReadyTimeout bounds VerifySingleSiteIndexersReady. On initial C3
// deploy under CI contention, peers can be SIGKILLed by the startup probe and need
// several restart cycles to converge; the 30m DefaultTimeout has expired seconds
// short (e.g. job 256831840). Callers with a shorter attemptCtx are unaffected.
const IndexerClusterReadyTimeout = 45 * time.Minute

// AppInstallTimeout is the timeout for waiting for apps to reach Install phase on a CR.
// C3 deployments require bundle push across all indexers and SHC deployer which can exceed
// 5 minutes; under nightly load the initial bundle push alone (gated by SHC/CM readiness
// flaps) has been observed to take up to ~4 minutes before the poll for the next phase
// change even starts, so 10 minutes leaves too little margin.
const AppInstallTimeout = 15 * time.Minute

// AppStateVerificationTimeout is the timeout for VerifyAppState polls that
// try to catch a transient app-framework phase (e.g. download-in-progress).
// M4 clusters need time to initialise before app processing begins, so this
// value is generous.
const AppStateVerificationTimeout = 60 * time.Minute

// MonitoringConsoleReadyTimeout is the outer Eventually budget used by tests
// that wait for the MonitoringConsole CR to reach the Ready phase. MC
// reconciliation can take longer than DefaultTimeout on M4 deployments
// while the operator settles SHC/IDXC peer registration; allow extra margin here.
const MonitoringConsoleReadyTimeout = 45 * time.Minute

// PasswordSyncEventTimeout is the budget for waiting for the
// PasswordSyncCompleted event on IndexerCluster / SearchHeadCluster CRs
// after a namespace-scoped secret update. The event is emitted shortly
// after the CR reaches Ready, so a small window is sufficient.
const PasswordSyncEventTimeout = 2 * time.Minute

// DetentionTimeoutEventBudget is the budget for WatchForEventWithReason calls
// waiting for DetentionTimeoutForced. Covers DetentionTimeoutSeconds (120s in
// tests) plus operator/Splunk startup overhead on loaded CI runners (~3 min).
const DetentionTimeoutEventBudget = 5 * time.Minute

// SecretUpdateClusterReadyTimeout is the per-CR budget used in the C3/M4
// secret-update tests when waiting for IndexerCluster and SearchHeadCluster
// to return to Ready after a namespace-scoped secret change. The cascading
// rolling restart (CM bundle push -> IDXC roll -> SHC roll) can exceed the
// generic 15m DefaultTimeout on busy CI workers, so allow a larger budget.
const SecretUpdateClusterReadyTimeout = MediumTimeout

// SetupTeardownTimeout limits BeforeEach setup and AfterEach teardown nodes.
// Sized to cover observed namespace Terminating durations of 16-18 minutes on
// loaded EKS nodes (CI job 226294339, 2026-05-27) while leaving a grace margin.
const SetupTeardownTimeout = 25 * time.Minute

// CleanupGraceFraction is the fraction of SetupTeardownTimeout used for
// cleanup context deadlines, leaving the remainder as a grace period so
// cleanup can fail gracefully before Ginkgo's NodeTimeout forcibly kills the node.
// 0.8 × 25m = 20m cleanup budget, 5m grace for the surrounding AfterEach node.
const CleanupGraceFraction = 0.8

// KubectlQuickTimeout bounds short-lived `kubectl get/delete` invocations used
// by dump/inspection helpers. Must be well below Ginkgo's default 30s grace
// period so the subprocess is reliably killed before the grace period elapses,
// preventing "running node failed to exit in time" goroutine leaks.
const KubectlQuickTimeout = 10 * time.Second

// OperatorRestartTimeout bounds intentional operator pod restarts in app
// framework tests. GKE can take longer than quick inspection helpers to
// acknowledge pod deletion and roll a replacement pod to Ready.
const OperatorRestartTimeout = 2 * time.Minute

// MCConfigMapPollTimeout bounds polls that wait for the Monitoring Console env
// config map to reflect a newly added/removed peer. The config map can lag
// briefly behind the MC CR's resource-version bump and Ready phase.
const MCConfigMapPollTimeout = 5 * time.Minute

// PhaseTransitionTimeout bounds polls that wait for a CR to enter a transient
// phase (ScalingUp/ScalingDown/Updating). Kept short so a missed transient
// phase surfaces as the real symptom instead of a Ginkgo NodeTimeout.
const PhaseTransitionTimeout = 10 * time.Minute

// SHCScalingTransitionTimeout bounds polls waiting for a SearchHeadCluster to
// enter ScalingUp/ScalingDown. Wider than PhaseTransitionTimeout because a SHC
// replica-count change updates SPLUNK_SEARCH_HEAD_URL (a function of replica
// count) on every member, so MergePodUpdates recycles the entire remaining
// fleet sequentially -- not just the joining/leaving member. While any member
// is mid-recycle, captain election fails, which short-circuits
// searchHeadClusterPodManager.Update to PhasePending before it ever reaches
// the code that reports ScalingUp/ScalingDown, so the transient phase can
// stay unobservable for as long as the fleet-wide recycle takes (~10 min
// observed on a 4-member SHC scale-down, CI job 242227286, 2026-07-13).
const SHCScalingTransitionTimeout = 20 * time.Minute

// KubectlExecTimeout bounds longer `kubectl exec` and `kubectl logs` calls
// (cat config files, dump pod logs). Still below the 30s Ginkgo grace period.
const KubectlExecTimeout = 25 * time.Second

// BestEffortProbeTimeout bounds the log-only diagnostic probes
// (VerifyIsDeploymentInProgressFlagIsSet) that run between an app-framework
// CR update and its C3/M4 readiness check. These probes only log a miss and
// never fail the spec, since a small app diff can complete within a single
// reconcile and flip the flag back before the probe observes it. A 2-minute
// poll on a guaranteed-miss adds real wall clock, so keep it just long enough
// for one or two PollInterval cycles.
const BestEffortProbeTimeout = 20 * time.Second

// Suite-level timeouts. Applied via GinkgoConfiguration().Timeout in suite files.
// Sized for sequential spec execution (no ginkgo -nodes parallelism).
// Each value must accommodate multiple specs running back-to-back.
const (
	// ShortSuiteTimeout for lightweight suites:
	// smartstore.
	ShortSuiteTimeout = 30 * time.Minute

	// MediumSuiteTimeout for moderate suites:
	// smoke, s1appfw, indingsep.
	MediumSuiteTimeout = 120 * time.Minute

	// MediumLongSuiteTimeout for mid-heavy suites:
	// mc, lmanager, secret.
	MediumLongSuiteTimeout = 150 * time.Minute

	// LongSuiteTimeout for heavy suites:
	// crcrud, c3appfw, m4appfw, postgrescontrollers.
	LongSuiteTimeout = 225 * time.Minute
)
