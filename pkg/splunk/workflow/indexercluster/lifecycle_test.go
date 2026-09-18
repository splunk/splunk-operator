// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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

package indexercluster

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

func TestNewScaleOutLifecycle(t *testing.T) {
	now := time.Date(2026, time.September, 17, 1, 0, 0, 0, time.UTC)
	target := scaleOutTarget(3, 6)

	lifecycle, err := NewScaleOutLifecycle(7, target, now)
	require.NoError(t, err)
	require.NotNil(t, lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleScaleOut, lifecycle.Kind)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, lifecycle.Checkpoint)
	assert.Equal(t, int64(7), lifecycle.Generation)
	assert.Equal(t, target, lifecycle.Target)
	assert.Equal(t, now, lifecycle.StartedAt.Time)
	assert.Equal(t, now, lifecycle.LastTransitionTime.Time)
	require.NotNil(t, lifecycle.PendingAction)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleSetReplicas, lifecycle.PendingAction.Type)

	// Planning owns a copy of the target rather than the caller's slice.
	target.Peers[0].PeerID = "changed"
	assert.NotEqual(t, target.Peers[0].PeerID, lifecycle.Target.Peers[0].PeerID)
}

func TestLifecycleJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, time.September, 17, 1, 0, 0, 0, time.UTC)
	rollout, err := NewRolloutLifecycle(7, rolloutTarget(), now)
	require.NoError(t, err)
	scaleIn, err := NewScaleInLifecycle(7, scaleInTarget(4, 3), now)
	require.NoError(t, err)
	scaleOut, err := NewScaleOutLifecycle(7, scaleOutTarget(3, 6), now)
	require.NoError(t, err)

	tests := []struct {
		name      string
		lifecycle *enterpriseApi.IndexerClusterLifecycleStatus
	}{
		{name: "rollout", lifecycle: rollout},
		{name: "scale in", lifecycle: scaleIn},
		{name: "scale out", lifecycle: scaleOut},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serialized, err := json.Marshal(test.lifecycle)
			require.NoError(t, err)
			var restored enterpriseApi.IndexerClusterLifecycleStatus
			require.NoError(t, json.Unmarshal(serialized, &restored))
			roundTrip, err := json.Marshal(&restored)
			require.NoError(t, err)

			assert.JSONEq(t, string(serialized), string(roundTrip))
		})
	}
}

func TestNewScaleOutLifecycleRejectsInvalidTargets(t *testing.T) {
	now := time.Date(2026, time.September, 17, 1, 0, 0, 0, time.UTC)
	tests := []struct {
		name              string
		invalidGeneration bool
		zeroTime          bool
		mutate            func(*enterpriseApi.IndexerClusterLifecycleTarget)
	}{
		{name: "non-positive generation", invalidGeneration: true},
		{name: "zero time", zeroTime: true},
		{name: "missing StatefulSet UID", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.StatefulSetUID = "" }},
		{name: "replicas do not increase", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) {
			target.TargetReplicas = target.SourceReplicas
		}},
		{name: "peer count does not match range", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.Peers = target.Peers[:2] }},
		{name: "peer identity is incomplete", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.Peers[0].PeerID = "" }},
		{name: "source Pod UID is present", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.Peers[0].SourcePodUID = "old-pod" }},
		{name: "duplicate ordinal", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) {
			target.Peers[1].Ordinal = target.Peers[0].Ordinal
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testNow := now
			target := scaleOutTarget(3, 6)
			generation := int64(7)
			if test.invalidGeneration {
				generation = 0
			}
			if test.zeroTime {
				testNow = time.Time{}
			} else if test.mutate != nil {
				test.mutate(&target)
			}

			lifecycle, err := NewScaleOutLifecycle(generation, target, testNow)
			assert.Nil(t, lifecycle)
			assert.ErrorIs(t, err, ErrInvalidLifecycle)
		})
	}
}

