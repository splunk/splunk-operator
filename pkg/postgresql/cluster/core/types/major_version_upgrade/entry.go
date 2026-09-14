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

package majorversionupgradetypes

import (
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MatchesIntent(entry platformv1alpha1.PostgresMajorUpgradeStatus, intent Intent) bool {
	if !MatchesUpgradeFamily(entry, intent) {
		return false
	}

	if intent.Strategy != MajorUpgradeFlowBlueGreen {
		return true
	}

	return intent.AttemptID != "" &&
		entry.BlueGreen != nil &&
		entry.BlueGreen.AttemptID == intent.AttemptID
}

// MatchesUpgradeFamily reports whether an entry belongs to the source, target,
// and strategy family selected by an intent. A blue/green family can have more
// than one durable attempt; callers that mutate an attempt must use
// MatchesIntent, which additionally requires the attempt ID.
func MatchesUpgradeFamily(entry platformv1alpha1.PostgresMajorUpgradeStatus, intent Intent) bool {
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return deref(entry.SourcePgVersion) == intent.SourcePgVersion &&
		deref(entry.TargetPgVersion) == intent.TargetPgVersion &&
		deref(entry.Strategy) == intent.Strategy
}

// IsBlueGreenAttemptCleaned reports whether an attempt is retained only as a
// cleaned history entry and is eligible to be re-armed by an observed false
// allow gate.
func IsBlueGreenAttemptCleaned(entry platformv1alpha1.PostgresMajorUpgradeStatus) bool {
	return entry.BlueGreen != nil &&
		entry.BlueGreen.Cleanup != nil &&
		entry.BlueGreen.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned
}

// IsBlueGreenAttemptActive reports whether a blue/green entry still owns active lifecycle state.
func IsBlueGreenAttemptActive(entry platformv1alpha1.PostgresMajorUpgradeStatus) bool {
	if entry.BlueGreen == nil || IsBlueGreenAttemptCleaned(entry) {
		return false
	}
	for _, condition := range entry.Conditions {
		if condition.Type == ConditionMajorUpgradeTerminalFailure &&
			condition.Reason == ReasonBlueGreenStrategyUnavailable {
			return false
		}
	}
	return true
}

func RetryRequestedAfterTerminalFailure(retryRequestedAt *metav1.Time, entry platformv1alpha1.PostgresMajorUpgradeStatus) bool {
	if retryRequestedAt == nil {
		return false
	}

	for _, condition := range entry.Conditions {
		if condition.Type != ConditionMajorUpgradeTerminalFailure || condition.LastTransitionTime.IsZero() {
			continue
		}
		return retryRequestedAt.Time.After(condition.LastTransitionTime.Time)
	}

	return false
}
