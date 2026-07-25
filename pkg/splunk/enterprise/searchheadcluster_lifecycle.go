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
	"sort"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var searchHeadClusterLifecycleNow = time.Now

var getSearchHeadCaptainMembers = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
	return mgr.getClient(ctx, n).GetSearchHeadCaptainMembers()
}

var requestSearchHeadDetention = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
) error {
	client := mgr.getClient(ctx, n)
	if err := client.InitiateUpgrade(); err != nil {
		return err
	}

	if mgr.cr.Status.UpgradeEndTimestamp >= mgr.cr.Status.UpgradeStartTimestamp {
		currentTime := searchHeadClusterLifecycleNow().Unix()
		mgr.cr.Status.UpgradeStartTimestamp = currentTime
		mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgrading
		metrics.UpgradeStartTime.Set(float64(currentTime))
	}
	return client.SetSearchHeadDetention(true)
}

var transferSearchHeadCaptain = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
	managementURI string,
) error {
	return mgr.getClient(ctx, n).TransferSearchHeadCaptain(managementURI)
}

var releaseSearchHeadDetention = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
) error {
	return mgr.getClient(ctx, n).SetSearchHeadDetention(false)
}

var getSearchHeadLifecyclePod = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	podName string,
) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	err := mgr.c.Get(ctx, types.NamespacedName{
		Namespace: mgr.cr.GetNamespace(),
		Name:      podName,
	}, pod)
	return pod, err
}

func searchHeadClusterLifecycleEnabled() bool {
	return config.DefaultMutableFeatureGate.Enabled(config.SearchHeadClusterLifecycle) &&
		config.DefaultMutableFeatureGate.Enabled(config.SplunkPodLifecycle)
}

func (mgr *searchHeadClusterPodManager) prepareLifecycleReplacement(
	ctx context.Context,
	n int32,
	intent enterpriseApi.SearchHeadClusterLifecycleIntent,
) (bool, error) {
	policy, err := ResolveSearchHeadClusterLifecyclePolicy(&mgr.cr.Spec)
	if err != nil {
		return false, err
	}

	now := searchHeadClusterLifecycleNow()
	targetPod := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
	current := mgr.cr.Status.LifecycleOperation
	desiredRevision := ""
	if intent == enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate && mgr.statefulSet != nil {
		desiredRevision = mgr.statefulSet.Status.UpdateRevision
	}
	if desiredRevision == "" &&
		current != nil &&
		current.Intent == intent &&
		current.TargetPod == targetPod {
		desiredRevision = current.DesiredRevision
	}
	if !lifecycleOperationMatches(current, intent, desiredRevision, targetPod, n) {
		operationID := fmt.Sprintf("%s:%s:%s", intent, targetPod, desiredRevision)
		mgr.cr.Status.LifecycleOperation = shcworkflow.StartReplacement(
			operationID,
			intent,
			desiredRevision,
			targetPod,
			n,
			now,
		)
		// Persist operation identity and its initial stage before evaluating or
		// executing any side effect.
		return false, nil
	}

	observation := mgr.observeLifecycleReplacement(ctx, n, now)
	beforeStage := current.Stage
	decision := shcworkflow.EvaluateReplacement(
		current,
		observation,
		shcworkflow.ReplacementPolicy{
			SearchDrainTimeout:     time.Duration(policy.SearchDrainTimeoutSeconds) * time.Second,
			CaptainTransferTimeout: time.Duration(policy.CaptainTransferTimeoutSeconds) * time.Second,
		},
		now,
	)
	mgr.cr.Status.LifecycleOperation = decision.Operation

	if decision.Operation == nil {
		return false, fmt.Errorf("SHC lifecycle decision did not return operation status")
	}
	if decision.Operation.Stage != beforeStage {
		// The controller's deferred status update creates a durable stage
		// barrier. Execute the action only after this stage is observed on a
		// later reconciliation.
		return false, nil
	}

	switch decision.Action.Type {
	case shcworkflow.ActionNone, shcworkflow.ActionObserveCluster:
		return false, nil
	case shcworkflow.ActionRequestDetention:
		return false, requestSearchHeadDetention(ctx, mgr, n)
	case shcworkflow.ActionTransferCaptain:
		if decision.Action.ManagementURI == "" {
			return false, fmt.Errorf("captain transfer target %s has no management URI", decision.Action.Target)
		}
		// The transfer endpoint can be called through any active member and is
		// proxied to the current captain. The next reconciliation must confirm
		// a different, ready captain before authorization.
		if err := transferSearchHeadCaptain(ctx, mgr, n, decision.Action.ManagementURI); err != nil {
			return false, err
		}
		requestedAt := metav1.NewTime(searchHeadClusterLifecycleNow())
		decision.Operation.CaptainTransferTarget = decision.Action.Target
		decision.Operation.CaptainTransferRequestedAt = &requestedAt
		return false, nil
	case shcworkflow.ActionAuthorizeReplacement:
		pod, err := getSearchHeadLifecyclePod(ctx, mgr, decision.Operation.TargetPod)
		if err != nil {
			return false, err
		}
		decision.Operation.TargetPodUID = string(pod.UID)
		authorizedAt := metav1.NewTime(searchHeadClusterLifecycleNow())
		decision.Operation.ReplacementAuthorizedAt = &authorizedAt
		return true, nil
	default:
		return false, fmt.Errorf("unsupported SHC lifecycle action %q", decision.Action.Type)
	}
}

