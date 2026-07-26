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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	if !mgr.cr.Status.Initialized || !mgr.cr.Status.MinPeersJoined || !mgr.cr.Status.CaptainReady {
		return corev1.ConditionFalse, "ClusterNotReady", "SHC formation or captain readiness is incomplete"
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation != nil &&
		operation.TargetOrdinal != nil &&
		*operation.TargetOrdinal == ordinal &&
		operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		return corev1.ConditionFalse, "LifecycleOperationActive", fmt.Sprintf(
			"SHC lifecycle operation is in stage %s",
			operation.Stage,
		)
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
	return podConditionStatus(pod, searchHeadServingCondition) == corev1.ConditionFalse &&
		podConditionStatus(pod, corev1.PodReady) == corev1.ConditionFalse, nil
}
