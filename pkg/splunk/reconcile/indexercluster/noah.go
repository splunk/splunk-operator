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

package indexercluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	indexerworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/indexercluster"
)

const (
	noahIndexerPollInterval                    = 5 * time.Second
	defaultNoahCacheWarmScaleOutTimeoutSeconds = int32(3600)
)

// applyNoahIndexerCluster reconciles the Kubernetes resources required to
// start and roll out a Noah-selected IndexerCluster and verifies its expected
// Noah peers.
func applyNoahIndexerCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (result reconcile.Result, err error) {
	result = reconcile.Result{RequeueAfter: noahIndexerPollInterval}
	previousPhase := cr.Status.Phase
	previousReplicas := cr.Status.Replicas

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
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
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-indexer", cr.GetName())

	if cr.GetDeletionTimestamp() != nil {
		setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource deletion is in progress")

		if cleanupErr := rclient.IgnoreNotFound(k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkIndexer)); cleanupErr != nil {
			return result, cleanupErr
		}

		statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
			Name:      splutil.GetSplunkStatefulsetName(splcommon.SplunkIndexer, cr.Name),
			Namespace: cr.Namespace,
		}}
		if deletionErr := rclient.IgnoreNotFound(client.Delete(ctx, statefulSet)); deletionErr != nil {
			return result, deletionErr
		}

		_, deletionErr := k8sops.CheckForDeletion(ctx, cr, client)
		if deletionErr == nil {
			result.RequeueAfter = 0
		}

		return result, deletionErr
	}

	if cr.Spec.NoahClusterRef == nil || cr.Spec.NoahClusterRef.Name == "" {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Noah Cluster reference is required")
		return reconcile.Result{}, splcommon.NewTerminalError(
			splcommon.EventReasonValidateSpecFailed,
			"Noah IndexerCluster spec validation failed",
			fmt.Errorf("noahClusterRef.name must not be empty"),
		)
	}

	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}
	if err = reconcileutil.ValidateCommonSplunkSpec(ctx, client, &cr.Spec.CommonSplunkSpec, cr); err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Indexer Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(
			splcommon.EventReasonValidateSpecFailed,
			"Noah IndexerCluster spec validation failed",
			err,
		)
	}

	dependency := reconcileutil.ResolveNoahDependency(ctx, client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)
	if dependency.Runtime == nil {
		if !dependency.StateKnown {
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to resolve Noah dependencies")
			return result, dependency.ReconcileErr
		}
		setOutcome(noahIndexerDependencyOutcome(dependency))
		return result, dependency.ReconcileErr
	}
	podManager := newNoahIndexerPodManager(client, cr, dependency.Runtime)

	statefulSet, phase, applyErr := applyNoahIndexerResources(ctx, client, cr, podManager)
	if applyErr != nil {
		if outcome, outcomeErr, handled := noahIndexerOutcomeFromError(applyErr, previousPhase); handled {
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
			mappedOutcome, mappedErr, handled := noahIndexerOutcomeFromError(err, previousPhase)
			if handled {
				outcome = mappedOutcome
				err = mappedErr
			} else {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to reconcile Noah IndexerCluster lifecycle")
				return result, err
			}
		}
	default:
		outcome = waitForNoahIndexerWorkload(phase, previousPhase, appliedReplicas, lifecycleIsScaleIn(cr.Status.Lifecycle))
	}

	setOutcome(outcome)
	return result, err
}

