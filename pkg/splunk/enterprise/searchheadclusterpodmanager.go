package enterprise

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	metrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// searchHeadClusterPodManager is used to manage the pods within a search head cluster
type searchHeadClusterPodManager struct {
	c                        splcommon.ControllerClient
	cr                       *enterpriseApi.SearchHeadCluster
	secrets                  *corev1.Secret
	statefulSet              *appsv1.StatefulSet
	newSplunkClient          func(managementURI, username, password string) *splclient.SplunkClient
	servingConditionChanged  map[int32]bool
	statefulSetUpdatePending bool
	// initialFormationContainersReady is recomputed from every desired Search
	// Head Pod before serving conditions are reconciled. It prevents an
	// earlier initialized member from entering Service traffic while another
	// first-formation member is still running image-owned initialization.
	initialFormationContainersReady bool
	// authorizedRevisionWithdrawalRequested is an in-memory signal from the
	// desired StatefulSet renderer. It is never persisted in a Kubernetes
	// object; Update converts it to durable lifecycle status before changing
	// the StatefulSet partition.
	authorizedRevisionWithdrawalRequested bool
}

// newSerachHeadClusterPodManager function to create pod manager this is added to write unit test case
var newSearchHeadClusterPodManager = func(client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) searchHeadClusterPodManager {
	return searchHeadClusterPodManager{
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
		c:               client,
	}
}

func (mgr *searchHeadClusterPodManager) reconcileSearchDrainContinuationApproval(
	ctx context.Context,
	eventPublisher *K8EventPublisher,
) bool {
	operation := mgr.cr.Status.LifecycleOperation
	approval := mgr.cr.Spec.LifecycleApproval
	if operation == nil || approval == nil {
		return false
	}

	var activeHistoricalSearches int32
	var activeRealtimeSearches int32
	targetObserved := false
	for i := range mgr.cr.Status.Members {
		member := mgr.cr.Status.Members[i]
		if member.Name != operation.TargetPod {
			continue
		}
		activeHistoricalSearches = int32(member.ActiveHistoricalSearchCount)
		activeRealtimeSearches = int32(member.ActiveRealtimeSearchCount)
		targetObserved = true
		break
	}
	if !targetObserved {
		return false
	}

	approvedOperation, applied :=
		shcworkflow.ApplySearchDrainContinuationApproval(
			operation,
			approval,
			mgr.cr.GetGeneration(),
			activeHistoricalSearches,
			activeRealtimeSearches,
			searchHeadClusterLifecycleNow(),
		)
	if !applied {
		return false
	}
	mgr.cr.Status.LifecycleOperation = approvedOperation
	metrics.SHCSearchDrainContinuationApprovalCounter.Inc()
	eventPublisher.Normal(
		ctx,
		EventReasonSHCSearchDrainContinuationApproved,
		fmt.Sprintf(
			"Approved continuation of lifecycle operation %s for %s after search-drain timeout with historical=%d realtime=%d; cluster and captain safety will be revalidated before replacement",
			approvedOperation.OperationID,
			approvedOperation.TargetPod,
			activeHistoricalSearches,
			activeRealtimeSearches,
		),
	)
	logging.FromContext(ctx).InfoContext(
		ctx,
		"Search Head search-drain continuation approval applied",
		"operationID",
		approvedOperation.OperationID,
		"targetPod",
		approvedOperation.TargetPod,
		"approvalGeneration",
		approvedOperation.SearchDrainContinuationApprovalGeneration,
		"activeHistoricalSearches",
		activeHistoricalSearches,
		"activeRealtimeSearches",
		activeRealtimeSearches,
	)
	return true
}

