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
	"log/slog"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splunkconfig "github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	indexerworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/indexercluster"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/workflow/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const pauseRetryDelay = 30 * time.Second

// apply owns the request-level IndexerCluster reconciliation boundary.
func apply(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
	logger := logging.FromContext(ctx).With("controller", "IndexerCluster", "name", namespacedName.Name, "namespace", namespacedName.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	instance := &enterpriseApi.IndexerCluster{}
	if err := client.Get(ctx, namespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not load indexer cluster data: %w", err)
	}

	if instance.GetAnnotations()[enterpriseApi.IndexerClusterPausedAnnotation] == "true" {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: true, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update paused status", "error", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true, RequeueAfter: pauseRetryDelay}, nil
	} else if condition := meta.FindStatusCondition(instance.Status.Conditions, string(enterpriseApi.ConditionPaused)); condition != nil && condition.Status == metav1.ConditionTrue {
		result := splcommon.SetPhaseAndConditions(instance.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: instance.Status.Phase, IsPaused: false, Message: "", Generation: instance.GetGeneration(),
		})
		instance.Status.Conditions = result.Conditions
		if err := client.Status().Update(ctx, instance); err != nil {
			logger.ErrorContext(ctx, "failed to update unpaused status", "error", err)
			return reconcile.Result{}, err
		}
	}

	logger.InfoContext(ctx, "start", "crVersion", instance.GetResourceVersion())
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)
	var result reconcile.Result
	var err error
	if instance.Spec.ClusterManagerRef.Name != "" {
		result, err = ApplyIndexerClusterManager(ctx, client, instance)
	} else {
		result, err = ApplyIndexerCluster(ctx, client, instance)
	}
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}

	fresh := &enterpriseApi.IndexerCluster{}
	if fetchErr := client.Get(ctx, namespacedName, fresh); fetchErr != nil {
		if apierrors.IsNotFound(fetchErr) {
			return result, nil
		}
		logger.WarnContext(ctx, "failed to refetch CR for stalled condition update", "error", fetchErr)
		return result, fetchErr
	}
	oldConditions := append([]metav1.Condition(nil), fresh.Status.Conditions...)
	if message, ok := splcommon.TerminalMessage(err); ok {
		reason, _ := splcommon.TerminalReason(err)
		fresh.Status.Conditions = splcommon.UpsertStalledCondition(fresh.Status.Conditions, reason, message, fresh.GetGeneration())
	} else {
		fresh.Status.Conditions = splcommon.ClearStalledCondition(fresh.Status.Conditions, fresh.GetGeneration())
	}
	eventPublisher, publisherErr := k8sops.NewK8EventPublisherWithRecorder(recorder, fresh)
	if publisherErr != nil {
		logger.WarnContext(ctx, "failed to create event publisher", "error", publisherErr)
		return result, publisherErr
	}
	k8sops.EmitStalledTransitionEvents(ctx, eventPublisher, fresh.GetName(), oldConditions, fresh.Status.Conditions)
	if updateErr := client.Status().Update(ctx, fresh); updateErr != nil {
		logger.WarnContext(ctx, "failed to upsert stalled condition", "error", updateErr)
		return result, updateErr
	}
	if _, ok := splcommon.TerminalMessage(err); ok {
		return reconcile.Result{}, err
	}
	return result, err
}

// Apply is the request-level entry point used by the controller.
var Apply = apply

// ApplyIndexerClusterManager and ApplyIndexerCluster are operation seams used by
// focused reconciliation tests and selected according to the referenced manager API.
var ApplyIndexerClusterManager = applyIndexerClusterManager
var ApplyIndexerCluster = applyIndexerCluster