func TestScaleOutLifecycleTransitions(t *testing.T) {
	now := time.Date(2026, time.September, 17, 2, 0, 0, 0, time.UTC)
	lifecycle, err := NewScaleOutLifecycle(7, scaleOutTarget(3, 6), now)
	require.NoError(t, err)

	observed := LifecycleObservation{
		StatefulSetUID:      lifecycle.Target.StatefulSetUID,
		StatefulSetReplicas: lifecycle.Target.SourceReplicas,
		Now:                 now.Add(time.Second),
	}

	transition := AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	require.NotNil(t, transition.Execute)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleSetReplicas, transition.Execute.Type)
	assert.Equal(t, lifecycle, transition.Lifecycle)
	assert.Equal(t, int64(7), transition.Lifecycle.Generation)

	// Repeated observation authorizes the same persisted action, not another
	// mutation or target.
	retry := AdvanceLifecycle(lifecycle, observed)
	assert.Equal(t, transition.Execute, retry.Execute)
	assert.Equal(t, transition.Lifecycle, retry.Lifecycle)

	observed.StatefulSetReplicas = lifecycle.Target.TargetReplicas
	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	lifecycle = transition.Lifecycle
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, lifecycle.Checkpoint)
	assert.Nil(t, lifecycle.PendingAction)

	observed.TargetPods = scaleOutPodObservations(lifecycle, false)
	observed.Now = observed.Now.Add(2 * time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, transition.Lifecycle.Checkpoint)
	for _, peer := range transition.Lifecycle.Target.Peers {
		assert.NotEmpty(t, peer.TargetPodUID)
	}
	lifecycle = transition.Lifecycle

	observed.TargetPods = scaleOutPodObservations(lifecycle, true)
	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, transition.Lifecycle.Checkpoint)
	lifecycle = transition.Lifecycle

	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	assert.Equal(t, lifecycle, transition.Lifecycle)

	observed.SatisfiedTargetMembership = true
	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, transition.Lifecycle.Checkpoint)
	require.NotNil(t, transition.Lifecycle.CompletedAt)
	assert.Equal(t, observed.Now, transition.Lifecycle.CompletedAt.Time)
	assert.Nil(t, transition.Execute)

	completed := AdvanceLifecycle(transition.Lifecycle, observed)
	assert.Equal(t, transition.Lifecycle, completed.Lifecycle)
	assert.Nil(t, completed.Execute)
}

func TestScaleInLifecycleTransitions(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	lifecycle, err := NewScaleInLifecycle(9, scaleInTarget(3, 2), now)
	require.NoError(t, err)

	serialized, err := json.Marshal(lifecycle)
	require.NoError(t, err)
	var restored enterpriseApi.IndexerClusterLifecycleStatus
	require.NoError(t, json.Unmarshal(serialized, &restored))

	observed := LifecycleObservation{
		StatefulSetUID:      restored.Target.StatefulSetUID,
		StatefulSetReplicas: restored.Target.SourceReplicas,
		TargetPods: map[int32]LifecyclePodObservation{
			2: {UID: "source-pod", Ready: true},
		},
		Now: now.Add(time.Second),
	}
	transition := AdvanceLifecycle(&restored, observed)
	require.NoError(t, transition.Err)
	require.NotNil(t, transition.Execute)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleSetReplicas, transition.Execute.Type)

	observed.StatefulSetReplicas = restored.Target.TargetReplicas
	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(&restored, observed)
	require.NoError(t, transition.Err)
	lifecycle = transition.Lifecycle
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, lifecycle.Checkpoint)
	assert.Nil(t, lifecycle.PendingAction)

	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, transition.Lifecycle.Checkpoint)

	delete(observed.TargetPods, 2)
	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, transition.Lifecycle.Checkpoint)

	observed.SatisfiedTargetMembership = true
	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, transition.Lifecycle.Checkpoint)
	require.NotNil(t, transition.Lifecycle.CompletedAt)
}

func TestNewScaleInLifecycleRejectsInvalidTargets(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*enterpriseApi.IndexerClusterLifecycleTarget)
	}{
		{name: "replicas do not decrease by one", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.TargetReplicas = 1 }},
		{name: "multiple peers", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) {
			target.Peers = append(target.Peers, target.Peers[0])
		}},
		{name: "peer is not highest ordinal", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.Peers[0].Ordinal = 1 }},
		{name: "source Pod UID is absent", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.Peers[0].SourcePodUID = "" }},
		{name: "target Pod UID is present", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) {
			target.Peers[0].TargetPodUID = "replacement"
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := scaleInTarget(3, 2)
			test.mutate(&target)
			lifecycle, err := NewScaleInLifecycle(9, target, now)
			assert.Nil(t, lifecycle)
			assert.ErrorIs(t, err, ErrInvalidLifecycle)
		})
	}
}