// Update for searchHeadClusterPodManager handles all updates for a statefulset of search heads
func (mgr *searchHeadClusterPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	logger := logging.FromContext(ctx).With("func", "searchHeadClusterPodManager.Update")
	podUpdateRecoveryCompleted := false

	// Assign client
	if mgr.c == nil {
		mgr.c = c
	}
	mgr.statefulSet = statefulSet

	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, mgr.cr)

	if mgr.authorizedRevisionWithdrawalRequested {
		started, err := mgr.startAuthorizedRevisionRecovery(
			ctx,
			eventPublisher,
			statefulSet,
		)
		if err != nil {
			return enterpriseApi.PhaseError, err
		}
		if started {
			// Persist recovery intent before raising the StatefulSet partition on
			// a later reconciliation.
			return enterpriseApi.PhaseUpdating, nil
		}
	}

	// update statefulset, if necessary
	statefulSetPhase, err := splctrl.ApplyStatefulSet(ctx, mgr.c, statefulSet)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}
	mgr.statefulSetUpdatePending = statefulSetPhase == enterpriseApi.PhaseUpdating
	if mgr.statefulSetUpdatePending &&
		mgr.cr.Status.LifecycleOperation != nil &&
		mgr.cr.Status.LifecycleOperation.RecoveryRevision != "" {
		mgr.cr.Status.Message =
			"SHC RollingUpdate AuthorizedRevisionWithdrawn: waiting for the StatefulSet controller to observe the last-known-good target recovery partition"
		return enterpriseApi.PhaseUpdating, nil
	}
	if recoveryPending, recoveryErr :=
		mgr.reconcileAuthorizedRevisionRecoveryPodDeletion(
			ctx,
			eventPublisher,
			statefulSet,
		); recoveryErr != nil {
		return enterpriseApi.PhaseError, recoveryErr
	} else if recoveryPending {
		return enterpriseApi.PhaseUpdating, nil
	}

	// for now pass the targetPodName as empty since we are going to fill it in ApplyShcSecret
	podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, "")

	// Check if a recycle of shc pods is necessary(due to shc_secret mismatch with namespace scoped secret)
	err = ApplyShcSecret(ctx, mgr, desiredReplicas, podExecClient)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}

	// update CR status with SHC information
	err = mgr.updateStatus(ctx, statefulSet)
	if err == nil &&
		searchHeadServingReadinessGateConfigured(statefulSet) {
		initialFormationContainersReady, readinessErr :=
			mgr.desiredSearchHeadContainersReady(
				ctx,
				statefulSet,
			)
		if readinessErr != nil {
			return enterpriseApi.PhaseError, readinessErr
		}
		mgr.initialFormationContainersReady =
			initialFormationContainersReady
		restartInitiated, restartErr :=
			mgr.reconcileInitialFormationRestart(
				ctx,
				eventPublisher,
				desiredReplicas,
			)
		if restartErr != nil {
			return enterpriseApi.PhaseError, restartErr
		}
		mgr.reconcileInitialFormationStage(desiredReplicas)
		if readinessErr = mgr.reconcileSearchHeadServingConditions(
			ctx,
			statefulSet,
		); readinessErr != nil {
			return enterpriseApi.PhaseError, readinessErr
		}
		if restartInitiated {
			return enterpriseApi.PhasePending, nil
		}
	}
	if err == nil &&
		mgr.cr.Status.LifecycleOperation != nil &&
		mgr.cr.Status.LifecycleOperation.Intent ==
			enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown {
		mgr.cr.Status.LifecycleOperation = shcworkflow.CompleteScaleDown(
			mgr.cr.Status.LifecycleOperation,
			statefulSet.Status.Replicas,
			searchHeadClusterLifecycleNow(),
		)
	}
	if err == nil && searchHeadClusterLifecycleEnabled() {
		var cancellationStarted bool
		mgr.cr.Status.LifecycleOperation, cancellationStarted =
			shcworkflow.StartScaleDownCancellation(
				mgr.cr.Status.LifecycleOperation,
				statefulSet.Status.Replicas,
				desiredReplicas,
				searchHeadClusterLifecycleNow(),
			)
		if cancellationStarted {
			eventPublisher.Normal(
				ctx,
				EventReasonSHCScaleDownCancelled,
				fmt.Sprintf(
					"Scale down of %s was cancelled before membership removal; restoring the member to service",
					mgr.cr.Status.LifecycleOperation.TargetPod,
				),
			)
			// Persist the cancellation stage before issuing a detention
			// release on a later reconciliation.
			return enterpriseApi.PhaseUpdating, nil
		}
	}
	if err == nil && searchHeadClusterLifecycleEnabled() {
		var cancellationStarted bool
		mgr.cr.Status.LifecycleOperation, cancellationStarted =
			shcworkflow.StartPodUpdateCancellation(
				mgr.cr.Status.LifecycleOperation,
				statefulSet.Status.UpdateRevision,
				searchHeadClusterLifecycleNow(),
			)
		if cancellationStarted {
			eventPublisher.Normal(
				ctx,
				EventReasonSHCPodUpdateCancelled,
				fmt.Sprintf(
					"Pod update of %s to revision %s was cancelled before replacement authorization; restoring the original member before revision %s",
					mgr.cr.Status.LifecycleOperation.TargetPod,
					mgr.cr.Status.LifecycleOperation.DesiredRevision,
					statefulSet.Status.UpdateRevision,
				),
			)
			// Persist the cancellation stage before releasing detention on a
			// later reconciliation.
			return enterpriseApi.PhaseUpdating, nil
		}
	}
	if err == nil &&
		searchHeadClusterLifecycleEnabled() &&
		mgr.reconcileSearchDrainContinuationApproval(
			ctx,
			eventPublisher,
		) {
		// Persist the approval and its current search-count snapshot before
		// revalidating safety or authorizing replacement.
		return enterpriseApi.PhaseUpdating, nil
	}
	onDeleteReplacementObserved := false
	if err == nil && searchHeadClusterLifecycleEnabled() {
		var replacementObservationErr error
		onDeleteReplacementObserved, replacementObservationErr =
			mgr.onDeleteReplacementObserved(
				ctx,
				statefulSet,
				mgr.cr.Status.LifecycleOperation,
			)
		if replacementObservationErr != nil {
			return enterpriseApi.PhaseError, replacementObservationErr
		}
	}
	if err == nil &&
		searchHeadClusterLifecycleEnabled() &&
		lifecycleRecoveryActiveForStatefulSet(
			statefulSet,
			mgr.cr.Status.LifecycleOperation,
			onDeleteReplacementObserved,
		) {
		stageBeforeRecovery := mgr.cr.Status.LifecycleOperation.Stage
		recoveryComplete, lifecycleErr := mgr.resumeLifecycleRecovery(
			ctx,
			*mgr.cr.Status.LifecycleOperation.TargetOrdinal,
		)
		if lifecycleErr != nil {
			return enterpriseApi.PhaseError, lifecycleErr
		}
		if recoveryComplete &&
			mgr.cr.Status.LifecycleOperation.Intent ==
				enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate {
			podUpdateRecoveryCompleted = true
		}
		if blockedErr := mgr.lifecycleBlockedError(
			ctx,
			stageBeforeRecovery,
		); blockedErr != nil {
			return enterpriseApi.PhaseError, blockedErr
		}
		authorizedRevisionRecovery :=
			mgr.cr.Status.LifecycleOperation.RecoveryRevision != ""
		if authorizedRevisionRecoveryCompletedOnThisReconcile(
			stageBeforeRecovery,
			mgr.cr.Status.LifecycleOperation,
		) {
			eventPublisher.Normal(
				ctx,
				EventReasonSHCAuthorizedRevisionRecovered,
				fmt.Sprintf(
					"Search Head %s recovered from withdrawn authorized revision %s at last known-good revision %s; the queued desired template may now reconcile",
					mgr.cr.Status.LifecycleOperation.TargetPod,
					mgr.cr.Status.LifecycleOperation.DesiredRevision,
					mgr.cr.Status.LifecycleOperation.RecoveryRevision,
				),
			)
			metrics.SHCAuthorizedRevisionRecoveryCounter.Inc()
			// The desired object was rendered before recovery completed and
			// still holds the superseded template. Requeue before planning
			// any further partition change.
			return enterpriseApi.PhaseUpdating, nil
		}
		if !recoveryComplete {
			if authorizedRevisionRecovery {
				mgr.cr.Status.Message = fmt.Sprintf(
					"SHC RollingUpdate AuthorizedRevisionWithdrawn: recovering %s at last known-good revision %s before reconciling the queued template",
					mgr.cr.Status.LifecycleOperation.TargetPod,
					mgr.cr.Status.LifecycleOperation.RecoveryRevision,
				)
				return enterpriseApi.PhaseUpdating, nil
			}
			if statefulSet.Spec.UpdateStrategy.Type ==
				appsv1.RollingUpdateStatefulSetStrategyType {
				if observeErr := mgr.recordRollingUpdateObservation(
					ctx,
					statefulSet,
				); observeErr != nil {
					return enterpriseApi.PhaseError, observeErr
				}
			}
			return enterpriseApi.PhaseUpdating, nil
		}
	}
	if err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.CaptainReady {
		if err == nil &&
			searchHeadClusterLifecycleEnabled() &&
			mgr.cr.Status.LifecycleOperation != nil &&
			mgr.cr.Status.LifecycleOperation.TargetOrdinal != nil &&
			mgr.cr.Status.LifecycleOperation.TargetPodUID == "" {
			_, lifecycleErr := mgr.prepareLifecycleReplacement(
				ctx,
				*mgr.cr.Status.LifecycleOperation.TargetOrdinal,
				mgr.cr.Status.LifecycleOperation.Intent,
			)
			if lifecycleErr != nil {
				return enterpriseApi.PhaseError, lifecycleErr
			}
		}
		if termErr := splctrl.CheckPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
			logger.ErrorContext(ctx, "terminal pod failure detected; setting PhaseError", "error", termErr)
			return enterpriseApi.PhaseError, termErr
		}
		logger.InfoContext(ctx, "SearchHeadCluster is not ready", "error", err)
		// A scale up/down can already be underway even while captain election
		// is in flight (e.g. the member being recycled was the captain). Report
		// the transient phase from replica counts alone so callers don't see a
		// false Pending for the whole election window; PrepareScaleDown/Recycle
		// still won't run until CaptainReady returns true on a later reconcile.
		if mgr.cr.Status.ReadyReplicas > desiredReplicas {
			return enterpriseApi.PhaseScalingDown, nil
		}
		if mgr.cr.Status.ReadyReplicas > 0 && mgr.cr.Status.ReadyReplicas < desiredReplicas {
			return normalizeSearchHeadClusterPodUpdatePhase(
				enterpriseApi.PhaseScalingUp,
				mgr.cr.Status.LifecycleOperation,
				podUpdateRecoveryCompleted,
			), nil
		}
		return enterpriseApi.PhasePending, nil
	}

	// manage scaling and updates
	phase, err := mgr.updateStatefulSetPods(
		ctx,
		statefulSet,
		desiredReplicas,
	)
	if err != nil {
		return phase, err
	}
	phase = normalizeSearchHeadClusterPodUpdatePhase(
		phase,
		mgr.cr.Status.LifecycleOperation,
		podUpdateRecoveryCompleted,
	)

	mgr.recordStableReplicaCount(
		ctx,
		eventPublisher,
		phase,
		desiredReplicas,
	)

	return phase, nil
}