// applyIndexerClusterManager reconciles an IndexerCluster using the ClusterManager API.
func applyIndexerClusterManager(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	logger := logging.FromContext(ctx).With("func", "ApplyIndexerClusterManager", "name", cr.GetName(), "namespace", cr.GetNamespace())

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "IndexerCluster"

	var err error
	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.IndexerClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		result := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = result.Phase
		cr.Status.Conditions = result.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateIndexerClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "IndexerClusterSpecValidationFailed", "Validation of Indexer Cluster spec failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Indexer Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "Indexer Cluster spec validation failed", err)
	}

	// updates status after function completes
	cr.Status.ClusterManagerPhase = enterpriseApi.PhaseError
	if cr.Status.Replicas < cr.Spec.Replicas {
		logger.InfoContext(ctx, "scaling up IndexerCluster", "previousReplicas", cr.Status.Replicas, "newReplicas", cr.Spec.Replicas)
	}
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-indexer", cr.GetName())
	if cr.Status.Peers == nil {
		cr.Status.Peers = []enterpriseApi.IndexerClusterMemberStatus{}
	}
	if cr.Status.IndexerSecretChanged == nil {
		cr.Status.IndexerSecretChanged = []bool{}
	}
	if cr.Status.IdxcPasswordChangedSecrets == nil {
		cr.Status.IdxcPasswordChangedSecrets = make(map[string]bool)
	}

	// create or update general config resources
	namespaceScopedSecret, err := k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfigFailed", "Create or update of general config failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      cr.Spec.ClusterManagerRef.Name,
	}
	managerIdxCluster := &enterpriseApi.ClusterManager{}
	err = client.Get(ctx, namespacedName, managerIdxCluster)
	if err == nil {
		// when user creates both cluster manager and index cluster yaml file at the same time
		// cluser manager status is not yet set so it will be blank
		if managerIdxCluster.Status.Phase == "" {
			cr.Status.ClusterManagerPhase = enterpriseApi.PhasePending
		} else {
			cr.Status.ClusterManagerPhase = managerIdxCluster.Status.Phase
		}
	} else {
		logger.WarnContext(ctx, "the configured ClusterMasterRef doesn't exist", "ClusterManagerRef", cr.Spec.ClusterManagerRef.Name)
		cr.Status.ClusterManagerPhase = enterpriseApi.PhaseError
	}

	mgr := newIndexerClusterPodManager(logger, cr, namespaceScopedSecret, splclient.NewSplunkClient, client)
	// Check if we have configured enough number(<= RF) of replicas
	if mgr.cr.Status.ClusterManagerPhase == enterpriseApi.PhaseReady {
		if VerifyRFPeersCall != nil {
			err = VerifyRFPeersCall(ctx, client, cr)
		} else if VerifyRFPeers != nil {
			err = VerifyRFPeers(ctx, mgr, client)
		} else {
			err = mgr.workflowManager().VerifyRFPeers(ctx, client)
		}
		if err != nil {
			eventPublisher.Warning(ctx, "VerifyRFPeersFailed", "Verification of RF peer failed. Check operator logs for details.")
			setPhaseAndConditions(enterpriseApi.PhaseError, "Replication factor peer verification failed")
			return result, fmt.Errorf("verify RF peers: %w", err)
		}
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkIndexer)

		terminating, err := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
			cr.Status.ClusterManagerPhase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, "DeletionFailed", "Deletion of custom resource failed. Check operator logs for details.")
		}
		return result, err
	}
	// create or update a headless service for indexer cluster
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailed", "Create or update of headless service for Indexer Cluster failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update headless service")
		return result, fmt.Errorf("apply headless service: %w", err)
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailed", "Create or update of service for Indexer Cluster failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update regular service")
		return result, fmt.Errorf("apply service: %w", err)
	}

	// ensure the SOK defaults resources exist: a ConfigMap for structural SmartBus
	// config and a Secret for the credentials (both mounted via SPLUNK_DEFAULTS_URL)
	defaultsConfigMap, defaultsSecret, err := ensureIndexerDefaults(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "EnsureDefaultsFailed", "Failed to ensure defaults ConfigMap/Secret. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to ensure defaults ConfigMap/Secret")
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonResolveQueueObjectStorageFailed, "referenced Queue, ObjectStorage CR, or credential Secret not found", err)
		}
		return result, fmt.Errorf("ensure defaults: %w", err)
	}

	// create or update statefulset for the indexers
	statefulSet, err := getIndexerStatefulSet(ctx, client, cr, defaultsConfigMap.AsStatefulSetOption(), defaultsSecret.AsStatefulSetOption())
	if err != nil {
		eventPublisher.Warning(ctx, "GetIndexerStatefulSetFailed", "Get Indexer stateful set failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	// Note:
	// This is a temporary fix for CSPL-1880. Splunk enterprise 9.0.0 fails when we migrate from 8.2.6.
	// Splunk 9.0.0 bundle push uses encryption while transferring data. If any of the
	// splunk instances were not able to support this option, then cluster manager fails to transfer, this leads
	// to splunkd restart at the peer level. For more information refer
	// https://splunk.atlassian.net/browse/SPL-223386?jql=text%20~%20%22The%20downloaded%20bundle%20checksum%20doesn%27t%20match%20the%20activeBundleChecksum%22
	// On Operator side we have set statefulset update strategy to OnDelete, so pods need to be
	// deleted by operator manually.  Before deleting the pod, operator controller code tries to decommission
	// the splunk instance, but splunkd is not running due to above splunk enterprise 9.0.0 issue. So controller
	// fail and returns. This goes on in a loop and we always try the same pod instance and rest of the replicas
	// are still in older version
	// As a temporary fix for 9.0.0 , if the image version do not  match with pod image version we delete the
	// splunk statefulset for indexer

	var phase enterpriseApi.Phase
	versionUpgrade := false
	// get all the pods in the namespace
	statefulsetPods := &corev1.PodList{}
	opts := []rclient.ListOption{
		rclient.InNamespace(cr.GetNamespace()),
	}

	err = client.List(ctx, statefulsetPods, opts...)
	if err != nil {
		return result, nil
	}

	// filter the pods which are owned by statefulset
	for _, v := range statefulsetPods.Items {
		for _, owner := range v.GetOwnerReferences() {
			if owner.UID == statefulSet.UID {
				// get the pod image name
				if imageUpdatedTo9(v.Spec.Containers[0].Image, cr.Spec.Image) {
					// image do not match that means its image upgrade
					versionUpgrade = true
					break
				}
			}
		}
	}

	cr.Kind = "IndexerCluster"
	// CSPL-3060 - If statefulSet is not created, avoid upgrade path validation
	if !statefulSet.CreationTimestamp.IsZero() {
		// check if the IndexerCluster is ready for version upgrade
		continueReconcile, err := upgrade.UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, func(ctx context.Context) (*splclient.ClusterInfo, error) {
			if GetClusterInfoForUpgradeCall != nil {
				return GetClusterInfoForUpgradeCall(ctx, client, cr)
			}
			return GetClusterInfoCall(ctx, &mgr, false)
		})
		if err != nil || !continueReconcile {
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Upgrade path validation failed")
			} else {
				// waiting on a dependency (e.g. LicenseManager recycling) is not an error,
				// so don't leave the earlier-staged PhaseError as the persisted status
				setPhaseAndConditions(enterpriseApi.PhasePending, "Waiting for upgrade path dependency to become ready")
			}
			return result, err
		}
	}

	// check if version upgrade is set
	if !versionUpgrade {
		phase, err = mgr.UpdateWorkflow(ctx, client, statefulSet, cr.Spec.Replicas)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateFailed", "Update of stateful set failed. Check operator logs for details.")
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods")
			return result, fmt.Errorf("update statefulset: %w", err)
		}
	} else {
		// Delete the statefulset and recreate new one
		err = client.Delete(ctx, statefulSet)
		if err != nil {
			eventPublisher.Warning(ctx, "DeleteFailed", "Delete of stateful set failed. Check operator logs for details.")
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to upgrade StatefulSet")
			return result, fmt.Errorf("delete statefulset: %w", err)
		}
		time.Sleep(1 * time.Second)
		// since we are creating new statefulset, setting resourceVersion to ""
		statefulSet.ResourceVersion = ""
		phase, err = mgr.UpdateWorkflow(ctx, client, statefulSet, cr.Spec.Replicas)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateFailed", "Update of stateful set failed. Check operator logs for details.")
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update pods after upgrade")
			return result, fmt.Errorf("update statefulset: %w", err)
		}
	}
	configworkflow.GarbageCollectConfigMaps(ctx, client, cr, defaultsConfigMap.Name, statefulSet.Spec.Selector)
	configworkflow.GarbageCollectSecrets(ctx, client, cr, defaultsSecret.Name, statefulSet.Spec.Selector)
	setPhaseAndConditions(phase, "")

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {

		//update MC
		//Retrieve monitoring  console ref from CM Spec
		cmMonitoringConsoleConfigRef, err := RetrieveCMSpec(ctx, client, cr)
		if err != nil {
			eventPublisher.Warning(ctx, "RetrieveCMSpecFailed", "Retrieval of Cluster Manager spec failed. Check operator logs for details.")
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to retrieve Cluster Manager spec")
			return result, fmt.Errorf("retrieve CM spec: %w", err)
		}
		if cmMonitoringConsoleConfigRef != "" {
			namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkMonitoringConsole, cmMonitoringConsoleConfigRef)}
			_, err := k8sops.GetStatefulSetByName(ctx, client, namespacedName)
			//if MC pod already exists
			if err == nil {
				c := mgr.getMonitoringConsoleClient(cr, cmMonitoringConsoleConfigRef)
				err := c.AutomateMCApplyChanges()
				if err != nil {
					eventPublisher.Warning(ctx, "AutomateMCApplyChangesFailed", "Get Monitoring Console client failed. Check operator logs for details.")
					setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to update Monitoring Console configuration")
					return result, fmt.Errorf("automate MC apply changes: %w", err)
				}
			}
			if len(cr.Spec.MonitoringConsoleRef.Name) > 0 && (cr.Spec.MonitoringConsoleRef.Name != cmMonitoringConsoleConfigRef) {
				logger.WarnContext(ctx, "IndexerCluster CR should not specify MonitoringConsoleRef and if specified, should be similar to ClusterManager spec")
			}
		}
		if len(cr.Status.IndexerSecretChanged) > 0 {
			var managerIdxcName string
			if len(cr.Spec.ClusterManagerRef.Name) > 0 {
				managerIdxcName = cr.Spec.ClusterManagerRef.Name
			} else {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Empty Cluster Manager reference")
				return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonEmptyClusterManagerRef, "empty Cluster Manager reference", nil)
			}
			cmPodName := fmt.Sprintf("splunk-%s-cluster-manager-%s", managerIdxcName, "0")
			podExecClient := splutil.GetPodExecClient(client, cr, cmPodName)
			// Disable maintenance mode
			err = SetClusterMaintenanceMode(ctx, client, cr, false, cmPodName, podExecClient)
			if err != nil {
				eventPublisher.Warning(ctx, "ClusterMaintenanceModeFailed", "Set Cluster maintenance mode failed. Check operator logs for details.")
				setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to set Cluster Maintenance Mode")
				return result, fmt.Errorf("set cluster maintenance mode: %w", err)
			}
		}

		// Reset idxc secret changed and namespace secret revision
		cr.Status.IndexerSecretChanged = []bool{}
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion
		cr.Status.IdxcPasswordChangedSecrets = make(map[string]bool)

		result.Requeue = false
		// Set indexer cluster CR as owner reference for clustermanager
		logger.DebugContext(ctx, "setting IndexerCluster as owner for ClusterManager")
		if len(cr.Spec.ClusterManagerRef.Name) > 0 {
			namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkClusterManager, cr.Spec.ClusterManagerRef.Name)}
		}
		err = k8sops.SetStatefulSetOwnerRef(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, "SetStatefulSetOwnerRefFailed", "Set stateful set owner reference failed. Check operator logs for details.")
			setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to set StatefulSet owner reference")
			result.Requeue = true
			return result, fmt.Errorf("set statefulset owner ref: %w", err)
		}
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}
	return result, nil
}

