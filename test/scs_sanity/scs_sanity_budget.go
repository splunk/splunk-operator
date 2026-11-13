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

import "time"

const (
	// Helm --wait already finished the rollout, so these only cover the settle after it.
	operatorRolloutTimeout = 2 * time.Minute
	leaderElectionTimeout  = 1 * time.Minute

	// These mirror scs-sanity-gate.sh (default SCS_SANITY_GATE_TIMEOUT, recheck reserve, canary
	// reserve of Ginkgo grace plus 60s, final wind-down margin); gate-test.sh asserts the script side.
	defaultGateBudget   = 30 * time.Minute
	recheckReserve      = 5 * time.Minute
	canaryGraceReserve  = 4 * time.Minute
	gateWindDownMargin  = 1 * time.Minute
	canarySuiteSlackMin = 1 * time.Minute
)

// healthWorstCase is every wait of the operator-health invocation expiring.
func healthWorstCase() time.Duration {
	return operatorRolloutTimeout + leaderElectionTimeout + fleetAdvisoryTimeout
}

// canaryWaitsWorstCase is the canary's waits without its cleanup.
func canaryWaitsWorstCase() time.Duration {
	return defaultCanaryReadyTimeout + defaultCanaryScaleUpTimeout + defaultCanaryScaleDownTimeout
}

// canaryWorstCase is the canary's waits plus one cleanup.
func canaryWorstCase() time.Duration {
	return canaryWaitsWorstCase() + canaryCleanupBudget
}