func TestRolloutLifecycleTransitions(t *testing.T) {
	now := time.Date(2026, time.September, 17, 8, 0, 0, 0, time.UTC)
	lifecycle, err := NewRolloutLifecycle(8, rolloutTarget(), now)
	require.NoError(t, err)

	observed := LifecycleObservation{
		StatefulSetUID:            lifecycle.Target.StatefulSetUID,
		StatefulSetReplicas:       lifecycle.Target.TargetReplicas,
		StatefulSetUpdateRevision: "revision-2",
		TargetPods: map[int32]LifecyclePodObservation{
			1: {UID: "source-pod", Revision: "revision-1", Ready: true},
		},
		Now: now.Add(time.Second),
	}

	transition := AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	require.NotNil(t, transition.Execute)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleDeletePod, transition.Execute.Type)
	assert.Equal(t, lifecycle, transition.Lifecycle)

	// The persisted target remains valid when a newer template is published
	// before the replacement is observed.
	observed.StatefulSetUpdateRevision = "revision-3"
	observed.TargetPods[1] = LifecyclePodObservation{UID: "target-pod", Revision: "revision-2", Ready: true}
	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	lifecycle = transition.Lifecycle
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, lifecycle.Checkpoint)
	assert.Equal(t, k8stypes.UID("target-pod"), lifecycle.Target.Peers[0].TargetPodUID)
	assert.Nil(t, lifecycle.PendingAction)

	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, transition.Lifecycle.Checkpoint)

	observed.SatisfiedTargetMembership = true
	observed.Now = observed.Now.Add(time.Second)
	transition = AdvanceLifecycle(lifecycle, observed)
	require.NoError(t, transition.Err)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, transition.Lifecycle.Checkpoint)
	require.NotNil(t, transition.Lifecycle.CompletedAt)
}

func TestRolloutLifecycleAcceptsReplacementAtInterveningRevision(t *testing.T) {
	now := time.Date(2026, time.September, 17, 8, 30, 0, 0, time.UTC)
	lifecycle, err := NewRolloutLifecycle(8, rolloutTarget(), now)
	require.NoError(t, err)

	transition := AdvanceLifecycle(lifecycle, LifecycleObservation{
		StatefulSetUID:            lifecycle.Target.StatefulSetUID,
		StatefulSetReplicas:       lifecycle.Target.TargetReplicas,
		StatefulSetUpdateRevision: "revision-4",
		TargetPods: map[int32]LifecyclePodObservation{
			1: {UID: "target-pod", Revision: "revision-3", Ready: true},
		},
		Now: now.Add(time.Second),
	})

	require.NoError(t, transition.Err)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, transition.Lifecycle.Checkpoint)
	assert.Equal(t, k8stypes.UID("target-pod"), transition.Lifecycle.Target.Peers[0].TargetPodUID)
}

func TestRolloutLifecycleRebindsRecreatedReplacement(t *testing.T) {
	now := time.Date(2026, time.September, 17, 8, 40, 0, 0, time.UTC)
	lifecycle, err := NewRolloutLifecycle(8, rolloutTarget(), now)
	require.NoError(t, err)

	waiting := AdvanceLifecycle(lifecycle, LifecycleObservation{
		StatefulSetUID:            lifecycle.Target.StatefulSetUID,
		StatefulSetReplicas:       lifecycle.Target.TargetReplicas,
		StatefulSetUpdateRevision: "revision-2",
		TargetPods: map[int32]LifecyclePodObservation{
			1: {UID: "target-pod", Revision: "revision-2", Ready: true},
		},
		Now: now.Add(time.Second),
	})
	require.NoError(t, waiting.Err)
	require.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, waiting.Lifecycle.Checkpoint)

	tests := []struct {
		name           string
		pod            LifecyclePodObservation
		updateRevision string
		membership     bool
		wantRebind     bool
	}{
		{
			name:           "persisted target revision before membership convergence",
			pod:            LifecyclePodObservation{UID: "recreated-pod", Revision: "revision-2", Ready: true},
			updateRevision: "revision-3",
			wantRebind:     true,
		},
		{
			name:           "newer current revision with membership already satisfied",
			pod:            LifecyclePodObservation{UID: "newer-pod", Revision: "revision-3", Ready: true},
			updateRevision: "revision-3",
			membership:     true,
			wantRebind:     true,
		},
		{
			name:           "intervening revision",
			pod:            LifecyclePodObservation{UID: "unrelated-pod", Revision: "revision-4", Ready: true},
			updateRevision: "revision-5",
			membership:     true,
			wantRebind:     true,
		},
		{
			name:           "original source incarnation",
			pod:            LifecyclePodObservation{UID: "source-pod", Revision: "revision-2", Ready: true},
			updateRevision: "revision-3",
			membership:     true,
		},
		{
			name:           "unready replacement",
			pod:            LifecyclePodObservation{UID: "unready-pod", Revision: "revision-2"},
			updateRevision: "revision-3",
			membership:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observedAt := now.Add(2 * time.Second)
			transition := AdvanceLifecycle(waiting.Lifecycle, LifecycleObservation{
				StatefulSetUID:            lifecycle.Target.StatefulSetUID,
				StatefulSetReplicas:       lifecycle.Target.TargetReplicas,
				StatefulSetUpdateRevision: test.updateRevision,
				TargetPods: map[int32]LifecyclePodObservation{
					1: test.pod,
				},
				Now:                       observedAt,
				SatisfiedTargetMembership: test.membership,
			})
			require.NoError(t, transition.Err)
			assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, transition.Lifecycle.Checkpoint)

			if !test.wantRebind {
				assert.Equal(t, k8stypes.UID("target-pod"), transition.Lifecycle.Target.Peers[0].TargetPodUID)
				return
			}

			assert.Equal(t, test.pod.UID, transition.Lifecycle.Target.Peers[0].TargetPodUID)
			assert.Equal(t, observedAt, transition.Lifecycle.LastTransitionTime.Time)
			assert.Nil(t, transition.Lifecycle.CompletedAt)
		})
	}
}