// applyIndexerCluster reconciles an IndexerCluster using the legacy ClusterMaster API.
func applyIndexerCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplyIndexerCluster", "name", cr.GetName(), "namespace", cr.GetNamespace())

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "IndexerCluster"

	// Initialize phase and conditions
	isPaused := cr.GetAnnotations()[enterpriseApi.IndexerClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		result := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase: phase, IsPaused: isPaused, Message: message, Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = result.Phase
		cr.Status.Conditions = result.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}

	var err error
	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateIndexerClusterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "ValidateIndexerClusterSpecFailed", "Validate Indexer Cluster spec failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Indexer Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "Indexer Cluster spec validation failed", err)
	}

	// updates status after function completes
	setPhaseAndConditions(enterpriseApi.PhaseError, "")
	cr.Status.ClusterMasterPhase = enterpriseApi.PhaseError
	if cr.Status.Replicas < cr.Spec.Replicas {
		logger.InfoContext(ctx, "scaling up IndexerCluster", "previousReplicas", cr.Status.Replicas, "newReplicas", cr.Spec.Replicas)
	}
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-indexer", cr.GetName())
	if cr.Status.Peers == nil {
		cr.Status.Peers = []enterpriseApi.IndexerClusterMemberStatus{}
	}
	if cr.Status.IndexerSecretChanged == nil {
		cr.Status.IndexerSecretChanged = []bool{}
	}
	if cr.Status.IdxcPasswordChangedSecrets == nil {
		cr.Status.IdxcPasswordChangedSecrets = make(map[string]bool)
	}

	// create or update general config resources
	namespaceScopedSecret, err := k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfigFailed", "Create or update of general config failed. Check operator logs for details.")
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      cr.Spec.ClusterMasterRef.Name,
	}
	managerIdxCluster := &enterpriseApiV3.ClusterMaster{}
	err = client.Get(ctx, namespacedName, managerIdxCluster)
	if err == nil {
		// when user creates both cluster manager and index cluster yaml file at the same time
		// cluser master status is not yet set so it will be blank
		if managerIdxCluster.Status.Phase == "" {
			cr.Status.ClusterMasterPhase = enterpriseApi.PhasePending
		} else {
			cr.Status.ClusterMasterPhase = managerIdxCluster.Status.Phase
		}
	} else {
		cr.Status.ClusterMasterPhase = enterpriseApi.PhaseError
	}

	mgr := newIndexerClusterPodManager(logger, cr, namespaceScopedSecret, splclient.NewSplunkClient, client)
	// Check if we have configured enough number(<= RF) of replicas
	if mgr.cr.Status.ClusterMasterPhase == enterpriseApi.PhaseReady {
		if VerifyRFPeersCall != nil {
			err = VerifyRFPeersCall(ctx, client, cr)
		} else if VerifyRFPeers != nil {
			err = VerifyRFPeers(ctx, mgr, client)
		} else {
			err = mgr.workflowManager().VerifyRFPeers(ctx, client)
		}
		if err != nil {
			eventPublisher.Warning(ctx, "VerifyRFPeersFailed", "Verify RF peer failed. Check operator logs for details.")
			return result, fmt.Errorf("verify RF peers: %w", err)
		}
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkIndexer)

		terminating, err := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource is being deleted")
			cr.Status.ClusterMasterPhase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, "DeleteFailed", "Delete custom resource failed. Check operator logs for details.")
		}
		return result, err
	}

	// create or update a headless service for indexer cluster
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailed", "Create or update of headless service for Indexer Cluster failed. Check operator logs for details.")
		return result, fmt.Errorf("apply headless service: %w", err)
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailed", "Create or update of service for Indexer Cluster failed. Check operator logs for details.")
		return result, fmt.Errorf("apply service: %w", err)
	}

	// ensure the SOK defaults resources exist: a ConfigMap for structural SmartBus
	// config and a Secret for the credentials (both mounted via SPLUNK_DEFAULTS_URL)
	defaultsConfigMap, credentialsSecret, err := ensureIndexerDefaults(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "EnsureDefaultsFailed", "Failed to ensure defaults ConfigMap/Secret. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to ensure defaults ConfigMap/Secret")
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, splcommon.NewTerminalError(splcommon.EventReasonResolveQueueObjectStorageFailed, "referenced Queue, ObjectStorage CR, or credential Secret not found", err)
		}
		return result, fmt.Errorf("ensure defaults: %w", err)
	}

	// create or update statefulset for the indexers
	statefulSet, err := getIndexerStatefulSet(ctx, client, cr, defaultsConfigMap.AsStatefulSetOption(), credentialsSecret.AsStatefulSetOption())
	if err != nil {
		eventPublisher.Warning(ctx, "GetIndexerStatefulSetFailed", "Get Indexer stateful set failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, err
	}

	// Note:
	// This is a fix for CSPL-1880. Splunk enterprise 9.0.0 fails when we migrate from 8.2.6.
	// Splunk 9.0.0 bundle push uses encryption while transferring data. If any of the
	// splunk instances were not able to support this option, then cluster master fails to transfer, this leads
	// to splunkd restart at the peer level. For more information refer
	// https://splunk.atlassian.net/browse/SPL-223386?jql=text%20~%20%22The%20downloaded%20bundle%20checksum%20doesn%27t%20match%20the%20activeBundleChecksum%22
	// On Operator side we have set statefulset update strategy to OnDelete, so pods need to be
	// deleted by operator manually.  Before deleting the pod, operator controller code tries to decommission
	// the splunk instance, but splunkd is not running due to above splunk enterprise 9.0.0 issue. So controller
	// fail and returns. This goes on in a loop and we always try the same pod instance and rest of the replicas
	// are still in older version
	// As a fix for 9.0.0 , if the image version do not  match with pod image version we delete the
	// splunk statefulset for indexer

	var phase enterpriseApi.Phase
	versionUpgrade := false
	// get all the pods in the namespace
	statefulsetPods := &corev1.PodList{}
	opts := []rclient.ListOption{
		rclient.InNamespace(cr.GetNamespace()),
	}

	err = client.List(ctx, statefulsetPods, opts...)
	if err != nil {
		return result, nil
	}

	// filter the pods which are owned by statefulset
	for _, v := range statefulsetPods.Items {
		for _, owner := range v.GetOwnerReferences() {
			if owner.UID == statefulSet.UID {
				// get the pod image name
				if imageUpdatedTo9(v.Spec.Containers[0].Image, cr.Spec.Image) {
					// image do not match that means its image upgrade
					versionUpgrade = true
					break
				}
			}
		}
	}

	cr.Kind = "IndexerCluster"
	// CSPL-3060 - If statefulSet is not created, avoid upgrade path validation
	if !statefulSet.CreationTimestamp.IsZero() {
		// check if the IndexerCluster is ready for version upgrade
		continueReconcile, err := upgrade.UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, func(ctx context.Context) (*splclient.ClusterInfo, error) {
			if GetClusterInfoForUpgradeCall != nil {
				return GetClusterInfoForUpgradeCall(ctx, client, cr)
			}
			return GetClusterInfoCall(ctx, &mgr, false)
		})
		if err != nil || !continueReconcile {
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Upgrade path validation failed")
			} else {
				// waiting on a dependency (e.g. ClusterManager recycling) is not an error,
				// so don't leave the earlier-staged PhaseError as the persisted status
				setPhaseAndConditions(enterpriseApi.PhasePending, "Waiting for upgrade path dependency to become ready")
			}
			return result, err
		}
	}

	// check if version upgrade is set
	if !versionUpgrade {
		phase, err = mgr.UpdateWorkflow(ctx, client, statefulSet, cr.Spec.Replicas)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateFailed", "Update of stateful set failed. Check operator logs for details.")
			return result, fmt.Errorf("update statefulset: %w", err)
		}
	} else {
		// Delete the statefulset and recreate new one
		err = client.Delete(ctx, statefulSet)
		if err != nil {
			eventPublisher.Warning(ctx, "DeleteFailed", "Delete of stateful set failed. Check operator logs for details.")
			return result, fmt.Errorf("delete statefulset: %w", err)
		}
		time.Sleep(1 * time.Second)
		// since we are creating new statefulset, setting resourceVersion to ""
		statefulSet.ResourceVersion = ""
		phase, err = mgr.UpdateWorkflow(ctx, client, statefulSet, cr.Spec.Replicas)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateFailed", "Update of stateful set failed. Check operator logs for details.")
			return result, fmt.Errorf("update statefulset: %w", err)
		}
	}
	configworkflow.GarbageCollectConfigMaps(ctx, client, cr, defaultsConfigMap.Name, statefulSet.Spec.Selector)
	configworkflow.GarbageCollectSecrets(ctx, client, cr, credentialsSecret.Name, statefulSet.Spec.Selector)
	setPhaseAndConditions(phase, "")

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//update MC
		//Retrieve monitoring  console ref from CM Spec
		cmMonitoringConsoleConfigRef, err := RetrieveCMSpec(ctx, client, cr)
		if err != nil {
			eventPublisher.Warning(ctx, "RetrieveCMSpecFailed", "Retrieve Cluster Master spec failed. Check operator logs for details.")
			return result, fmt.Errorf("retrieve CM spec: %w", err)
		}
		if cmMonitoringConsoleConfigRef != "" {
			namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkMonitoringConsole, cmMonitoringConsoleConfigRef)}
			_, err := k8sops.GetStatefulSetByName(ctx, client, namespacedName)
			//if MC pod already exists
			if err == nil {
				c := mgr.getMonitoringConsoleClient(cr, cmMonitoringConsoleConfigRef)
				err := c.AutomateMCApplyChanges()
				if err != nil {
					eventPublisher.Warning(ctx, "AutomateMCApplyChangesFailed", "Automate MC Apply Changes failed. Check operator logs for details.")
					return result, fmt.Errorf("automate MC apply changes: %w", err)
				}
			}
			if len(cr.Spec.MonitoringConsoleRef.Name) > 0 && (cr.Spec.MonitoringConsoleRef.Name != cmMonitoringConsoleConfigRef) {
				logger.WarnContext(ctx, "IndexerCluster CR should not specify MonitoringConsoleRef and if specified, should be similar to ClusterMaster spec")
			}
		}
		if len(cr.Status.IndexerSecretChanged) > 0 {
			var managerIdxcName string
			if len(cr.Spec.ClusterMasterRef.Name) > 0 {
				managerIdxcName = cr.Spec.ClusterMasterRef.Name
			} else {
				return result, errors.New("empty Cluster Master reference")
			}
			cmPodName := fmt.Sprintf("splunk-%s-cluster-master-%s", managerIdxcName, "0")
			podExecClient := splutil.GetPodExecClient(client, cr, cmPodName)
			// Disable maintenance mode
			err = SetClusterMaintenanceMode(ctx, client, cr, false, cmPodName, podExecClient)
			if err != nil {
				eventPublisher.Warning(ctx, "SetClusterMaintenanceModeFailed", "Set Cluster Master maintenance mode failed. Check operator logs for details.")
				return result, fmt.Errorf("set cluster maintenance mode: %w", err)
			}
		}

		// Reset idxc secret changed and namespace secret revision
		cr.Status.IndexerSecretChanged = []bool{}
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion
		cr.Status.IdxcPasswordChangedSecrets = make(map[string]bool)

		result.Requeue = false
		// Set indexer cluster CR as owner reference for clustermaster
		logger.DebugContext(ctx, "setting IndexerCluster as owner for ClusterMaster")
		namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkClusterMaster, cr.Spec.ClusterMasterRef.Name)}
		err = k8sops.SetStatefulSetOwnerRef(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, "SetStatefulSetOwnerRefFailed", "Set stateful set owner reference failed. Check operator logs for details.")
			result.Requeue = true
			return result, fmt.Errorf("set statefulset owner ref: %w", err)
		}
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}
	return result, nil
}