func applyNoahIndexerResources(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, podManager *noahIndexerPodManager) (*appsv1.StatefulSet, enterpriseApi.Phase, error) {
	if cr.Spec.LicenseManagerRef.Name != "" {
		statefulSetKey := types.NamespacedName{
			Name:      splutil.GetSplunkStatefulsetName(splcommon.SplunkIndexer, cr.Name),
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

	if _, err := k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer); err != nil {
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
		if err := k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer, service.headless)); err != nil {
			return nil, enterpriseApi.PhaseError, fmt.Errorf("apply %s Service: %w", service.name, err)
		}
	}

	noahSpec := podManager.runtime.Spec()
	defaultsConfigMap, defaultsSecret, err := ensureNoahIndexerDefaults(ctx, client, cr, noahSpec, string(podManager.runtime.Credential()))
	if err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("ensure indexer defaults: %w", err)
	}

	statefulSet, err := getIndexerStatefulSet(ctx, client, cr,
		defaultsConfigMap.AsStatefulSetOption(),
		defaultsSecret.AsStatefulSetOption(),
		withNoahIndexerLabels(cr.Name, cr.Spec.NoahClusterRef.Name),
		resources.WithNoahPodIdentity(os.Getenv(resources.ClusterDomainEnvName)),
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

func ensureNoahIndexerDefaults(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, noahSpec enterpriseApi.NoahClusterSpec, credential string) (resources.DefaultsConfigMap, resources.DefaultsSecret, error) {
	structuralEntries := splunkconfig.NoahIndexerConf(noahSpec.Endpoint, noahSpec.Tenant)
	credentialEntries := splunkconfig.NoahCredentialsConf(credential)

	if (cr.Spec.QueueRef != nil && cr.Spec.QueueRef.Name != "") ||
		(cr.Spec.ObjectStorageRef != nil && cr.Spec.ObjectStorageRef.Name != "") {
		var queueRef, objectStorageRef corev1.ObjectReference
		if cr.Spec.QueueRef != nil {
			queueRef = *cr.Spec.QueueRef
		}
		if cr.Spec.ObjectStorageRef != nil {
			objectStorageRef = *cr.Spec.ObjectStorageRef
		}

		resolved, err := configworkflow.ResolveQueueAndObjectStorage(ctx, client, cr, queueRef, objectStorageRef)
		if err != nil {
			return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, fmt.Errorf("resolve queue config: %w", err)
		}
		smartBusEntries, smartBusCredentialEntries, err := splunkconfig.BuildSmartBusConfig(&resolved.Queue, &resolved.OS, resolved.AccessKey, resolved.SecretKey)
		if err != nil {
			return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
		}
		structuralEntries = append(structuralEntries, smartBusEntries...)
		credentialEntries = append(credentialEntries, smartBusCredentialEntries...)
	}

	owner := splcommon.AsOwner(cr, true)
	configMap, err := configworkflow.EnsureConfigMap(ctx, client, cr, structuralEntries, &owner, resources.WithDictionaryConf())
	if err != nil {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
	}
	secret, err := configworkflow.EnsureSecret(ctx, client, cr, credentialEntries, &owner, resources.WithDictionaryConf())
	if err != nil {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
	}
	return configMap, secret, nil
}

func withNoahIndexerLabels(indexerClusterName, noahClusterName string) resources.StatefulSetOption {
	labels := resources.GetSplunkLabels(indexerClusterName, splcommon.SplunkIndexer, noahClusterName)
	return func(statefulSet *appsv1.StatefulSet) {
		statefulSet.Labels = mergeNoahIndexerLabels(statefulSet.Labels, labels)
		statefulSet.Spec.Selector.MatchLabels = labels
		statefulSet.Spec.Template.Labels = mergeNoahIndexerLabels(statefulSet.Spec.Template.Labels, labels)
		for i := range statefulSet.Spec.VolumeClaimTemplates {
			claim := &statefulSet.Spec.VolumeClaimTemplates[i]
			claim.Labels = mergeNoahIndexerLabels(claim.Labels, labels)
		}
	}
}

func mergeNoahIndexerLabels(current, desired map[string]string) map[string]string {
	if current == nil {
		current = make(map[string]string, len(desired))
	}
	for key, value := range desired {
		current[key] = value
	}
	return current
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
		// An outcome that reports no condition of its own leaves a zero value
		// here; writing it would add an unusable, untyped status entry.
		if condition.Type == "" {
			continue
		}
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
	runtime          *configworkflow.NoahRuntime
	cacheWarmEnabled bool
	cacheWarmTimeout time.Duration
}

var (
	_ splcommon.StatefulSetPodManager        = (*noahIndexerPodManager)(nil)
	_ splcommon.StatefulSetScaleOutPlanner   = (*noahIndexerPodManager)(nil)
	_ splcommon.StatefulSetScaleDownFinisher = (*noahIndexerPodManager)(nil)
	_ splcommon.StatefulSetRecycleOrderer    = (*noahIndexerPodManager)(nil)
)

func newNoahIndexerPodManager(client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, runtime *configworkflow.NoahRuntime) *noahIndexerPodManager {
	spec := runtime.Spec()
	return &noahIndexerPodManager{
		client:           client,
		cr:               cr,
		runtime:          runtime,
		cacheWarmEnabled: noahCacheWarmScaleOutEnabled(spec),
		cacheWarmTimeout: noahCacheWarmScaleOutTimeout(spec),
	}
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
	decision, err := mgr.reconcileLifecycle(appliedReplicas)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}
	lifecycle := mgr.cr.Status.Lifecycle
	waitingForScaleOutMembership := lifecycle != nil &&
		lifecycle.Kind == enterpriseApi.IndexerClusterLifecycleScaleOut &&
		lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleWaitingForMembership

	if decision.block || waitingForScaleOutMembership {
		// A newer template revision belongs to the subsequent rollout. This
		// operation only requires its exact replica batch to be ready.
		if waitingForScaleOutMembership &&
			phase == enterpriseApi.PhaseReady &&
			appliedReplicas == lifecycle.Target.TargetReplicas &&
			statefulSet.Status.ReadyReplicas == appliedReplicas {
			return enterpriseApi.PhaseReady, nil
		}

		if statefulSet.Status.ReadyReplicas < appliedReplicas {
			if err := k8sops.CheckPodsForTerminalFailures(ctx, mgr.client, statefulSet); err != nil {
				return enterpriseApi.PhaseError, err
			}
		}

		if decision.block {
			return decision.phaseOverride, nil
		}

		return workloadPhase, nil
	}

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

	if appliedReplicas == desiredReplicas &&
		noahIndexerStatefulSetConverged(statefulSet, appliedReplicas) &&
		mgr.cr.Status.Phase != enterpriseApi.PhaseUpdating &&
		mgr.cr.Status.Lifecycle == nil {
		return enterpriseApi.PhaseReady, nil
	}

	if lifecycle := mgr.cr.Status.Lifecycle; lifecycle != nil {
		desiredReplicas = lifecycle.Target.TargetReplicas
	}

	workloadPhase, err = k8sops.UpdateStatefulSetPods(ctx, mgr.client, statefulSet, mgr, desiredReplicas)
	if err != nil {
		return workloadPhase, err
	}
	if decision.phaseOverride != "" {
		switch workloadPhase {
		case enterpriseApi.PhasePending, enterpriseApi.PhaseScalingUp:
			workloadPhase = decision.phaseOverride
		}
	}
	return workloadPhase, nil
}