func (mgr *searchHeadClusterPodManager) startAuthorizedRevisionRecovery(
	ctx context.Context,
	eventPublisher *K8EventPublisher,
	desiredStatefulSet *appsv1.StatefulSet,
) (bool, error) {
	if desiredStatefulSet == nil {
		return false, nil
	}
	current := &appsv1.StatefulSet{}
	if err := mgr.c.Get(ctx, types.NamespacedName{
		Namespace: desiredStatefulSet.GetNamespace(),
		Name:      desiredStatefulSet.GetName(),
	}, current); err != nil {
		return false, err
	}
	safe, err := authorizedRevisionRecoverySafe(
		ctx,
		mgr.c,
		mgr.cr,
		current,
	)
	if err != nil || !safe {
		return false, err
	}
	operation, started :=
		shcworkflow.StartAuthorizedPodUpdateRevisionRecovery(
			mgr.cr.Status.LifecycleOperation,
			current.Status.CurrentRevision,
			searchHeadClusterLifecycleNow(),
		)
	if !started {
		return false, nil
	}

	mgr.cr.Status.LifecycleOperation = operation
	mgr.cr.Status.Message = fmt.Sprintf(
		"SHC RollingUpdate %s: %s",
		operation.Reason,
		operation.Message,
	)
	eventPublisher.Normal(
		ctx,
		EventReasonSHCAuthorizedRevisionWithdrawn,
		fmt.Sprintf(
			"Withdrawing authorized revision %s for %s after a startup failure; recover last known-good revision %s before reconciling the queued desired template",
			operation.DesiredRevision,
			operation.TargetPod,
			operation.RecoveryRevision,
		),
	)
	metrics.SHCAuthorizedRevisionWithdrawalCounter.Inc()
	logging.FromContext(ctx).InfoContext(
		ctx,
		"Search Head authorized revision withdrawal recorded",
		"operationID",
		operation.OperationID,
		"targetPod",
		operation.TargetPod,
		"withdrawnRevision",
		operation.DesiredRevision,
		"recoveryRevision",
		operation.RecoveryRevision,
	)
	return true, nil
}