// VerifyRFPeers function pointer to mock
var VerifyRFPeers func(context.Context, indexerClusterPodManager, splcommon.ControllerClient) error

// VerifyRFPeersCall is the public reconcile seam for RF validation. The
// manager-shaped VerifyRFPeers seam remains for tests in this package.
var VerifyRFPeersCall func(context.Context, splcommon.ControllerClient, *enterpriseApi.IndexerCluster) error

// GetClusterInfoCall is a narrow seam for the indexer upgrade workflow.
var GetClusterInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager, mockCall bool) (*splclient.ClusterInfo, error) {
	return mgr.getClusterManagerClient(ctx).GetClusterInfo(false)
}

// GetClusterInfoForUpgradeCall is the public reconcile seam for upgrade
// validation. It preserves the manager-shaped seam for package-local tests.
var GetClusterInfoForUpgradeCall func(context.Context, splcommon.ControllerClient, *enterpriseApi.IndexerCluster) (*splclient.ClusterInfo, error)

// GetClusterManagerInfoForReconcileCall and GetClusterManagerPeersForReconcileCall
// are public seams for callers that exercise the IndexerCluster workflow outside
// this package. Package-local tests continue to use the manager-shaped seams.
var GetClusterManagerInfoForReconcileCall func(context.Context, splcommon.ControllerClient, *enterpriseApi.IndexerCluster) (*splclient.ClusterManagerInfo, error)