// NextReplicas plans or resumes one durable Noah scale-out batch.
func (mgr *noahIndexerPodManager) NextReplicas(ctx context.Context, appliedReplicas, requestedReplicas int32) (splcommon.ScaleOutPlan, error) {
	blocked := splcommon.ScaleOutPlan{TargetReplicas: appliedReplicas}
	if appliedReplicas >= requestedReplicas {
		blocked.Complete = true
		return blocked, nil
	}

	lifecycle := mgr.cr.Status.Lifecycle
	if lifecycle == nil {
		membership, err := mgr.observePeers(ctx, appliedReplicas)
		if err != nil {
			return blocked, newNoahIndexerObservationError(err, enterpriseApi.PhaseScalingUp)
		}

		if membership.TimedOutPeerID != "" {
			return blocked, &noahIndexerCacheWarmTimeoutError{peerID: membership.TimedOutPeerID}
		}

		plan := indexerworkflow.PlanNoahScaleOut(membership, appliedReplicas, requestedReplicas, mgr.cacheWarmEnabled)
		if plan.TargetReplicas == appliedReplicas {
			return plan, nil
		}

		lifecycle, err = mgr.newScaleOutLifecycle(appliedReplicas, plan.TargetReplicas)
		if err != nil {
			return blocked, err
		}

		mgr.cr.Status.Lifecycle = lifecycle

		return blocked, nil
	}

	if lifecycle.Kind != enterpriseApi.IndexerClusterLifecycleScaleOut ||
		lifecycle.Checkpoint != enterpriseApi.IndexerClusterLifecycleActionPending ||
		lifecycle.PendingAction == nil ||
		lifecycle.PendingAction.Type != enterpriseApi.IndexerClusterLifecycleSetReplicas ||
		lifecycle.Target.SourceReplicas != appliedReplicas ||
		lifecycle.Target.TargetReplicas != requestedReplicas {
		return blocked, nil
	}

	return splcommon.ScaleOutPlan{TargetReplicas: lifecycle.Target.TargetReplicas}, nil
}

// PrepareScaleDown persists or resumes graceful removal of one Noah peer.
func (mgr *noahIndexerPodManager) PrepareScaleDown(ctx context.Context, ordinal int32) (bool, error) {
	lifecycle := mgr.cr.Status.Lifecycle
	if lifecycle != nil {
		if !lifecycleAuthorizesScaleIn(lifecycle, ordinal) {
			return false, nil
		}

		target := lifecycle.Target.Peers[0]
		pod := &corev1.Pod{}
		err := mgr.client.Get(ctx, types.NamespacedName{Namespace: mgr.statefulSet.Namespace, Name: target.PodName}, pod)
		if k8serrors.IsNotFound(err) {
			mgr.cr.Status.Lifecycle = nil
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("revalidate scale-in source Pod %s: %w", target.PodName, err)
		}
		if pod.UID != target.SourcePodUID {
			mgr.cr.Status.Lifecycle = nil
			return false, nil
		}
	}

	appliedReplicas, err := noahIndexerStatefulSetReplicas(mgr.statefulSet)
	if err != nil {
		return false, err
	}

	observation, err := mgr.observePeers(ctx, appliedReplicas)
	if err != nil {
		return false, newNoahIndexerObservationError(err, enterpriseApi.PhaseScalingDown)
	}
	if !observation.AllReady {
		return false, nil
	}

	if lifecycle != nil {
		return true, nil
	}

	lifecycle, err = mgr.newScaleInLifecycle(ctx, ordinal, appliedReplicas)
	if err != nil {
		return false, err
	}
	mgr.cr.Status.Lifecycle = lifecycle

	return false, nil
}

// FinishScaleDown deletes the removed Pod's PVCs, unregisters its peer, and
// waits for Noah's latest bucket map to exclude it.
func (mgr *noahIndexerPodManager) FinishScaleDown(ctx context.Context, ordinal int32) (bool, error) {
	lifecycle := mgr.cr.Status.Lifecycle
	if !lifecycleIsScaleIn(lifecycle) {
		return true, nil
	}
	if lifecycle.Checkpoint != enterpriseApi.IndexerClusterLifecycleWaitingForMembership {
		return true, nil
	}

	if len(lifecycle.Target.Peers) != 1 {
		return false, fmt.Errorf("scale-in lifecycle must target exactly one peer")
	}

	target := lifecycle.Target.Peers[0]
	if ordinal != target.Ordinal {
		return false, fmt.Errorf("scale-in finisher received ordinal %d, expected %d", ordinal, target.Ordinal)
	}

	pod := &corev1.Pod{}
	err := mgr.client.Get(ctx, types.NamespacedName{Namespace: mgr.statefulSet.Namespace, Name: target.PodName}, pod)
	if err == nil {
		if pod.UID != target.SourcePodUID {
			return false, newNoahIndexerOperationError(
				fmt.Errorf("removed Noah indexer Pod %s was replaced with UID %s", target.PodName, pod.UID),
				enterpriseApi.PhaseScalingDown,
			)
		}
		return false, nil
	}
	if !k8serrors.IsNotFound(err) {
		return false, newNoahIndexerOperationError(
			fmt.Errorf("get removed Noah indexer Pod %s: %w", target.PodName, err),
			enterpriseApi.PhaseScalingDown,
		)
	}
	if err := k8sops.DeleteStatefulSetPodPVCs(ctx, mgr.client, mgr.statefulSet, target.PodName); err != nil {
		return false, newNoahIndexerOperationError(
			fmt.Errorf("delete PVCs for removed Noah indexer Pod %s: %w", target.PodName, err),
			enterpriseApi.PhaseScalingDown,
		)
	}

	noahClient, err := mgr.runtime.Client()
	if err != nil {
		return false, newNoahIndexerOperationError(err, enterpriseApi.PhaseScalingDown)
	}

	if err := noahClient.UnregisterPeer(ctx, target.PeerID); err != nil {
		noahErr, ok := errors.AsType[*noah.Error](err)
		if !ok || noahErr.Kind != noah.ErrorKindNotFound {
			return false, newNoahIndexerOperationError(
				fmt.Errorf("unregister Noah peer %s: %w", target.PeerID, err),
				enterpriseApi.PhaseScalingDown,
			)
		}
	}

	bucketMap, err := noahClient.GetLatestBucketMap(ctx)
	if err != nil {
		return false, newNoahIndexerOperationError(
			fmt.Errorf("get latest Noah bucket map: %w", err),
			enterpriseApi.PhaseScalingDown,
		)
	}

	remainingPeerIDs, err := noahIndexerPeerIDs(mgr.statefulSet, lifecycle.Target.TargetReplicas)
	if err != nil {
		return false, err
	}

	if !indexerworkflow.NoahBucketMapConfirmsScaleDown(bucketMap, remainingPeerIDs, target.PeerID) {
		return false, nil
	}

	_, err = mgr.applyLifecycleObservation(indexerworkflow.LifecycleObservation{
		StatefulSetUID:            mgr.statefulSet.UID,
		StatefulSetReplicas:       lifecycle.Target.TargetReplicas,
		TargetPods:                map[int32]indexerworkflow.LifecyclePodObservation{},
		Now:                       time.Now(),
		SatisfiedTargetMembership: true,
	})
	return false, err
}