// authorizedRevisionRecoveryCompletedOnThisReconcile distinguishes the one
// completion transition that must requeue from later reconciliations that see
// the already-completed recovery. The next reconciliation is then free to
// apply and plan the queued revision as a fresh operation.
func authorizedRevisionRecoveryCompletedOnThisReconcile(
	stageBefore enterpriseApi.SearchHeadClusterLifecycleStage,
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) bool {
	return operation != nil &&
		operation.RecoveryRevision != "" &&
		stageBefore != enterpriseApi.SearchHeadClusterLifecycleStageCompleted &&
		operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageCompleted
}

func normalizeSearchHeadClusterPodUpdatePhase(
	phase enterpriseApi.Phase,
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	recoveryCompleted bool,
) enterpriseApi.Phase {
	if phase != enterpriseApi.PhaseScalingUp ||
		operation == nil ||
		operation.Intent !=
			enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate {
		return phase
	}

	switch operation.Stage {
	case enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		enterpriseApi.SearchHeadClusterLifecycleStageFailed:
		if !recoveryCompleted {
			return phase
		}
	}

	return enterpriseApi.PhaseUpdating
}

func (mgr *searchHeadClusterPodManager) recordStableReplicaCount(
	ctx context.Context,
	eventPublisher *K8EventPublisher,
	phase enterpriseApi.Phase,
	desiredReplicas int32,
) {
	if phase != enterpriseApi.PhaseReady ||
		mgr.cr.Status.ReadyReplicas != desiredReplicas ||
		mgr.searchHeadInitialFormationRestartPending() {
		return
	}

	previous := mgr.cr.Status.LastStableReplicas
	current := desiredReplicas
	mgr.cr.Status.LastStableReplicas = &current
	if previous == nil || *previous == desiredReplicas ||
		eventPublisher == nil {
		return
	}

	if desiredReplicas > *previous {
		eventPublisher.Normal(
			ctx,
			EventReasonScaledUp,
			fmt.Sprintf(
				"Successfully scaled %s up from %d to %d replicas",
				mgr.cr.GetName(),
				*previous,
				desiredReplicas,
			),
		)
		return
	}

	eventPublisher.Normal(
		ctx,
		EventReasonScaledDown,
		fmt.Sprintf(
			"Successfully scaled %s down from %d to %d replicas",
			mgr.cr.GetName(),
			*previous,
			desiredReplicas,
		),
	)
}

func (mgr *searchHeadClusterPodManager) updateStatefulSetPods(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
	desiredReplicas int32,
) (enterpriseApi.Phase, error) {
	currentMemberJoined := true
	if statefulSet.Spec.Replicas != nil &&
		*statefulSet.Spec.Replicas < desiredReplicas {
		currentMemberJoined = mgr.currentScaleUpMemberJoined(
			ctx,
			statefulSet,
		)
		if statefulSet.Status.ReadyReplicas >=
			*statefulSet.Spec.Replicas &&
			!currentMemberJoined {
			return enterpriseApi.PhaseScalingUp, nil
		}
	}
	reconcileReplicas := nextSearchHeadClusterReplicaTarget(
		statefulSet,
		desiredReplicas,
		currentMemberJoined,
	)
	if statefulSet.Spec.UpdateStrategy.Type ==
		appsv1.RollingUpdateStatefulSetStrategyType {
		return mgr.updateRollingStatefulSetPods(
			ctx,
			statefulSet,
			reconcileReplicas,
		)
	}
	mgr.cr.Status.Message = ""
	return splctrl.UpdateStatefulSetPods(
		ctx,
		mgr.c,
		statefulSet,
		mgr,
		reconcileReplicas,
	)
}

func nextSearchHeadClusterReplicaTarget(
	statefulSet *appsv1.StatefulSet,
	desiredReplicas int32,
	currentMemberJoined bool,
) int32 {
	if statefulSet.Spec.Replicas == nil {
		return desiredReplicas
	}
	currentReplicas := *statefulSet.Spec.Replicas
	if currentReplicas >= desiredReplicas {
		return desiredReplicas
	}
	if statefulSet.Status.ReadyReplicas < currentReplicas ||
		!currentMemberJoined {
		return currentReplicas
	}
	return currentReplicas + 1
}

