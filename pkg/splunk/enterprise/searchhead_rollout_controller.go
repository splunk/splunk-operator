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

package enterprise

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splmetrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/workflow/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

const shcRollingUpdateStatusPrefix = "SHC RollingUpdate "

// updateRollingStatefulSetPods adapts Kubernetes and durable SHC observations
// to the pure rollout coordinator. It never deletes a Pod. Replica-count
// changes remain on the existing lifecycle-aware scaling path.
func (mgr *searchHeadClusterPodManager) updateRollingStatefulSetPods(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
	desiredReplicas int32,
) (enterpriseApi.Phase, error) {
	if statefulSet.Spec.Replicas == nil {
		return enterpriseApi.PhaseError, fmt.Errorf(
			"RollingUpdate StatefulSet %s has no replica count",
			statefulSet.GetName(),
		)
	}
	if *statefulSet.Spec.Replicas != desiredReplicas {
		return splctrl.UpdateStatefulSetPods(
			ctx,
			mgr.c,
			statefulSet,
			mgr,
			desiredReplicas,
		)
	}
	if statefulSet.Status.Replicas > desiredReplicas {
		return enterpriseApi.PhaseScalingDown, nil
	}
	if statefulSet.Status.Replicas < desiredReplicas &&
		statefulSet.Status.CurrentRevision == statefulSet.Status.UpdateRevision {
		return enterpriseApi.PhaseScalingUp, nil
	}

	state, err := mgr.observeRollingStatefulSet(ctx, statefulSet)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}
	policy, err := ResolveSearchHeadClusterLifecyclePolicy(&mgr.cr.Spec)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}
	if policy.PodUpdateStrategy !=
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate {
		decision := upgrade.SHCRolloutDecision{
			Action: upgrade.SHCRolloutActionWait,
			Reason: upgrade.SHCRolloutReasonRollbackPending,
			Message: fmt.Sprintf(
				"rollback requested; hold partition %d until the active lifecycle operation completes recovery",
				state.Partition,
			),
		}
		if mgr.cr.Status.LifecycleOperation != nil {
			decision.TargetOrdinal =
				mgr.cr.Status.LifecycleOperation.TargetOrdinal
		}
		mgr.recordRollingUpdateDecision(ctx, state, decision)
		return enterpriseApi.PhaseUpdating, nil
	}
	decision := upgrade.EvaluateSHCRollout(state)
	mgr.recordRollingUpdateDecision(ctx, state, decision)

	switch decision.Action {
	case upgrade.SHCRolloutActionPrepareTarget:
		if decision.TargetOrdinal == nil {
			return enterpriseApi.PhaseError, fmt.Errorf(
				"SHC rollout preparation has no target: %s",
				decision.Message,
			)
		}
		operationBefore := mgr.cr.Status.LifecycleOperation
		startingTarget := !rolloutOperationMatches(
			operationBefore,
			statefulSet.Status.UpdateRevision,
			*decision.TargetOrdinal,
		)
		_, err := mgr.PrepareRecycle(ctx, *decision.TargetOrdinal)
		if err != nil {
			return enterpriseApi.PhaseError, err
		}
		if startingTarget {
			eventPublisher := GetEventPublisher(ctx, mgr.cr)
			eventPublisher.Normal(
				ctx,
				EventReasonSHCRolloutTargetStarted,
				fmt.Sprintf(
					"Preparing Search Head ordinal %d for revision %s",
					*decision.TargetOrdinal,
					statefulSet.Status.UpdateRevision,
				),
			)
		}
		return enterpriseApi.PhaseUpdating, nil

	case upgrade.SHCRolloutActionSetPartition:
		if decision.DesiredPartition == nil ||
			statefulSet.Spec.UpdateStrategy.RollingUpdate == nil {
			return enterpriseApi.PhaseError, fmt.Errorf(
				"SHC rollout partition decision is incomplete: %s",
				decision.Message,
			)
		}
		partition := *decision.DesiredPartition
		statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition = &partition
		if err := splutil.UpdateResource(ctx, mgr.c, statefulSet); err != nil {
			return enterpriseApi.PhaseError, err
		}
		splmetrics.SHCRolloutPartitionAdvanceCounter.Inc()
		GetEventPublisher(ctx, mgr.cr).Normal(
			ctx,
			EventReasonSHCRolloutAdvanced,
			fmt.Sprintf(
				"Authorized StatefulSet partition advancement to ordinal %d",
				partition,
			),
		)
		return enterpriseApi.PhaseUpdating, nil

	case upgrade.SHCRolloutActionWait, upgrade.SHCRolloutActionNone:
		return enterpriseApi.PhaseUpdating, nil

	case upgrade.SHCRolloutActionComplete:
		if statefulSet.Spec.UpdateStrategy.RollingUpdate == nil ||
			statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
			return enterpriseApi.PhaseError, fmt.Errorf(
				"SHC rollout completion has no partition",
			)
		}
		if *statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition !=
			*statefulSet.Spec.Replicas {
			partition := *statefulSet.Spec.Replicas
			statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition = &partition
			if err := splutil.UpdateResource(ctx, mgr.c, statefulSet); err != nil {
				return enterpriseApi.PhaseError, err
			}
			splmetrics.SHCRolloutPartitionAdvanceCounter.Inc()
			GetEventPublisher(ctx, mgr.cr).Normal(
				ctx,
				EventReasonSHCRolloutCompleted,
				fmt.Sprintf(
					"Search Head rollout recovered; reset StatefulSet partition to %d",
					partition,
				),
			)
			return enterpriseApi.PhaseUpdating, nil
		}
		if err := mgr.FinishUpgrade(ctx, 0); err != nil {
			return enterpriseApi.PhaseError, err
		}
		return enterpriseApi.PhaseReady, nil

	case upgrade.SHCRolloutActionBlock:
		return enterpriseApi.PhaseError, fmt.Errorf(
			"SHC RollingUpdate blocked (%s): %s",
			decision.Reason,
			decision.Message,
		)

	default:
		return enterpriseApi.PhaseError, fmt.Errorf(
			"unsupported SHC rollout action %q",
			decision.Action,
		)
	}
}

