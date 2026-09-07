// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
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
	"os"
	"slices"
	"strconv"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	noahclient "github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/noah"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	indexerworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/indexercluster"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	noahAuthVolumeName                         = "mnt-noah-auth"
	noahAuthMountPath                          = "/mnt/noah-auth"
	noahAuthRevisionAnnotation                 = "enterprise.splunk.com/noah-auth-secret-revision"
	pendingScaleDownOrdinalAnnotation          = "enterprise.splunk.com/pending-scale-down-ordinal"
	noahIndexerPollInterval                    = 5 * time.Second
	defaultNoahCacheWarmScaleOutTimeoutSeconds = int32(3600)
)

// ApplyNoahIndexerCluster reconciles the Kubernetes resources required to
// start and roll out a Noah-selected IndexerCluster and verifies its expected
// Noah peers. Scale-down is development-only and does not decommission peers.
func ApplyNoahIndexerCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (result reconcile.Result, err error) {
	result = reconcile.Result{RequeueAfter: noahIndexerPollInterval}
	previousPhase := cr.Status.Phase
	previousReplicas := cr.Status.Replicas

	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "IndexerCluster"

	isPaused := cr.GetAnnotations()[enterpriseApi.IndexerClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string, additionalConditions ...metav1.Condition) {
		setNoahIndexerPhaseAndConditions(cr, isPaused, phase, message, additionalConditions...)
	}
	setOutcome := func(outcome noahIndexerOutcome) {
		result.RequeueAfter = outcome.requeueAfter
		setPhaseAndConditions(outcome.phase, outcome.phaseMessage, outcome.condition)
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")
	defer updateCRStatus(ctx, client, cr, &err)

	if cr.Spec.NoahClusterRef == nil || cr.Spec.NoahClusterRef.Name == "" {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Noah Cluster reference is required")
		return reconcile.Result{}, splcommon.NewTerminalError(
			EventReasonValidateSpecFailed,
			"Noah IndexerCluster spec validation failed",
			fmt.Errorf("noahClusterRef.name must not be empty"),
		)
	}

	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}
	if err = validateCommonSplunkSpec(ctx, client, &cr.Spec.CommonSplunkSpec, cr); err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Indexer Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(
			EventReasonValidateSpecFailed,
			"Noah IndexerCluster spec validation failed",
			err,
		)
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-indexer", cr.GetName())

	if cr.GetDeletionTimestamp() != nil {
		if cleanupErr := DeleteOwnerReferencesForResources(ctx, client, cr, SplunkIndexer); cleanupErr != nil {
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Failed to clean up owned resources")
			return result, cleanupErr
		}
		terminating, deletionErr := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && deletionErr != nil {
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource deletion is in progress")
		} else {
			result.RequeueAfter = 0
		}
		return result, deletionErr
	}

	podManager := newNoahIndexerPodManager(client, cr)
	statefulSet, phase, applyErr := applyNoahIndexerResources(ctx, client, cr, podManager)
	if applyErr != nil {
		if outcome, outcomeErr, handled := noahIndexerOutcomeFromError(applyErr); handled {
			setOutcome(outcome)
			return result, outcomeErr
		}
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply Noah IndexerCluster resources")
		return result, applyErr
	}

	appliedReplicas, replicaErr := noahIndexerStatefulSetReplicas(statefulSet)
	if replicaErr != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Unable to determine applied Noah IndexerCluster replicas")
		return result, replicaErr
	}
	cr.Status.Replicas = appliedReplicas
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	var outcome noahIndexerOutcome
	switch phase {
	case enterpriseApi.PhaseReady:
		outcome, err = podManager.observeReady(ctx, appliedReplicas, previousPhase, previousReplicas)
		if err != nil {
			mappedOutcome, mappedErr, handled := noahIndexerOutcomeFromError(err)
			if handled {
				outcome = mappedOutcome
				err = mappedErr
			} else {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to reconcile Noah IndexerCluster lifecycle")
				return result, err
			}
		}
	default:
		outcome = waitForNoahIndexerWorkload(phase, previousPhase, appliedReplicas)
	}

	setOutcome(outcome)
	return result, err
}