var GetClusterManagerPeersForReconcileCall func(context.Context, splcommon.ControllerClient, *enterpriseApi.IndexerCluster) (map[string]splclient.ClusterManagerPeerInfo, error)

// indexerClusterPodManager is used to manage the pods within an indexer cluster
type indexerClusterPodManager struct {
	c               splcommon.ControllerClient
	log             *slog.Logger
	cr              *enterpriseApi.IndexerCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// newIndexerClusterPodManager function to create pod manager this is added to write unit test case
var newIndexerClusterPodManager = func(log *slog.Logger, cr *enterpriseApi.IndexerCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc, c splcommon.ControllerClient) indexerClusterPodManager {
	return indexerClusterPodManager{
		log:             log,
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
		c:               c,
	}
}

// workflowManager adapts the legacy test seams to the workflow-owned manager.
// Production reconciliation uses this adapter; the local manager methods below
// remain only as compatibility shims for package-local tests during migration.
func (mgr *indexerClusterPodManager) workflowManager() *indexerworkflow.PodManager {
	workflowMgr := &indexerworkflow.PodManager{
		Client:          mgr.c,
		Log:             mgr.log,
		CR:              mgr.cr,
		Secrets:         mgr.secrets,
		NewSplunkClient: mgr.newSplunkClient,
	}
	if GetClusterManagerInfoForReconcileCall != nil {
		workflowMgr.GetManagerInfo = func(ctx context.Context, _ *indexerworkflow.PodManager) (*splclient.ClusterManagerInfo, error) {
			return GetClusterManagerInfoForReconcileCall(ctx, mgr.c, mgr.cr)
		}
	} else if GetClusterManagerInfoCall != nil {
		workflowMgr.GetManagerInfo = func(ctx context.Context, _ *indexerworkflow.PodManager) (*splclient.ClusterManagerInfo, error) {
			return GetClusterManagerInfoCall(ctx, mgr)
		}
	}
	if GetClusterManagerPeersForReconcileCall != nil {
		workflowMgr.GetManagerPeers = func(ctx context.Context, _ *indexerworkflow.PodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
			return GetClusterManagerPeersForReconcileCall(ctx, mgr.c, mgr.cr)
		}
	} else if GetClusterManagerPeersCall != nil {
		workflowMgr.GetManagerPeers = func(ctx context.Context, _ *indexerworkflow.PodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
			return GetClusterManagerPeersCall(ctx, mgr)
		}
	}
	return workflowMgr
}

func (mgr *indexerClusterPodManager) UpdateWorkflow(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {

	var err error

	// Get event publisher from context
	eventPublisher := k8sops.GetEventPublisher(ctx, mgr.cr)

	// Track previous ready replicas for scaling events
	previousReadyReplicas := mgr.cr.Status.ReadyReplicas

	// Assign client
	if mgr.c == nil {
		mgr.c = c
	}
	// update statefulset, if necessary
	if mgr.cr.Status.ClusterManagerPhase != enterpriseApi.PhaseReady && mgr.cr.Status.ClusterMasterPhase != enterpriseApi.PhaseReady {
		mgr.log.InfoContext(ctx, "ClusterManager is not ready yet", "error", err)
		return enterpriseApi.PhaseError, err
	}
	_, err = k8sops.ApplyStatefulSet(ctx, mgr.c, statefulSet)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}

	// Get the podExecClient with empty targetPodName.
	// This will be set inside ApplyIdxcSecret
	podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, "")
	// Check if a recycle of idxc pods is necessary(due to idxc_secret mismatch with CM)
	if err := ApplyIdxcSecret(ctx, mgr, desiredReplicas, podExecClient); err != nil {
		return enterpriseApi.PhaseError, err
	}
	if err := mgr.updateStatus(ctx, statefulSet); err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.IndexingReady || !mgr.cr.Status.ServiceReady {
		if terminalErr := k8sops.CheckPodsForTerminalFailures(ctx, c, statefulSet); terminalErr != nil {
			mgr.log.ErrorContext(ctx, "terminal pod failure detected; setting PhaseError", "error", terminalErr)
			return enterpriseApi.PhaseError, terminalErr
		}
		mgr.log.InfoContext(ctx, "IndexerCluster is not ready", "error ", err)
		return enterpriseApi.PhasePending, nil
	}

	// manage scaling and updates
	phase, err := k8sops.UpdateStatefulSetPods(ctx, c, statefulSet, mgr, desiredReplicas)
	if err != nil {
		return phase, err
	}
	if phase == enterpriseApi.PhaseReady && mgr.cr.Status.ReadyReplicas == desiredReplicas && previousReadyReplicas != desiredReplicas && eventPublisher != nil {
		if desiredReplicas > previousReadyReplicas {
			eventPublisher.Normal(ctx, "ScaledUp", fmt.Sprintf("Successfully scaled %s up from %d to %d replicas", mgr.cr.GetName(), previousReadyReplicas, desiredReplicas))
		} else if desiredReplicas < previousReadyReplicas {
			eventPublisher.Normal(ctx, "ScaledDown", fmt.Sprintf("Successfully scaled %s down from %d to %d replicas", mgr.cr.GetName(), previousReadyReplicas, desiredReplicas))
		}
	}
	return phase, nil
}

