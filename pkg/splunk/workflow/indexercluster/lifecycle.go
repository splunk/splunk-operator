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
	"errors"
	"fmt"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// ErrInvalidLifecycle identifies a lifecycle record or observation that
// cannot be advanced safely.
var ErrInvalidLifecycle = errors.New("invalid IndexerCluster lifecycle")

// LifecycleObservation contains current evidence used by the pure lifecycle
// transition function. Callers derive membership satisfaction from the active
// provider before invoking the workflow.
type LifecycleObservation struct {
	StatefulSetUID            k8stypes.UID
	StatefulSetReplicas       int32
	StatefulSetUpdateRevision string
	TargetPods                map[int32]LifecyclePodObservation

	Now                       time.Time
	SatisfiedTargetMembership bool
}

// LifecyclePodObservation contains the Kubernetes evidence required for one
// target peer.
type LifecyclePodObservation struct {
	UID      k8stypes.UID
	Revision string
	Ready    bool
}

// LifecycleTransition describes the next durable lifecycle state and, only
// when already persisted in the input, the single action that may execute.
// Replan instructs the caller to clear the stale record without performing a
// workload mutation and plan again on a later reconcile.
type LifecycleTransition struct {
	Lifecycle *enterpriseApi.IndexerClusterLifecycleStatus
	Execute   *enterpriseApi.IndexerClusterLifecyclePendingAction
	Replan    bool
	Err       error
}

// NewRolloutLifecycle authorizes replacement of one exact Pod incarnation.
// The caller must persist the returned record before allowing deletion.
func NewRolloutLifecycle(generation int64, target enterpriseApi.IndexerClusterLifecycleTarget, now time.Time) (*enterpriseApi.IndexerClusterLifecycleStatus, error) {
	return newLifecycle(
		enterpriseApi.IndexerClusterLifecycleRollout,
		enterpriseApi.IndexerClusterLifecycleDeletePod,
		generation,
		target,
		now,
	)
}

// NewScaleInLifecycle authorizes graceful removal of one exact highest ordinal.
// The caller must persist the returned record before reducing replicas.
func NewScaleInLifecycle(generation int64, target enterpriseApi.IndexerClusterLifecycleTarget, now time.Time) (*enterpriseApi.IndexerClusterLifecycleStatus, error) {
	return newLifecycle(
		enterpriseApi.IndexerClusterLifecycleScaleIn,
		enterpriseApi.IndexerClusterLifecycleSetReplicas,
		generation,
		target,
		now,
	)
}

// NewScaleOutLifecycle authorizes one exact scale-out batch. The caller must
// persist the returned record before asking AdvanceLifecycle to execute it.
func NewScaleOutLifecycle(generation int64, target enterpriseApi.IndexerClusterLifecycleTarget, now time.Time) (*enterpriseApi.IndexerClusterLifecycleStatus, error) {
	return newLifecycle(
		enterpriseApi.IndexerClusterLifecycleScaleOut,
		enterpriseApi.IndexerClusterLifecycleSetReplicas,
		generation,
		target,
		now,
	)
}

func newLifecycle(kind enterpriseApi.IndexerClusterLifecycleKind, action enterpriseApi.IndexerClusterLifecycleActionType, generation int64, target enterpriseApi.IndexerClusterLifecycleTarget, now time.Time) (*enterpriseApi.IndexerClusterLifecycleStatus, error) {
	lifecycle := &enterpriseApi.IndexerClusterLifecycleStatus{
		Kind:       kind,
		Checkpoint: enterpriseApi.IndexerClusterLifecycleActionPending,
		Generation: generation,
		Target:     *target.DeepCopy(),
		PendingAction: &enterpriseApi.IndexerClusterLifecyclePendingAction{
			Type: action,
		},
		StartedAt:          metav1.NewTime(now),
		LastTransitionTime: metav1.NewTime(now),
	}

	if err := validateLifecycle(lifecycle); err != nil {
		return nil, err
	}

	return lifecycle, nil
}