func (mgr *searchHeadClusterPodManager) currentScaleUpMemberJoined(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
) bool {
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas == 0 {
		return true
	}
	currentReplicas := *statefulSet.Spec.Replicas
	targetOrdinal := currentReplicas - 1
	if targetOrdinal >= int32(len(mgr.cr.Status.Members)) ||
		!mgr.cr.Status.Initialized ||
		!mgr.cr.Status.MinPeersJoined ||
		!mgr.cr.Status.CaptainReady {
		return false
	}
	targetPod := fmt.Sprintf("%s-%d", statefulSet.GetName(), targetOrdinal)
	target := mgr.cr.Status.Members[targetOrdinal]
	if target.Name != targetPod ||
		!target.Registered ||
		target.Status != "Up" {
		return false
	}

	captainOrdinal := int32(-1)
	for ordinal := range mgr.cr.Status.Members {
		if mgr.cr.Status.Members[ordinal].Name == mgr.cr.Status.Captain {
			captainOrdinal = int32(ordinal)
			break
		}
	}
	if captainOrdinal < 0 {
		return false
	}
	members, err := getSearchHeadCaptainMembers(ctx, mgr, captainOrdinal)
	if err != nil {
		return false
	}
	captainCount := 0
	authoritativeCaptain := false
	for _, member := range members {
		if member.Captain {
			captainCount++
			authoritativeCaptain = member.Label == mgr.cr.Status.Captain
		}
	}
	targetFromCaptain, targetObserved := members[targetPod]
	return captainCount == 1 &&
		authoritativeCaptain &&
		targetObserved &&
		targetFromCaptain.Identifier != "" &&
		targetFromCaptain.Status == "Up"
}

// PrepareScaleDown for searchHeadClusterPodManager prepares search head pod to be removed via scale down event; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	logger := logging.FromContext(ctx).With("func", "PrepareScaleDown")
	// start by quarantining the pod
	var result bool
	var err error
	if searchHeadClusterLifecycleEnabled() {
		result, err = mgr.prepareLifecycleReplacement(ctx, n, enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown)
	} else {
		result, err = mgr.prepareRecycleLegacy(ctx, n)
	}
	if err != nil || !result {
		return result, err
	}
	if searchHeadClusterLifecycleEnabled() {
		return mgr.requestScaleDownMembershipRemoval(ctx, n)
	}

	// pod is quarantined; decommission it
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
	logger.WarnContext(ctx, "member leaving SearchHeadCluster",
		"member", memberName,
		"remaining_count", len(mgr.cr.Status.Members)-1)

	err = removeSearchHeadClusterMember(ctx, mgr, n)
	if err != nil {
		return false, err
	}

	// all done -> ok to scale down the statefulset
	return true, nil
}

var removeSearchHeadClusterMember = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
) error {
	return mgr.getClient(ctx, n).
		RemoveSearchHeadClusterMember()
}

func (mgr *searchHeadClusterPodManager) requestScaleDownMembershipRemoval(
	ctx context.Context,
	n int32,
) (bool, error) {
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.Intent != enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != n {
		return false, fmt.Errorf(
			"SHC scale-down authorization does not match ordinal %d",
			n,
		)
	}
	if operation.MembershipRemovalRequestedAt != nil {
		return true, nil
	}
	logger := logging.FromContext(ctx).With("func", "PrepareScaleDown")
	logger.WarnContext(
		ctx,
		"member leaving SearchHeadCluster",
		"member", operation.TargetPod,
		"remaining_count", len(mgr.cr.Status.Members)-1,
	)
	if err := removeSearchHeadClusterMember(ctx, mgr, n); err != nil {
		return false, err
	}
	requestedAt := metav1.NewTime(searchHeadClusterLifecycleNow())
	operation.MembershipRemovalRequestedAt = &requestedAt
	// Persist successful membership removal before changing replicas.
	return false, nil
}

// PrepareRecycle for searchHeadClusterPodManager prepares search head pod to be recycled for updates; it returns true when ready
func (mgr *searchHeadClusterPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	if searchHeadClusterLifecycleEnabled() {
		return mgr.prepareLifecycleReplacement(ctx, n, enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate)
	}
	return mgr.prepareRecycleLegacy(ctx, n)
}