func TestScaleOutLifecycleReplansOnReplicaDriftDuringMembershipWait(t *testing.T) {
	now := time.Date(2026, time.September, 17, 8, 45, 0, 0, time.UTC)
	lifecycle, err := NewScaleOutLifecycle(8, scaleOutTarget(3, 4), now)
	require.NoError(t, err)

	transition := AdvanceLifecycle(lifecycle, LifecycleObservation{
		StatefulSetUID:      lifecycle.Target.StatefulSetUID,
		StatefulSetReplicas: lifecycle.Target.TargetReplicas,
		Now:                 now.Add(time.Second),
	})
	require.NoError(t, transition.Err)
	require.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, transition.Lifecycle.Checkpoint)

	for _, replicas := range []int32{2, 5} {
		t.Run(fmt.Sprintf("replicas_%d", replicas), func(t *testing.T) {
			drifted := AdvanceLifecycle(transition.Lifecycle, LifecycleObservation{
				StatefulSetUID:      lifecycle.Target.StatefulSetUID,
				StatefulSetReplicas: replicas,
				Now:                 now.Add(2 * time.Second),
			})
			require.NoError(t, drifted.Err)
			assert.True(t, drifted.Replan)
			assert.Nil(t, drifted.Lifecycle)
			assert.Nil(t, drifted.Execute)
		})
	}
}

func TestRolloutLifecycleReplansOnReplicaDrift(t *testing.T) {
	now := time.Date(2026, time.September, 17, 8, 50, 0, 0, time.UTC)
	pending, err := NewRolloutLifecycle(8, rolloutTarget(), now)
	require.NoError(t, err)

	waiting := AdvanceLifecycle(pending, LifecycleObservation{
		StatefulSetUID:            pending.Target.StatefulSetUID,
		StatefulSetReplicas:       pending.Target.TargetReplicas,
		StatefulSetUpdateRevision: pending.Target.TargetRevision,
		TargetPods: map[int32]LifecyclePodObservation{
			1: {UID: "target-pod", Revision: pending.Target.TargetRevision, Ready: true},
		},
		Now: now.Add(time.Second),
	})
	require.NoError(t, waiting.Err)
	require.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, waiting.Lifecycle.Checkpoint)

	for name, lifecycle := range map[string]*enterpriseApi.IndexerClusterLifecycleStatus{
		"action pending":         pending,
		"waiting for membership": waiting.Lifecycle,
	} {
		t.Run(name, func(t *testing.T) {
			transition := AdvanceLifecycle(lifecycle, LifecycleObservation{
				StatefulSetUID:      lifecycle.Target.StatefulSetUID,
				StatefulSetReplicas: lifecycle.Target.TargetReplicas - 1,
				Now:                 now.Add(2 * time.Second),
			})

			require.NoError(t, transition.Err)
			assert.True(t, transition.Replan)
			assert.Nil(t, transition.Lifecycle)
			assert.Nil(t, transition.Execute)
		})
	}
}