// DeferRecycle serializes stale Pods behind the active rollout target.
func (mgr *noahIndexerPodManager) DeferRecycle(_ context.Context, ordinal int32, _ string) (bool, error) {
	lifecycle := mgr.cr.Status.Lifecycle
	if lifecycle == nil || lifecycle.Kind != enterpriseApi.IndexerClusterLifecycleRollout {
		return false, nil
	}

	if len(lifecycle.Target.Peers) != 1 {
		return true, fmt.Errorf("rollout lifecycle must target exactly one peer")
	}

	return lifecycle.Target.Peers[0].Ordinal != ordinal, nil
}

// PrepareRecycle persists or resumes authorization to replace one Pod.
func (mgr *noahIndexerPodManager) PrepareRecycle(ctx context.Context, ordinal int32) (bool, error) {
	lifecycle := mgr.cr.Status.Lifecycle
	if lifecycle != nil {
		if !lifecycleTargetsRolloutOrdinal(lifecycle, ordinal) {
			return false, nil
		}

		decision, err := mgr.observeRolloutLifecycle(ctx, lifecycle)
		if err != nil {
			return false, err
		}
		if decision.action == nil {
			return false, nil
		}
		if decision.action.Type != enterpriseApi.IndexerClusterLifecycleDeletePod {
			return false, fmt.Errorf("unsupported rollout action %q", decision.action.Type)
		}

		appliedReplicas, err := noahIndexerStatefulSetReplicas(mgr.statefulSet)
		if err != nil {
			return false, err
		}
		observation, err := mgr.observePeers(ctx, appliedReplicas)
		if err != nil {
			return false, newNoahIndexerObservationError(err, enterpriseApi.PhaseUpdating)
		}
		if !observation.AllReady {
			return false, nil
		}

		return true, nil
	}

	appliedReplicas, err := noahIndexerStatefulSetReplicas(mgr.statefulSet)
	if err != nil {
		return false, err
	}

	observation, err := mgr.observePeers(ctx, appliedReplicas)
	if err != nil {
		return false, newNoahIndexerObservationError(err, enterpriseApi.PhaseUpdating)
	}
	if !observation.AllReady {
		return false, nil
	}

	lifecycle, err = mgr.newRolloutLifecycle(ctx, ordinal)
	if err != nil {
		return false, err
	}
	mgr.cr.Status.Lifecycle = lifecycle

	return false, nil
}

// FinishRecycle advances the rollout while the replacement converges.
func (mgr *noahIndexerPodManager) FinishRecycle(ctx context.Context, ordinal int32) (bool, error) {
	lifecycle := mgr.cr.Status.Lifecycle
	if lifecycle == nil {
		return true, nil
	}
	if !lifecycleTargetsRolloutOrdinal(lifecycle, ordinal) {
		return true, nil
	}

	_, err := mgr.observeRolloutLifecycle(ctx, lifecycle)
	if err != nil {
		return false, err
	}

	return false, nil
}

// FinishUpgrade completes the shared upgrade hook after durable rollout convergence.
func (mgr *noahIndexerPodManager) FinishUpgrade(context.Context, int32) error {
	return nil
}

type lifecycleDecision struct {
	action        *enterpriseApi.IndexerClusterLifecyclePendingAction
	block         bool
	phaseOverride enterpriseApi.Phase
}