func (mgr *searchHeadClusterPodManager) prepareRecycleLegacy(ctx context.Context, n int32) (bool, error) {
	logger := logging.FromContext(ctx).With("func", "PrepareRecycle")
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// Detain search head
		logger.InfoContext(ctx, "detaining SearchHeadCluster member", "memberName", memberName)
		c := mgr.getClient(ctx, n)

		podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, getApplicablePodNameForK8Probes(mgr.cr, n))

		err := setProbeLevelOnSplunkPod(ctx, podExecClient, livenessProbeLevelOne)

		if err != nil {
			// During the Recycle, our reconcile loop is entered multiple times. If the Pod is already down,
			// there is a chance of readiness probe failing, in which case, even the podExec will not be successful.
			// So, just log the message, and ignore the error.
			logger.WarnContext(ctx, "setting Probe level failed. Probably, the Pod is already down", "memberName", memberName)
		}

		logger.InfoContext(ctx, "initializes rolling upgrade process")
		err = c.InitiateUpgrade()

		if err != nil {
			logger.ErrorContext(ctx, "initialization of rolling upgrade failed", "error", err)
			return false, err
		}

		start := mgr.cr.Status.UpgradeStartTimestamp
		end := mgr.cr.Status.UpgradeEndTimestamp

		if end >= start {
			currentTime := time.Now().Unix()
			mgr.cr.Status.UpgradeStartTimestamp = currentTime

			metrics.UpgradeStartTime.Set(float64(currentTime))

			mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgrading
		}

		return false, c.SetSearchHeadDetention(true)

	case "ManualDetention":

		metrics.ActiveHistoricalSearchCount.With(prometheus.Labels{
			"sh_name": mgr.cr.Status.Members[n].Name,
		}).Set(float64(mgr.cr.Status.Members[n].ActiveHistoricalSearchCount))

		metrics.ActiveRealtimeSearchCount.With(prometheus.Labels{
			"sh_name": mgr.cr.Status.Members[n].Name,
		}).Set(float64(mgr.cr.Status.Members[n].ActiveRealtimeSearchCount))

		// Wait until active searches have drained
		searchesComplete := mgr.cr.Status.Members[n].ActiveHistoricalSearchCount+mgr.cr.Status.Members[n].ActiveRealtimeSearchCount == 0
		if searchesComplete {
			logger.InfoContext(ctx, "detention complete", "memberName", memberName)
		} else {
			logger.InfoContext(ctx, "waiting for active searches to complete", "memberName", memberName)
		}
		return searchesComplete, nil

	case "": // this can happen after the member has already been recycled and we're just waiting for state to update
		logger.InfoContext(ctx, "member has empty Status", "memberName", memberName)
		return false, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// FinishRecycle for searchHeadClusterPodManager completes recycle event for search head pod; it returns true when complete
func (mgr *searchHeadClusterPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	if searchHeadClusterLifecycleEnabled() {
		operation := mgr.cr.Status.LifecycleOperation
		if operation != nil &&
			operation.TargetOrdinal != nil &&
			*operation.TargetOrdinal == n {
			return operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted, nil
		}
		// This Pod is not the active lifecycle target. Up-to-date higher
		// ordinals from an earlier completed step must not block traversal to
		// the current target.
		return true, nil
	}

	logger := logging.FromContext(ctx).With("func", "FinishRecycle")
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// not in detention
		return true, nil

	case "ManualDetention":
		// release from detention
		logger.InfoContext(ctx, "releasing SearchHeadCluster member from detention", "memberName", memberName)
		c := mgr.getClient(ctx, n)
		return false, c.SetSearchHeadDetention(false)

	case "": // member info is transiently unavailable (e.g. pod mid-restart); wait for it to come back
		logger.InfoContext(ctx, "member status is transiently unavailable, waiting for it to be reported again", "memberName", memberName)
		return false, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

func (mgr *searchHeadClusterPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	// check if shc is in an upgrade process
	if mgr.cr.Status.UpgradePhase == enterpriseApi.UpgradePhaseUpgrading {
		logger := logging.FromContext(ctx).With("func", "FinishUpgrade")
		c := mgr.getClient(ctx, n)

		// stop gathering metrics
		currentTime := time.Now().Unix()
		mgr.cr.Status.UpgradeEndTimestamp = currentTime

		metrics.UpgradeEndTime.Set(float64(currentTime))

		// revert upgrade state status
		mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgraded

		logger.InfoContext(ctx, "finalize Upgrade")
		return c.FinalizeUpgrade()
	}

	return nil
}

// getClient for searchHeadClusterPodManager returns a SplunkClient for the member n
func (mgr *searchHeadClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	logger := logging.FromContext(ctx).With("func", "searchHeadClusterPodManager.getClient")
	// Get Pod Name
	memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, splcommon.GetSplunkServiceName(SplunkSearchHead, mgr.cr.GetName(), true)))

	// Retrieve admin password from Pod
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, memberName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		logger.ErrorContext(ctx, "couldn't retrieve the admin password from Pod", "member", memberName, "error", err)
	}

	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPwd)
}

// GetSearchHeadClusterMemberInfo used in mocking this function
var GetSearchHeadClusterMemberInfo = func(ctx context.Context, mgr *searchHeadClusterPodManager, n int32) (*splclient.SearchHeadClusterMemberInfo, error) {
	c := mgr.getClient(ctx, n)
	return c.GetSearchHeadClusterMemberInfo()
}

// GetSearchHeadCaptainInfo used in mocking this function
var GetSearchHeadCaptainInfo = func(ctx context.Context, mgr *searchHeadClusterPodManager, n int32) (*splclient.SearchHeadCaptainInfo, error) {
	c := mgr.getClient(ctx, n)
	return c.GetSearchHeadCaptainInfo()
}

// GetSearchHeadCaptainMembersForStatus is replaceable in tests. The captain's
// member view is authoritative for a peer that Splunk has selected for an
// internal rolling restart; the peer's local member endpoint can still report
// Up during that transition.
var GetSearchHeadCaptainMembersForStatus = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
	n int32,
) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
	return mgr.getClient(ctx, n).GetSearchHeadCaptainMembers()
}

// InitiateSearchHeadInitialFormationRollingRestart is replaceable in tests.
// Splunk proxies the captain control endpoint when ordinal zero is not the
// active captain.
var InitiateSearchHeadInitialFormationRollingRestart = func(
	ctx context.Context,
	mgr *searchHeadClusterPodManager,
) error {
	return mgr.getClient(ctx, 0).
		InitiateSearchHeadRollingRestart(true)
}

