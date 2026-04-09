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
	// ShortTimeout for quick tests (observed max ≤10 min):
	// smartstore (4m), indingsep (8m), s1 appfw (9m), deletecr s1 (3m),
	// crcrud s1 (8m), lmanager s1 (6m), smoke s1 (est).
	ShortTimeout = 15 * time.Minute

	// MediumTimeout for moderate tests (observed max 10–30 min):
	// mc s1/m4 (10–28m), crcrud shc/PVC (11–20m), lmanager c3 (20m),
	// secret s1 (18m), deletecr c3 (11m), most c3/m4 appfw (11–28m),
	// smoke c3/m4 (est).
	MediumTimeout = 45 * time.Minute

	// LongTimeout for heavy tests (observed max 30–88 min):
	// secret m4 (88m), c3appfw max (54m), m4appfw max (52m),
	// crcrud c3 (49m), mc c3 (46m), crcrud m4 (39m), lmanager m4 (31m).
	LongTimeout = 135 * time.Minute
)

// TeardownTimeout limits AfterEach teardown (namespace/resource cleanup).
// Prevents hung teardowns from consuming the entire suite timeout.
const TeardownTimeout = 10 * time.Minute

// Suite-level timeouts. Applied via GinkgoConfiguration().Timeout in suite files.
// Sized for sequential spec execution (no ginkgo -nodes parallelism).
// Each value must accommodate multiple specs running back-to-back.
const (
	// ShortSuiteTimeout for lightweight suites (≤~20 specs, ≤15 min each).
	ShortSuiteTimeout = 60 * time.Minute

	// MediumSuiteTimeout for moderate suites (≤~20 specs, ≤45 min each).
	MediumSuiteTimeout = 120 * time.Minute

	// LongSuiteTimeout for heavy suites (c3/m4 appfw, crcrud, secret — many specs at ≤50 min each).
	LongSuiteTimeout = 240 * time.Minute
)