func applyNoahIndexerResources(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, podManager *noahIndexerPodManager) (*appsv1.StatefulSet, enterpriseApi.Phase, error) {
	runtime, err := noah.ResolveConnection(ctx, client, cr.GetNamespace(), *cr.Spec.NoahClusterRef)
	if err != nil {
		return nil, enterpriseApi.PhaseError, err
	}
	podManager.setRuntime(runtime)

	if cr.Spec.LicenseManagerRef.Name != "" {
		statefulSetKey := types.NamespacedName{
			Name:      GetSplunkStatefulsetName(SplunkIndexer, cr.Name),
			Namespace: cr.Namespace,
		}
		currentStatefulSet := &appsv1.StatefulSet{}
		if err := client.Get(ctx, statefulSetKey, currentStatefulSet); err == nil {
			continueReconcile, err := validateNoahIndexerUpgradePath(ctx, client, cr)
			if err != nil {
				return currentStatefulSet, enterpriseApi.PhaseError, err
			}
			if !continueReconcile {
				return currentStatefulSet, enterpriseApi.PhasePending, nil
			}
		} else if !k8serrors.IsNotFound(err) {
			return nil, enterpriseApi.PhaseError, fmt.Errorf("get Noah indexer StatefulSet %s: %w", statefulSetKey, err)
		}
	}

	if _, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer); err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("apply Splunk config: %w", err)
	}

	services := []struct {
		headless bool
		name     string
	}{
		{headless: true, name: "headless"},
		{headless: false, name: "regular"},
	}
	for _, service := range services {
		if err := k8sops.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, service.headless)); err != nil {
			return nil, enterpriseApi.PhaseError, fmt.Errorf("apply %s Service: %w", service.name, err)
		}
	}

	noahSpec := runtime.Spec()
	noahConf := splunkconfig.NoahIndexerConf(noahSpec.Endpoint, noahSpec.Tenant)
	defaultsConfigMap, defaultsSecret, err := ensureIndexerDefaults(ctx, client, cr, noahConf...)
	if err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("ensure indexer defaults: %w", err)
	}

	statefulSet, err := getNoahIndexerStatefulSet(ctx, client, cr,
		defaultsConfigMap.AsStatefulSetOption(),
		defaultsSecret.AsStatefulSetOption(),
		noahAuthSecretOption(runtime.AuthSecretName(), runtime.AuthSecretResourceVersion()),
	)
	if err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("build Noah indexer StatefulSet: %w", err)
	}

	phase, err := podManager.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	if err != nil {
		return statefulSet, enterpriseApi.PhaseError, fmt.Errorf("apply Noah indexer StatefulSet: %w", err)
	}

	configworkflow.GarbageCollectConfigMaps(ctx, client, cr, defaultsConfigMap.Name, statefulSet.Spec.Selector)
	configworkflow.GarbageCollectSecrets(ctx, client, cr, defaultsSecret.Name, statefulSet.Spec.Selector)
	return statefulSet, phase, nil
}

func noahIndexerStatefulSetReplicas(statefulSet *appsv1.StatefulSet) (int32, error) {
	if statefulSet == nil {
		return 0, fmt.Errorf("Noah indexer StatefulSet is nil")
	}
	if statefulSet.Spec.Replicas == nil {
		return 0, fmt.Errorf("Noah indexer StatefulSet %s/%s has no replica count", statefulSet.Namespace, statefulSet.Name)
	}
	return *statefulSet.Spec.Replicas, nil
}

// noahIndexerStatefulSetConverged reports whether the StatefulSet controller
// has observed the latest template and every desired pod is running that
// revision. It deliberately does not initiate a rollout.
func noahIndexerStatefulSetConverged(statefulSet *appsv1.StatefulSet, desiredReplicas int32) bool {
	return statefulSet != nil &&
		statefulSet.Status.ObservedGeneration >= statefulSet.Generation &&
		statefulSet.Status.UpdateRevision != "" &&
		statefulSet.Status.CurrentRevision == statefulSet.Status.UpdateRevision &&
		statefulSet.Status.UpdatedReplicas == desiredReplicas &&
		statefulSet.Status.ReadyReplicas == desiredReplicas
}

// noahIndexerWorkloadPhase distinguishes a pending pod-template revision from
// ordinary replica readiness. Both block peer observation, but only revision
// drift is an update; a same-revision replica wait is classified by the caller
// as pending or scaling up.
func noahIndexerWorkloadPhase(phase enterpriseApi.Phase, statefulSet *appsv1.StatefulSet, desiredReplicas int32) enterpriseApi.Phase {
	if phase != enterpriseApi.PhaseReady || noahIndexerStatefulSetConverged(statefulSet, desiredReplicas) {
		return phase
	}
	if statefulSet != nil &&
		statefulSet.Status.UpdateRevision != "" &&
		statefulSet.Status.CurrentRevision != statefulSet.Status.UpdateRevision {
		return enterpriseApi.PhaseUpdating
	}
	return enterpriseApi.PhasePending
}

func newNoahPeersReadyCondition(status metav1.ConditionStatus, reason enterpriseApi.ConditionReason, message string) metav1.Condition {
	return metav1.Condition{
		Type:    string(enterpriseApi.ConditionNoahPeersReady),
		Status:  status,
		Reason:  string(reason),
		Message: message,
	}
}