// reconcileLifecycle handles lifecycle state that must be resolved before the
// generic StatefulSet engine may mutate the workload.
func (mgr *noahIndexerPodManager) reconcileLifecycle(appliedReplicas int32) (lifecycleDecision, error) {
	current := mgr.cr.Status.Lifecycle
	if current == nil {
		return lifecycleDecision{}, nil
	}

	observed := indexerworkflow.LifecycleObservation{
		StatefulSetUID:      mgr.statefulSet.UID,
		StatefulSetReplicas: appliedReplicas,
		Now:                 time.Now(),
	}
	if mgr.statefulSet.UID != current.Target.StatefulSetUID {
		return mgr.applyLifecycleObservation(observed)
	}

	if current.Checkpoint == enterpriseApi.IndexerClusterLifecycleCompleted {
		mgr.cr.Status.Lifecycle = nil
		return lifecycleDecision{block: true, phaseOverride: lifecyclePhase(current.Kind)}, nil
	}
	if current.Checkpoint == enterpriseApi.IndexerClusterLifecycleFailed {
		return lifecycleDecision{block: true, phaseOverride: lifecyclePhase(current.Kind)}, fmt.Errorf("IndexerCluster lifecycle failed")
	}

	switch current.Kind {
	case enterpriseApi.IndexerClusterLifecycleRollout:
		if mgr.statefulSet.UID == current.Target.StatefulSetUID && appliedReplicas == current.Target.TargetReplicas {
			return lifecycleDecision{phaseOverride: enterpriseApi.PhaseUpdating}, nil
		}
	case enterpriseApi.IndexerClusterLifecycleScaleIn:
		if current.Checkpoint == enterpriseApi.IndexerClusterLifecycleWaitingForMembership &&
			appliedReplicas == current.Target.TargetReplicas {
			return lifecycleDecision{phaseOverride: enterpriseApi.PhaseScalingDown}, nil
		}
	case enterpriseApi.IndexerClusterLifecycleScaleOut:
		if mgr.statefulSet.UID == current.Target.StatefulSetUID &&
			current.Checkpoint == enterpriseApi.IndexerClusterLifecycleWaitingForMembership &&
			appliedReplicas == current.Target.TargetReplicas {
			return lifecycleDecision{phaseOverride: enterpriseApi.PhaseScalingUp}, nil
		}
	default:
		return lifecycleDecision{block: true}, fmt.Errorf("unsupported IndexerCluster lifecycle kind %q", current.Kind)
	}

	return mgr.applyLifecycleObservation(observed)
}

func (mgr *noahIndexerPodManager) applyLifecycleObservation(observed indexerworkflow.LifecycleObservation) (lifecycleDecision, error) {
	current := mgr.cr.Status.Lifecycle
	if current == nil {
		return lifecycleDecision{}, nil
	}

	decision := lifecycleDecision{phaseOverride: lifecyclePhase(current.Kind)}
	transition := indexerworkflow.AdvanceLifecycle(current, observed)
	if transition.Replan {
		mgr.cr.Status.Lifecycle = nil
		decision.block = true
		return decision, nil
	}
	if transition.Lifecycle == nil {
		decision.block = true
		return decision, fmt.Errorf("lifecycle transition returned no state")
	}

	changed := !apiequality.Semantic.DeepEqual(current, transition.Lifecycle)
	mgr.cr.Status.Lifecycle = transition.Lifecycle
	if transition.Err != nil {
		decision.block = true
		return decision, transition.Err
	}
	if transition.Lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleFailed {
		decision.block = true
		return decision, fmt.Errorf("IndexerCluster lifecycle failed")
	}
	if changed {
		decision.block = true
		return decision, nil
	}

	decision.action = transition.Execute
	decision.block = transition.Execute == nil
	return decision, nil
}

func lifecyclePhase(kind enterpriseApi.IndexerClusterLifecycleKind) enterpriseApi.Phase {
	switch kind {
	case enterpriseApi.IndexerClusterLifecycleRollout:
		return enterpriseApi.PhaseUpdating
	case enterpriseApi.IndexerClusterLifecycleScaleIn:
		return enterpriseApi.PhaseScalingDown
	case enterpriseApi.IndexerClusterLifecycleScaleOut:
		return enterpriseApi.PhaseScalingUp
	default:
		return enterpriseApi.PhaseError
	}
}

func (mgr *noahIndexerPodManager) newRolloutLifecycle(ctx context.Context, ordinal int32) (*enterpriseApi.IndexerClusterLifecycleStatus, error) {
	replicas, err := noahIndexerStatefulSetReplicas(mgr.statefulSet)
	if err != nil {
		return nil, err
	}

	peer, pod, err := mgr.newLifecycleSourcePeerTarget(ctx, ordinal)
	if err != nil {
		return nil, err
	}

	return indexerworkflow.NewRolloutLifecycle(mgr.cr.Generation, enterpriseApi.IndexerClusterLifecycleTarget{
		StatefulSetUID: mgr.statefulSet.UID,
		SourceReplicas: replicas,
		TargetReplicas: replicas,
		SourceRevision: pod.Labels["controller-revision-hash"],
		TargetRevision: mgr.statefulSet.Status.UpdateRevision,
		Peers:          []enterpriseApi.IndexerClusterLifecyclePeerTarget{peer},
	}, time.Now())
}

func (mgr *noahIndexerPodManager) newScaleInLifecycle(ctx context.Context, ordinal, sourceReplicas int32) (*enterpriseApi.IndexerClusterLifecycleStatus, error) {
	peer, _, err := mgr.newLifecycleSourcePeerTarget(ctx, ordinal)
	if err != nil {
		return nil, err
	}

	return indexerworkflow.NewScaleInLifecycle(mgr.cr.Generation, enterpriseApi.IndexerClusterLifecycleTarget{
		StatefulSetUID: mgr.statefulSet.UID,
		SourceReplicas: sourceReplicas,
		TargetReplicas: ordinal,
		Peers:          []enterpriseApi.IndexerClusterLifecyclePeerTarget{peer},
	}, time.Now())
}

func (mgr *noahIndexerPodManager) newScaleOutLifecycle(sourceReplicas, targetReplicas int32) (*enterpriseApi.IndexerClusterLifecycleStatus, error) {
	peers := make([]enterpriseApi.IndexerClusterLifecyclePeerTarget, 0, targetReplicas-sourceReplicas)
	for ordinal := sourceReplicas; ordinal < targetReplicas; ordinal++ {
		peer, err := mgr.newLifecyclePeerTarget(ordinal)
		if err != nil {
			return nil, err
		}
		peers = append(peers, peer)
	}

	return indexerworkflow.NewScaleOutLifecycle(mgr.cr.Generation, enterpriseApi.IndexerClusterLifecycleTarget{
		StatefulSetUID: mgr.statefulSet.UID,
		SourceReplicas: sourceReplicas,
		TargetReplicas: targetReplicas,
		Peers:          peers,
	}, time.Now())
}