func (mgr *searchHeadClusterPodManager) observeLifecycleReplacement(
	ctx context.Context,
	n int32,
	now time.Time,
) shcworkflow.Observation {
	observation := shcworkflow.Observation{
		ObservedAt:      now,
		Available:       false,
		Fresh:           false,
		Initialized:     mgr.cr.Status.Initialized,
		MinPeersJoined:  mgr.cr.Status.MinPeersJoined,
		MaintenanceMode: mgr.cr.Status.MaintenanceMode,
		Captain:         mgr.cr.Status.Captain,
		CaptainReady:    mgr.cr.Status.CaptainReady,
	}

	if n < 0 || n >= int32(len(mgr.cr.Status.Members)) {
		return observation
	}
	target := mgr.cr.Status.Members[n]
	observation.TargetMemberObserved = target.Name != ""
	observation.TargetMemberStatus = target.Status
	observation.TargetMemberRegistered = target.Registered
	observation.ActiveHistoricalSearches = int32(target.ActiveHistoricalSearchCount)
	observation.ActiveRealtimeSearches = int32(target.ActiveRealtimeSearchCount)

	captainOrdinal := int32(-1)
	for ordinal := range mgr.cr.Status.Members {
		if mgr.cr.Status.Members[ordinal].Name == mgr.cr.Status.Captain {
			captainOrdinal = int32(ordinal)
			break
		}
	}
	if captainOrdinal < 0 {
		return observation
	}

	members, err := getSearchHeadCaptainMembers(ctx, mgr, captainOrdinal)
	if err != nil {
		return observation
	}

	reportedCaptains := make([]string, 0, 1)
	candidates := make([]splclient.SearchHeadCaptainMemberInfo, 0, len(members))
	for _, member := range members {
		if member.Captain {
			reportedCaptains = append(reportedCaptains, member.Label)
		}
		if member.Label != target.Name &&
			member.Status == "Up" &&
			member.ManagementURI != "" {
			candidates = append(candidates, member)
		}
	}
	sort.Strings(reportedCaptains)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].PreferredCaptain != candidates[j].PreferredCaptain {
			return candidates[i].PreferredCaptain
		}
		return candidates[i].Label < candidates[j].Label
	})

	observation.ConflictingCaptain = len(reportedCaptains) != 1 ||
		reportedCaptains[0] != mgr.cr.Status.Captain
	if targetMember, ok := members[target.Name]; ok {
		observation.TargetMemberID = targetMember.Identifier
	}
	if len(candidates) > 0 {
		observation.CaptainTransferTarget = candidates[0].Label
		observation.CaptainTransferTargetManagementURI = candidates[0].ManagementURI
	}
	observation.Available = true
	observation.Fresh = true
	return observation
}

func (mgr *searchHeadClusterPodManager) resumeLifecycleRecovery(
	ctx context.Context,
	n int32,
) (bool, error) {
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil {
		return false, nil
	}
	if operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		return true, nil
	}

	policy, err := ResolveSearchHeadClusterLifecyclePolicy(&mgr.cr.Spec)
	if err != nil {
		return false, err
	}
	now := searchHeadClusterLifecycleNow()
	observation, err := mgr.observeLifecycleRecovery(ctx, n)
	if err != nil {
		return false, err
	}
	beforeStage := operation.Stage
	decision := shcworkflow.EvaluateRecovery(
		operation,
		observation,
		shcworkflow.RecoveryPolicy{
			TerminationTimeout:  time.Duration(policy.TerminationGracePeriodSeconds) * time.Second,
			MemberRejoinTimeout: time.Duration(policy.MemberRejoinTimeoutSeconds) * time.Second,
		},
		now,
	)
	if decision.Operation == nil {
		return false, fmt.Errorf("SHC recovery decision did not return operation status")
	}
	mgr.cr.Status.LifecycleOperation = decision.Operation
	if decision.Operation.Stage != beforeStage {
		return decision.Operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted, nil
	}

	switch decision.Action.Type {
	case shcworkflow.ActionNone, shcworkflow.ActionObserveCluster:
		return false, nil
	case shcworkflow.ActionReleaseDetention:
		if err := releaseSearchHeadDetention(ctx, mgr, n); err != nil {
			return false, err
		}
		releasedAt := metav1.NewTime(searchHeadClusterLifecycleNow())
		decision.Operation.DetentionReleaseRequestedAt = &releasedAt
		return false, nil
	default:
		return false, fmt.Errorf("unsupported SHC recovery action %q", decision.Action.Type)
	}
}