func setNoahIndexerPhaseAndConditions(cr *enterpriseApi.IndexerCluster, isPaused bool, phase enterpriseApi.Phase, message string, conditions ...metav1.Condition) {
	status := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
		Phase:      phase,
		IsPaused:   isPaused,
		Message:    message,
		Generation: cr.GetGeneration(),
	})
	for _, condition := range conditions {
		condition.ObservedGeneration = cr.GetGeneration()
		status.Conditions = splcommon.UpsertCondition(status.Conditions, condition)
	}
	cr.Status.Phase = status.Phase
	cr.Status.Conditions = status.Conditions
	cr.Status.ObservedGeneration = cr.GetGeneration()
}

type noahIndexerPodManager struct {
	client           splcommon.ControllerClient
	cr               *enterpriseApi.IndexerCluster
	statefulSet      *appsv1.StatefulSet
	runtime          *noah.Connection
	cacheWarmEnabled bool
	cacheWarmTimeout time.Duration
}

var _ splcommon.StatefulSetPodManager = (*noahIndexerPodManager)(nil)
var _ splcommon.StatefulSetScaleOutPlanner = (*noahIndexerPodManager)(nil)
var _ splcommon.StatefulSetScaleDownFinisher = (*noahIndexerPodManager)(nil)
var _ splcommon.StatefulSetScaleDownPVCPolicy = (*noahIndexerPodManager)(nil)

func newNoahIndexerPodManager(client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) *noahIndexerPodManager {
	return &noahIndexerPodManager{client: client, cr: cr}
}

// Update applies the desired StatefulSet and delegates existing-workload
// scaling and rollout to the shared pod lifecycle engine. Initial creation
// retains the full requested replica count.
func (mgr *noahIndexerPodManager) Update(ctx context.Context, client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	if mgr.client == nil {
		mgr.client = client
	}
	mgr.statefulSet = statefulSet

	phase, err := k8sops.ApplyStatefulSet(ctx, mgr.client, statefulSet)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}

	appliedReplicas, err := noahIndexerStatefulSetReplicas(statefulSet)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}
	workloadPhase := noahIndexerWorkloadPhase(phase, statefulSet, appliedReplicas)
	if phase != enterpriseApi.PhaseReady {
		return workloadPhase, nil
	}

	// Wait until the StatefulSet controller publishes the revision to compare
	// against. Once it does, the shared lifecycle engine owns the established
	// scale-out-before-rollout ordering and inspects individual Pod revisions.
	if statefulSet.Status.ObservedGeneration < statefulSet.Generation || statefulSet.Status.UpdateRevision == "" {
		if err := k8sops.CheckPodsForTerminalFailures(ctx, mgr.client, statefulSet); err != nil {
			return enterpriseApi.PhaseError, err
		}
		return workloadPhase, nil
	}
	_, scaleDownPending := statefulSet.Annotations[pendingScaleDownOrdinalAnnotation]
	if appliedReplicas == desiredReplicas &&
		noahIndexerStatefulSetConverged(statefulSet, appliedReplicas) &&
		mgr.cr.Status.Phase != enterpriseApi.PhaseUpdating &&
		!scaleDownPending {
		return enterpriseApi.PhaseReady, nil
	}
	return k8sops.UpdateStatefulSetPods(ctx, mgr.client, statefulSet, mgr, desiredReplicas)
}

func (mgr *noahIndexerPodManager) NextReplicas(ctx context.Context, appliedReplicas, requestedReplicas int32) (splcommon.ScaleOutPlan, error) {
	plan := splcommon.ScaleOutPlan{TargetReplicas: appliedReplicas}

	observation, err := mgr.observePeers(ctx, appliedReplicas)
	if err != nil {
		return plan, &noahIndexerPeerObservationError{err: err}
	}

	if observation.TimedOutPeerID != "" {
		return plan, &noahIndexerCacheWarmTimeoutError{peerID: observation.TimedOutPeerID}
	}

	plan.Complete = observation.AllReady && appliedReplicas == requestedReplicas

	canAdvance := observation.AllReady
	if !mgr.cacheWarmEnabled {
		canAdvance = observation.AllRegistered
	}

	if canAdvance && appliedReplicas < requestedReplicas {
		plan.TargetReplicas++
	}

	return plan, nil
}

func (mgr *noahIndexerPodManager) PrepareScaleDown(ctx context.Context, ordinal int32) (bool, error) {
	appliedReplicas, err := noahIndexerStatefulSetReplicas(mgr.statefulSet)
	if err != nil {
		return false, err
	}

	observation, err := mgr.observePeers(ctx, appliedReplicas)
	if err != nil {
		return false, &noahIndexerPeerObservationError{err: err, phase: enterpriseApi.PhaseScalingDown}
	}
	if !observation.AllReady {
		return false, nil
	}

	if mgr.statefulSet.Annotations == nil {
		mgr.statefulSet.Annotations = map[string]string{}
	}
	mgr.statefulSet.Annotations[pendingScaleDownOrdinalAnnotation] = strconv.FormatInt(int64(ordinal), 10)

	return true, nil
}

