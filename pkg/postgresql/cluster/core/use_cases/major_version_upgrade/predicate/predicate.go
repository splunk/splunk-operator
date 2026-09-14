/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package majorversionupgradepredicate

import (
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
)

// Predicate reports whether the major-version upgrade use case is possibly
// relevant for the given spec. It is a NECESSARY (not sufficient) condition —
// it only eliminates the common steady state where the feature is switched off,
// so the reconciler can skip construction and status reads entirely. The use
// case's own Schedule makes the precise decision that needs live CNPG reads.
// When spec is nil it returns true so a missing cluster falls through to Schedule.
func Predicate(spec *platformv1alpha1.PostgresClusterSpec) bool {
	if spec == nil {
		return true
	}
	cfg := spec.PostgresMajorUpgradeConfig
	if cfg == nil || cfg.Allow == nil {
		return false
	}
	if *cfg.Allow {
		return true
	}
	// A blue/green false gate is an explicit re-arm signal after a cleaned
	// attempt. Construct the use case so its state adapter can durably observe
	// that edge; retain the no-work fast path for the common pgUpgrade case.
	return cfg.Strategy != nil && *cfg.Strategy == mvutypes.MajorUpgradeFlowBlueGreen
}
