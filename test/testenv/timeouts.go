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
	// secret s1, deletecr c3, most c3/m4 appfw, smoke m4.
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

// ReadinessPollTimeout is the per-attempt timeout for individual CR readiness
// polls inside Verify*Ready helpers. It must be shorter than DefaultTimeout so
// that an outer Eventually wrapper has room to retry on transient failures.
const ReadinessPollTimeout = 5 * time.Minute

// AppInstallTimeout is the timeout for waiting for apps to reach Install phase on a CR.
// C3 deployments require bundle push across all indexers and SHC deployer which can exceed 5 minutes.
const AppInstallTimeout = 10 * time.Minute

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

// SecretUpdateClusterReadyTimeout is the per-CR budget used in the C3/M4
// secret-update tests when waiting for IndexerCluster and SearchHeadCluster
// to return to Ready after a namespace-scoped secret change. The cascading
// rolling restart (CM bundle push -> IDXC roll -> SHC roll) can exceed the
// generic 15m DefaultTimeout on busy CI workers, so allow a larger budget.
const SecretUpdateClusterReadyTimeout = MediumTimeout

// SetupTeardownTimeout limits BeforeEach setup and AfterEach teardown nodes.
// Prevents hung setup or cleanup from consuming the entire suite timeout.
const SetupTeardownTimeout = 10 * time.Minute

// CleanupGraceFraction is the fraction of SetupTeardownTimeout used for
// cleanup context deadlines, leaving the remainder as a grace period so
// cleanup can fail gracefully before Ginkgo's NodeTimeout forcibly kills the node.
const CleanupGraceFraction = 0.8

// KubectlQuickTimeout bounds short-lived `kubectl get/delete` invocations used
// by dump/inspection helpers. Must be well below Ginkgo's default 30s grace
// period so the subprocess is reliably killed before the grace period elapses,
// preventing "running node failed to exit in time" goroutine leaks.
const KubectlQuickTimeout = 10 * time.Second

// KubectlExecTimeout bounds longer `kubectl exec` and `kubectl logs` calls
// (cat config files, dump pod logs). Still below the 30s Ginkgo grace period.
const KubectlExecTimeout = 25 * time.Second

// Suite-level timeouts. Applied via GinkgoConfiguration().Timeout in suite files.
// Sized for sequential spec execution (no ginkgo -nodes parallelism).
// Each value must accommodate multiple specs running back-to-back.
const (
	// ShortSuiteTimeout for lightweight suites:
	// smartstore, indingsep.
	ShortSuiteTimeout = 30 * time.Minute

	// MediumSuiteTimeout for moderate suites:
	// smoke, s1appfw.
	MediumSuiteTimeout = 90 * time.Minute

	// MediumLongSuiteTimeout for mid-heavy suites:
	// mc, lmanager, secret.
	MediumLongSuiteTimeout = 150 * time.Minute

	// LongSuiteTimeout for heavy suites:
	// crcrud, m4appfw, c3appfw.
	LongSuiteTimeout = 225 * time.Minute
)
