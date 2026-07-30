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
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const searchHeadServingCondition corev1.PodConditionType = "enterprise.splunk.com/shc-serving"

func applySearchHeadServingReadinessGate(spec *corev1.PodSpec, instanceType InstanceType) {
	if spec == nil || !searchHeadPodLifecycleEnabled(instanceType) {
		return
	}
	for _, gate := range spec.ReadinessGates {
		if gate.ConditionType == searchHeadServingCondition {
			return
		}
	}
	spec.ReadinessGates = append(spec.ReadinessGates, corev1.PodReadinessGate{
		ConditionType: searchHeadServingCondition,
	})
}

func searchHeadServingReadinessGateConfigured(statefulSet *appsv1.StatefulSet) bool {
	if statefulSet == nil {
		return false
	}
	for _, gate := range statefulSet.Spec.Template.Spec.ReadinessGates {
		if gate.ConditionType == searchHeadServingCondition {
			return true
		}
	}
	return false
}

func podConditionStatus(pod *corev1.Pod, conditionType corev1.PodConditionType) corev1.ConditionStatus {
	if pod == nil {
		return corev1.ConditionUnknown
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return corev1.ConditionUnknown
}

// searchHeadRollingUpdatePending reports the interval after an existing SHC
// StatefulSet's Pod template changes but before the durable per-Pod lifecycle
// operation is recorded. The partition prevents Kubernetes from replacing a
// member during this interval, so healthy members must remain eligible for
// Service traffic even if the cluster-wide captain observation is transiently
// unavailable while the new revision is being established.
func (mgr *searchHeadClusterPodManager) searchHeadRollingUpdatePending() bool {
	statefulSet := mgr.statefulSet
	if statefulSet == nil ||
		statefulSet.Spec.UpdateStrategy.Type != appsv1.RollingUpdateStatefulSetStrategyType ||
		statefulSet.Status.CurrentRevision == "" {
		return false
	}
	if mgr.statefulSetUpdatePending ||
		statefulSet.Generation > statefulSet.Status.ObservedGeneration {
		return true
	}
	return statefulSet.Status.UpdateRevision != "" &&
		statefulSet.Status.CurrentRevision != statefulSet.Status.UpdateRevision
}

// searchHeadTopologyPreviouslyStable distinguishes an established replica
// topology from initial formation. LastStableReplicas is recorded only after
// the SHC and every desired Pod have reached Ready. It remains durable while a
// Splunk-managed operation, such as an App Framework bundle push, restarts
// splunkd inside otherwise unchanged Pods.
func (mgr *searchHeadClusterPodManager) searchHeadTopologyPreviouslyStable() bool {
	if mgr.statefulSet == nil ||
		mgr.statefulSet.Spec.Replicas == nil ||
		mgr.cr.Status.LastStableReplicas == nil {
		return false
	}
	return *mgr.cr.Status.LastStableReplicas ==
		*mgr.statefulSet.Spec.Replicas
}

func (mgr *searchHeadClusterPodManager) desiredSearchHeadServingCondition(
	pod *corev1.Pod,
	ordinal int32,
) (corev1.ConditionStatus, string, string) {
	if pod == nil || pod.DeletionTimestamp != nil {
		return corev1.ConditionFalse, "PodTerminating", "Pod is terminating"
	}
	if podConditionStatus(pod, corev1.ContainersReady) != corev1.ConditionTrue {
		return corev1.ConditionFalse, "ContainersNotReady", "Splunk container readiness has not succeeded"
	}
	if ordinal < 0 || ordinal >= int32(len(mgr.cr.Status.Members)) {
		return corev1.ConditionFalse, "MemberUnobserved", "SHC member has not been observed"
	}
	member := mgr.cr.Status.Members[ordinal]
	if !member.Registered {
		return corev1.ConditionFalse, "MemberNotRegistered", "SHC member is not registered with the captain"
	}
	if member.Status != "Up" {
		return corev1.ConditionFalse, "MemberNotUp", fmt.Sprintf("SHC member status is %q", member.Status)
	}
	operation := mgr.cr.Status.LifecycleOperation
	lifecycleActive := operation != nil &&
		operation.TargetOrdinal != nil &&
		operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	if lifecycleActive && *operation.TargetOrdinal == ordinal {
		return corev1.ConditionFalse, "LifecycleOperationActive", fmt.Sprintf(
			"SHC lifecycle operation is in stage %s",
			operation.Stage,
		)
	}
	clusterReady := mgr.cr.Status.Initialized &&
		mgr.cr.Status.MinPeersJoined &&
		mgr.cr.Status.CaptainReady
	if !clusterReady && !lifecycleActive {
		if mgr.searchHeadRollingUpdatePending() {
			return corev1.ConditionTrue, "PeerServingDuringRolloutPlanning",
				"healthy SHC member remains eligible while the coordinated rolling revision is established"
		}
		if mgr.searchHeadTopologyPreviouslyStable() {
			return corev1.ConditionTrue, "MemberServingAfterStableFormation",
				"healthy SHC member remains eligible while cluster-wide readiness observation recovers"
		}
		return corev1.ConditionFalse, "ClusterNotReady", "SHC formation or captain readiness is incomplete"
	}
	if !clusterReady {
		return corev1.ConditionTrue, "PeerServingDuringLifecycle",
			"healthy non-target SHC member remains eligible during lifecycle orchestration"
	}
	return corev1.ConditionTrue, "MemberServing", "SHC member is eligible for Kubernetes Service traffic"
}

func (mgr *searchHeadClusterPodManager) reconcileSearchHeadServingConditions(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
) error {
	if !searchHeadServingReadinessGateConfigured(statefulSet) {
		return nil
	}
	mgr.servingConditionChanged = make(map[int32]bool)
	for ordinal := int32(0); ordinal < statefulSet.Status.Replicas; ordinal++ {
		podName := GetSplunkStatefulsetPodName(
			SplunkSearchHead,
			mgr.cr.GetName(),
			ordinal,
		)
		pod := &corev1.Pod{}
		err := mgr.c.Get(ctx, types.NamespacedName{
			Namespace: mgr.cr.GetNamespace(),
			Name:      podName,
		}, pod)
		if k8serrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("get Search Head Pod %s for serving readiness: %w", podName, err)
		}
		status, reason, message := mgr.desiredSearchHeadServingCondition(pod, ordinal)
		changed, err := mgr.setSearchHeadServingCondition(ctx, pod, status, reason, message)
		if err != nil {
			return err
		}
		mgr.servingConditionChanged[ordinal] = changed
	}
	return nil
}

