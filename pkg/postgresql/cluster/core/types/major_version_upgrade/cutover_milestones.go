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

// SwitchoverMilestone defines a durable timestamp once for commit authorization
// and write-once validation.
type SwitchoverMilestone struct {
	Field                string
	Name                 string
	RequiredBeforeCommit bool
	timestamp            func(*platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time
}

func (m SwitchoverMilestone) Time(s *platformv1alpha1.BlueGreenSwitchoverStatus) *metav1.Time {
	if s == nil {
		return nil
	}
	return *m.timestamp(s)
}

func (m SwitchoverMilestone) Record(s *platformv1alpha1.BlueGreenSwitchoverStatus, now metav1.Time) {
	*m.timestamp(s) = now.DeepCopy()
}

// SwitchoverMilestones returns independent lifecycle timestamp descriptors. The
// field inventory test requires every top-level status timestamp to be covered.
func SwitchoverMilestones() []SwitchoverMilestone {
	return []SwitchoverMilestone{
		{Field: "RetryRequestedAt", Name: "retry request", timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.RetryRequestedAt }},
		{Field: "StartedAt", Name: "start time", RequiredBeforeCommit: true, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.StartedAt }},
		{Field: "FenceAppliedAt", Name: "fence applied time", RequiredBeforeCommit: false, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.FenceAppliedAt }},
		{Field: "FenceVerifiedAt", Name: "fence verified time", RequiredBeforeCommit: true, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.FenceVerifiedAt }},
		{Field: "DrainCompletedAt", Name: "drain completion time", RequiredBeforeCommit: true, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.DrainCompletedAt }},
		{Field: "CandidateGuardsRemovalStartedAt", Name: "candidate guard removal authorization time", RequiredBeforeCommit: false, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time {
			return &s.CandidateGuardsRemovalStartedAt
		}},
		{Field: "CandidateGuardsRemovedAt", Name: "candidate guard removal time", RequiredBeforeCommit: true, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.CandidateGuardsRemovedAt }},
		{Field: "FinalParityVerifiedAt", Name: "final parity verification time", RequiredBeforeCommit: true, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.FinalParityVerifiedAt }},
		{Field: "EndpointCommittedAt", Name: "endpoint commit time", RequiredBeforeCommit: false, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.EndpointCommittedAt }},
		{Field: "BlueSessionsTerminatedAt", Name: "blue session termination time", RequiredBeforeCommit: false, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.BlueSessionsTerminatedAt }},
		{Field: "GreenFenceRemovedAt", Name: "green fence removal time", RequiredBeforeCommit: false, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.GreenFenceRemovedAt }},
		{Field: "TargetAvailableAt", Name: "target availability time", RequiredBeforeCommit: false, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.TargetAvailableAt }},
		{Field: "SourceFenceRestoredAt", Name: "source fence restoration time", RequiredBeforeCommit: false, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.SourceFenceRestoredAt }},
		{Field: "DeadlineAt", Name: "deadline", RequiredBeforeCommit: true, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.DeadlineAt }},
		{Field: "TimedOutAt", Name: "timeout time", RequiredBeforeCommit: false, timestamp: func(s *platformv1alpha1.BlueGreenSwitchoverStatus) **metav1.Time { return &s.TimedOutAt }},
	}
}
