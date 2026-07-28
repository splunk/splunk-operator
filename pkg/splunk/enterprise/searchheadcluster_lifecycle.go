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
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/logging"
	"github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	appsv1 "k8s.io/api/apps/v1"
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

var getSearchHeadKVStoreStatus = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
) (string, error) {
	status, err := mgr.getClient(ctx, n).GetKVStoreStatus()
	if err != nil {
		return "", err
	}
	return status.Current.Status, nil
}

type searchHeadKVStoreObservation struct {
	Available          bool
	Statuses           map[int32]string
	NotReadyMembers    []string
	UnavailableMembers []string
}

func (mgr *searchHeadClusterPodManager) observeSearchHeadKVStores(
	ctx context.Context,
	ordinals []int32,
) searchHeadKVStoreObservation {
	observation := searchHeadKVStoreObservation{
		Available: true,
		Statuses:  make(map[int32]string, len(ordinals)),
	}
	if len(ordinals) == 0 {
		observation.Available = false
		return observation
	}

	type result struct {
		ordinal int32
		name    string
		status  string
		err     error
	}
	results := make(chan result, len(ordinals))
	for _, ordinal := range ordinals {
		name := fmt.Sprintf("search-head-ordinal-%d", ordinal)
		if ordinal >= 0 && ordinal < int32(len(mgr.cr.Status.Members)) &&
			mgr.cr.Status.Members[ordinal].Name != "" {
			name = mgr.cr.Status.Members[ordinal].Name
		}
		go func(ordinal int32, name string) {
			status, err := getSearchHeadKVStoreStatus(ctx, mgr, ordinal)
			results <- result{
				ordinal: ordinal,
				name:    name,
				status:  strings.TrimSpace(status),
				err:     err,
			}
		}(ordinal, name)
	}

	for range ordinals {
		result := <-results
		if result.err != nil || result.status == "" {
			observation.Available = false
			observation.UnavailableMembers = append(
				observation.UnavailableMembers,
				result.name,
			)
			continue
		}
		observation.Statuses[result.ordinal] = result.status
		if result.status != "ready" {
			observation.NotReadyMembers = append(
				observation.NotReadyMembers,
				fmt.Sprintf("%s=%s", result.name, result.status),
			)
		}
	}
	sort.Strings(observation.NotReadyMembers)
	sort.Strings(observation.UnavailableMembers)
	return observation
}

func (mgr *searchHeadClusterPodManager) searchHeadMemberOrdinals() []int32 {
	count := len(mgr.cr.Status.Members)
	if int(mgr.cr.Spec.Replicas) > count {
		count = int(mgr.cr.Spec.Replicas)
	}
	ordinals := make([]int32, count)
	for ordinal := range count {
		ordinals[ordinal] = int32(ordinal)
	}
	return ordinals
}

func kvStorePreflightMessage(
	observation searchHeadKVStoreObservation,
) string {
	if !observation.Available {
		return fmt.Sprintf(
			"wait for KV Store status from every Search Head member: unavailable=%s",
			strings.Join(observation.UnavailableMembers, ", "),
		)
	}
	if len(observation.NotReadyMembers) > 0 {
		return fmt.Sprintf(
			"wait for every Search Head KV Store to report ready: %s",
			strings.Join(observation.NotReadyMembers, ", "),
		)
	}
	return ""
}

var requestSearchHeadDetention = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
) error {
	if !shcRollingUpdateOwnsClusterUpgradeLifecycle(mgr) {
		captainOrdinal, err := observedReadyCaptainOrdinal(mgr)
		if err != nil {
			return err
		}
		if err := initiateSearchHeadClusterUpgrade(
			ctx,
			mgr,
			captainOrdinal,
		); err != nil {
			return err
		}

		if mgr.cr.Status.UpgradeEndTimestamp >= mgr.cr.Status.UpgradeStartTimestamp {
			currentTime := searchHeadClusterLifecycleNow().Unix()
			mgr.cr.Status.UpgradeStartTimestamp = currentTime
			mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgrading
			metrics.UpgradeStartTime.Set(float64(currentTime))
		}
	}
	return setSearchHeadDetention(ctx, mgr, n, true)
}

func observedReadyCaptainOrdinal(
	mgr *searchHeadClusterPodManager,
) (int32, error) {
	if mgr == nil || mgr.cr == nil ||
		!mgr.cr.Status.CaptainReady ||
		mgr.cr.Status.Captain == "" {
		return -1, fmt.Errorf("SHC has no observed ready captain")
	}
	for ordinal, member := range mgr.cr.Status.Members {
		if member.Name != mgr.cr.Status.Captain {
			continue
		}
		if !member.Registered || member.Status != "Up" {
			return -1, fmt.Errorf(
				"observed SHC captain %s is not a registered Up member",
				mgr.cr.Status.Captain,
			)
		}
		return int32(ordinal), nil
	}
	return -1, fmt.Errorf(
		"observed SHC captain %s is missing from member status",
		mgr.cr.Status.Captain,
	)
}