// Update delegates the stateful multi-step workflow to workflow/indexercluster.
// It remains on this adapter because k8sops.UpdateStatefulSetPods consumes the
// pod-manager interface and package-local tests still construct this adapter.
func (mgr *indexerClusterPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	return mgr.UpdateWorkflow(ctx, c, statefulSet, desiredReplicas)
}

// getMonitoringConsoleClient for indexerClusterPodManager returns a SplunkClient for monitoring console
func (mgr *indexerClusterPodManager) getMonitoringConsoleClient(cr *enterpriseApi.IndexerCluster, cmMonitoringConsoleConfigRef string) *splclient.SplunkClient {
	return mgr.workflowManager().GetMonitoringConsoleClient(cr, cmMonitoringConsoleConfigRef)
}

// SetClusterMaintenanceMode enables/disables cluster maintenance mode
func SetClusterMaintenanceMode(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, enable bool, cmPodName string, podExecClient splutil.PodExecClientImpl) error {
	return indexerworkflow.SetClusterMaintenanceMode(ctx, c, cr, enable, cmPodName, podExecClient, func(state bool) {
		cr.Status.MaintenanceMode = state
	})
}

// ApplyIdxcSecret synchronizes the namespace idxc_secret with indexer peers.
func ApplyIdxcSecret(ctx context.Context, mgr *indexerClusterPodManager, replicas int32, podExecClient splutil.PodExecClientImpl) error {
	var indIdxcSecret string

	// Get event publisher from context
	eventPublisher := k8sops.GetEventPublisher(ctx, mgr.cr)

	// Get namespace scoped secret
	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mgr.c, mgr.cr.GetNamespace())
	if err != nil {
		return err
	}

	logger := slog.With("func", "ApplyIdxcSecret", "name", mgr.cr.GetName(), "namespace", mgr.cr.GetNamespace())
	logger.InfoContext(ctx, "applying idxc secret to indexers", "desiredReplicas", replicas, "idxcSecretChanged", mgr.cr.Status.IndexerSecretChanged, "crStatusNamespaceSecretResourceVersion", mgr.cr.Status.NamespaceSecretResourceVersion, "namespaceSecretResourceVersion", namespaceSecret.GetObjectMeta().GetResourceVersion())

	// If namespace scoped secret revision is the same ignore
	if len(mgr.cr.Status.NamespaceSecretResourceVersion) == 0 {
		// First time, set resource version in CR
		mgr.cr.Status.NamespaceSecretResourceVersion = namespaceSecret.ObjectMeta.ResourceVersion
		logger.DebugContext(ctx, "setting CrStatusNamespaceSecretResourceVersion for the first time")
		return nil
	} else if mgr.cr.Status.NamespaceSecretResourceVersion == namespaceSecret.ObjectMeta.ResourceVersion {
		// If resource version hasn't changed don't return
		return nil
	}

	logger.InfoContext(ctx, "namespaced scoped secret revision has changed")

	// Retrieve idxc_secret password from secret data
	nsIdxcSecret := string(namespaceSecret.Data[splcommon.IdxcSecret])

	// Log configuration push start
	pushStartTime := time.Now()
	logger.InfoContext(ctx, "starting configuration push to peers", "peerCount", replicas, "configVersion", namespaceSecret.ObjectMeta.ResourceVersion)

	// Loop over all indexer pods and get individual pod's idxc password
	howManyPodsHaveSecretChanged := 0
	for i := int32(0); i <= replicas-1; i++ {
		// Get Indexer's name
		indexerPodName := splutil.GetSplunkStatefulsetPodName(splcommon.SplunkIndexer, mgr.cr.GetName(), i)
		pod := &corev1.Pod{}

		// Check if pod exists before updating secrets
		namespacedName := types.NamespacedName{Namespace: mgr.cr.GetNamespace(), Name: indexerPodName}
		logger.DebugContext(ctx, "check if pod is created before updating its secrets")
		err := mgr.c.Get(ctx, namespacedName, pod)
		if err != nil {
			logger.WarnContext(ctx, "peer doesn't exists", "peerName", indexerPodName)
			continue
		}

		// Retrieve secret from pod
		podSecret, err := splutil.GetSecretFromPod(ctx, mgr.c, indexerPodName, mgr.cr.GetNamespace())
		if err != nil {
			return fmt.Errorf(splcommon.PodSecretNotFoundError, indexerPodName)
		}
		// Retrieve idxc_secret token
		if indIdxcSecretByte, ok := podSecret.Data[splcommon.IdxcSecret]; ok {
			indIdxcSecret = string(indIdxcSecretByte)
		} else {
			return fmt.Errorf(splcommon.SecretTokenNotRetrievable, splcommon.IdxcSecret)
		}

		if indIdxcSecret == nsIdxcSecret {
			continue
		}
		logger.InfoContext(ctx, "IDXC Secret is different from namespace scoped secret")

		// Enable maintenance mode
		if len(mgr.cr.Status.IndexerSecretChanged) == 0 && !mgr.cr.Status.MaintenanceMode {
			var managerIdxcName string
			var cmPodName string
			if len(mgr.cr.Spec.ClusterManagerRef.Name) > 0 {
				managerIdxcName = mgr.cr.Spec.ClusterManagerRef.Name
				cmPodName = fmt.Sprintf("splunk-%s-cluster-manager-%s", managerIdxcName, "0")
			} else if len(mgr.cr.Spec.ClusterMasterRef.Name) > 0 {
				managerIdxcName = mgr.cr.Spec.ClusterMasterRef.Name
				cmPodName = fmt.Sprintf("splunk-%s-cluster-master-%s", managerIdxcName, "0")
			} else {
				return errors.New("empty cluster manager reference")
			}
			podExecClient.SetTargetPodName(ctx, cmPodName)
			err = SetClusterMaintenanceMode(ctx, mgr.c, mgr.cr, true, cmPodName, podExecClient)
			if err != nil {
				return err
			}
			logger.InfoContext(ctx, "set CM in maintenance mode")
		}

		// If idxc secret already changed, ignore
		if i < int32(len(mgr.cr.Status.IndexerSecretChanged)) && mgr.cr.Status.IndexerSecretChanged[i] {
			continue
		}

		// Get client for indexer Pod
		idxcClient := mgr.getClient(ctx, i)

		// Change idxc secret key
		if err := idxcClient.SetIdxcSecret(nsIdxcSecret); err != nil {
			// Emit event for password sync failure
			if eventPublisher != nil {
				eventPublisher.Warning(ctx, "PasswordSyncFailed", fmt.Sprintf("Password sync failed for pod '%s'. Check operator logs for details.", indexerPodName))
			}
			mgr.log.ErrorContext(ctx, "configuration push failed", "failedPeer", indexerPodName, "error", err.Error())
			return err
		}
		logger.InfoContext(ctx, "changed idxc secret")

		howManyPodsHaveSecretChanged += 1

		// Restart splunk instance on pod
		if err := idxcClient.RestartSplunk(); err != nil {
			// Emit event for password sync failure
			if eventPublisher != nil {
				eventPublisher.Warning(ctx, "PasswordSyncFailed", fmt.Sprintf("Password sync failed for pod '%s'. Check operator logs for details.", indexerPodName))
			}
			return fmt.Errorf("configuration push failed during restart for peer %s: %w", indexerPodName, err)
		}
		logger.InfoContext(ctx, "restarted splunk")

		// Keep a track of all the secrets on pods to change their idxc secret below
		mgr.cr.Status.IdxcPasswordChangedSecrets[podSecret.GetName()] = true

		// Set the idxc_secret changed flag to true
		if i < int32(len(mgr.cr.Status.IndexerSecretChanged)) {
			mgr.cr.Status.IndexerSecretChanged[i] = true
		} else {
			mgr.cr.Status.IndexerSecretChanged = append(mgr.cr.Status.IndexerSecretChanged, true)
		}
	}

	/*
		During the recycle of indexer pods due to an idxc secret change, if there is a container
		restart(for example if the splunkd process dies) before the operator
		deletes the pod, the container restart fails due to mismatch of idxc password between Cluster
		manager and that particular indexer.

		Changing the idxc passwords on the secrets mounted on the indexer pods to avoid the above.
	*/
	for podSecretName, changed := range mgr.cr.Status.IdxcPasswordChangedSecrets {
		if !changed {
			continue
		}
		podSecret, err := splutil.GetSecretByName(ctx, mgr.c, mgr.cr.GetNamespace(), podSecretName)
		if err != nil {
			return fmt.Errorf("could not read secret %s, reason - %v", podSecretName, err)
		}
		splunkReadableData, err := splutil.GetSplunkReadableNamespaceScopedSecretData(ctx, mgr.c, mgr.cr.GetNamespace())
		if err != nil {
			return err
		}
		podSecret.Data[splcommon.IdxcSecret] = splunkReadableData[splcommon.IdxcSecret]
		podSecret.Data["default.yml"] = splunkReadableData["default.yml"]
		if _, err := k8sops.ApplySecret(ctx, mgr.c, podSecret); err != nil {
			return err
		}
		logger.InfoContext(ctx, "IDXC password changed on the secret mounted on pod", "podSecretName", podSecretName)

		// Set to false marking the idxc password change in the secret
		mgr.cr.Status.IdxcPasswordChangedSecrets[podSecretName] = false
	}

	// Emit event for password sync completed
	if eventPublisher != nil {
		eventPublisher.Normal(ctx, "PasswordSyncCompleted", fmt.Sprintf("Password synchronized for %d pods", howManyPodsHaveSecretChanged))
	}

	// Log configuration push completion
	logger.InfoContext(ctx, "configuration push completed", "successCount", howManyPodsHaveSecretChanged, "duration", time.Since(pushStartTime))
	return nil
}