func (mgr *searchHeadClusterPodManager) reconcileInitialFormationRestart(
	ctx context.Context,
	eventPublisher *K8EventPublisher,
	desiredReplicas int32,
) (bool, error) {
	if !mgr.searchHeadInitialFormationPending() ||
		normalizedSearchHeadInitialFormationStage(
			mgr.cr.Status.InitialFormationStage,
		) !=
			enterpriseApi.SearchHeadClusterInitialFormationStageClusterFormation ||
		mgr.cr.Status.InitialFormationRestartInitiated ||
		mgr.cr.Status.TelAppInstalled ||
		!mgr.initialFormationContainersReady ||
		!mgr.cr.Status.CaptainMembersObserved ||
		mgr.cr.Status.CaptainRollingRestart ||
		!mgr.cr.Status.Initialized ||
		!mgr.cr.Status.MinPeersJoined ||
		!mgr.cr.Status.CaptainReady ||
		mgr.cr.Status.MaintenanceMode ||
		int32(len(mgr.cr.Status.Members)) != desiredReplicas {
		return false, nil
	}

	advertisedMembers := make([]string, 0, desiredReplicas)
	for i := range mgr.cr.Status.Members {
		member := mgr.cr.Status.Members[i]
		if member.Status != "Up" ||
			member.CaptainStatus != "Up" ||
			!member.Registered {
			return false, nil
		}
		if member.AdvertiseRestartRequired {
			advertisedMembers = append(advertisedMembers, member.Name)
		}
	}
	if len(advertisedMembers) == 0 {
		return false, nil
	}

	if err := InitiateSearchHeadInitialFormationRollingRestart(
		ctx,
		mgr,
	); err != nil {
		// A request accepted by the captain sets rolling_restart_flag before
		// returning. If the response raced a retry, use that authoritative
		// state instead of treating an already-running restart as a failure.
		captainInfo, observeErr := GetSearchHeadCaptainInfo(ctx, mgr, 0)
		if observeErr != nil || !captainInfo.RollingRestart {
			return false, fmt.Errorf(
				"unable to initiate first-formation SHC rolling restart: %w",
				err,
			)
		}
	}

	mgr.cr.Status.CaptainRollingRestart = true
	mgr.cr.Status.InitialFormationRestartInitiated = true
	mgr.cr.Status.InitialFormationStableSince = nil
	if eventPublisher != nil {
		eventPublisher.Normal(
			ctx,
			EventReasonSHCInitialFormationRestartStarted,
			fmt.Sprintf(
				"Splunk accepted a first-formation rolling restart for restart-required Search Heads: %s",
				strings.Join(advertisedMembers, ", "),
			),
		)
	}
	logging.FromContext(ctx).InfoContext(
		ctx,
		"first-formation Search Head rolling restart initiated",
		"members",
		advertisedMembers,
	)
	return true, nil
}