// AdvanceLifecycle applies current observations to one persisted lifecycle
// record. It performs no I/O and never mutates current.
func AdvanceLifecycle(current *enterpriseApi.IndexerClusterLifecycleStatus, observed LifecycleObservation) LifecycleTransition {
	if current == nil {
		return LifecycleTransition{}
	}

	lifecycle := current.DeepCopy()
	if err := validateLifecycle(lifecycle); err != nil {
		return failLifecycle(lifecycle, observed.Now, err)
	}
	if observed.StatefulSetUID != "" && observed.StatefulSetUID != lifecycle.Target.StatefulSetUID {
		return replanLifecycle()
	}
	if lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleFailed || lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleCompleted {
		return LifecycleTransition{Lifecycle: lifecycle}
	}
	if observed.Now.IsZero() {
		return failLifecycle(lifecycle, observed.Now, fmt.Errorf("%w: observation time must be set", ErrInvalidLifecycle))
	}
	if observed.StatefulSetUID == "" {
		return LifecycleTransition{Lifecycle: lifecycle}
	}

	switch lifecycle.Kind {
	case enterpriseApi.IndexerClusterLifecycleRollout:
		return advanceRollout(lifecycle, observed)
	case enterpriseApi.IndexerClusterLifecycleScaleIn:
		return advanceScaleIn(lifecycle, observed)
	case enterpriseApi.IndexerClusterLifecycleScaleOut:
		return advanceScaleOut(lifecycle, observed)
	default:
		return failLifecycle(lifecycle, observed.Now, fmt.Errorf("%w: unsupported kind %q", ErrInvalidLifecycle, lifecycle.Kind))
	}
}

func advanceRollout(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, observed LifecycleObservation) LifecycleTransition {
	if observed.StatefulSetReplicas != lifecycle.Target.TargetReplicas {
		return replanLifecycle()
	}

	target := &lifecycle.Target.Peers[0]
	pod, found := observed.TargetPods[target.Ordinal]

	switch lifecycle.Checkpoint {
	case enterpriseApi.IndexerClusterLifecycleActionPending:
		if !found || pod.UID == "" {
			return LifecycleTransition{Lifecycle: lifecycle}
		}

		if pod.UID == target.SourcePodUID {
			if pod.Revision != lifecycle.Target.SourceRevision {
				return failLifecycle(lifecycle, observed.Now, fmt.Errorf(
					"%w: source Pod %s changed revision without changing UID",
					ErrInvalidLifecycle,
					target.PodName,
				))
			}

			if observed.StatefulSetUpdateRevision == lifecycle.Target.SourceRevision {
				return completeLifecycle(lifecycle, observed.Now)
			}

			return LifecycleTransition{
				Lifecycle: lifecycle,
				Execute:   lifecycle.PendingAction.DeepCopy(),
			}
		}

		if !pod.Ready {
			return LifecycleTransition{Lifecycle: lifecycle}
		}

		target.TargetPodUID = pod.UID
		lifecycle.PendingAction = nil
		setLifecycleCheckpoint(lifecycle, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, observed.Now)
		return LifecycleTransition{Lifecycle: lifecycle}

	case enterpriseApi.IndexerClusterLifecycleWaitingForMembership:
		if !found || pod.UID == "" || pod.UID == target.SourcePodUID || !pod.Ready {
			return LifecycleTransition{Lifecycle: lifecycle}
		}

		if pod.UID != target.TargetPodUID {
			setLifecycleTargetPodUID(lifecycle, target, pod.UID, observed.Now)
			return LifecycleTransition{Lifecycle: lifecycle}
		}

		if !observed.SatisfiedTargetMembership {
			return LifecycleTransition{Lifecycle: lifecycle}
		}

		return completeLifecycle(lifecycle, observed.Now)

	default:
		return failLifecycle(lifecycle, observed.Now, fmt.Errorf("%w: unsupported rollout checkpoint %q", ErrInvalidLifecycle, lifecycle.Checkpoint))
	}
}

