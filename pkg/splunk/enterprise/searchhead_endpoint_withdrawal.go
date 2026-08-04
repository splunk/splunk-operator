// Copyright (c) 2026 Splunk Inc. All rights reserved.
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
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splmetrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var searchHeadEndpointWithdrawalNow = time.Now

// ensureSearchHeadEndpointWithdrawalBarrier proves that Kubernetes has
// continuously withdrawn the exact lifecycle target from client traffic for
// the configured propagation interval. It fails closed across reconciliations
// and controller restarts by preserving the proof in lifecycle status.
func (mgr *searchHeadClusterPodManager) ensureSearchHeadEndpointWithdrawalBarrier(
	ctx context.Context,
	ordinal int32,
	delay time.Duration,
) (bool, error) {
	operation := mgr.cr.Status.LifecycleOperation
	if delay <= 0 {
		return false, fmt.Errorf(
			"Search Head endpoint-withdrawal delay must be positive",
		)
	}
	if operation == nil || operation.TargetOrdinal == nil {
		return false, nil
	}
	expectedPodName := GetSplunkStatefulsetPodName(
		SplunkSearchHead,
		mgr.cr.GetName(),
		ordinal,
	)
	if *operation.TargetOrdinal != ordinal ||
		operation.TargetPod != expectedPodName ||
		operation.TargetPodUID == "" {
		return false, fmt.Errorf(
			"Search Head endpoint-withdrawal target does not match lifecycle operation %s",
			operation.OperationID,
		)
	}

	targetPod := &corev1.Pod{}
	if err := mgr.c.Get(ctx, types.NamespacedName{
		Namespace: mgr.cr.GetNamespace(),
		Name:      operation.TargetPod,
	}, targetPod); err != nil {
		return false, fmt.Errorf(
			"get Search Head Pod %s before detention: %w",
			operation.TargetPod,
			err,
		)
	}
	if string(targetPod.UID) != operation.TargetPodUID {
		return false, fmt.Errorf(
			"Search Head endpoint-withdrawal target UID changed from %s to %s",
			operation.TargetPodUID,
			targetPod.UID,
		)
	}
	if mgr.servingConditionChanged[ordinal] {
		return false, nil
	}

	withdrawn, err := mgr.searchHeadPodServingWithdrawalObserved(ctx, targetPod)
	if err != nil {
		return false, err
	}
	exactProof := operation.EndpointWithdrawalObservedAt != nil &&
		operation.EndpointWithdrawalDeadline != nil &&
		operation.EndpointWithdrawalPodUID == operation.TargetPodUID &&
		operation.EndpointWithdrawalSequence >
			operation.EndpointWithdrawalInvalidatedSequence
	if operation.EndpointWithdrawalSequence >
		operation.EndpointWithdrawalInvalidatedSequence &&
		operation.EndpointWithdrawalSequence > 0 &&
		!exactProof {
		return false, fmt.Errorf(
			"Search Head endpoint-withdrawal proof does not match target Pod UID %s",
			operation.TargetPodUID,
		)
	}
	if !withdrawn {
		if exactProof {
			now := metav1.NewTime(searchHeadEndpointWithdrawalNow())
			operation.EndpointWithdrawalInvalidatedSequence =
				operation.EndpointWithdrawalSequence
			operation.LastTransitionTime = &now
			operation.Reason =
				enterpriseApi.SearchHeadClusterLifecycleReasonEndpointWithdrawalInvalidated
			operation.Message =
				"Target became routable again before the endpoint propagation delay elapsed"
			splmetrics.SearchHeadEndpointWithdrawalCounters.WithLabelValues(
				"invalidated",
			).Inc()
			if eventPublisher := GetEventPublisher(ctx, mgr.cr); eventPublisher != nil {
				eventPublisher.Warning(
					ctx,
					"SearchHeadEndpointWithdrawalInvalidated",
					fmt.Sprintf(
						"%s became routable again before detention",
						operation.TargetPod,
					),
				)
			}
		}
		return false, nil
	}

	if !exactProof {
		nowTime := searchHeadEndpointWithdrawalNow()
		now := metav1.NewTime(nowTime)
		deadline := metav1.NewTime(nowTime.Add(delay))
		operation.EndpointWithdrawalObservedAt = &now
		operation.EndpointWithdrawalDeadline = &deadline
		operation.EndpointWithdrawalPodUID = operation.TargetPodUID
		operation.EndpointWithdrawalSequence++
		operation.LastTransitionTime = &now
		operation.Reason =
			enterpriseApi.SearchHeadClusterLifecycleReasonEndpointWithdrawalObserved
		operation.Message =
			"Target is not Ready and is no longer routable through the Search Head Service EndpointSlices"
		splmetrics.SearchHeadEndpointWithdrawalCounters.WithLabelValues(
			"observed",
		).Inc()
		if eventPublisher := GetEventPublisher(ctx, mgr.cr); eventPublisher != nil {
			eventPublisher.Normal(
				ctx,
				"SearchHeadEndpointWithdrawalObserved",
				fmt.Sprintf(
					"%s is absent from routable Search Head Service endpoints",
					operation.TargetPod,
				),
			)
		}
		return false, nil
	}

	if !operation.EndpointWithdrawalDeadline.After(
		operation.EndpointWithdrawalObservedAt.Time,
	) {
		return false, fmt.Errorf(
			"Search Head endpoint-withdrawal deadline must be after its observation time",
		)
	}
	if searchHeadEndpointWithdrawalNow().Before(
		operation.EndpointWithdrawalDeadline.Time,
	) {
		if operation.Reason !=
			enterpriseApi.SearchHeadClusterLifecycleReasonEndpointWithdrawalPending {
			now := metav1.NewTime(searchHeadEndpointWithdrawalNow())
			operation.LastTransitionTime = &now
			operation.Reason =
				enterpriseApi.SearchHeadClusterLifecycleReasonEndpointWithdrawalPending
			operation.Message = fmt.Sprintf(
				"Waiting %s after EndpointSlice withdrawal before detention",
				operation.EndpointWithdrawalDeadline.Sub(
					operation.EndpointWithdrawalObservedAt.Time,
				),
			)
		}
		return false, nil
	}
	return true, nil
}
