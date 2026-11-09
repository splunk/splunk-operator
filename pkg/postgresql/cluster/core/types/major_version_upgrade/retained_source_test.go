/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRetainedSourceNeedsAnExactCommittedAttemptAndFenceProof(t *testing.T) {
	now := metav1.Now()
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID:     "attempt",
		Green:         &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "canonical-uid"}, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{
			ControlIdentity: &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: "control", PreparedAt: &now},
			FenceVerifiedAt: &now, DrainCompletedAt: &now, FinalBlueLSN: new("0/100"),
		},
	}
	require.True(t, HasRetainedSourceFenceProof(status, "attempt", "control"))
	require.False(t, HasRetainedSourceFenceProof(status, "other-attempt", "control"))
	require.False(t, HasRetainedSourceFenceProof(status, "attempt", "other-control"))
	require.False(t, HasRetainedSourceFenceProof(nil, "attempt", "control"))
	for _, test := range []struct {
		name   string
		mutate func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus)
	}{
		{"receipt missing", func(s *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { s.CommitReceipt = nil }},
		{"replacement target", func(s *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { s.Green.Ref.UID = "replacement" }},
		{"missing canonical UID", func(s *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { s.CommitReceipt.CanonicalConfigMap.UID = "" }},
		{"control unprepared", func(s *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			s.Switchover.ControlIdentity.PreparedAt = nil
		}},
		{"fence unverified", func(s *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { s.Switchover.FenceVerifiedAt = nil }},
		{"undrained", func(s *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { s.Switchover.DrainCompletedAt = nil }},
		{"no final LSN", func(s *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { s.Switchover.FinalBlueLSN = nil }},
		{"no commit timestamp", func(s *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { s.CommitReceipt.CommittedAt = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			next := status.DeepCopy()
			test.mutate(next)
			require.False(t, HasRetainedSourceFenceProof(next, "attempt", "control"))
		})
	}
}