// CanProceedWithPodUpdateDespiteNotReadyReplicas allows the legacy OnDelete
// replacement loop to continue after the lifecycle controller intentionally
// withdraws exactly one target from Service traffic. Every durable operation,
// revision, target, Pod UID, container, serving-gate, and healthy-peer
// invariant is revalidated. Any unrelated unready Pod keeps the StatefulSet
// fail closed.
func (mgr *searchHeadClusterPodManager) CanProceedWithPodUpdateDespiteNotReadyReplicas(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
	desiredReplicas int32,
) (bool, error) {
	return mgr.canProceedWithOwnedReadinessWithdrawal(
		ctx,
		statefulSet,
		desiredReplicas,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
}

// CanProceedWithScaleDownDespiteNotReadyReplicas proves that exactly the
// highest ordinal is not ready because the active durable SHC scale-down
// operation withdrew it from Service traffic. This permits the generic
// StatefulSet controller to re-enter PrepareScaleDown without weakening its
// readiness checks for other workloads or unrelated failures.
func (mgr *searchHeadClusterPodManager) CanProceedWithScaleDownDespiteNotReadyReplicas(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
	desiredReplicas int32,
) (bool, error) {
	return mgr.canProceedWithOwnedReadinessWithdrawal(
		ctx,
		statefulSet,
		desiredReplicas,
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
	)
}

func (mgr *searchHeadClusterPodManager) canProceedWithOwnedReadinessWithdrawal(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
	desiredReplicas int32,
	intent enterpriseApi.SearchHeadClusterLifecycleIntent,
) (bool, error) {
	if !searchHeadClusterLifecycleEnabled() ||
		statefulSet == nil ||
		!searchHeadServingReadinessGateConfigured(statefulSet) ||
		statefulSet.Spec.Replicas == nil {
		return false, nil
	}
	replicas := *statefulSet.Spec.Replicas
	if replicas < 3 ||
		statefulSet.Status.Replicas != replicas ||
		statefulSet.Status.ReadyReplicas != replicas-1 {
		return false, nil
	}

	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.Intent != intent ||
		operation.TargetOrdinal == nil ||
		operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted ||
		operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
		operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageFailed {
		return false, nil
	}
	targetOrdinal := *operation.TargetOrdinal
	if targetOrdinal < 0 || targetOrdinal >= replicas {
		return false, nil
	}
	switch intent {
	case enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate:
		if statefulSet.Spec.UpdateStrategy.Type !=
			appsv1.OnDeleteStatefulSetStrategyType ||
			desiredReplicas != replicas ||
			operation.DesiredRevision == "" ||
			operation.DesiredRevision != statefulSet.Status.UpdateRevision {
			return false, nil
		}
	case enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown:
		if desiredReplicas >= replicas || targetOrdinal != replicas-1 {
			return false, nil
		}
	default:
		return false, nil
	}

	for ordinal := int32(0); ordinal < replicas; ordinal++ {
		podName := GetSplunkStatefulsetPodName(
			SplunkSearchHead,
			mgr.cr.GetName(),
			ordinal,
		)
		pod := &corev1.Pod{}
		if err := mgr.c.Get(ctx, types.NamespacedName{
			Namespace: mgr.cr.GetNamespace(),
			Name:      podName,
		}, pod); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf(
				"get Search Head Pod %s while validating readiness withdrawal: %w",
				podName,
				err,
			)
		}
		if pod.DeletionTimestamp != nil ||
			podConditionStatus(pod, corev1.ContainersReady) != corev1.ConditionTrue {
			return false, nil
		}
		if ordinal != targetOrdinal {
			if podConditionStatus(pod, corev1.PodReady) != corev1.ConditionTrue {
				return false, nil
			}
			continue
		}
		if operation.TargetPod != "" && operation.TargetPod != pod.Name {
			return false, nil
		}
		if operation.TargetPodUID != "" &&
			operation.TargetPodUID != string(pod.UID) {
			return false, nil
		}
		servingConditionMatched := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type != searchHeadServingCondition ||
				condition.Status != corev1.ConditionFalse {
				continue
			}
			servingConditionMatched =
				condition.Reason == "LifecycleOperationActive"
			if !servingConditionMatched &&
				condition.Reason == "MemberNotUp" &&
				targetOrdinal < int32(len(mgr.cr.Status.Members)) {
				targetMember := mgr.cr.Status.Members[targetOrdinal]
				servingConditionMatched =
					targetMember.Name == pod.Name &&
						targetMember.Registered &&
						targetMember.Status == "ManualDetention"
			}
			break
		}
		if !servingConditionMatched ||
			podConditionStatus(pod, corev1.PodReady) != corev1.ConditionFalse {
			return false, nil
		}
	}
	return true, nil
}