func (mgr *noahIndexerPodManager) newLifecycleSourcePeerTarget(ctx context.Context, ordinal int32) (enterpriseApi.IndexerClusterLifecyclePeerTarget, *corev1.Pod, error) {
	peer, err := mgr.newLifecyclePeerTarget(ordinal)
	if err != nil {
		return enterpriseApi.IndexerClusterLifecyclePeerTarget{}, nil, err
	}

	pod := &corev1.Pod{}
	if err := mgr.client.Get(ctx, types.NamespacedName{Namespace: mgr.statefulSet.Namespace, Name: peer.PodName}, pod); err != nil {
		return enterpriseApi.IndexerClusterLifecyclePeerTarget{}, nil, fmt.Errorf("get lifecycle source Pod %s: %w", peer.PodName, err)
	}
	peer.SourcePodUID = pod.UID

	return peer, pod, nil
}

func (mgr *noahIndexerPodManager) newLifecyclePeerTarget(ordinal int32) (enterpriseApi.IndexerClusterLifecyclePeerTarget, error) {
	peerID, err := noahIndexerPeerID(mgr.statefulSet, ordinal)
	if err != nil {
		return enterpriseApi.IndexerClusterLifecyclePeerTarget{}, err
	}
	return enterpriseApi.IndexerClusterLifecyclePeerTarget{
		Ordinal: ordinal,
		PeerID:  peerID,
		PodName: noahIndexerPodName(mgr.statefulSet, ordinal),
	}, nil
}

func lifecycleIsScaleIn(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus) bool {
	return lifecycle != nil && lifecycle.Kind == enterpriseApi.IndexerClusterLifecycleScaleIn
}

func lifecycleAuthorizesScaleIn(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, ordinal int32) bool {
	return lifecycleIsScaleIn(lifecycle) &&
		lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleActionPending &&
		lifecycle.PendingAction != nil &&
		lifecycle.PendingAction.Type == enterpriseApi.IndexerClusterLifecycleSetReplicas &&
		lifecycle.Target.SourceReplicas == ordinal+1 &&
		lifecycle.Target.TargetReplicas == ordinal &&
		len(lifecycle.Target.Peers) == 1 &&
		lifecycle.Target.Peers[0].Ordinal == ordinal
}

func (mgr *noahIndexerPodManager) observeLifecycleTargetPods(ctx context.Context, peers []enterpriseApi.IndexerClusterLifecyclePeerTarget) (map[int32]indexerworkflow.LifecyclePodObservation, error) {
	pods := make(map[int32]indexerworkflow.LifecyclePodObservation, len(peers))
	for _, peer := range peers {
		pod := &corev1.Pod{}
		key := types.NamespacedName{Namespace: mgr.statefulSet.Namespace, Name: peer.PodName}
		if err := mgr.client.Get(ctx, key, pod); err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("get target Noah indexer Pod %s: %w", key, err)
		}

		pods[peer.Ordinal] = indexerworkflow.LifecyclePodObservation{
			UID:      pod.UID,
			Revision: pod.Labels["controller-revision-hash"],
			Ready: pod.Status.Phase == corev1.PodRunning && slices.ContainsFunc(pod.Status.Conditions, func(condition corev1.PodCondition) bool {
				return condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue
			}),
		}
	}

	return pods, nil
}

func (mgr *noahIndexerPodManager) observeRolloutLifecycle(ctx context.Context, lifecycle *enterpriseApi.IndexerClusterLifecycleStatus) (lifecycleDecision, error) {
	targetPods, err := mgr.observeLifecycleTargetPods(ctx, lifecycle.Target.Peers)
	if err != nil {
		return lifecycleDecision{}, err
	}

	membershipSatisfied := false
	if lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleWaitingForMembership {
		observation, err := mgr.observePeers(ctx, lifecycle.Target.TargetReplicas)
		if err != nil {
			return lifecycleDecision{}, newNoahIndexerObservationError(err, enterpriseApi.PhaseUpdating)
		}
		membershipSatisfied = observation.AllReady
	}

	lifecycleObservation, err := mgr.lifecycleObservation(targetPods, membershipSatisfied)
	if err != nil {
		return lifecycleDecision{}, err
	}

	return mgr.applyLifecycleObservation(lifecycleObservation)
}

func (mgr *noahIndexerPodManager) lifecycleObservation(targetPods map[int32]indexerworkflow.LifecyclePodObservation, membershipSatisfied bool) (indexerworkflow.LifecycleObservation, error) {
	replicas, err := noahIndexerStatefulSetReplicas(mgr.statefulSet)
	if err != nil {
		return indexerworkflow.LifecycleObservation{}, err
	}

	return indexerworkflow.LifecycleObservation{
		StatefulSetUID:            mgr.statefulSet.UID,
		StatefulSetReplicas:       replicas,
		StatefulSetUpdateRevision: mgr.statefulSet.Status.UpdateRevision,
		TargetPods:                targetPods,
		Now:                       time.Now(),
		SatisfiedTargetMembership: membershipSatisfied,
	}, nil
}

func lifecycleTargetsRolloutOrdinal(lifecycle *enterpriseApi.IndexerClusterLifecycleStatus, ordinal int32) bool {
	return lifecycle.Kind == enterpriseApi.IndexerClusterLifecycleRollout &&
		len(lifecycle.Target.Peers) == 1 &&
		lifecycle.Target.Peers[0].Ordinal == ordinal
}