func (mgr *searchHeadClusterPodManager) observeLifecycleRecovery(
	ctx context.Context,
	n int32,
) (shcworkflow.RecoveryObservation, error) {
	observation := shcworkflow.RecoveryObservation{}
	pod, err := getSearchHeadLifecyclePod(ctx, mgr, mgr.cr.Status.LifecycleOperation.TargetPod)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return observation, err
		}
	} else {
		observation.PodExists = true
		observation.PodUID = string(pod.UID)
		observation.PodDeleting = pod.DeletionTimestamp != nil
		observation.PodRevision = pod.Labels["controller-revision-hash"]
		for _, condition := range pod.Status.Conditions {
			switch condition.Type {
			case corev1.PodScheduled:
				observation.PodScheduled = condition.Status == corev1.ConditionTrue
				observation.PodUnschedulable = condition.Reason == corev1.PodReasonUnschedulable
			case corev1.PodReady:
				observation.PodReady = condition.Status == corev1.ConditionTrue
			}
		}
		if observation.PodReady {
			observation.PodScheduled = true
		}
		containerStatuses := append(
			append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...),
			pod.Status.ContainerStatuses...,
		)
		for _, status := range containerStatuses {
			if status.State.Waiting == nil {
				continue
			}
			reason := status.State.Waiting.Reason
			message := strings.ToLower(status.State.Waiting.Message)
			switch reason {
			case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "ErrInvalidImage":
				observation.ImagePullFailed = true
			case "CrashLoopBackOff", "RunContainerError":
				observation.ContainerStartupFailed = true
			case "ContainerCreating":
				if strings.Contains(message, "volume") ||
					strings.Contains(message, "attach") ||
					strings.Contains(message, "mount") {
					observation.StoragePending = true
				}
			}
		}
	}

	if n < 0 || n >= int32(len(mgr.cr.Status.Members)) {
		return observation, nil
	}
	target := mgr.cr.Status.Members[n]
	observation.MemberObserved = target.Name == mgr.cr.Status.LifecycleOperation.TargetPod &&
		target.Status != ""
	observation.MemberStatus = target.Status
	observation.MemberRegistered = target.Registered
	observation.CaptainReady = mgr.cr.Status.CaptainReady

	captainOrdinal := int32(-1)
	for ordinal := range mgr.cr.Status.Members {
		if mgr.cr.Status.Members[ordinal].Name == mgr.cr.Status.Captain {
			captainOrdinal = int32(ordinal)
			break
		}
	}
	if captainOrdinal < 0 {
		return observation, nil
	}
	members, err := getSearchHeadCaptainMembers(ctx, mgr, captainOrdinal)
	if err != nil {
		return observation, nil
	}
	captainCount := 0
	for _, member := range members {
		if member.Captain {
			captainCount++
			observation.AuthoritativeCaptain = member.Label == mgr.cr.Status.Captain
		}
	}
	observation.AuthoritativeCaptain = observation.AuthoritativeCaptain && captainCount == 1
	if member, ok := members[mgr.cr.Status.LifecycleOperation.TargetPod]; ok {
		observation.CaptainMemberObserved = true
		observation.CaptainMemberID = member.Identifier
		observation.CaptainMemberStatus = member.Status
	}
	return observation, nil
}

func lifecycleOperationMatches(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	intent enterpriseApi.SearchHeadClusterLifecycleIntent,
	desiredRevision string,
	targetPod string,
	targetOrdinal int32,
) bool {
	return operation != nil &&
		operation.Intent == intent &&
		operation.DesiredRevision == desiredRevision &&
		operation.TargetPod == targetPod &&
		operation.TargetOrdinal != nil &&
		*operation.TargetOrdinal == targetOrdinal
}

func lifecycleRecoveryActive(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) bool {
	return operation != nil &&
		operation.Intent == enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate &&
		operation.TargetOrdinal != nil &&
		operation.TargetPodUID != "" &&
		operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageCompleted
}
