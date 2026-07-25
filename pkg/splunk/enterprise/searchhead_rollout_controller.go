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
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/workflow/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

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
	decision := upgrade.EvaluateSHCRollout(state)

	switch decision.Action {
	case upgrade.SHCRolloutActionPrepareTarget:
		if decision.TargetOrdinal == nil {
			return enterpriseApi.PhaseError, fmt.Errorf(
				"SHC rollout preparation has no target: %s",
				decision.Message,
			)
		}
		_, err := mgr.PrepareRecycle(ctx, *decision.TargetOrdinal)
		if err != nil {
			return enterpriseApi.PhaseError, err
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
		return enterpriseApi.PhaseUpdating, nil

	case upgrade.SHCRolloutActionWait, upgrade.SHCRolloutActionNone:
		return enterpriseApi.PhaseUpdating, nil

	case upgrade.SHCRolloutActionComplete:
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
