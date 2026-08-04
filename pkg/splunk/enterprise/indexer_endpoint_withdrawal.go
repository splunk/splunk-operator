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

	splmetrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var indexerEndpointWithdrawalNow = time.Now

// indexerEndpointWithdrawalObserved fails closed until both the target Pod and
// the Indexer client Service agree that the target is no longer eligible for
// traffic. A nil EndpointSlice ready condition remains routable through the
// shared endpointSlicesRoutePod helper.
func (mgr *indexerClusterPodManager) indexerEndpointWithdrawalObserved(
	ctx context.Context,
	pod *corev1.Pod,
) (bool, error) {
	if pod == nil ||
		podConditionStatus(pod, corev1.PodReady) != corev1.ConditionFalse {
		return false, nil
	}

	endpointSlices := &discoveryv1.EndpointSliceList{}
	serviceName := splcommon.GetSplunkServiceName(
		SplunkIndexer,
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
			"list EndpointSlices for Indexer Service %s before decommission: %w",
			serviceName,
			err,
		)
	}
	return !endpointSlicesRoutePod(endpointSlices.Items, pod), nil
}

func (mgr *indexerClusterPodManager) ensureIndexerEndpointWithdrawalBarrier(
	ctx context.Context,
) (bool, error) {
	operation := mgr.cr.Status.PodUpdate
	if operation == nil {
		return false, nil
	}
	var targetPod corev1.Pod
	if err := mgr.c.Get(
		ctx,
		types.NamespacedName{
			Namespace: mgr.cr.GetNamespace(),
			Name:      operation.TargetPod,
		},
		&targetPod,
	); err != nil {
		return false, err
	}
	if err := validateIndexerPodUpdateTarget(
		operation,
		&targetPod,
		operation.TargetOrdinal,
	); err != nil {
		return false, fmt.Errorf(
			"validate Indexer endpoint-withdrawal target: %w",
			err,
		)
	}
	if isKubernetesPodReady(&targetPod) {
		podExecClient := splutil.GetPodExecClient(
			mgr.c,
			mgr.cr,
			operation.TargetPod,
		)
		if err := setIndexerReadinessWithdrawalOnSplunkPod(
			ctx,
			podExecClient,
		); err != nil {
			return false, err
		}
		mgr.log.InfoContext(
			ctx,
			"waiting for Indexer Pod readiness withdrawal before decommission",
			"operationID",
			operation.OperationID,
			"peerName",
			operation.TargetPod,
		)
		return false, nil
	}
	return mgr.indexerEndpointWithdrawalDelayElapsed(ctx, &targetPod)
}

// indexerEndpointWithdrawalDelayElapsed persists the exact EndpointSlice
// observation before starting its propagation delay. A later routable
// observation invalidates the proof monotonically so stale status updates
// cannot authorize decommission.
func (mgr *indexerClusterPodManager) indexerEndpointWithdrawalDelayElapsed(
	ctx context.Context,
	pod *corev1.Pod,
) (bool, error) {
	operation := mgr.cr.Status.PodUpdate
	if operation == nil || pod == nil {
		return false, nil
	}

	withdrawn, err := mgr.indexerEndpointWithdrawalObserved(ctx, pod)
	if err != nil {
		return false, err
	}
	exactProof := operation.EndpointWithdrawalObservedAt != nil &&
		operation.EndpointWithdrawalDeadline != nil &&
		operation.EndpointWithdrawalPodUID == string(pod.UID) &&
		operation.EndpointWithdrawalSequence >
			operation.EndpointWithdrawalInvalidatedSequence
	if !withdrawn {
		if exactProof {
			now := metav1.Now()
			operation.EndpointWithdrawalInvalidatedSequence =
				operation.EndpointWithdrawalSequence
			operation.LastTransitionTime = &now
			operation.Reason = "IndexerEndpointWithdrawalInvalidated"
			operation.Message =
				"Target became routable again before the endpoint propagation delay elapsed"
			splmetrics.IndexerEndpointWithdrawalCounters.WithLabelValues(
				"invalidated",
			).Inc()
			if eventPublisher := GetEventPublisher(ctx, mgr.cr); eventPublisher != nil {
				eventPublisher.Warning(
					ctx,
					"IndexerEndpointWithdrawalInvalidated",
					fmt.Sprintf(
						"%s became routable again before decommission",
						operation.TargetPod,
					),
				)
			}
		}
		return false, nil
	}

	if !exactProof {
		nowTime := indexerEndpointWithdrawalNow()
		now := metav1.NewTime(nowTime)
		deadline := metav1.NewTime(
			nowTime.Add(indexerEndpointWithdrawalDelay(&mgr.cr.Spec)),
		)
		operation.EndpointWithdrawalObservedAt = &now
		operation.EndpointWithdrawalDeadline = &deadline
		operation.EndpointWithdrawalPodUID = string(pod.UID)
		operation.EndpointWithdrawalSequence++
		operation.LastTransitionTime = &now
		operation.Reason = "IndexerEndpointWithdrawalObserved"
		operation.Message =
			"Target is not Ready and is no longer routable through the Indexer Service EndpointSlices"
		splmetrics.IndexerEndpointWithdrawalCounters.WithLabelValues(
			"observed",
		).Inc()
		if eventPublisher := GetEventPublisher(ctx, mgr.cr); eventPublisher != nil {
			eventPublisher.Normal(
				ctx,
				"IndexerEndpointWithdrawalObserved",
				fmt.Sprintf(
					"%s is absent from routable Indexer Service endpoints",
					operation.TargetPod,
				),
			)
		}
		return false, nil
	}

	delay := operation.EndpointWithdrawalDeadline.Sub(
		operation.EndpointWithdrawalObservedAt.Time,
	)
	if indexerEndpointWithdrawalNow().Before(
		operation.EndpointWithdrawalDeadline.Time,
	) {
		if operation.Reason != "IndexerEndpointWithdrawalPropagationPending" {
			now := metav1.Now()
			operation.LastTransitionTime = &now
			operation.Reason = "IndexerEndpointWithdrawalPropagationPending"
			operation.Message = fmt.Sprintf(
				"Waiting %s after EndpointSlice withdrawal before decommission",
				delay,
			)
		}
		return false, nil
	}
	return true, nil
}