func (mgr *searchHeadClusterPodManager) setSearchHeadServingCondition(
	ctx context.Context,
	pod *corev1.Pod,
	status corev1.ConditionStatus,
	reason string,
	message string,
) (bool, error) {
	before := pod.DeepCopy()
	now := metav1.Now()
	for index := range pod.Status.Conditions {
		condition := &pod.Status.Conditions[index]
		if condition.Type != searchHeadServingCondition {
			continue
		}
		if condition.Status == status && condition.Reason == reason && condition.Message == message {
			return false, nil
		}
		if condition.Status != status {
			condition.LastTransitionTime = now
		}
		condition.Status = status
		condition.Reason = reason
		condition.Message = message
		if err := mgr.c.Status().Patch(ctx, pod, client.MergeFrom(before)); err != nil {
			return false, fmt.Errorf("patch Search Head Pod %s serving condition: %w", pod.Name, err)
		}
		return true, nil
	}
	pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
		Type:               searchHeadServingCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
	if err := mgr.c.Status().Patch(ctx, pod, client.MergeFrom(before)); err != nil {
		return false, fmt.Errorf("patch Search Head Pod %s serving condition: %w", pod.Name, err)
	}
	return true, nil
}

func (mgr *searchHeadClusterPodManager) searchHeadServingWithdrawalObserved(
	ctx context.Context,
	ordinal int32,
) (bool, error) {
	if mgr.servingConditionChanged[ordinal] {
		return false, nil
	}
	podName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), ordinal)
	pod := &corev1.Pod{}
	if err := mgr.c.Get(ctx, types.NamespacedName{
		Namespace: mgr.cr.GetNamespace(),
		Name:      podName,
	}, pod); err != nil {
		return false, fmt.Errorf("get Search Head Pod %s before detention: %w", podName, err)
	}
	if podConditionStatus(pod, searchHeadServingCondition) != corev1.ConditionFalse ||
		podConditionStatus(pod, corev1.PodReady) != corev1.ConditionFalse {
		return false, nil
	}

	endpointSlices := &discoveryv1.EndpointSliceList{}
	serviceName := splcommon.GetSplunkServiceName(
		SplunkSearchHead,
		mgr.cr.GetName(),
		false,
	)
	if err := mgr.c.List(
		ctx,
		endpointSlices,
		client.InNamespace(mgr.cr.GetNamespace()),
		client.MatchingLabels{discoveryv1.LabelServiceName: serviceName},
	); err != nil {
		return false, fmt.Errorf(
			"list EndpointSlices for Search Head Service %s before detention: %w",
			serviceName,
			err,
		)
	}
	return !endpointSlicesRoutePod(endpointSlices.Items, pod), nil
}

// endpointSlicesRoutePod reports whether a Service EndpointSlice still makes
// the Pod eligible for routing. A nil ready condition means "unknown" in the
// API and is deliberately treated as routable so lifecycle actions fail
// closed until withdrawal is explicit or the endpoint disappears.
func endpointSlicesRoutePod(endpointSlices []discoveryv1.EndpointSlice, pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for sliceIndex := range endpointSlices {
		for endpointIndex := range endpointSlices[sliceIndex].Endpoints {
			endpoint := &endpointSlices[sliceIndex].Endpoints[endpointIndex]
			target := endpoint.TargetRef
			if target == nil || target.Name != pod.Name {
				continue
			}
			if pod.UID != "" && target.UID != "" && target.UID != pod.UID {
				continue
			}
			if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
				return true
			}
		}
	}
	return false
}