func advanceScaleIn(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, observed LifecycleObservation) LifecycleTransition {
	if transition, handled := advanceSetReplicas(lifecycle, observed); handled {
		return transition
	}

	if lifecycle.Checkpoint != enterpriseApi.IndexerClusterLifecycleWaitingForMembership {
		return failLifecycle(lifecycle, observed.Now, fmt.Errorf("%w: unsupported scale-in checkpoint %q", ErrInvalidLifecycle, lifecycle.Checkpoint))
	}

	if observed.StatefulSetReplicas != lifecycle.Target.TargetReplicas {
		return replanLifecycle()
	}

	target := lifecycle.Target.Peers[0]
	if pod, found := observed.TargetPods[target.Ordinal]; found && pod.UID != "" {
		if pod.UID != target.SourcePodUID {
			return failLifecycle(lifecycle, observed.Now, fmt.Errorf(
				"%w: removed Pod %s was replaced with UID %s",
				ErrInvalidLifecycle,
				target.PodName,
				pod.UID,
			))
		}

		return LifecycleTransition{Lifecycle: lifecycle}
	}

	if !observed.SatisfiedTargetMembership {
		return LifecycleTransition{Lifecycle: lifecycle}
	}

	return completeLifecycle(lifecycle, observed.Now)
}

func advanceScaleOut(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, observed LifecycleObservation) LifecycleTransition {
	if transition, handled := advanceSetReplicas(lifecycle, observed); handled {
		return transition
	}

	if lifecycle.Checkpoint != enterpriseApi.IndexerClusterLifecycleWaitingForMembership {
		return failLifecycle(lifecycle, observed.Now, fmt.Errorf("%w: unsupported scale-out checkpoint %q", ErrInvalidLifecycle, lifecycle.Checkpoint))
	}

	if observed.StatefulSetReplicas != lifecycle.Target.TargetReplicas {
		return replanLifecycle()
	}

	podsConverged, identitiesChanged := observeScaleOutPods(lifecycle, observed)
	if identitiesChanged {
		return LifecycleTransition{Lifecycle: lifecycle}
	}
	if !podsConverged {
		return LifecycleTransition{Lifecycle: lifecycle}
	}
	if !observed.SatisfiedTargetMembership {
		return LifecycleTransition{Lifecycle: lifecycle}
	}

	return completeLifecycle(lifecycle, observed.Now)
}

func advanceSetReplicas(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, observed LifecycleObservation) (LifecycleTransition, bool) {
	if lifecycle.Checkpoint != enterpriseApi.IndexerClusterLifecycleActionPending {
		return LifecycleTransition{}, false
	}

	switch observed.StatefulSetReplicas {
	case lifecycle.Target.SourceReplicas:
		return LifecycleTransition{
			Lifecycle: lifecycle,
			Execute:   lifecycle.PendingAction.DeepCopy(),
		}, true
	case lifecycle.Target.TargetReplicas:
		lifecycle.PendingAction = nil
		setLifecycleCheckpoint(lifecycle, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, observed.Now)
		return LifecycleTransition{Lifecycle: lifecycle}, true
	default:
		return failLifecycle(lifecycle, observed.Now, fmt.Errorf(
			"%w: StatefulSet has %d replicas, expected source %d or target %d",
			ErrInvalidLifecycle,
			observed.StatefulSetReplicas,
			lifecycle.Target.SourceReplicas,
			lifecycle.Target.TargetReplicas,
		)), true
	}
}

func observeScaleOutPods(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, observed LifecycleObservation) (bool, bool) {
	converged := true
	identitiesChanged := false
	for i := range lifecycle.Target.Peers {
		target := &lifecycle.Target.Peers[i]

		pod, found := observed.TargetPods[target.Ordinal]
		if !found || pod.UID == "" {
			converged = false
			continue
		}

		if target.TargetPodUID != pod.UID {
			target.TargetPodUID = pod.UID
			identitiesChanged = true
		}

		if !pod.Ready {
			converged = false
		}
	}
	return converged, identitiesChanged
}