// lifecycleRecoveryActiveForStatefulSet prevents the recovery state machine
// from waiting for termination before a partitioned RollingUpdate has made
// the authorized target eligible for replacement. OnDelete retains the
// existing Operator-owned replacement ordering.
func lifecycleRecoveryActiveForStatefulSet(
	statefulSet *appsv1.StatefulSet,
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) bool {
	if !lifecycleRecoveryActive(operation) {
		return false
	}
	if statefulSet == nil ||
		statefulSet.Spec.UpdateStrategy.Type != appsv1.RollingUpdateStatefulSetStrategyType {
		return true
	}
	if operation.TargetOrdinal == nil ||
		statefulSet.Spec.UpdateStrategy.RollingUpdate == nil ||
		statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
		return false
	}

	return *statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition ==
		*operation.TargetOrdinal
}

// recordRollingUpdateObservation projects a coordinator decision without
// executing it. Recovery orchestration uses this on its early-return path so
// waiting and blocked time remains visible without creating a second actor.
func (mgr *searchHeadClusterPodManager) recordRollingUpdateObservation(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
) error {
	state, err := mgr.observeRollingStatefulSet(ctx, statefulSet)
	if err != nil {
		return err
	}
	decision := upgrade.EvaluateSHCRollout(state)
	mgr.recordRollingUpdateDecision(ctx, state, decision)
	return nil
}