func shcRollingUpdateOwnsClusterUpgradeLifecycle(
	mgr *searchHeadClusterPodManager,
) bool {
	lifecycle := mgr.cr.Status.LifecycleOperation
	return mgr.statefulSet != nil &&
		mgr.statefulSet.Spec.UpdateStrategy.Type ==
			appsv1.RollingUpdateStatefulSetStrategyType &&
		lifecycle != nil &&
		lifecycle.Intent ==
			enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate
}

var transferSearchHeadCaptain = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
	managementURI string,
) error {
	return mgr.getClient(ctx, n).TransferSearchHeadCaptain(managementURI)
}

var setSearchHeadDetention = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
	detain bool,
) error {
	return mgr.getClient(ctx, n).SetSearchHeadDetention(detain)
}

var releaseSearchHeadDetention = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
) error {
	return setSearchHeadDetention(ctx, mgr, n, false)
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
		operationID := fmt.Sprintf(
			"%s:%s:%s:%d",
			intent,
			targetPod,
			desiredRevision,
			mgr.cr.GetGeneration(),
		)
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
	if current.TargetPodUID == "" {
		pod, err := getSearchHeadLifecyclePod(ctx, mgr, current.TargetPod)
		if err != nil {
			return false, err
		}
		// Capture the original Pod identity before detention or captain
		// transfer. This makes a pre-authorization cancellation fail closed if
		// an unplanned replacement races the requested rollout.
		current.TargetPodUID = string(pod.UID)
		// Persist the identity as its own durable barrier. No Splunk side
		// effect or Kubernetes replacement authorization may run in the same
		// reconciliation that first observes the original Pod UID.
		return false, nil
	}

	observation := mgr.observeLifecycleReplacement(ctx, n, now)
	beforeStage := current.Stage
	decision := shcworkflow.EvaluateReplacement(
		current,
		observation,
		shcworkflow.ReplacementPolicy{
			DetentionTimeout:       time.Duration(policy.DetentionTimeoutSeconds) * time.Second,
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
		if blockedErr := mgr.lifecycleBlockedError(
			ctx,
			beforeStage,
		); blockedErr != nil {
			return false, blockedErr
		}
		// The controller's deferred status update creates a durable stage
		// barrier. Execute the action only after this stage is observed on a
		// later reconciliation.
		return false, nil
	}
	if blockedErr := mgr.lifecycleBlockedError(
		ctx,
		beforeStage,
	); blockedErr != nil {
		return false, blockedErr
	}

	switch decision.Action.Type {
	case shcworkflow.ActionNone, shcworkflow.ActionObserveCluster:
		return false, nil
	case shcworkflow.ActionRequestDetention:
		if searchHeadServingReadinessGateConfigured(mgr.statefulSet) {
			withdrawn, err := mgr.searchHeadServingWithdrawalObserved(ctx, n)
			if err != nil {
				return false, err
			}
			if !withdrawn {
				return false, nil
			}
		}
		detentionErr := requestSearchHeadDetention(ctx, mgr, n)
		if detentionErr != nil && !detentionOutcomeUnknown(detentionErr) {
			return false, detentionErr
		}
		decision.Operation = shcworkflow.RecordDetentionRequestAttempt(
			decision.Operation,
			searchHeadClusterLifecycleNow(),
		)
		mgr.cr.Status.LifecycleOperation = decision.Operation
		if detentionErr != nil {
			logging.FromContext(ctx).WarnContext(
				ctx,
				"detention request outcome is unknown; retaining progressing state and retrying",
				"targetPod",
				decision.Operation.TargetPod,
				"attemptCount",
				decision.Operation.DetentionRequestAttemptCount,
				"error",
				detentionErr,
			)
		}
		return false, nil
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
		var authorized bool
		decision.Operation, authorized =
			shcworkflow.RecordReplacementAuthorization(
				decision.Operation,
				string(pod.UID),
				searchHeadClusterLifecycleNow(),
			)
		mgr.cr.Status.LifecycleOperation = decision.Operation
		if !authorized {
			return false, mgr.lifecycleBlockedError(
				ctx,
				beforeStage,
			)
		}
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

	operation := mgr.cr.Status.LifecycleOperation
	if operation != nil {
		kvStoreOrdinals := []int32(nil)
		switch {
		case operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster:
			kvStoreOrdinals = mgr.searchHeadMemberOrdinals()
		case operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget &&
			target.Status == "ManualDetention",
			operation.Stage ==
				enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
			operation.Stage ==
				enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain,
			operation.Stage ==
				enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement:
			kvStoreOrdinals = []int32{n}
		}
		if len(kvStoreOrdinals) > 0 {
			kvStoreObservation := mgr.observeSearchHeadKVStores(
				ctx,
				kvStoreOrdinals,
			)
			observation.KVStoreObservationRequired = true
			observation.KVStoreObservationAvailable =
				kvStoreObservation.Available
			observation.KVStoreNotReadyMembers = append(
				[]string(nil),
				kvStoreObservation.NotReadyMembers...,
			)
			observation.TargetKVStoreReady =
				kvStoreObservation.Statuses[n] == "ready"
		}
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
			PodStartupTimeout:   time.Duration(policy.PodStartupTimeoutSeconds) * time.Second,
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
		releaseErr := releaseSearchHeadDetention(ctx, mgr, n)
		if releaseErr != nil &&
			!detentionOutcomeUnknown(releaseErr) {
			return false, releaseErr
		}
		decision.Operation = shcworkflow.RecordDetentionReleaseAttempt(
			decision.Operation,
			searchHeadClusterLifecycleNow(),
		)
		mgr.cr.Status.LifecycleOperation = decision.Operation
		if releaseErr != nil {
			logging.FromContext(ctx).WarnContext(
				ctx,
				"detention release outcome is unknown; retaining progressing state and retrying",
				"targetPod",
				decision.Operation.TargetPod,
				"retryCount",
				decision.Operation.RetryCount,
				"error",
				releaseErr,
			)
		}
		return false, nil
	default:
		return false, fmt.Errorf("unsupported SHC recovery action %q", decision.Action.Type)
	}
}

func detentionOutcomeUnknown(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return true
	}
	var networkErr net.Error
	return errors.As(err, &networkErr)
}

func (mgr *searchHeadClusterPodManager) lifecycleBlockedError(
	ctx context.Context,
	previousStage enterpriseApi.SearchHeadClusterLifecycleStage,
) error {
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		(operation.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked &&
			operation.Stage !=
				enterpriseApi.SearchHeadClusterLifecycleStageFailed) {
		return nil
	}

	reason := operation.Reason
	if reason == "" {
		reason = enterpriseApi.SearchHeadClusterLifecycleReasonClusterNotSafe
	}
	message := operation.Message
	if message == "" {
		message = fmt.Sprintf(
			"lifecycle operation %s for %s is %s",
			operation.OperationID,
			operation.TargetPod,
			operation.Stage,
		)
	}
	mgr.cr.Status.Message = fmt.Sprintf(
		"%s%s: %s",
		shcRollingUpdateStatusPrefix,
		reason,
		message,
	)

	if previousStage != operation.Stage {
		GetEventPublisher(ctx, mgr.cr).Warning(
			ctx,
			EventReasonSHCRolloutBlocked,
			fmt.Sprintf("%s: %s", reason, message),
		)
	}
	return splcommon.NewTerminalError(
		string(reason),
		message,
		fmt.Errorf(
			"SHC lifecycle operation %s is %s (%s)",
			operation.OperationID,
			operation.Stage,
			reason,
		),
	)
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
			case corev1.ContainersReady:
				observation.ContainersReady = condition.Status == corev1.ConditionTrue
			case corev1.PodReady:
				observation.PodReady = condition.Status == corev1.ConditionTrue
			}
		}
		if observation.PodReady {
			observation.ContainersReady = true
			observation.PodScheduled = true
		}
		containerStatuses := append(
			append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...),
			pod.Status.ContainerStatuses...,
		)
		for _, status := range containerStatuses {
			if !observation.ContainersReady {
				if status.State.Terminated != nil &&
					status.State.Terminated.ExitCode != 0 {
					observation.ContainerStartupFailed = true
				}
				if status.RestartCount > 0 &&
					status.LastTerminationState.Terminated != nil &&
					status.LastTerminationState.Terminated.ExitCode != 0 {
					observation.ContainerStartupFailed = true
				}
			}
			if status.State.Waiting == nil {
				continue
			}
			reason := status.State.Waiting.Reason
			message := strings.ToLower(status.State.Waiting.Message)
			switch reason {
			case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "ErrInvalidImage":
				observation.ImagePullFailed = true
			case "CrashLoopBackOff":
				observation.ContainerStartupFailed = true
			case "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
				observation.ContainerStartupFailed = true
				observation.ContainerFailureTerminal = true
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
	observation.ActiveHistoricalSearches = int32(target.ActiveHistoricalSearchCount)
	observation.ActiveRealtimeSearches = int32(target.ActiveRealtimeSearchCount)
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
		// A completed scale-down record describes one historical change in
		// desired replica count. If that ordinal is added again and a later
		// scale-down targets it, a new operation must be created even though
		// the intent and Pod name are identical. Pod-update completion remains
		// revision-scoped and is intentionally reusable by rollout recovery.
		!(intent ==
			enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown &&
			operation.Stage ==
				enterpriseApi.SearchHeadClusterLifecycleStageCompleted) &&
		operation.DesiredRevision == desiredRevision &&
		operation.TargetPod == targetPod &&
		operation.TargetOrdinal != nil &&
		*operation.TargetOrdinal == targetOrdinal
}

func lifecycleRecoveryActive(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) bool {
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		return false
	}
	if operation.Intent ==
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate {
		return operation.TargetPodUID != ""
	}
	return operation.Intent ==
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown &&
		operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery &&
		operation.MembershipRemovalRequestedAt == nil
}