// PrepareScaleDown prepares an indexer pod for removal through the workflow package.
func (mgr *indexerClusterPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	return mgr.workflowManager().PrepareScaleDown(ctx, n)
}

func (mgr *indexerClusterPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	return mgr.workflowManager().PrepareRecycle(ctx, n)
}

func (mgr *indexerClusterPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	return mgr.workflowManager().FinishUpgrade(ctx, n)
}

func (mgr *indexerClusterPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	return mgr.workflowManager().FinishRecycle(ctx, n)
}

func (mgr *indexerClusterPodManager) decommission(ctx context.Context, n int32, enforceCounts bool) (bool, error) {
	return mgr.workflowManager().Decommission(ctx, n, enforceCounts)
}

func (mgr *indexerClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	return mgr.workflowManager().GetClient(ctx, n)
}

func (mgr *indexerClusterPodManager) getClusterManagerClient(ctx context.Context) *splclient.SplunkClient {
	return mgr.workflowManager().GetClusterManagerClient(ctx)
}

func (mgr *indexerClusterPodManager) verifyRFPeers(ctx context.Context, c splcommon.ControllerClient) error {
	return mgr.workflowManager().VerifyRFPeers(ctx, c)
}

var GetClusterManagerInfoCall func(context.Context, *indexerClusterPodManager) (*splclient.ClusterManagerInfo, error)