func (mgr *searchHeadClusterPodManager) recordRollingUpdateDecision(
	ctx context.Context,
	state upgrade.SHCRolloutState,
	decision upgrade.SHCRolloutDecision,
) {
	stable := decision.Action == upgrade.SHCRolloutActionComplete &&
		state.Partition == state.Replicas &&
		state.CurrentRevision == state.UpdateRevision
	if stable {
		mgr.cr.Status.Message = ""
		return
	}

	splmetrics.SHCRolloutDecisionCounters.WithLabelValues(
		string(decision.Action),
		string(decision.Reason),
	).Inc()
	mgr.cr.Status.Message = fmt.Sprintf(
		"%s%s: %s",
		shcRollingUpdateStatusPrefix,
		decision.Reason,
		decision.Message,
	)

	target := int32(-1)
	if decision.TargetOrdinal != nil {
		target = *decision.TargetOrdinal
	}
	logging.FromContext(ctx).InfoContext(
		ctx,
		"Search Head rollout decision",
		"action", decision.Action,
		"reason", decision.Reason,
		"message", decision.Message,
		"partition", state.Partition,
		"replicas", state.Replicas,
		"targetOrdinal", target,
		"currentRevision", state.CurrentRevision,
		"updateRevision", state.UpdateRevision,
		"lifecycleStage", state.Lifecycle.Stage,
	)
	if decision.Action == upgrade.SHCRolloutActionBlock {
		GetEventPublisher(ctx, mgr.cr).Warning(
			ctx,
			EventReasonSHCRolloutBlocked,
			fmt.Sprintf("%s: %s", decision.Reason, decision.Message),
		)
	}
}

func rolloutOperationMatches(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	desiredRevision string,
	targetOrdinal int32,
) bool {
	return operation != nil &&
		operation.Intent == enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate &&
		operation.DesiredRevision == desiredRevision &&
		operation.TargetOrdinal != nil &&
		*operation.TargetOrdinal == targetOrdinal
}

func (mgr *searchHeadClusterPodManager) observeRollingStatefulSet(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
) (upgrade.SHCRolloutState, error) {
	if statefulSet.Spec.Replicas == nil ||
		statefulSet.Spec.UpdateStrategy.RollingUpdate == nil ||
		statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
		return upgrade.SHCRolloutState{}, fmt.Errorf(
			"StatefulSet %s has an incomplete RollingUpdate strategy",
			statefulSet.GetName(),
		)
	}

	replicas := *statefulSet.Spec.Replicas
	state := upgrade.SHCRolloutState{
		Replicas:        replicas,
		Partition:       *statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition,
		CurrentRevision: statefulSet.Status.CurrentRevision,
		UpdateRevision:  statefulSet.Status.UpdateRevision,
		Paused:          mgr.cr.GetAnnotations()[enterpriseApi.SearchHeadClusterPausedAnnotation] == "true",
		Pods:            make([]upgrade.SHCRolloutPod, 0, replicas),
	}

	operation := mgr.cr.Status.LifecycleOperation
	if operation != nil &&
		operation.Intent == enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate &&
		operation.DesiredRevision == statefulSet.Status.UpdateRevision {
		state.Lifecycle = upgrade.SHCRolloutLifecycle{
			TargetOrdinal:         operation.TargetOrdinal,
			Stage:                 operation.Stage,
			ReplacementAuthorized: operation.ReplacementAuthorizedAt != nil,
		}
	}

	for ordinal := int32(0); ordinal < replicas; ordinal++ {
		pod := &corev1.Pod{}
		err := mgr.c.Get(ctx, types.NamespacedName{
			Namespace: statefulSet.GetNamespace(),
			Name:      fmt.Sprintf("%s-%d", statefulSet.GetName(), ordinal),
		}, pod)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				state.Pods = append(state.Pods, upgrade.SHCRolloutPod{
					Ordinal: ordinal,
				})
				continue
			}
			return upgrade.SHCRolloutState{}, err
		}

		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				ready = condition.Status == corev1.ConditionTrue
				break
			}
		}
		state.Pods = append(state.Pods, upgrade.SHCRolloutPod{
			Ordinal:  ordinal,
			Exists:   true,
			Ready:    ready,
			Deleting: pod.DeletionTimestamp != nil,
			Revision: pod.GetLabels()["controller-revision-hash"],
		})
	}

	return state, nil
}