func (mgr *noahIndexerPodManager) observeReady(ctx context.Context, appliedReplicas int32, previousPhase enterpriseApi.Phase, previousReplicas int32) (noahIndexerOutcome, error) {
	observation, err := mgr.observePeers(ctx, appliedReplicas)
	if err != nil {
		return noahIndexerOutcome{}, newNoahIndexerObservationError(err, previousPhase)
	}

	if observation.TimedOutPeerID != "" {
		return noahIndexerOutcome{}, &noahIndexerCacheWarmTimeoutError{peerID: observation.TimedOutPeerID}
	}

	if lifecycle := mgr.cr.Status.Lifecycle; lifecycle != nil &&
		lifecycle.Kind == enterpriseApi.IndexerClusterLifecycleScaleOut &&
		lifecycle.Checkpoint == enterpriseApi.IndexerClusterLifecycleWaitingForMembership {
		targetPods, err := mgr.observeLifecycleTargetPods(ctx, lifecycle.Target.Peers)
		if err != nil {
			return noahIndexerOutcome{}, err
		}

		membershipSatisfied := observation.AllReady
		if !mgr.cacheWarmEnabled {
			membershipSatisfied = observation.AllRegistered
		}

		if _, err := mgr.applyLifecycleObservation(indexerworkflow.LifecycleObservation{
			StatefulSetUID:            mgr.statefulSet.UID,
			StatefulSetReplicas:       appliedReplicas,
			TargetPods:                targetPods,
			Now:                       time.Now(),
			SatisfiedTargetMembership: membershipSatisfied,
		}); err != nil {
			return noahIndexerOutcome{}, err
		}

		return noahIndexerOutcome{
			phase:        enterpriseApi.PhaseScalingUp,
			phaseMessage: "Completing durable scale-out",
			condition: newNoahPeersReadyCondition(
				metav1.ConditionFalse,
				enterpriseApi.ReasonNoahPeersNotReady,
				"Waiting for the scale-out lifecycle to complete",
			),
			requeueAfter: noahIndexerPollInterval,
		}, nil
	}

	if !observation.AllReady {
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
	expectedPeers, err := currentNoahIndexerPeerIncarnations(ctx, mgr.client, replicas, mgr.statefulSet)
	if err != nil {
		return indexerworkflow.NoahMembership{}, err
	}
	if len(expectedPeers) != int(replicas) {
		return indexerworkflow.NoahMembership{}, nil
	}

	noahClient, err := mgr.runtime.Client()
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

	// Peer status is the last successful complete Noah observation. Workload
	// waits and observation failures retain the previous snapshot.
	mgr.cr.Status.Peers = noahIndexerPeerStatuses(observation)

	return observation, nil
}

func noahIndexerPeerStatuses(membership indexerworkflow.NoahMembership) []enterpriseApi.IndexerClusterMemberStatus {
	statuses := make([]enterpriseApi.IndexerClusterMemberStatus, 0, len(membership.Peers))
	for _, peer := range membership.Peers {
		status := enterpriseApi.IndexerClusterMemberStatus{
			Name:   peer.Expected.PodName,
			Status: noahIndexerPeerStatus(peer),
		}
		if peer.Classification == indexerworkflow.NoahPeerCurrent {
			status.ID = peer.Expected.ID
			status.Searchable = peer.Ready
		}
		statuses = append(statuses, status)
	}

	return statuses
}

func noahIndexerPeerStatus(peer indexerworkflow.NoahPeerMembership) string {
	if peer.Classification != indexerworkflow.NoahPeerCurrent {
		return string(peer.Classification)
	}

	switch peer.Status {
	case noah.PeerStatusStarted:
		return "Started"
	case noah.PeerStatusWarming:
		return "Warming"
	case noah.PeerStatusWarmed:
		return "Warmed"
	case noah.PeerStatusUp:
		return "Up"
	case noah.PeerStatusDown:
		return "Down"
	case noah.PeerStatusDecommissionReady:
		return "DecommissionReady"
	case noah.PeerStatusDecommissioning:
		return "Decommissioning"
	case noah.PeerStatusDecommissioned:
		return "Decommissioned"
	default:
		return "Unknown"
	}
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

	statefulSet := &appsv1.StatefulSet{}
	statefulSetKey := types.NamespacedName{
		Name:      splutil.GetSplunkStatefulsetName(splcommon.SplunkLicenseManager, licenseManager.Name),
		Namespace: licenseManager.Namespace,
	}
	if err := client.Get(ctx, statefulSetKey, statefulSet); err != nil {
		return false, fmt.Errorf("get LicenseManager %s StatefulSet: %w", key, err)
	}
	if len(statefulSet.Spec.Template.Spec.Containers) == 0 {
		return false, fmt.Errorf("LicenseManager %s StatefulSet has no containers", key)
	}
	image := statefulSet.Spec.Template.Spec.Containers[0].Image
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

func waitForNoahIndexerWorkload(phase, previousPhase enterpriseApi.Phase, appliedReplicas int32, scaleInPending bool) noahIndexerOutcome {
	if phase == enterpriseApi.PhasePending {
		phase = noahIndexerLifecyclePhase(previousPhase)
	}

	phaseMessage := ""
	conditionMessage := "Waiting for the indexer workload before observing Noah peers"
	switch phase {
	case enterpriseApi.PhaseScalingDown:
		phaseMessage = "Gracefully scaling down the indexer workload"
		if scaleInPending {
			phaseMessage = "Waiting for graceful Noah scale-in cleanup"
			conditionMessage = "Waiting for removed-peer cleanup and active bucket-map confirmation"
		}
	case enterpriseApi.PhaseUpdating:
		phaseMessage = "Waiting for the StatefulSet pod-template revision to be applied"
		conditionMessage = "Waiting for the updated indexer workload before observing Noah peers"
	case enterpriseApi.PhaseScalingUp:
		phaseMessage = fmt.Sprintf("Waiting for %d applied replicas to become ready before continuing scale-out", appliedReplicas)
		conditionMessage = "Waiting for applied indexer replicas before observing Noah peers"
	}

	return noahIndexerOutcome{
		phase:        phase,
		phaseMessage: phaseMessage,
		condition: newNoahPeersReadyCondition(
			metav1.ConditionFalse,
			enterpriseApi.ReasonNoahPeersNotReady,
			conditionMessage,
		),
		requeueAfter: noahIndexerPollInterval,
	}
}

func noahIndexerOutcomeFromError(err error, fallbackPhase enterpriseApi.Phase) (noahIndexerOutcome, error, bool) {
	if cacheWarmTimeoutErr, ok := errors.AsType[*noahIndexerCacheWarmTimeoutError](err); ok {
		message := cacheWarmTimeoutErr.Error()
		return noahIndexerOutcome{
			phase:        enterpriseApi.PhaseError,
			phaseMessage: message,
			condition: newNoahPeersReadyCondition(
				metav1.ConditionFalse,
				enterpriseApi.ReasonNoahCacheWarmTimeout,
				message,
			),
		}, splcommon.NewTerminalError(splcommon.EventReasonNoahCacheWarmTimeout, message, cacheWarmTimeoutErr), true
	}

	if lifecycleErr, ok := errors.AsType[*noahIndexerLifecycleError](err); ok {
		phase := lifecycleErr.phase
		if phase == "" {
			phase = fallbackPhase
		}
		phase = noahIndexerLifecyclePhase(phase)
		reason := lifecycleErr.reason
		if reason == "" {
			reason = enterpriseApi.ReasonNoahPeerObservationFailed
		}
		phaseMessage := "Unable to observe Noah peers"
		if reason == enterpriseApi.ReasonNoahOperationFailed {
			phaseMessage = "Unable to complete Noah operation"
		}

		if noahErr, ok := errors.AsType[*noah.Error](lifecycleErr); ok {
			message := fmt.Sprintf("Noah operation failed: %s", noahErr)
			if noahErr.Kind == noah.ErrorKindCanceled {
				return noahIndexerOutcome{
					phase:        phase,
					phaseMessage: "Noah operation was canceled",
					condition: newNoahPeersReadyCondition(
						metav1.ConditionUnknown,
						reason,
						message,
					),
				}, lifecycleErr, true
			}
			if !noahErr.Retryable() {
				return noahIndexerOutcome{
					phase:        enterpriseApi.PhaseError,
					phaseMessage: message,
					condition: newNoahPeersReadyCondition(
						metav1.ConditionFalse,
						enterpriseApi.ReasonNoahOperationFailed,
						message,
					),
				}, splcommon.NewTerminalError(splcommon.EventReasonNoahOperationFailed, message, lifecycleErr), true
			}
			return noahIndexerOutcome{
				phase:        phase,
				phaseMessage: phaseMessage,
				condition: newNoahPeersReadyCondition(
					metav1.ConditionUnknown,
					reason,
					message,
				),
				requeueAfter: noahIndexerPollInterval,
			}, nil, true
		}
		return noahIndexerOutcome{
			phase:        phase,
			phaseMessage: phaseMessage,
			condition: newNoahPeersReadyCondition(
				metav1.ConditionUnknown,
				reason,
				fmt.Sprintf("%s: %v", phaseMessage, lifecycleErr),
			),
			requeueAfter: noahIndexerPollInterval,
		}, nil, true
	}

	return noahIndexerOutcome{}, err, false
}

func noahIndexerDependencyOutcome(dependency reconcileutil.NoahDependencyResult) noahIndexerOutcome {
	requeueAfter := time.Duration(0)
	if dependency.Phase == enterpriseApi.PhasePending {
		requeueAfter = noahIndexerPollInterval
	}
	return noahIndexerOutcome{
		phase:        dependency.Phase,
		phaseMessage: dependency.Message,
		condition: newNoahPeersReadyCondition(
			metav1.ConditionUnknown,
			enterpriseApi.ReasonNoahPeerObservationFailed,
			"Noah peers were not observed because the Noah dependency is unavailable",
		),
		requeueAfter: requeueAfter,
	}
}

func noahIndexerLifecyclePhase(phase enterpriseApi.Phase) enterpriseApi.Phase {
	switch phase {
	case enterpriseApi.PhaseScalingUp, enterpriseApi.PhaseScalingDown, enterpriseApi.PhaseUpdating:
		return phase
	default:
		return enterpriseApi.PhasePending
	}
}

type noahIndexerCacheWarmTimeoutError struct {
	peerID string
}

func (err *noahIndexerCacheWarmTimeoutError) Error() string {
	return fmt.Sprintf("Cache warming timed out for Noah peer %s", err.peerID)
}

type noahIndexerLifecycleError struct {
	err    error
	phase  enterpriseApi.Phase
	reason enterpriseApi.ConditionReason
}

func newNoahIndexerObservationError(err error, phase enterpriseApi.Phase) *noahIndexerLifecycleError {
	return &noahIndexerLifecycleError{
		err:    err,
		phase:  phase,
		reason: enterpriseApi.ReasonNoahPeerObservationFailed,
	}
}

func newNoahIndexerOperationError(err error, phase enterpriseApi.Phase) *noahIndexerLifecycleError {
	return &noahIndexerLifecycleError{
		err:    err,
		phase:  phase,
		reason: enterpriseApi.ReasonNoahOperationFailed,
	}
}

func (e *noahIndexerLifecycleError) Error() string {
	return e.err.Error()
}

func (e *noahIndexerLifecycleError) Unwrap() error {
	return e.err
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
			PodName:   podName,
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