// updateStatus for searchHeadClusterPodManager uses the REST API to update the status for a SearcHead custom resource
func (mgr *searchHeadClusterPodManager) updateStatus(ctx context.Context, statefulSet *appsv1.StatefulSet) error {
	// populate members status using REST API to get search head cluster member info
	previousCaptain := mgr.cr.Status.Captain
	previousMemberCount := int32(len(mgr.cr.Status.Members))

	mgr.cr.Status.Captain = ""
	mgr.cr.Status.CaptainReady = false
	mgr.cr.Status.CaptainRollingRestart = false
	mgr.cr.Status.CaptainMembersObserved = false
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if mgr.cr.Status.ReadyReplicas == 0 &&
		!searchHeadServingReadinessGateConfigured(statefulSet) {
		return nil
	}

	shcLogger := logging.FromContext(ctx)

	memberObservationCount := searchHeadClusterMemberObservationCount(
		statefulSet,
		mgr.cr.Status.LifecycleOperation,
	)
	gotCaptainInfo := false
	captainObservationOrdinal := int32(-1)
	observeCaptainMembers := false
	for n := int32(0); n < memberObservationCount; n++ {
		memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
		memberStatus := enterpriseApi.SearchHeadClusterMemberStatus{Name: memberName}
		memberInfo, err := GetSearchHeadClusterMemberInfo(ctx, mgr, n)
		if err == nil {
			memberStatus.Status = memberInfo.Status
			memberStatus.RestartState = memberInfo.RestartState
			memberStatus.Adhoc = memberInfo.Adhoc
			memberStatus.Registered = memberInfo.Registered
			memberStatus.ActiveHistoricalSearchCount = memberInfo.ActiveHistoricalSearchCount
			memberStatus.ActiveRealtimeSearchCount = memberInfo.ActiveRealtimeSearchCount
		} else if lifecycleMemberObservationExpectedUnavailable(
			mgr.cr.Status.LifecycleOperation,
			n,
		) {
			shcLogger.InfoContext(
				ctx,
				"SearchHeadCluster lifecycle target is temporarily unavailable",
				"memberName",
				memberName,
				"operationID",
				mgr.cr.Status.LifecycleOperation.OperationID,
				"stage",
				mgr.cr.Status.LifecycleOperation.Stage,
				"error",
				err,
			)
		} else if scaleUpMemberObservationExpectedUnavailable(
			mgr.cr.Status.LastStableReplicas,
			statefulSet,
			n,
		) {
			shcLogger.InfoContext(
				ctx,
				"SearchHeadCluster scale-up member is temporarily unavailable",
				"memberName",
				memberName,
				"lastStableReplicas",
				*mgr.cr.Status.LastStableReplicas,
				"targetReplicas",
				*statefulSet.Spec.Replicas,
				"error",
				err,
			)
		} else {
			shcLogger.ErrorContext(ctx, "unable to retrieve SearchHeadCluster member info", "memberName", memberName, "error", err)
		}

		if err == nil && !gotCaptainInfo {
			// try querying captain api; note that this should work on any node
			captainInfo, err := GetSearchHeadCaptainInfo(ctx, mgr, n)
			if err == nil {
				mgr.cr.Status.Captain = captainInfo.Label
				mgr.cr.Status.CaptainReady = captainInfo.ServiceReady
				mgr.cr.Status.Initialized = captainInfo.Initialized
				mgr.cr.Status.MinPeersJoined = captainInfo.MinPeersJoined
				mgr.cr.Status.MaintenanceMode = captainInfo.MaintenanceMode
				mgr.cr.Status.CaptainRollingRestart =
					captainInfo.RollingRestart
				gotCaptainInfo = true
				captainObservationOrdinal = n
				observeCaptainMembers = captainInfo.RollingRestart ||
					mgr.searchHeadInitialFormationPending() ||
					(previousCaptain != "" && previousCaptain != captainInfo.Label)

				if previousCaptain != "" && previousCaptain != captainInfo.Label {
					shcLogger.InfoContext(ctx, "captain election completed",
						"old_captain", previousCaptain,
						"new_captain", captainInfo.Label)
				}
			} else {
				mgr.cr.Status.CaptainReady = false
				shcLogger.ErrorContext(ctx, "captain election failed",
					"member", memberName,
					"error", err)
			}
		}

		if n < int32(len(mgr.cr.Status.Members)) {
			mgr.cr.Status.Members[n] = memberStatus
		} else {
			mgr.cr.Status.Members = append(mgr.cr.Status.Members, memberStatus)
		}
	}

	if observeCaptainMembers {
		captainMembers, err := GetSearchHeadCaptainMembersForStatus(
			ctx,
			mgr,
			captainObservationOrdinal,
		)
		if err != nil {
			shcLogger.ErrorContext(
				ctx,
				"unable to retrieve authoritative SearchHeadCluster member status",
				"captain", mgr.cr.Status.Captain,
				"error", err,
			)
		} else {
			observedCaptainMembers := 0
			for n := range mgr.cr.Status.Members {
				member := &mgr.cr.Status.Members[n]
				if captainMember, ok := captainMembers[member.Name]; ok {
					member.CaptainStatus = captainMember.Status
					member.AdvertiseRestartRequired =
						captainMember.AdvertiseRestartRequired
					observedCaptainMembers++
				}
			}
			mgr.cr.Status.CaptainMembersObserved =
				observedCaptainMembers == int(memberObservationCount)
		}
	}

	// truncate any extra members that we didn't check (leftover from scale down)
	if memberObservationCount < int32(len(mgr.cr.Status.Members)) {
		mgr.cr.Status.Members = mgr.cr.Status.Members[:memberObservationCount]
	}

	newMemberCount := int32(len(mgr.cr.Status.Members))
	if newMemberCount > previousMemberCount {
		shcLogger.InfoContext(ctx, "member joined SearchHeadCluster",
			"total_members", newMemberCount,
			"previous_members", previousMemberCount)
	} else if newMemberCount < previousMemberCount {
		shcLogger.WarnContext(ctx, "member left SearchHeadCluster",
			"total_members", newMemberCount,
			"previous_members", previousMemberCount)
	}

	return nil
}

func searchHeadClusterMemberObservationCount(
	statefulSet *appsv1.StatefulSet,
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) int32 {
	count := statefulSet.Status.Replicas
	if statefulSet.Spec.Replicas == nil ||
		*statefulSet.Spec.Replicas <= count ||
		operation == nil ||
		operation.Intent !=
			enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate ||
		operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		return count
	}

	// StatefulSet status.replicas is a count, not the highest surviving
	// ordinal. While a lower ordinal is being replaced it can temporarily
	// decrease even though higher ordinals still exist. Observe every desired
	// ordinal during the durable Pod-update workflow so those higher members
	// are not dropped from SHC status and misclassified as out-of-order.
	return *statefulSet.Spec.Replicas
}

func lifecycleMemberObservationExpectedUnavailable(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	ordinal int32,
) bool {
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != ordinal {
		return false
	}
	if operation.Intent ==
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown &&
		operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement &&
		operation.MembershipRemovalRequestedAt != nil {
		return true
	}

	switch operation.Stage {
	case enterpriseApi.SearchHeadClusterLifecycleStageWaitingForTermination,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForPodInfrastructure,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForStorage,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery:
		return true
	default:
		return false
	}
}

func scaleUpMemberObservationExpectedUnavailable(
	lastStableReplicas *int32,
	statefulSet *appsv1.StatefulSet,
	ordinal int32,
) bool {
	return lastStableReplicas != nil &&
		statefulSet.Spec.Replicas != nil &&
		*statefulSet.Spec.Replicas > *lastStableReplicas &&
		statefulSet.Status.Replicas > *lastStableReplicas &&
		statefulSet.Status.ReadyReplicas < statefulSet.Status.Replicas &&
		ordinal >= *lastStableReplicas
}
