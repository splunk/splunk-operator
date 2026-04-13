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
	// secret s1, deletecr c3, most c3/m4 appfw, smoke c3/m4.
	MediumTimeout = 45 * time.Minute

	// MediumLongTimeout for heavier tests:
	// m4appfw scale-up, crcrud c3, mc c3,
	// m4appfw install-local, crcrud m4, lmanager m4.
	MediumLongTimeout = 70 * time.Minute

	// LongTimeout for heavy tests:
	// secret m4, c3appfw image-upgrade variants.
	LongTimeout = 100 * time.Minute
)

// TeardownTimeout limits AfterEach teardown (namespace/resource cleanup).
// Prevents hung teardowns from consuming the entire suite timeout.
const TeardownTimeout = 10 * time.Minute

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
	LongSuiteTimeout = 200 * time.Minute
)
