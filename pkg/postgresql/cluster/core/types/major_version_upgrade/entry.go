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
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MatchesIntent(entry enterprisev4.PostgresMajorUpgradeStatus, intent Intent) bool {
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

func RetryRequestedAfterTerminalFailure(retryRequestedAt *metav1.Time, entry enterprisev4.PostgresMajorUpgradeStatus) bool {
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