func TestNewRolloutLifecycleRejectsInvalidTargets(t *testing.T) {
	now := time.Date(2026, time.September, 17, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*enterpriseApi.IndexerClusterLifecycleTarget)
	}{
		{name: "replica count changes", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.TargetReplicas++ }},
		{name: "missing source revision", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.SourceRevision = "" }},
		{name: "unchanged revision", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) {
			target.TargetRevision = target.SourceRevision
		}},
		{name: "multiple peers", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) {
			target.Peers = append(target.Peers, target.Peers[0])
		}},
		{name: "missing source Pod UID", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.Peers[0].SourcePodUID = "" }},
		{name: "ordinal outside replicas", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) {
			target.Peers[0].Ordinal = target.SourceReplicas
		}},
		{name: "target Pod already recorded", mutate: func(target *enterpriseApi.IndexerClusterLifecycleTarget) { target.Peers[0].TargetPodUID = "target-pod" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := rolloutTarget()
			test.mutate(&target)
			lifecycle, err := NewRolloutLifecycle(8, target, now)
			assert.Nil(t, lifecycle)
			assert.ErrorIs(t, err, ErrInvalidLifecycle)
		})
	}
}

func TestRolloutLifecycleFailsOnContradictorySourcePod(t *testing.T) {
	now := time.Date(2026, time.September, 17, 10, 0, 0, 0, time.UTC)
	lifecycle, err := NewRolloutLifecycle(8, rolloutTarget(), now)
	require.NoError(t, err)

	transition := AdvanceLifecycle(lifecycle, LifecycleObservation{
		StatefulSetUID:            lifecycle.Target.StatefulSetUID,
		StatefulSetReplicas:       lifecycle.Target.TargetReplicas,
		StatefulSetUpdateRevision: "revision-2",
		TargetPods: map[int32]LifecyclePodObservation{
			1: {UID: "source-pod", Revision: "revision-2", Ready: true},
		},
		Now: now.Add(time.Second),
	})

	assert.ErrorIs(t, transition.Err, ErrInvalidLifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleFailed, transition.Lifecycle.Checkpoint)
}

func TestRolloutLifecycleCompletesWhenTemplateRevertsBeforeDeletion(t *testing.T) {
	now := time.Date(2026, time.September, 17, 11, 0, 0, 0, time.UTC)
	lifecycle, err := NewRolloutLifecycle(8, rolloutTarget(), now)
	require.NoError(t, err)

	transition := AdvanceLifecycle(lifecycle, LifecycleObservation{
		StatefulSetUID:            lifecycle.Target.StatefulSetUID,
		StatefulSetReplicas:       lifecycle.Target.TargetReplicas,
		StatefulSetUpdateRevision: lifecycle.Target.SourceRevision,
		TargetPods: map[int32]LifecyclePodObservation{
			1: {UID: "source-pod", Revision: "revision-1", Ready: true},
		},
		Now: now.Add(time.Second),
	})

	require.NoError(t, transition.Err)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, transition.Lifecycle.Checkpoint)
	assert.Nil(t, transition.Execute)

	completed := AdvanceLifecycle(transition.Lifecycle, LifecycleObservation{
		StatefulSetUID: transition.Lifecycle.Target.StatefulSetUID,
		Now:            now.Add(2 * time.Second),
	})
	require.NoError(t, completed.Err)
	assert.Equal(t, transition.Lifecycle, completed.Lifecycle)
}

func TestLifecycleStatefulSetIncarnationFencesActions(t *testing.T) {
	now := time.Date(2026, time.September, 17, 4, 0, 0, 0, time.UTC)
	lifecycle, err := NewScaleOutLifecycle(7, scaleOutTarget(3, 4), now)
	require.NoError(t, err)
	missing := AdvanceLifecycle(lifecycle, LifecycleObservation{Now: now.Add(time.Second)})
	assert.Nil(t, missing.Execute)

	replaced := AdvanceLifecycle(lifecycle, LifecycleObservation{
		StatefulSetUID:      "replacement",
		StatefulSetReplicas: lifecycle.Target.SourceReplicas,
		Now:                 now.Add(time.Second),
	})
	assert.True(t, replaced.Replan)
	assert.Nil(t, replaced.Lifecycle)
	assert.Nil(t, replaced.Execute)

	current := AdvanceLifecycle(lifecycle, LifecycleObservation{
		StatefulSetUID:      lifecycle.Target.StatefulSetUID,
		StatefulSetReplicas: lifecycle.Target.SourceReplicas,
		Now:                 now.Add(time.Second),
	})
	require.NotNil(t, current.Execute)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleSetReplicas, current.Execute.Type)
}

