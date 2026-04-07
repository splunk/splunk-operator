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

// Per-test-case NodeTimeout tiers derived from historic JUnit duration data.
// Each value is approximately 2x the observed p95 duration for that tier,
// rounded to a convenient value.
//
// Usage in test specs:
//
//	It("test name", NodeTimeout(testenv.LongTimeout), func() { ... })
const (
	// ShortTimeout for quick tests: smoke S1, smartstore, indingsep, deletecr.
	// Observed p95: ≤15 min.
	ShortTimeout = 30 * time.Minute

	// MediumTimeout for moderate tests: smoke C3/M4/M1, licensemanager, crcrud, s1 appfw, s1 secret.
	// Observed p95: 15–60 min.
	MediumTimeout = 90 * time.Minute

	// LongTimeout for heavy tests: c3/m4 appfw, C3/M4 secret, monitoring console.
	// Observed p95: 60–100 min.
	LongTimeout = 150 * time.Minute

	// ExtraLongTimeout for the heaviest tests: c3 appfw downgrade, azure big-volume.
	// Observed p95: 100+ min.
	ExtraLongTimeout = 210 * time.Minute
)

// Suite-level timeouts. Applied via GinkgoConfiguration().Timeout in suite files.
const (
	// ShortSuiteTimeout for suites with only short tests (smartstore, deletecr, indingsep).
	ShortSuiteTimeout = 30 * time.Minute

	// MediumSuiteTimeout for suites with moderate tests (smoke, licensemanager).
	MediumSuiteTimeout = 120 * time.Minute

	// LongSuiteTimeout for suites with heavy tests (crcrud, mc, secret, s1appfw).
	// Set to max(NodeTimeout) + buffer; tests run in parallel via ginkgo -nodes.
	LongSuiteTimeout = 165 * time.Minute

	// ExtraLongSuiteTimeout for appframework c3/m4 suites.
	// Set to max(NodeTimeout) + buffer; tests run in parallel via ginkgo -nodes.
	ExtraLongSuiteTimeout = 225 * time.Minute
)