func validateLifecycle(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus) error {
	if lifecycle == nil {
		return fmt.Errorf("%w: lifecycle is nil", ErrInvalidLifecycle)
	}
	if lifecycle.Generation <= 0 {
		return fmt.Errorf("%w: generation must be positive", ErrInvalidLifecycle)
	}
	if lifecycle.StartedAt.IsZero() || lifecycle.LastTransitionTime.IsZero() {
		return fmt.Errorf("%w: lifecycle timestamps must be set", ErrInvalidLifecycle)
	}
	if lifecycle.Target.StatefulSetUID == "" {
		return fmt.Errorf("%w: StatefulSet UID must be set", ErrInvalidLifecycle)
	}

	switch lifecycle.Kind {
	case enterpriseApi.IndexerClusterLifecycleScaleOut:
		if err := validateScaleOutTarget(lifecycle.Target); err != nil {
			return err
		}
	case enterpriseApi.IndexerClusterLifecycleRollout:
		if err := validateRolloutTarget(lifecycle); err != nil {
			return err
		}
	case enterpriseApi.IndexerClusterLifecycleScaleIn:
		if err := validateScaleInTarget(lifecycle.Target); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unsupported kind %q", ErrInvalidLifecycle, lifecycle.Kind)
	}

	actionPending := lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleActionPending
	if actionPending != (lifecycle.PendingAction != nil) {
		return fmt.Errorf("%w: checkpoint %q and pending action are inconsistent", ErrInvalidLifecycle, lifecycle.Checkpoint)
	}
	if lifecycle.PendingAction != nil && lifecycle.PendingAction.Type != lifecycleAction(lifecycle.Kind) {
		return fmt.Errorf("%w: unsupported %s action %q", ErrInvalidLifecycle, lifecycle.Kind, lifecycle.PendingAction.Type)
	}
	if lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleCompleted && lifecycle.CompletedAt == nil {
		return fmt.Errorf("%w: completed lifecycle has no completion time", ErrInvalidLifecycle)
	}
	if lifecycle.Checkpoint != enterpriseApi.IndexerClusterLifecycleCompleted && lifecycle.CompletedAt != nil {
		return fmt.Errorf("%w: incomplete lifecycle has a completion time", ErrInvalidLifecycle)
	}

	return nil
}

func lifecycleAction(kind enterpriseApi.IndexerClusterLifecycleKind) enterpriseApi.IndexerClusterLifecycleActionType {
	switch kind {
	case enterpriseApi.IndexerClusterLifecycleScaleOut, enterpriseApi.IndexerClusterLifecycleScaleIn:
		return enterpriseApi.IndexerClusterLifecycleSetReplicas
	case enterpriseApi.IndexerClusterLifecycleRollout:
		return enterpriseApi.IndexerClusterLifecycleDeletePod
	default:
		return ""
	}
}

func validateScaleInTarget(target enterpriseApi.IndexerClusterLifecycleTarget) error {
	if target.SourceReplicas <= 0 || target.TargetReplicas != target.SourceReplicas-1 {
		return fmt.Errorf(
			"%w: scale-in target must reduce replicas by one from %d to %d",
			ErrInvalidLifecycle,
			target.SourceReplicas,
			target.TargetReplicas,
		)
	}
	if len(target.Peers) != 1 {
		return fmt.Errorf("%w: scale-in must target exactly one peer", ErrInvalidLifecycle)
	}

	peer := target.Peers[0]
	if peer.Ordinal != target.TargetReplicas || peer.PeerID == "" || peer.PodName == "" || peer.SourcePodUID == "" {
		return fmt.Errorf("%w: scale-in peer has an incomplete or non-highest identity", ErrInvalidLifecycle)
	}
	if peer.TargetPodUID != "" {
		return fmt.Errorf("%w: scale-in peer must not have a target Pod UID", ErrInvalidLifecycle)
	}

	return nil
}