// FinishScaleDown unregisters the removed peer and waits for Noah's latest
// bucket map to exclude it before another lifecycle operation can begin.
func (mgr *noahIndexerPodManager) FinishScaleDown(ctx context.Context, _ int32) (bool, error) {
	value, pending := mgr.statefulSet.Annotations[pendingScaleDownOrdinalAnnotation]
	if !pending {
		return true, nil
	}

	parsedOrdinal, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsedOrdinal < 0 {
		return false, fmt.Errorf("invalid pending scale-down ordinal %q", value)
	}
	ordinal := int32(parsedOrdinal)

	podName := noahIndexerPodName(mgr.statefulSet, ordinal)
	pod := &corev1.Pod{}
	err = mgr.client.Get(ctx, types.NamespacedName{Namespace: mgr.statefulSet.Namespace, Name: podName}, pod)
	if err == nil {
		return false, nil
	}
	if !k8serrors.IsNotFound(err) {
		return false, &noahIndexerPeerObservationError{
			err:   fmt.Errorf("get removed Noah indexer Pod %s: %w", podName, err),
			phase: enterpriseApi.PhaseScalingDown,
		}
	}

	peerID, err := noahIndexerPeerID(mgr.statefulSet, ordinal)
	if err != nil {
		return false, &noahIndexerPeerObservationError{err: err, phase: enterpriseApi.PhaseScalingDown}
	}

	runtime, err := mgr.noahRuntime(ctx)
	if err != nil {
		return false, &noahIndexerPeerObservationError{err: err, phase: enterpriseApi.PhaseScalingDown}
	}

	noahClient, err := runtime.Client()
	if err != nil {
		return false, &noahIndexerPeerObservationError{err: err, phase: enterpriseApi.PhaseScalingDown}
	}

	if err := noahClient.UnregisterPeer(ctx, peerID); err != nil {
		var noahErr *noahclient.Error
		if !errors.As(err, &noahErr) || noahErr.Kind != noahclient.ErrorKindNotFound {
			return false, &noahIndexerPeerObservationError{err: fmt.Errorf("unregister Noah peer %s: %w", peerID, err), phase: enterpriseApi.PhaseScalingDown}
		}
	}

	bucketMap, err := noahClient.GetLatestBucketMap(ctx)
	if err != nil {
		return false, &noahIndexerPeerObservationError{err: fmt.Errorf("get latest Noah bucket map: %w", err), phase: enterpriseApi.PhaseScalingDown}
	}

	remainingPeerIDs, err := noahIndexerPeerIDs(mgr.statefulSet, ordinal)
	if err != nil {
		return false, err
	}

	if !indexerworkflow.NoahBucketMapConfirmsScaleDown(bucketMap, remainingPeerIDs, peerID) {
		return false, nil
	}

	delete(mgr.statefulSet.Annotations, pendingScaleDownOrdinalAnnotation)
	if err := splutil.UpdateResource(ctx, mgr.client, mgr.statefulSet); err != nil {
		return false, fmt.Errorf("clear pending scale-down ordinal: %w", err)
	}
	return true, nil
}

func (*noahIndexerPodManager) RetainPVCsOnScaleDown() bool {
	return true
}

func (mgr *noahIndexerPodManager) PrepareRecycle(ctx context.Context, _ int32) (bool, error) {
	appliedReplicas, err := noahIndexerStatefulSetReplicas(mgr.statefulSet)
	if err != nil {
		return false, err
	}

	observation, err := mgr.observePeers(ctx, appliedReplicas)
	if err != nil {
		return false, &noahIndexerPeerObservationError{err: err}
	}

	return observation.AllReady, nil
}

func (mgr *noahIndexerPodManager) FinishRecycle(ctx context.Context, _ int32) (bool, error) {
	appliedReplicas, err := noahIndexerStatefulSetReplicas(mgr.statefulSet)
	if err != nil {
		return false, err
	}

	observation, err := mgr.observePeers(ctx, appliedReplicas)
	if err != nil {
		return false, &noahIndexerPeerObservationError{err: err}
	}

	return observation.AllReady, nil
}

func (mgr *noahIndexerPodManager) FinishUpgrade(context.Context, int32) error {
	return nil
}

