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
	"time"

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
const shcImageUpgradeStatusPrefix = "SHC ImageUpgrade "

var searchHeadClusterImageUpgradeNow = time.Now

var initiateSearchHeadClusterUpgrade = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	ordinal int32,
) error {
	return mgr.getClient(ctx, ordinal).InitiateUpgrade()
}

var finalizeSearchHeadClusterUpgrade = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	ordinal int32,
) error {
	return mgr.getClient(ctx, ordinal).FinalizeUpgrade()
}

var validateSearchHeadClusterImageUpgradePath = func(
	context.Context,
	string,
	string,
) (upgrade.SHCImageUpgradePathDecision, error) {
	// Production enablement requires an approved authoritative compatibility
	// source. Do not infer support from image tag syntax.
	return upgrade.SHCImageUpgradePathUnknown, nil
}

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
	if !mgr.cr.Status.Initialized ||
		!mgr.cr.Status.MinPeersJoined {
		decision := upgrade.SHCRolloutDecision{
			Action:  upgrade.SHCRolloutActionWait,
			Reason:  upgrade.SHCRolloutReasonInitialFormationPending,
			Message: "wait for initialized SHC and minimum joined peers before rollout management",
		}
		mgr.recordRollingUpdateDecision(ctx, state, decision)
		return enterpriseApi.PhasePending, nil
	}
	if mgr.cr.Status.Captain == "" || !mgr.cr.Status.CaptainReady {
		decision := upgrade.SHCRolloutDecision{
			Action:  upgrade.SHCRolloutActionWait,
			Reason:  upgrade.SHCRolloutReasonCaptainUnavailable,
			Message: "wait for one authoritative service-ready captain before rollout management",
		}
		mgr.recordRollingUpdateDecision(ctx, state, decision)
		return enterpriseApi.PhasePending, nil
	}
	if !shcPodRolloutActive(mgr.cr.Status.LifecycleOperation) &&
		!shcImageUpgradeActive(mgr.cr.Status.ImageUpgrade) &&
		shcAppFrameworkWorkActive(&mgr.cr.Status.AppContext) {
		// App Framework acquired the durable operation first. Keep the SHC
		// Ready so its playbook can finish a pending or in-progress bundle
		// push, while retaining the fail-closed partition.
		mgr.cr.Status.Message = shcRollingUpdateStatusPrefix +
			"AppFrameworkOperationActive: wait for App Framework work to complete before starting a Pod rollout"
		return enterpriseApi.PhaseReady, nil
	}
	completionRecorded, err := mgr.reconcileImageUpgradeMemberCompletion(state)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}
	if completionRecorded {
		// Persist the recovered ordinal before the rollout evaluator can
		// prepare the next member.
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
		membersAllowed, err := mgr.reconcileImageUpgradeInitialization(
			ctx,
			statefulSet,
			state,
		)
		if err != nil {
			return enterpriseApi.PhaseError, err
		}
		if !membersAllowed {
			return enterpriseApi.PhaseUpdating, nil
		}
		operationBefore := mgr.cr.Status.LifecycleOperation
		startingTarget := !rolloutOperationMatches(
			operationBefore,
			statefulSet.Status.UpdateRevision,
			*decision.TargetOrdinal,
		)
		_, err = mgr.PrepareRecycle(ctx, *decision.TargetOrdinal)
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
		if decision.Reason == upgrade.SHCRolloutReasonScaleUpMemberPending {
			return enterpriseApi.PhaseScalingUp, nil
		}
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
		if mgr.cr.Status.ImageUpgrade == nil {
			// An ordinary RollingUpdate does not use Splunk's cluster image
			// upgrade endpoints.
			return enterpriseApi.PhaseReady, nil
		}
		return mgr.reconcileImageUpgradeFinalization(
			ctx,
			statefulSet,
			state,
		)

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
// the authorized target eligible for replacement. In-place cancellation does
// not need partition advancement because Kubernetes never received replacement
// authorization. OnDelete retains the existing Operator-owned replacement
// ordering.
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
	if operation.Intent ==
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown {
		// A cancelled scale-down restores the existing target in place. No
		// partition change or Pod replacement is being authorized.
		return true
	}
	if operation.Intent ==
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate &&
		operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery &&
		operation.ReplacementAuthorizedAt == nil {
		// A withdrawn or superseded Pod update restores the original target in
		// place. Its partition remains above the target ordinal because
		// replacement was never authorized.
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

func (mgr *searchHeadClusterPodManager) reconcileImageUpgradeMemberCompletion(
	state upgrade.SHCRolloutState,
) (bool, error) {
	imageUpgrade := mgr.cr.Status.ImageUpgrade
	lifecycle := mgr.cr.Status.LifecycleOperation
	if imageUpgrade == nil ||
		imageUpgrade.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers ||
		lifecycle == nil ||
		lifecycle.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageCompleted ||
		lifecycle.Intent !=
			enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate ||
		lifecycle.DesiredRevision != imageUpgrade.DesiredRevision ||
		lifecycle.TargetOrdinal == nil {
		return false, nil
	}
	ordinal := *lifecycle.TargetOrdinal
	if ordinal < 0 ||
		ordinal >= int32(len(state.Pods)) ||
		ordinal >= int32(len(mgr.cr.Status.Members)) {
		return false, fmt.Errorf(
			"completed SHC image-upgrade lifecycle has invalid ordinal %d",
			ordinal,
		)
	}
	var observedPod *upgrade.SHCRolloutPod
	for index := range state.Pods {
		if state.Pods[index].Ordinal == ordinal {
			observedPod = &state.Pods[index]
			break
		}
	}
	member := mgr.cr.Status.Members[ordinal]
	if observedPod == nil ||
		!observedPod.Exists ||
		!observedPod.Ready ||
		observedPod.Deleting ||
		observedPod.Revision != imageUpgrade.DesiredRevision ||
		observedPod.Image != imageUpgrade.TargetImage ||
		!member.Registered ||
		member.Status != "Up" {
		return false, nil
	}

	decision := upgrade.RecordSHCImageUpgradeCompletedOrdinal(
		imageUpgrade,
		ordinal,
		searchHeadClusterImageUpgradeNow(),
	)
	if decision.Operation != nil {
		mgr.cr.Status.ImageUpgrade = decision.Operation
	}
	mgr.recordImageUpgradeStatus(decision.Reason, decision.Message)
	switch decision.Action {
	case upgrade.SHCImageUpgradeOrdinalPersist:
		return true, nil
	case upgrade.SHCImageUpgradeOrdinalWait:
		return false, nil
	case upgrade.SHCImageUpgradeOrdinalBlock:
		return false, fmt.Errorf(
			"record SHC image-upgrade member %d (%s): %s",
			ordinal,
			decision.Reason,
			decision.Message,
		)
	default:
		return false, fmt.Errorf(
			"unsupported SHC image-upgrade ordinal action %q",
			decision.Action,
		)
	}
}

func (mgr *searchHeadClusterPodManager) reconcileImageUpgradeFinalization(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
	state upgrade.SHCRolloutState,
) (enterpriseApi.Phase, error) {
	imageUpgrade := mgr.cr.Status.ImageUpgrade
	if imageUpgrade == nil {
		return enterpriseApi.PhaseReady, nil
	}
	targetImage, err := statefulSetSplunkImage(statefulSet)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}
	conflictingOperation := shcAppFrameworkWorkActive(
		&mgr.cr.Status.AppContext,
	) || shcImageUpgradeHasConflictingLifecycle(
		imageUpgrade,
		mgr.cr.Status.LifecycleOperation,
	)
	targetOrdinal := int32(-1)
	targetEligible := false
	if imageUpgrade.Phase ==
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing &&
		imageUpgrade.FinalizationIntentAt != nil &&
		imageUpgrade.FinalizationSucceededAt == nil &&
		!conflictingOperation {
		targetOrdinal, targetEligible =
			mgr.selectImageUpgradeManagementTarget(ctx)
	}
	now := searchHeadClusterImageUpgradeNow()
	decision := upgrade.EvaluateSHCImageUpgradeFinalization(
		upgrade.SHCImageUpgradeFinalizationInput{
			Current:                    imageUpgrade,
			Pods:                       mgr.imageUpgradeFinalizationPods(state),
			StatefulSetReplicas:        state.Replicas,
			StatefulSetPartition:       state.Partition,
			StatefulSetCurrentRevision: state.CurrentRevision,
			StatefulSetUpdateRevision:  state.UpdateRevision,
			StatefulSetTargetImage:     targetImage,
			LatestMemberLifecycleDone: shcImageUpgradeLatestLifecycleComplete(
				imageUpgrade,
				mgr.cr.Status.LifecycleOperation,
			),
			Initialized:                 mgr.cr.Status.Initialized,
			MinPeersJoined:              mgr.cr.Status.MinPeersJoined,
			CaptainReady:                mgr.cr.Status.CaptainReady,
			CoordinationOwned:           !conflictingOperation,
			ConflictingPlannedOperation: conflictingOperation,
			ManagementTargetEligible:    targetEligible,
			Now:                         now,
		},
	)
	if decision.Operation != nil {
		mgr.cr.Status.ImageUpgrade = decision.Operation
	}
	mgr.recordImageUpgradeStatus(decision.Reason, decision.Message)

	switch decision.Action {
	case upgrade.SHCImageUpgradeFinalizationPersist,
		upgrade.SHCImageUpgradeFinalizationWait:
		return enterpriseApi.PhaseUpdating, nil

	case upgrade.SHCImageUpgradeFinalizationCall:
		if !targetEligible {
			return enterpriseApi.PhaseError, fmt.Errorf(
				"SHC image-upgrade finalization authorized without an eligible management target",
			)
		}
		endpointErr := finalizeSearchHeadClusterUpgrade(
			ctx,
			mgr,
			targetOrdinal,
		)
		attemptedAt := searchHeadClusterImageUpgradeNow()
		attempt := upgrade.RecordSHCImageUpgradeFinalizationAttempt(
			mgr.cr.Status.ImageUpgrade,
			endpointErr == nil,
			attemptedAt,
		)
		if attempt.Operation != nil {
			mgr.cr.Status.ImageUpgrade = attempt.Operation
		}
		mgr.recordImageUpgradeStatus(attempt.Reason, attempt.Message)
		if endpointErr != nil {
			return enterpriseApi.PhaseError, fmt.Errorf(
				"finalize Search Head Cluster image upgrade: %w",
				endpointErr,
			)
		}
		mgr.projectImageUpgradeFinalizationEnd(attemptedAt)
		return enterpriseApi.PhaseUpdating, nil

	case upgrade.SHCImageUpgradeFinalizationFinished:
		return enterpriseApi.PhaseReady, nil

	case upgrade.SHCImageUpgradeFinalizationBlock:
		return enterpriseApi.PhaseError, fmt.Errorf(
			"SHC image-upgrade finalization blocked (%s): %s",
			decision.Reason,
			decision.Message,
		)

	default:
		return enterpriseApi.PhaseError, fmt.Errorf(
			"unsupported SHC image-upgrade finalization action %q",
			decision.Action,
		)
	}
}

func (mgr *searchHeadClusterPodManager) imageUpgradeFinalizationPods(
	state upgrade.SHCRolloutState,
) []upgrade.SHCImageUpgradeFinalizationPod {
	pods := make(
		[]upgrade.SHCImageUpgradeFinalizationPod,
		0,
		len(state.Pods),
	)
	for _, pod := range state.Pods {
		observation := upgrade.SHCImageUpgradeFinalizationPod{
			Ordinal:  pod.Ordinal,
			Exists:   pod.Exists,
			Ready:    pod.Ready,
			Deleting: pod.Deleting,
			Revision: pod.Revision,
			Image:    pod.Image,
		}
		if pod.Ordinal >= 0 &&
			pod.Ordinal < int32(len(mgr.cr.Status.Members)) {
			member := mgr.cr.Status.Members[pod.Ordinal]
			observation.MemberRegistered = member.Registered
			observation.MemberStatus = member.Status
		}
		pods = append(pods, observation)
	}
	return pods
}

func (mgr *searchHeadClusterPodManager) projectImageUpgradeFinalizationEnd(
	now time.Time,
) {
	completedAt := now.Unix()
	mgr.cr.Status.UpgradeEndTimestamp = completedAt
	mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgraded
	splmetrics.UpgradeEndTime.Set(float64(completedAt))
}

func shcImageUpgradeLatestLifecycleComplete(
	imageUpgrade *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	lifecycle *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) bool {
	return imageUpgrade != nil &&
		lifecycle != nil &&
		lifecycle.Intent ==
			enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate &&
		lifecycle.DesiredRevision == imageUpgrade.DesiredRevision &&
		lifecycle.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageCompleted
}

// reconcileImageUpgradeInitialization gates only a previously recorded image
// workflow. Image-change classification and workflow creation are a separate
// adapter boundary; an ordinary template rollout is not inferred here.
func (mgr *searchHeadClusterPodManager) reconcileImageUpgradeInitialization(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
	state upgrade.SHCRolloutState,
) (bool, error) {
	current := mgr.cr.Status.ImageUpgrade
	if statefulSet.Spec.Replicas == nil {
		return false, fmt.Errorf(
			"SHC image upgrade StatefulSet %s has no replica count",
			statefulSet.GetName(),
		)
	}
	targetImage, err := statefulSetSplunkImage(statefulSet)
	if err != nil {
		return false, err
	}
	now := searchHeadClusterImageUpgradeNow()
	pathDecision := upgrade.SHCImageUpgradePathUnknown
	sourceImage := ""
	uniformSourceImage := false
	classifyingNewRequest := current == nil ||
		(current.Phase ==
			enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted &&
			(current.DesiredRevision != statefulSet.Status.UpdateRevision ||
				current.TargetImage != targetImage ||
				current.TargetReplicas != *statefulSet.Spec.Replicas))
	if classifyingNewRequest {
		sourceImage, uniformSourceImage =
			uniformSHCRolloutPodImage(state.Pods)
		if uniformSourceImage &&
			sourceImage != "" &&
			sourceImage != targetImage {
			pathDecision, err =
				validateSearchHeadClusterImageUpgradePath(
					ctx,
					sourceImage,
					targetImage,
				)
			if err != nil {
				return false, fmt.Errorf(
					"validate Search Head Cluster image upgrade path: %w",
					err,
				)
			}
		}
	}
	lifecycle := mgr.cr.Status.LifecycleOperation
	resumingOwnedOrdinaryRollout := current == nil &&
		shcPodRolloutActive(lifecycle) &&
		lifecycle.DesiredRevision == state.UpdateRevision &&
		lifecycle.TargetOrdinal != nil &&
		uniformSourceImage &&
		sourceImage == targetImage
	if resumingOwnedOrdinaryRollout {
		// The image decision preceded the persisted member lifecycle. Once the
		// lifecycle withdraws its target from service, readiness can no longer
		// be used to repeat that classification. Uniform source and target
		// images prove this is still an ordinary template rollout; an image
		// change without its durable image-upgrade workflow remains fail-closed.
		return true, nil
	}
	observedConflictingOperation := shcAppFrameworkWorkActive(
		&mgr.cr.Status.AppContext,
	) || shcImageUpgradeHasConflictingLifecycle(
		current,
		mgr.cr.Status.LifecycleOperation,
	)
	activeCurrent := current != nil &&
		current.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted
	conflictingPlannedOperation := observedConflictingOperation &&
		(activeCurrent ||
			(uniformSourceImage && sourceImage != targetImage))
	classification := upgrade.ClassifySHCImageUpgrade(
		upgrade.SHCImageUpgradeClassificationInput{
			StatefulSetName:             statefulSet.GetName(),
			DesiredRevision:             statefulSet.Status.UpdateRevision,
			TargetImage:                 targetImage,
			TargetReplicas:              *statefulSet.Spec.Replicas,
			Pods:                        imageUpgradePodsFromRolloutState(state),
			PathDecision:                pathDecision,
			ConflictingPlannedOperation: conflictingPlannedOperation,
			Current:                     current,
			Now:                         now,
		},
	)
	if classification.Operation != nil {
		mgr.cr.Status.ImageUpgrade = classification.Operation
		current = classification.Operation
	}
	switch classification.Classification {
	case upgrade.SHCImageUpgradeRecord:
		mgr.recordImageUpgradeStatus(
			classification.Reason,
			classification.Message,
		)
		// Persist workflow identity before recording initialization intent.
		return false, nil
	case upgrade.SHCImageUpgradeOrdinaryRollout:
		return true, nil
	case upgrade.SHCImageUpgradeWait:
		mgr.recordImageUpgradeStatus(
			classification.Reason,
			classification.Message,
		)
		return false, nil
	case upgrade.SHCImageUpgradeBlock:
		mgr.recordImageUpgradeStatus(
			classification.Reason,
			classification.Message,
		)
		return false, fmt.Errorf(
			"SHC image upgrade blocked (%s): %s",
			classification.Reason,
			classification.Message,
		)
	case upgrade.SHCImageUpgradeResume:
		// Continue into the initialization state machine.
	default:
		return false, fmt.Errorf(
			"unsupported SHC image-upgrade classification %q",
			classification.Classification,
		)
	}

	conflictingOperation := shcImageUpgradeHasConflictingLifecycle(
		current,
		mgr.cr.Status.LifecycleOperation,
	)
	coordinationOwned := !conflictingOperation &&
		!shcAppFrameworkWorkActive(&mgr.cr.Status.AppContext)
	targetOrdinal := int32(-1)
	targetEligible := false
	kvStoreReady := false
	kvStoreMessage := ""
	if coordinationOwned &&
		current.Phase ==
			enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing &&
		current.InitializationIntentAt != nil &&
		current.InitializationSucceededAt == nil {
		targetOrdinal, targetEligible = mgr.selectImageUpgradeManagementTarget(
			ctx,
		)
		if targetEligible {
			kvStoreObservation := mgr.observeSearchHeadKVStores(
				ctx,
				mgr.searchHeadMemberOrdinals(),
			)
			kvStoreReady = kvStoreObservation.Available &&
				len(kvStoreObservation.NotReadyMembers) == 0
			kvStoreMessage = kvStorePreflightMessage(kvStoreObservation)
		}
	}

	initialization := upgrade.EvaluateSHCImageUpgradeInitialization(
		upgrade.SHCImageUpgradeInitializationInput{
			Current:                     current,
			CoordinationOwned:           coordinationOwned,
			ConflictingPlannedOperation: conflictingOperation,
			ManagementTargetEligible:    targetEligible,
			KVStoreReady:                kvStoreReady,
			KVStoreMessage:              kvStoreMessage,
			Now:                         now,
		},
	)
	if initialization.Operation != nil {
		mgr.cr.Status.ImageUpgrade = initialization.Operation
	}
	mgr.recordImageUpgradeInitializationDecision(initialization)

	switch initialization.Action {
	case upgrade.SHCImageUpgradeInitializationPersist,
		upgrade.SHCImageUpgradeInitializationWait:
		return false, nil

	case upgrade.SHCImageUpgradeInitializationCall:
		if !targetEligible {
			return false, fmt.Errorf(
				"SHC image upgrade initialization authorized without an eligible management target",
			)
		}
		endpointErr := initiateSearchHeadClusterUpgrade(
			ctx,
			mgr,
			targetOrdinal,
		)
		attemptedAt := searchHeadClusterImageUpgradeNow()
		attempt := upgrade.RecordSHCImageUpgradeInitializationAttempt(
			mgr.cr.Status.ImageUpgrade,
			endpointErr == nil,
			attemptedAt,
		)
		if attempt.Operation != nil {
			mgr.cr.Status.ImageUpgrade = attempt.Operation
		}
		mgr.recordImageUpgradeInitializationDecision(attempt)
		if endpointErr != nil {
			return false, fmt.Errorf(
				"initialize Search Head Cluster image upgrade: %w",
				endpointErr,
			)
		}
		mgr.projectImageUpgradeInitializationStart(attemptedAt)
		return false, nil

	case upgrade.SHCImageUpgradeInitializationAllowMembers:
		return true, nil

	case upgrade.SHCImageUpgradeInitializationBlock:
		return false, fmt.Errorf(
			"SHC image upgrade initialization blocked (%s): %s",
			initialization.Reason,
			initialization.Message,
		)

	default:
		return false, fmt.Errorf(
			"unsupported SHC image-upgrade initialization action %q",
			initialization.Action,
		)
	}
}

func (mgr *searchHeadClusterPodManager) selectImageUpgradeManagementTarget(
	ctx context.Context,
) (int32, bool) {
	if !mgr.cr.Status.CaptainReady || mgr.cr.Status.Captain == "" {
		return -1, false
	}
	type candidate struct {
		ordinal int32
		name    string
	}
	candidates := make([]candidate, 0, len(mgr.cr.Status.Members))
	for ordinal, member := range mgr.cr.Status.Members {
		if member.Name == "" || !member.Registered || member.Status != "Up" {
			continue
		}
		candidates = append(candidates, candidate{
			ordinal: int32(ordinal),
			name:    member.Name,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].name < candidates[j].name
	})
	for _, candidate := range candidates {
		pod := &corev1.Pod{}
		err := mgr.c.Get(ctx, types.NamespacedName{
			Namespace: mgr.cr.GetNamespace(),
			Name:      candidate.name,
		}, pod)
		if err != nil || pod.DeletionTimestamp != nil || !podIsReady(pod) {
			continue
		}
		return candidate.ordinal, true
	}
	return -1, false
}

func (mgr *searchHeadClusterPodManager) projectImageUpgradeInitializationStart(
	now time.Time,
) {
	if mgr.cr.Status.UpgradeEndTimestamp < mgr.cr.Status.UpgradeStartTimestamp {
		return
	}
	startedAt := now.Unix()
	mgr.cr.Status.UpgradeStartTimestamp = startedAt
	mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgrading
	splmetrics.UpgradeStartTime.Set(float64(startedAt))
}

func (mgr *searchHeadClusterPodManager) recordImageUpgradeInitializationDecision(
	decision upgrade.SHCImageUpgradeInitializationDecision,
) {
	mgr.recordImageUpgradeStatus(decision.Reason, decision.Message)
}

func (mgr *searchHeadClusterPodManager) recordImageUpgradeStatus(
	reason enterpriseApi.SearchHeadClusterImageUpgradeReason,
	message string,
) {
	mgr.cr.Status.Message = fmt.Sprintf(
		"%s%s: %s",
		shcImageUpgradeStatusPrefix,
		reason,
		message,
	)
}

func statefulSetSplunkImage(statefulSet *appsv1.StatefulSet) (string, error) {
	for _, container := range statefulSet.Spec.Template.Spec.Containers {
		if container.Name == "splunk" && container.Image != "" {
			return container.Image, nil
		}
	}
	return "", fmt.Errorf(
		"StatefulSet %s has no declared splunk container image",
		statefulSet.GetName(),
	)
}

func imageUpgradePodsFromRolloutState(
	state upgrade.SHCRolloutState,
) []upgrade.SHCImageUpgradePod {
	pods := make([]upgrade.SHCImageUpgradePod, 0, len(state.Pods))
	for _, pod := range state.Pods {
		pods = append(pods, upgrade.SHCImageUpgradePod{
			Ordinal:  pod.Ordinal,
			Exists:   pod.Exists,
			Ready:    pod.Ready,
			Deleting: pod.Deleting,
			Revision: pod.Revision,
			Image:    pod.Image,
		})
	}
	return pods
}

func uniformSHCRolloutPodImage(
	pods []upgrade.SHCRolloutPod,
) (string, bool) {
	image := ""
	for _, pod := range pods {
		if !pod.Exists || pod.Image == "" {
			return "", false
		}
		if image == "" {
			image = pod.Image
			continue
		}
		if image != pod.Image {
			return "", false
		}
	}
	return image, len(pods) > 0
}

func shcImageUpgradeHasConflictingLifecycle(
	imageUpgrade *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	lifecycle *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) bool {
	if lifecycle == nil ||
		lifecycle.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		return false
	}
	return imageUpgrade == nil ||
		imageUpgrade.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers ||
		lifecycle.Intent !=
			enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate ||
		lifecycle.DesiredRevision != imageUpgrade.DesiredRevision
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

	statusMessage := fmt.Sprintf(
		"%s%s: %s",
		shcRollingUpdateStatusPrefix,
		decision.Reason,
		decision.Message,
	)
	lifecycleTerminal := false
	if operation := mgr.cr.Status.LifecycleOperation; operation != nil &&
		(operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
			operation.Stage ==
				enterpriseApi.SearchHeadClusterLifecycleStageFailed) {
		lifecycleTerminal = true
		reason := operation.Reason
		if reason == "" {
			reason =
				enterpriseApi.SearchHeadClusterLifecycleReasonClusterNotSafe
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
		statusMessage = fmt.Sprintf(
			"%s%s: %s",
			shcRollingUpdateStatusPrefix,
			reason,
			message,
		)
	}
	statusChanged := mgr.cr.Status.Message != statusMessage
	mgr.cr.Status.Message = statusMessage
	if statusChanged {
		splmetrics.SHCRolloutDecisionCounters.WithLabelValues(
			string(decision.Action),
			string(decision.Reason),
		).Inc()
	}

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
	if decision.Action == upgrade.SHCRolloutActionBlock &&
		statusChanged &&
		!lifecycleTerminal {
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
	if mgr.cr.Status.LastStableReplicas != nil &&
		replicas > *mgr.cr.Status.LastStableReplicas {
		baseline := *mgr.cr.Status.LastStableReplicas
		state.ScaleUpFromReplicas = &baseline
	}

	operation := mgr.cr.Status.LifecycleOperation
	inPlaceRecovery := shcInPlaceLifecycleRecovery(
		operation,
		statefulSet.Status.UpdateRevision,
	)
	if operation != nil &&
		((operation.Intent ==
			enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate &&
			operation.DesiredRevision == statefulSet.Status.UpdateRevision) ||
			inPlaceRecovery) {
		state.Lifecycle = upgrade.SHCRolloutLifecycle{
			TargetOrdinal:         operation.TargetOrdinal,
			Stage:                 operation.Stage,
			ReplacementAuthorized: operation.ReplacementAuthorizedAt != nil,
			InPlaceRecovery:       inPlaceRecovery,
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
			Image:    podSplunkImage(pod),
		})
		if ordinal < int32(len(mgr.cr.Status.Members)) {
			member := mgr.cr.Status.Members[ordinal]
			state.Pods[len(state.Pods)-1].MemberRegistered =
				member.Registered
			state.Pods[len(state.Pods)-1].MemberStatus = member.Status
		}
	}

	return state, nil
}

func shcInPlaceLifecycleRecovery(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	updateRevision string,
) bool {
	if operation == nil || operation.TargetOrdinal == nil {
		return false
	}
	switch operation.Intent {
	case enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate:
		return operation.TargetPodUID != "" &&
			operation.ReplacementAuthorizedAt == nil &&
			operation.DesiredRevision != "" &&
			updateRevision != "" &&
			operation.DesiredRevision != updateRevision
	case enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown:
		return operation.MembershipRemovalRequestedAt == nil &&
			(operation.Stage ==
				enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery ||
				operation.Stage ==
					enterpriseApi.SearchHeadClusterLifecycleStageCompleted ||
				operation.Stage ==
					enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
				operation.Stage ==
					enterpriseApi.SearchHeadClusterLifecycleStageFailed)
	default:
		return false
	}
}

func podSplunkImage(pod *corev1.Pod) string {
	for _, container := range pod.Spec.Containers {
		if container.Name == "splunk" {
			return container.Image
		}
	}
	return ""
}