var GetClusterManagerPeersCall func(context.Context, *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error)

func (mgr *indexerClusterPodManager) updateStatus(ctx context.Context, statefulSet *appsv1.StatefulSet) error {
	return mgr.workflowManager().UpdateStatus(ctx, statefulSet)
}

// ensureIndexerDefaults resolves the IndexerCluster's SmartBus queue/object-storage
// configuration once and ensures both SOK defaults resources exist:
//   - a content-addressed ConfigMap holding the structural SmartBus config, and
//   - a content-addressed Secret holding only the credentials (access_key/secret_key).
//
// Both are immutable and mounted into every container via SPLUNK_DEFAULTS_URL.
// Returns a zero-value DefaultsConfigMap when smartbus is not configured, and a zero-value
// DefaultsSecret when no static credentials were resolved (e.g. IRSA / workload identity,
// where the Queue VolList is empty). Resolving once guarantees
// the ConfigMap and Secret are derived from a single consistent read of the source
// queue/storage/secret.
func ensureIndexerDefaults(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (resources.DefaultsConfigMap, resources.DefaultsSecret, error) {
	queueRefName := ""
	if cr.Spec.QueueRef != nil {
		queueRefName = cr.Spec.QueueRef.Name
	}
	osRefName := ""
	if cr.Spec.ObjectStorageRef != nil {
		osRefName = cr.Spec.ObjectStorageRef.Name
	}
	if queueRefName == "" && osRefName == "" {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, nil
	}
	var queueRef, osRef corev1.ObjectReference
	if cr.Spec.QueueRef != nil {
		queueRef = *cr.Spec.QueueRef
	}
	if cr.Spec.ObjectStorageRef != nil {
		osRef = *cr.Spec.ObjectStorageRef
	}
	qosCfg, err := configworkflow.ResolveQueueAndObjectStorage(ctx, c, cr, queueRef, osRef)
	if err != nil {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, fmt.Errorf("resolve queue config: %w", err)
	}
	structuralEntries, credentialEntries, err := splunkconfig.BuildSmartBusConfig(&qosCfg.Queue, &qosCfg.OS, qosCfg.AccessKey, qosCfg.SecretKey)
	if err != nil {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
	}

	owner := splcommon.AsOwner(cr, true)

	var configMap resources.DefaultsConfigMap
	if entries := structuralEntries; len(entries) > 0 {
		configMap, err = configworkflow.EnsureConfigMap(ctx, c, cr, entries, &owner)
		if err != nil {
			return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
		}
	}

	var secret resources.DefaultsSecret
	if entries := credentialEntries; len(entries) > 0 {
		secret, err = configworkflow.EnsureSecret(ctx, c, cr, entries, &owner)
		if err != nil {
			return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
		}
	}

	return configMap, secret, nil
}

// getIndexerStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise indexers.
func getIndexerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, reconcileutil.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNames(splcommon.SplunkIndexer, cr.GetName(), cr.GetNamespace(), cr.Spec.Replicas)))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	// Note: SPLUNK_INDEXER_URL is not used by the indexer pod containers,
	// hence avoided the call to getIndexerExtraEnv.
	// If other indexer CR specific env variables are required:
	// 1. Introduce the new env variables in the function getIndexerExtraEnv
	// 2. Avoid SPLUNK_INDEXER_URL in getIndexerExtraEnv for idxc CR
	// 3. Re-introduce the call to getIndexerExtraEnv here.
	statefulSet, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer, cr.Spec.Replicas, make([]corev1.EnvVar, 0), opts...)
	if err != nil {
		return nil, err
	}
	certs.InjectCertMounts(&statefulSet.Spec.Template, certMounts)
	return statefulSet, nil
}

// validateIndexerClusterSpec checks validity and makes default updates to a IndexerClusterSpec, and returns error if something is wrong.
func validateIndexerClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) error {
	// We cannot have 0 replicas in IndexerCluster spec, since this refers to number of indexers in an indexer cluster
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}

	// queueRef and objectStorageRef are both-or-neither: if one name is set the other must be too
	queueRefName := ""
	if cr.Spec.QueueRef != nil {
		queueRefName = cr.Spec.QueueRef.Name
	}
	osRefName := ""
	if cr.Spec.ObjectStorageRef != nil {
		osRefName = cr.Spec.ObjectStorageRef.Name
	}
	if (queueRefName == "") != (osRefName == "") {
		return fmt.Errorf("queueRef and objectStorageRef must both be set or both be empty")
	}

	// Cannot leave clusterManagerRef field empty or else we cannot connect to CM
	if len(cr.Spec.ClusterManagerRef.Name) == 0 && len(cr.Spec.ClusterMasterRef.Name) == 0 {
		return fmt.Errorf("IndexerCluster spec should refer to ClusterManager via clusterManagerRef")
	}

	// Multisite / multipart clusters: can't reference a cluster manager located in another namespace because of Service and Secret limitations
	if len(cr.Spec.ClusterManagerRef.Namespace) > 0 && cr.Spec.ClusterManagerRef.Namespace != cr.GetNamespace() ||
		len(cr.Spec.ClusterMasterRef.Namespace) > 0 && cr.Spec.ClusterMasterRef.Namespace != cr.GetNamespace() {
		return fmt.Errorf("multisite cluster does not support cluster manager to be located in a different namespace")
	}

	return reconcileutil.ValidateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// RetrieveCMSpec finds monitoringConsole ref from cm spec
func RetrieveCMSpec(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (string, error) {
	if len(cr.Spec.ClusterMasterRef.Name) > 0 && len(cr.Spec.ClusterManagerRef.Name) == 0 {
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.ClusterMasterRef.Name}
		var cmCR enterpriseApiV3.ClusterMaster
		err := client.Get(ctx, namespacedName, &cmCR)
		if err == nil {
			return cmCR.Spec.MonitoringConsoleRef.Name, nil
		}
	} else if len(cr.Spec.ClusterManagerRef.Name) > 0 && len(cr.Spec.ClusterMasterRef.Name) == 0 {
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.ClusterManagerRef.Name}
		var cmCR enterpriseApi.ClusterManager
		err := client.Get(ctx, namespacedName, &cmCR)
		if err == nil {
			return cmCR.Spec.MonitoringConsoleRef.Name, nil
		}
	}

	return "", nil
}