func (mgr *noahIndexerPodManager) observeReady(ctx context.Context, appliedReplicas int32, previousPhase enterpriseApi.Phase, previousReplicas int32) (noahIndexerOutcome, error) {
	plan, err := mgr.NextReplicas(ctx, appliedReplicas, mgr.cr.Spec.Replicas)
	if err != nil {
		return noahIndexerOutcome{}, err
	}
	if plan.TargetReplicas > appliedReplicas {
		return noahIndexerOutcome{}, fmt.Errorf("Noah indexer pod manager reported Ready before applying replica target %d", plan.TargetReplicas)
	}
	if !plan.Complete {
		phase := enterpriseApi.PhasePending
		phaseMessage := "Waiting for expected Noah peers"
		if previousPhase == enterpriseApi.PhaseScalingUp || previousReplicas < appliedReplicas || appliedReplicas < mgr.cr.Spec.Replicas {
			phase = enterpriseApi.PhaseScalingUp
			phaseMessage = fmt.Sprintf("Waiting for %d applied replicas to become ready before continuing scale-out", appliedReplicas)
		}
		return noahIndexerOutcome{
			phase:        phase,
			phaseMessage: phaseMessage,
			condition: newNoahPeersReadyCondition(
				metav1.ConditionFalse,
				enterpriseApi.ReasonNoahPeersNotReady,
				fmt.Sprintf("Not all %d applied Noah peers are up", appliedReplicas),
			),
			requeueAfter: noahIndexerPollInterval,
		}, nil
	}

	return noahIndexerOutcome{
		phase: enterpriseApi.PhaseReady,
		condition: newNoahPeersReadyCondition(
			metav1.ConditionTrue,
			enterpriseApi.ReasonNoahPeersReady,
			"All expected Noah peers are up",
		),
	}, nil
}

func (mgr *noahIndexerPodManager) observePeers(ctx context.Context, replicas int32) (indexerworkflow.NoahMembership, error) {
	runtime, err := mgr.noahRuntime(ctx)
	if err != nil {
		return indexerworkflow.NoahMembership{}, err
	}

	expectedPeers, err := currentNoahIndexerPeerIncarnations(ctx, mgr.client, replicas, mgr.statefulSet)
	if err != nil {
		return indexerworkflow.NoahMembership{}, err
	}
	if len(expectedPeers) != int(replicas) {
		return indexerworkflow.NoahMembership{}, nil
	}

	noahClient, err := runtime.Client()
	if err != nil {
		return indexerworkflow.NoahMembership{}, err
	}

	peers, err := noahClient.ListPeers(ctx)
	if err != nil {
		return indexerworkflow.NoahMembership{}, fmt.Errorf("list Noah peers: %w", err)
	}

	observation := indexerworkflow.EvaluateNoahMembership(expectedPeers, peers, indexerworkflow.NoahCacheWarmPolicy{
		Required: mgr.cacheWarmEnabled,
		Timeout:  mgr.cacheWarmTimeout,
	}, time.Now())

	return observation, nil
}

func (mgr *noahIndexerPodManager) noahRuntime(ctx context.Context) (*noah.Connection, error) {
	if mgr.runtime != nil {
		return mgr.runtime, nil
	}
	runtime, err := noah.ResolveConnection(ctx, mgr.client, mgr.cr.GetNamespace(), *mgr.cr.Spec.NoahClusterRef)
	if err != nil {
		return nil, err
	}
	mgr.setRuntime(runtime)
	return runtime, nil
}

func (mgr *noahIndexerPodManager) setRuntime(runtime *noah.Connection) {
	mgr.runtime = runtime
	spec := runtime.Spec()
	mgr.cacheWarmEnabled = noahCacheWarmScaleOutEnabled(spec)
	mgr.cacheWarmTimeout = noahCacheWarmScaleOutTimeout(spec)
}