func TestLifecycleFailsClosedOnInvalidState(t *testing.T) {
	now := time.Date(2026, time.September, 17, 5, 0, 0, 0, time.UTC)
	lifecycle, err := NewScaleOutLifecycle(7, scaleOutTarget(3, 4), now)
	require.NoError(t, err)
	lifecycle.Checkpoint = enterpriseApi.IndexerClusterLifecycleWaitingForMembership

	transition := AdvanceLifecycle(lifecycle, LifecycleObservation{
		StatefulSetUID: lifecycle.Target.StatefulSetUID,
		Now:            now.Add(time.Second),
	})
	assert.ErrorIs(t, transition.Err, ErrInvalidLifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleFailed, transition.Lifecycle.Checkpoint)
	assert.Nil(t, transition.Lifecycle.PendingAction)
	assert.Nil(t, transition.Execute)
}

func TestScaleOutLifecycleRejectsContradictoryReplicaCount(t *testing.T) {
	now := time.Date(2026, time.September, 17, 7, 0, 0, 0, time.UTC)
	for _, replicas := range []int32{2, 5} {
		t.Run(fmt.Sprintf("replicas-%d", replicas), func(t *testing.T) {
			lifecycle, err := NewScaleOutLifecycle(7, scaleOutTarget(3, 4), now)
			require.NoError(t, err)

			transition := AdvanceLifecycle(lifecycle, LifecycleObservation{
				StatefulSetUID:      lifecycle.Target.StatefulSetUID,
				StatefulSetReplicas: replicas,
				Now:                 now.Add(time.Second),
			})
			assert.ErrorIs(t, transition.Err, ErrInvalidLifecycle)
			assert.Equal(t, enterpriseApi.IndexerClusterLifecycleFailed, transition.Lifecycle.Checkpoint)
		})
	}
}

func scaleOutTarget(sourceReplicas, targetReplicas int32) enterpriseApi.IndexerClusterLifecycleTarget {
	peers := make([]enterpriseApi.IndexerClusterLifecyclePeerTarget, 0, targetReplicas-sourceReplicas)
	for ordinal := sourceReplicas; ordinal < targetReplicas; ordinal++ {
		peers = append(peers, enterpriseApi.IndexerClusterLifecyclePeerTarget{
			Ordinal: ordinal,
			PeerID:  fmt.Sprintf("peer-%d.example", ordinal),
			PodName: fmt.Sprintf("splunk-test-indexer-%d", ordinal),
		})
	}
	return enterpriseApi.IndexerClusterLifecycleTarget{
		StatefulSetUID: k8stypes.UID("statefulset-uid"),
		SourceReplicas: sourceReplicas,
		TargetReplicas: targetReplicas,
		Peers:          peers,
	}
}

func rolloutTarget() enterpriseApi.IndexerClusterLifecycleTarget {
	return enterpriseApi.IndexerClusterLifecycleTarget{
		StatefulSetUID: "statefulset-uid",
		SourceReplicas: 2,
		TargetReplicas: 2,
		SourceRevision: "revision-1",
		TargetRevision: "revision-2",
		Peers: []enterpriseApi.IndexerClusterLifecyclePeerTarget{{
			Ordinal:      1,
			PeerID:       "peer-1.example",
			PodName:      "splunk-test-indexer-1",
			SourcePodUID: "source-pod",
		}},
	}
}

func scaleInTarget(sourceReplicas, targetReplicas int32) enterpriseApi.IndexerClusterLifecycleTarget {
	return enterpriseApi.IndexerClusterLifecycleTarget{
		StatefulSetUID: "statefulset-uid",
		SourceReplicas: sourceReplicas,
		TargetReplicas: targetReplicas,
		Peers: []enterpriseApi.IndexerClusterLifecyclePeerTarget{{
			Ordinal:      targetReplicas,
			PeerID:       fmt.Sprintf("peer-%d.example", targetReplicas),
			PodName:      fmt.Sprintf("splunk-test-indexer-%d", targetReplicas),
			SourcePodUID: "source-pod",
		}},
	}
}

func scaleOutPodObservations(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, ready bool) map[int32]LifecyclePodObservation {
	pods := make(map[int32]LifecyclePodObservation, len(lifecycle.Target.Peers))
	for _, peer := range lifecycle.Target.Peers {
		pods[peer.Ordinal] = LifecyclePodObservation{
			UID:      k8stypes.UID(fmt.Sprintf("pod-%d", peer.Ordinal)),
			Revision: lifecycle.Target.TargetRevision,
			Ready:    ready,
		}
	}
	return pods
}
