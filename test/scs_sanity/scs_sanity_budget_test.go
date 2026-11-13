// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scssanity

import (
	"testing"
	"time"
)

func TestDefaultBudgetsFitTheGate(t *testing.T) {
	if total := healthWorstCase() + canaryWorstCase() + recheckReserve; total > defaultGateBudget-gateWindDownMargin {
		t.Errorf("worst case health %v + canary %v + recheck reserve %v = %v exceeds the %v gate minus its %v wind-down margin",
			healthWorstCase(), canaryWorstCase(), recheckReserve, total, defaultGateBudget, gateWindDownMargin)
	}
}

func TestCanarySuiteTimeoutCoversItsWaits(t *testing.T) {
	// Health used its full worst case, so the canary suite gets what is left minus the recheck and its own grace.
	if suite := defaultGateBudget - healthWorstCase() - recheckReserve - canaryGraceReserve; suite < canaryWaitsWorstCase()+canarySuiteSlackMin {
		t.Errorf("canary suite timeout %v is below its waits %v plus %v slack", suite, canaryWaitsWorstCase(), canarySuiteSlackMin)
	}
}

func TestSuiteTimeoutLeavesTheGraceForCleanup(t *testing.T) {
	if canaryGraceReserve < canaryCleanupBudget+canarySuiteSlackMin {
		t.Errorf("canary reserve %v must cover one cleanup (%v) plus %v margin", canaryGraceReserve, canaryCleanupBudget, canarySuiteSlackMin)
	}
}

func TestDocumentedDefaults(t *testing.T) {
	for name, got := range map[string]time.Duration{
		"Standalone Ready": defaultCanaryReadyTimeout, "scale up": defaultCanaryScaleUpTimeout,
		"scale down": defaultCanaryScaleDownTimeout, "cleanup": canaryCleanupBudget,
	} {
		want := map[string]time.Duration{
			"Standalone Ready": 6 * time.Minute, "scale up": 5 * time.Minute, "scale down": 3 * time.Minute,
			"cleanup": 3 * time.Minute,
		}[name]
		if got != want {
			t.Errorf("%s default is %v, the READMEs document %v", name, got, want)
		}
	}
}