// validateNoahIndexerUpgradePath preserves the common LicenseManager upgrade
// ordering without invoking classic IndexerCluster ClusterManager and multisite
// checks, which do not apply to a Noah-backed IndexerCluster.
func validateNoahIndexerUpgradePath(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (bool, error) {
	ref := cr.Spec.LicenseManagerRef
	if ref.Name == "" {
		return true, nil
	}

	namespace := ref.Namespace
	if namespace == "" {
		namespace = cr.Namespace
	}
	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	licenseManager := &enterpriseApi.LicenseManager{}
	if err := client.Get(ctx, key, licenseManager); err != nil {
		if k8serrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}

	image, err := getCurrentImage(ctx, client, licenseManager, SplunkLicenseManager)
	if err != nil {
		return false, fmt.Errorf("get LicenseManager %s image: %w", key, err)
	}
	if image != cr.Spec.Image {
		return false, fmt.Errorf("license manager current image (%s) is different than CR image (%s)", image, cr.Spec.Image)
	}
	return licenseManager.Status.Phase == enterpriseApi.PhaseReady, nil
}

type noahIndexerOutcome struct {
	phase        enterpriseApi.Phase
	phaseMessage string
	condition    metav1.Condition
	requeueAfter time.Duration
}

func waitForNoahIndexerWorkload(phase, previousPhase enterpriseApi.Phase, appliedReplicas int32) noahIndexerOutcome {
	phaseMessage := ""
	switch phase {
	case enterpriseApi.PhaseScalingDown:
		phaseMessage = "Scaling down without Noah decommission; development use only"
	case enterpriseApi.PhaseUpdating:
		phaseMessage = "Waiting for the StatefulSet pod-template revision to be applied"
	case enterpriseApi.PhaseScalingUp:
		phaseMessage = fmt.Sprintf("Waiting for %d applied replicas to become ready before continuing scale-out", appliedReplicas)
	case enterpriseApi.PhasePending:
		switch previousPhase {
		case enterpriseApi.PhaseScalingDown:
			phase = enterpriseApi.PhaseScalingDown
			phaseMessage = "Scaling down without Noah decommission; development use only"
		case enterpriseApi.PhaseUpdating:
			phase = enterpriseApi.PhaseUpdating
			phaseMessage = "Waiting for the StatefulSet pod-template revision to be applied"
		case enterpriseApi.PhaseScalingUp:
			phase = enterpriseApi.PhaseScalingUp
			phaseMessage = fmt.Sprintf("Waiting for %d applied replicas to become ready before continuing scale-out", appliedReplicas)
		}
	}

	return noahIndexerOutcome{
		phase:        phase,
		phaseMessage: phaseMessage,
		condition: newNoahPeersReadyCondition(
			metav1.ConditionFalse,
			enterpriseApi.ReasonNoahPeersNotReady,
			"Waiting for the indexer workload before observing Noah peers",
		),
		requeueAfter: noahIndexerPollInterval,
	}
}

func noahIndexerOutcomeFromError(err error) (noahIndexerOutcome, error, bool) {
	if dependencyOutcome, handled := noahDependencyOutcome(err); handled {
		requeueAfter := time.Duration(0)
		if dependencyOutcome.phase == enterpriseApi.PhasePending {
			requeueAfter = noahIndexerPollInterval
		}
		return noahIndexerOutcome{
			phase:        dependencyOutcome.phase,
			phaseMessage: dependencyOutcome.message,
			condition: newNoahPeersReadyCondition(
				dependencyOutcome.conditionStatus,
				dependencyOutcome.conditionReason,
				dependencyOutcome.message,
			),
			requeueAfter: requeueAfter,
		}, dependencyOutcome.err, true
	}

	var cacheWarmTimeoutErr *noahIndexerCacheWarmTimeoutError
	if errors.As(err, &cacheWarmTimeoutErr) {
		message := cacheWarmTimeoutErr.Error()
		return noahIndexerOutcome{
			phase:        enterpriseApi.PhaseError,
			phaseMessage: message,
			condition: newNoahPeersReadyCondition(
				metav1.ConditionFalse,
				enterpriseApi.ReasonNoahCacheWarmTimeout,
				message,
			),
		}, splcommon.NewTerminalError(EventReasonNoahCacheWarmTimeout, message, cacheWarmTimeoutErr), true
	}

	var observationErr *noahIndexerPeerObservationError
	if errors.As(err, &observationErr) {
		phase := observationErr.phase
		if phase == "" {
			phase = enterpriseApi.PhasePending
		}
		return noahIndexerOutcome{
			phase:        phase,
			phaseMessage: "Unable to observe Noah peers",
			condition: newNoahPeersReadyCondition(
				metav1.ConditionUnknown,
				enterpriseApi.ReasonNoahPeerObservationFailed,
				fmt.Sprintf("Unable to observe Noah peers: %v", observationErr),
			),
			requeueAfter: noahIndexerPollInterval,
		}, nil, true
	}

	return noahIndexerOutcome{}, err, false
}

type noahIndexerCacheWarmTimeoutError struct {
	peerID string
}

func (err *noahIndexerCacheWarmTimeoutError) Error() string {
	return fmt.Sprintf("Cache warming timed out for Noah peer %s", err.peerID)
}

type noahIndexerPeerObservationError struct {
	err   error
	phase enterpriseApi.Phase
}

func (err *noahIndexerPeerObservationError) Error() string {
	return err.err.Error()
}

func (err *noahIndexerPeerObservationError) Unwrap() error {
	return err.err
}

func noahCacheWarmScaleOutEnabled(spec enterpriseApi.NoahClusterSpec) bool {
	return spec.CacheWarmScaleOutEnabled == nil || *spec.CacheWarmScaleOutEnabled
}

func noahCacheWarmScaleOutTimeout(spec enterpriseApi.NoahClusterSpec) time.Duration {
	timeoutSeconds := defaultNoahCacheWarmScaleOutTimeoutSeconds
	if spec.CacheWarmScaleOutTimeoutSeconds != nil {
		timeoutSeconds = *spec.CacheWarmScaleOutTimeoutSeconds
	}
	return time.Duration(timeoutSeconds) * time.Second
}

func currentNoahIndexerPeerIncarnations(ctx context.Context, client splcommon.ControllerClient, replicas int32, statefulSet *appsv1.StatefulSet) ([]indexerworkflow.ExpectedNoahPeer, error) {
	expectedPeers := make([]indexerworkflow.ExpectedNoahPeer, 0, replicas)
	for ordinal := range replicas {
		podName := noahIndexerPodName(statefulSet, ordinal)
		pod := &corev1.Pod{}
		key := types.NamespacedName{Namespace: statefulSet.Namespace, Name: podName}
		if err := client.Get(ctx, key, pod); err != nil {
			return nil, fmt.Errorf("get expected Noah indexer Pod %s: %w", key, err)
		}

		statusIndex := slices.IndexFunc(pod.Status.ContainerStatuses, func(status corev1.ContainerStatus) bool {
			return status.Name == "splunk"
		})
		if statusIndex < 0 {
			continue
		}
		status := pod.Status.ContainerStatuses[statusIndex]
		if !status.Ready || status.State.Running == nil || status.State.Running.StartedAt.IsZero() {
			continue
		}

		peerID, err := noahIndexerPeerID(statefulSet, ordinal)
		if err != nil {
			return nil, err
		}
		expectedPeers = append(expectedPeers, indexerworkflow.ExpectedNoahPeer{
			ID:        peerID,
			StartedAt: time.Unix(status.State.Running.StartedAt.Unix(), 0),
		})
	}
	return expectedPeers, nil
}

func noahIndexerPeerID(statefulSet *appsv1.StatefulSet, ordinal int32) (string, error) {
	clusterDomain, err := noahIndexerClusterDomain(statefulSet)
	if err != nil {
		return "", err
	}
	podName := noahIndexerPodName(statefulSet, ordinal)
	return fmt.Sprintf("%s.%s.%s.svc.%s", podName, statefulSet.Spec.ServiceName, statefulSet.Namespace, clusterDomain), nil
}

func noahIndexerPeerIDs(statefulSet *appsv1.StatefulSet, replicas int32) ([]string, error) {
	peerIDs := make([]string, 0, replicas)
	for ordinal := range replicas {
		peerID, err := noahIndexerPeerID(statefulSet, ordinal)
		if err != nil {
			return nil, err
		}
		peerIDs = append(peerIDs, peerID)
	}
	return peerIDs, nil
}

func noahIndexerPodName(statefulSet *appsv1.StatefulSet, ordinal int32) string {
	return fmt.Sprintf("%s-%d", statefulSet.Name, ordinal)
}

func noahIndexerClusterDomain(statefulSet *appsv1.StatefulSet) (string, error) {
	if statefulSet == nil {
		return "", fmt.Errorf("Noah indexer StatefulSet is nil")
	}
	containerIndex := slices.IndexFunc(statefulSet.Spec.Template.Spec.Containers, func(container corev1.Container) bool {
		return container.Name == "splunk"
	})
	if containerIndex < 0 {
		return "", fmt.Errorf("Noah indexer StatefulSet %s/%s is missing the splunk container", statefulSet.Namespace, statefulSet.Name)
	}
	container := statefulSet.Spec.Template.Spec.Containers[containerIndex]
	envIndex := slices.IndexFunc(container.Env, func(env corev1.EnvVar) bool {
		return env.Name == resources.ClusterDomainEnvName
	})
	if envIndex < 0 || container.Env[envIndex].Value == "" || container.Env[envIndex].ValueFrom != nil {
		return "", fmt.Errorf("Noah indexer StatefulSet %s/%s has no literal %s value", statefulSet.Namespace, statefulSet.Name, resources.ClusterDomainEnvName)
	}
	return container.Env[envIndex].Value, nil
}

// getNoahIndexerStatefulSet constructs an indexer StatefulSet with the startup
// staging and stable pod identity required by a Noah-aware Splunk image.
func getNoahIndexerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {
	bootstrapOptions := make([]resources.StatefulSetOption, 0, len(opts)+1)
	bootstrapOptions = append(bootstrapOptions, noahInitEtcOption(&cr.Spec.CommonSplunkSpec))
	bootstrapOptions = append(bootstrapOptions, opts...)
	return getIndexerStatefulSet(ctx, client, cr, noahIndexerStatefulSetOptions(os.Getenv(resources.ClusterDomainEnvName), bootstrapOptions...)...)
}

func noahIndexerStatefulSetOptions(clusterDomain string, opts ...resources.StatefulSetOption) []resources.StatefulSetOption {
	result := make([]resources.StatefulSetOption, 0, len(opts)+1)
	result = append(result, opts...)
	return append(result, resources.WithNoahPodIdentity(clusterDomain))
}

// noahAuthSecretOption exposes the plaintext credential only to init-etc. The
// main Splunk container receives it through the staged server.conf on its etc
// volume, not through an environment variable or Secret mount.
func noahAuthSecretOption(secretName, secretResourceVersion string) resources.StatefulSetOption {
	return func(statefulSet *appsv1.StatefulSet) {
		mode := int32(0444)
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: noahAuthVolumeName,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName:  secretName,
				Items:       []corev1.KeyToPath{{Key: noah.AuthSecretKey, Path: noah.AuthSecretKey}},
				DefaultMode: &mode,
			}},
		})
		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = make(map[string]string)
		}
		statefulSet.Spec.Template.Annotations[noahAuthRevisionAnnotation] = secretResourceVersion
		for i := range statefulSet.Spec.Template.Spec.InitContainers {
			initContainer := &statefulSet.Spec.Template.Spec.InitContainers[i]
			if initContainer.Name != "init-etc" {
				continue
			}
			initContainer.VolumeMounts = append(initContainer.VolumeMounts, corev1.VolumeMount{
				Name:      noahAuthVolumeName,
				MountPath: noahAuthMountPath,
				ReadOnly:  true,
			})
		}
	}
}