func validateRolloutTarget(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus) error {
	target := lifecycle.Target
	if target.SourceReplicas <= 0 || target.TargetReplicas != target.SourceReplicas {
		return fmt.Errorf(
			"%w: rollout target must preserve a positive replica count, got %d to %d",
			ErrInvalidLifecycle,
			target.SourceReplicas,
			target.TargetReplicas,
		)
	}
	if target.SourceRevision == "" || target.TargetRevision == "" || target.SourceRevision == target.TargetRevision {
		return fmt.Errorf("%w: rollout requires distinct source and target revisions", ErrInvalidLifecycle)
	}
	if len(target.Peers) != 1 {
		return fmt.Errorf("%w: rollout must target exactly one peer", ErrInvalidLifecycle)
	}

	peer := target.Peers[0]
	if peer.Ordinal < 0 || peer.Ordinal >= target.SourceReplicas || peer.PeerID == "" || peer.PodName == "" || peer.SourcePodUID == "" {
		return fmt.Errorf("%w: rollout peer has an incomplete identity", ErrInvalidLifecycle)
	}
	if lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleActionPending && peer.TargetPodUID != "" {
		return fmt.Errorf("%w: pending rollout must not have a target Pod UID", ErrInvalidLifecycle)
	}
	if lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleWaitingForMembership && peer.TargetPodUID == "" {
		return fmt.Errorf("%w: rollout waiting for membership has no target Pod UID", ErrInvalidLifecycle)
	}
	if peer.TargetPodUID != "" && peer.TargetPodUID == peer.SourcePodUID {
		return fmt.Errorf("%w: rollout source and target Pod UIDs must differ", ErrInvalidLifecycle)
	}

	return nil
}

func validateScaleOutTarget(target enterpriseApi.IndexerClusterLifecycleTarget) error {
	if target.SourceReplicas < 0 || target.TargetReplicas <= target.SourceReplicas {
		return fmt.Errorf(
			"%w: scale-out target must increase replicas from %d to %d",
			ErrInvalidLifecycle,
			target.SourceReplicas,
			target.TargetReplicas,
		)
	}
	if int32(len(target.Peers)) != target.TargetReplicas-target.SourceReplicas {
		return fmt.Errorf("%w: scale-out peer count does not match the replica range", ErrInvalidLifecycle)
	}

	peersByOrdinal := make(map[int32]enterpriseApi.IndexerClusterLifecyclePeerTarget, len(target.Peers))
	for _, peer := range target.Peers {
		if peer.PeerID == "" || peer.PodName == "" {
			return fmt.Errorf("%w: scale-out peer %d has an incomplete identity", ErrInvalidLifecycle, peer.Ordinal)
		}
		if peer.SourcePodUID != "" {
			return fmt.Errorf("%w: scale-out peer %d must not have a source Pod UID", ErrInvalidLifecycle, peer.Ordinal)
		}
		if _, duplicate := peersByOrdinal[peer.Ordinal]; duplicate {
			return fmt.Errorf("%w: duplicate scale-out peer ordinal %d", ErrInvalidLifecycle, peer.Ordinal)
		}

		peersByOrdinal[peer.Ordinal] = peer
	}

	for ordinal := target.SourceReplicas; ordinal < target.TargetReplicas; ordinal++ {
		if _, found := peersByOrdinal[ordinal]; !found {
			return fmt.Errorf("%w: scale-out peer ordinal %d is missing", ErrInvalidLifecycle, ordinal)
		}
	}

	return nil
}

func setLifecycleCheckpoint(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, checkpoint enterpriseApi.IndexerClusterLifecycleCheckpoint, now time.Time) {
	lifecycle.Checkpoint = checkpoint
	lifecycle.LastTransitionTime = metav1.NewTime(now)
}

func setLifecycleTargetPodUID(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, target *enterpriseApi.IndexerClusterLifecyclePeerTarget, uid k8stypes.UID, now time.Time) {
	target.TargetPodUID = uid
	lifecycle.LastTransitionTime = metav1.NewTime(now)
}

func completeLifecycle(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, now time.Time) LifecycleTransition {
	lifecycle.PendingAction = nil
	setLifecycleCheckpoint(lifecycle, enterpriseApi.IndexerClusterLifecycleCompleted, now)
	lifecycle.CompletedAt = new(metav1.NewTime(now))
	return LifecycleTransition{Lifecycle: lifecycle}
}

func replanLifecycle() LifecycleTransition {
	return LifecycleTransition{Replan: true}
}

func failLifecycle(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, now time.Time, err error) LifecycleTransition {
	lifecycle.PendingAction = nil
	lifecycle.Checkpoint = enterpriseApi.IndexerClusterLifecycleFailed
	if !now.IsZero() {
		lifecycle.LastTransitionTime = metav1.NewTime(now)
	}
	return LifecycleTransition{Lifecycle: lifecycle, Err: err}
}