// noahInitEtcOption stages the minimum safe Noah configuration required by
// splunk-provision before it applies SPLUNK_DEFAULTS_URL. Noah remains disabled
// during the provisioner's temporary startup and is enabled from the generated
// defaults before the first full splunkd start.
func noahInitEtcOption(spec *enterpriseApi.CommonSplunkSpec) resources.StatefulSetOption {
	// TODO: Remove this operator-owned etc mutation once splunk-provision can
	// stage its Noah pre-start configuration from the resolved defaults. Writing
	// application configuration into the image-owned etc tree belongs in the
	// provisioner, not the operator's StatefulSet construction.
	return func(statefulSet *appsv1.StatefulSet) {
		etcVolumeName := "pvc-etc"
		if len(statefulSet.Spec.Template.Spec.Containers) > 0 {
			for _, mount := range statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.MountPath == "/opt/splunk/etc" {
					etcVolumeName = mount.Name
					break
				}
			}
		}

		initRunAsUser := int64(41812)
		initRunAsNonRoot := true
		initAllowPrivilegeEscalation := false
		statefulSet.Spec.Template.Spec.InitContainers = append(statefulSet.Spec.Template.Spec.InitContainers, corev1.Container{
			Name:            "init-etc",
			Image:           spec.Image,
			ImagePullPolicy: corev1.PullPolicy(spec.ImagePullPolicy),
			Command: []string{
				"sh", "-c",
				`set -eu
if [ ! -f /mnt/splunk-etc/log.cfg ]; then
  cp --remove-destination -R /opt/splunk/etc/. /mnt/splunk-etc/
  printf '[default]\nSPLUNK_HOME=/opt/splunk\nSPLUNK_DB=/opt/splunk/var/lib/splunk\nPYTHONUTF8=1\n' \
    > /mnt/splunk-etc/splunk-launch.conf
fi
server_conf=/mnt/splunk-etc/system/local/server.conf
mkdir -p "$(dirname "$server_conf")"
touch "$server_conf"
awk -v key_file=/mnt/noah-auth/pass4SymmKey '
  function print_key( key) {
    if ((getline key < key_file) <= 0) exit 42
    close(key_file)
    printf "pass4SymmKey = %s\n", key
  }
  /^\[noahService\][[:space:]]*$/ {
    in_noah=1
    saw_noah=1
    print
    print_key()
    print "disabled = true"
    next
  }
  in_noah && /^[[:space:]]*pass4SymmKey[[:space:]]*=/ { next }
  in_noah && /^[[:space:]]*disabled[[:space:]]*=/ { next }
  /^\[/ { in_noah=0 }
  { print }
  END {
    if (!saw_noah) {
      print ""
      print "[noahService]"
      print "disabled = true"
      print_key()
    }
  }
' "$server_conf" > /tmp/server_clean.conf
mv /tmp/server_clean.conf "$server_conf"`,
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:                &initRunAsUser,
				RunAsNonRoot:             &initRunAsNonRoot,
				AllowPrivilegeEscalation: &initAllowPrivilegeEscalation,
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			VolumeMounts: []corev1.VolumeMount{{
				Name:      etcVolumeName,
				MountPath: "/mnt/splunk-etc",
			}},
		})
	}
}
