// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	splunkconfig "github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NewSplunkClientFunc function pointer type
type NewSplunkClientFunc func(managementURI, username, password string) *splclient.SplunkClient

// ApplyIndexerClusterManager reconciles the state of a Splunk Enterprise indexer cluster.
func ApplyIndexerClusterManager(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	logger := logging.FromContext(ctx).With("func", "ApplyIndexerClusterManager", "name", cr.GetName(), "namespace", cr.GetNamespace())

	eventPublisher := GetEventPublisher(ctx, cr)
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
		return reconcile.Result{}, splcommon.NewTerminalError(EventReasonValidateSpecFailed, "Indexer Cluster spec validation failed", err)
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
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
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
		err = VerifyRFPeers(ctx, mgr, client)
		if err != nil {
			eventPublisher.Warning(ctx, "VerifyRFPeersFailed", "Verification of RF peer failed. Check operator logs for details.")
			setPhaseAndConditions(enterpriseApi.PhaseError, "Replication factor peer verification failed")
			return result, fmt.Errorf("verify RF peers: %w", err)
		}
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkIndexer)

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
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
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailed", "Create or update of headless service for Indexer Cluster failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update headless service")
		return result, fmt.Errorf("apply headless service: %w", err)
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, false))
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
			return reconcile.Result{}, splcommon.NewTerminalError(EventReasonResolveQueueObjectStorageFailed, "referenced Queue or ObjectStorage CR not found", err)
		}
		return result, fmt.Errorf("ensure defaults: %w", err)
	}

	// create or update statefulset for the indexers
	statefulSet, err := getIndexerStatefulSet(ctx, client, cr, defaultsConfigMap.AsStatefulSetOption(), defaultsSecret.AsStatefulSetOption())
	if err != nil {
		eventPublisher.Warning(ctx, "GetIndexerStatefulSetFailed", "Get Indexer stateful set failed. Check operator logs for details.")
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
		return result, fmt.Errorf("get indexer statefulset: %w", err)
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
		continueReconcile, err := UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, &mgr)
		if err != nil || !continueReconcile {
			if err != nil {
				setPhaseAndConditions(enterpriseApi.PhaseError, "Upgrade path validation failed")
			}
			return result, err
		}
	}

	// check if version upgrade is set
	if !versionUpgrade {
		phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
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
		phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
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
			namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cmMonitoringConsoleConfigRef)}
			_, err := splctrl.GetStatefulSetByName(ctx, client, namespacedName)
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
				return reconcile.Result{}, splcommon.NewTerminalError(EventReasonEmptyClusterManagerRef, "empty Cluster Manager reference", nil)
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
			namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkClusterManager, cr.Spec.ClusterManagerRef.Name)}
		}
		err = splctrl.SetStatefulSetOwnerRef(ctx, client, cr, namespacedName)
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

// ApplyIndexerCluster reconciles the state of a Splunk Enterprise indexer cluster for Older CM CRDs.
func ApplyIndexerCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplyIndexerCluster", "name", cr.GetName(), "namespace", cr.GetNamespace())

	eventPublisher := GetEventPublisher(ctx, cr)
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
		return reconcile.Result{}, splcommon.NewTerminalError(EventReasonValidateSpecFailed, "Indexer Cluster spec validation failed", err)
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
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
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
		err = VerifyRFPeers(ctx, mgr, client)
		if err != nil {
			eventPublisher.Warning(ctx, "VerifyRFPeersFailed", "Verify RF peer failed. Check operator logs for details.")
			return result, fmt.Errorf("verify RF peers: %w", err)
		}
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		DeleteOwnerReferencesForResources(ctx, client, cr, SplunkIndexer)

		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
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
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyServiceFailed", "Create or update of headless service for Indexer Cluster failed. Check operator logs for details.")
		return result, fmt.Errorf("apply headless service: %w", err)
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, false))
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
			return reconcile.Result{}, splcommon.NewTerminalError(EventReasonResolveQueueObjectStorageFailed, "referenced Queue or ObjectStorage CR not found", err)
		}
		return result, fmt.Errorf("ensure defaults: %w", err)
	}

	// create or update statefulset for the indexers
	statefulSet, err := getIndexerStatefulSet(ctx, client, cr, defaultsConfigMap.AsStatefulSetOption(), credentialsSecret.AsStatefulSetOption())
	if err != nil {
		eventPublisher.Warning(ctx, "GetIndexerStatefulSetFailed", "Get Indexer stateful set failed. Check operator logs for details.")
		return result, fmt.Errorf("get indexer statefulset: %w", err)
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
		continueReconcile, err := UpgradePathValidation(ctx, client, cr, cr.Spec.CommonSplunkSpec, &mgr)
		if err != nil || !continueReconcile {
			return result, err
		}
	}

	// check if version upgrade is set
	if !versionUpgrade {
		phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
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
		phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
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
			namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cmMonitoringConsoleConfigRef)}
			_, err := splctrl.GetStatefulSetByName(ctx, client, namespacedName)
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
		namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkClusterMaster, cr.Spec.ClusterMasterRef.Name)}
		err = splctrl.SetStatefulSetOwnerRef(ctx, client, cr, namespacedName)
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
var VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
	return mgr.verifyRFPeers(ctx, client)
}

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

// getMonitoringConsoleClient for indexerClusterPodManager returns a SplunkClient for monitoring console
func (mgr *indexerClusterPodManager) getMonitoringConsoleClient(cr *enterpriseApi.IndexerCluster, cmMonitoringConsoleConfigRef string) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), splcommon.GetSplunkServiceName(SplunkMonitoringConsole, cmMonitoringConsoleConfigRef, false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// SetClusterMaintenanceMode enables/disables cluster maintenance mode
func SetClusterMaintenanceMode(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, enable bool, cmPodName string, podExecClient splutil.PodExecClientImpl) error {
	// Retrieve admin password from Pod
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, c, cmPodName, cr.GetNamespace(), "password")
	if err != nil {
		return err
	}

	var command string
	if enable {
		command = fmt.Sprintf("/opt/splunk/bin/splunk enable maintenance-mode --answer-yes -auth admin:%s", adminPwd)
	} else {
		command = fmt.Sprintf("/opt/splunk/bin/splunk disable maintenance-mode --answer-yes -auth admin:%s", adminPwd)
	}
	streamOptions := splutil.NewStreamOptionsObject(command)

	_, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil {
		return err
	}

	// Set cluster manager maintenance mode
	if enable {
		cr.Status.MaintenanceMode = true
	} else {
		cr.Status.MaintenanceMode = false
	}

	return nil
}

// ApplyIdxcSecret checks if any of the indexer's have a different idxc_secret from namespace scoped secret and changes it
func ApplyIdxcSecret(ctx context.Context, mgr *indexerClusterPodManager, replicas int32, podExecClient splutil.PodExecClientImpl) error {
	var indIdxcSecret string

	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, mgr.cr)

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
		indexerPodName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), i)

		// Check if pod exists before updating secrets
		pod := &corev1.Pod{}
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

		// If idxc secret is different from namespace scoped secret change it
		if indIdxcSecret != nsIdxcSecret {
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
			if i < int32(len(mgr.cr.Status.IndexerSecretChanged)) {
				if mgr.cr.Status.IndexerSecretChanged[i] {
					continue
				}
			}

			// Get client for indexer Pod
			idxcClient := mgr.getClient(ctx, i)

			// Change idxc secret key
			err = idxcClient.SetIdxcSecret(nsIdxcSecret)
			if err != nil {
				// Emit event for password sync failure
				if eventPublisher != nil {
					eventPublisher.Warning(ctx, "PasswordSyncFailed",
						fmt.Sprintf("Password sync failed for pod '%s'. Check operator logs for details.", indexerPodName))
				}
				mgr.log.ErrorContext(ctx, "configuration push failed", "failedPeer", indexerPodName, "error", err.Error())
				return err
			}
			logger.InfoContext(ctx, "changed idxc secret")

			howManyPodsHaveSecretChanged += 1

			// Restart splunk instance on pod
			err = idxcClient.RestartSplunk()
			if err != nil {
				// Emit event for password sync failure
				if eventPublisher != nil {
					eventPublisher.Warning(ctx, "PasswordSyncFailed",
						fmt.Sprintf("Password sync failed for pod '%s'. Check operator logs for details.", indexerPodName))
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
	}

	/*
		During the recycle of indexer pods due to an idxc secret change, if there is a container
		restart(for example if the splunkd process dies) before the operator
		deletes the pod, the container restart fails due to mismatch of idxc password between Cluster
		manager and that particular indexer.

		Changing the idxc passwords on the secrets mounted on the indexer pods to avoid the above.
	*/
	if len(mgr.cr.Status.IdxcPasswordChangedSecrets) > 0 {
		for podSecretName := range mgr.cr.Status.IdxcPasswordChangedSecrets {
			if mgr.cr.Status.IdxcPasswordChangedSecrets[podSecretName] {
				podSecret, err := splutil.GetSecretByName(ctx, mgr.c, mgr.cr.GetNamespace(), podSecretName)
				if err != nil {
					return fmt.Errorf("could not read secret %s, reason - %v", podSecretName, err)
				}

				// Retrieve namespaced scoped secret data in splunk readable format
				splunkReadableData, err := splutil.GetSplunkReadableNamespaceScopedSecretData(ctx, mgr.c, mgr.cr.GetNamespace())
				if err != nil {
					return err
				}

				podSecret.Data[splcommon.IdxcSecret] = splunkReadableData[splcommon.IdxcSecret]
				podSecret.Data["default.yml"] = splunkReadableData["default.yml"]

				_, err = splctrl.ApplySecret(ctx, mgr.c, podSecret)
				if err != nil {
					return err
				}
				logger.InfoContext(ctx, "IDXC password changed on the secret mounted on pod", "podSecretName", podSecretName)

				// Set to false marking the idxc password change in the secret
				mgr.cr.Status.IdxcPasswordChangedSecrets[podSecretName] = false
			}
		}
	}

	// Emit event for password sync completed
	if eventPublisher != nil {
		eventPublisher.Normal(ctx, "PasswordSyncCompleted",
			fmt.Sprintf("Password synchronized for %d pods", howManyPodsHaveSecretChanged))
	}

	// Log configuration push completion
	logger.InfoContext(ctx, "configuration push completed", "successCount", howManyPodsHaveSecretChanged, "duration", time.Since(pushStartTime))

	return nil
}

// Update for indexerClusterPodManager handles all updates for a statefulset of indexers
func (mgr *indexerClusterPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {

	var err error

	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, mgr.cr)

	// Track previous ready replicas for scaling events
	previousReadyReplicas := mgr.cr.Status.ReadyReplicas

	// Assign client
	if mgr.c == nil {
		mgr.c = c
	}
	// update statefulset, if necessary
	if mgr.cr.Status.ClusterManagerPhase == enterpriseApi.PhaseReady || mgr.cr.Status.ClusterMasterPhase == enterpriseApi.PhaseReady {
		_, err = splctrl.ApplyStatefulSet(ctx, mgr.c, statefulSet)
		if err != nil {
			return enterpriseApi.PhaseError, err
		}
	} else {
		mgr.log.InfoContext(ctx, "ClusterManager is not ready yet", "error", err)
		return enterpriseApi.PhaseError, err
	}

	// Get the podExecClient with empty targetPodName.
	// This will be set inside ApplyIdxcSecret
	podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, "")
	// Check if a recycle of idxc pods is necessary(due to idxc_secret mismatch with CM)
	err = ApplyIdxcSecret(ctx, mgr, desiredReplicas, podExecClient)
	if err != nil {
		return enterpriseApi.PhaseError, err
	}

	// update CR status with IDXC information
	err = mgr.updateStatus(ctx, statefulSet)
	if err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.IndexingReady || !mgr.cr.Status.ServiceReady {
		if termErr := splctrl.CheckPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
			mgr.log.ErrorContext(ctx, "terminal pod failure detected; setting PhaseError", "error", termErr)
			return enterpriseApi.PhaseError, termErr
		}
		mgr.log.InfoContext(ctx, "IndexerCluster is not ready", "error ", err)
		return enterpriseApi.PhasePending, nil
	}

	// manage scaling and updates
	phase, err := splctrl.UpdateStatefulSetPods(ctx, c, statefulSet, mgr, desiredReplicas)
	if err != nil {
		return phase, err
	}

	// Emit scale events when phase is ready and ready replicas changed to match desired
	if phase == enterpriseApi.PhaseReady {
		if mgr.cr.Status.ReadyReplicas == desiredReplicas && previousReadyReplicas != desiredReplicas {
			if desiredReplicas > previousReadyReplicas {
				if eventPublisher != nil {
					eventPublisher.Normal(ctx, "ScaledUp",
						fmt.Sprintf("Successfully scaled %s up from %d to %d replicas", mgr.cr.GetName(), previousReadyReplicas, desiredReplicas))
				}
			} else if desiredReplicas < previousReadyReplicas {
				if eventPublisher != nil {
					eventPublisher.Normal(ctx, "ScaledDown",
						fmt.Sprintf("Successfully scaled %s down from %d to %d replicas", mgr.cr.GetName(), previousReadyReplicas, desiredReplicas))
				}
			}
		}
	}

	return phase, nil
}

// PrepareScaleDown for indexerClusterPodManager prepares indexer pod to be removed via scale down event; it returns true when ready
func (mgr *indexerClusterPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	// first, decommission indexer peer with enforceCounts=true; this will rebalance buckets across other peers
	complete, err := mgr.decommission(ctx, n, true)
	if err != nil {
		return false, err
	}
	if !complete {
		return false, nil
	}

	// next, remove the peer
	c := mgr.getClusterManagerClient(ctx)
	peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)
	remainingPeers := int32(len(mgr.cr.Status.Peers)) - 1
	mgr.log.InfoContext(ctx, "deregistering peer from ClusterManager", "peerName", peerName, "remainingPeers", remainingPeers)
	return true, c.RemoveIndexerClusterPeer(mgr.cr.Status.Peers[n].ID)
}

// PrepareRecycle for indexerClusterPodManager prepares indexer pod to be recycled for updates; it returns true when ready
func (mgr *indexerClusterPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	return mgr.decommission(ctx, n, false)
}

func (mgr *indexerClusterPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	return nil
}

// FinishRecycle for indexerClusterPodManager completes recycle event for indexer pod; it returns true when complete
func (mgr *indexerClusterPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	if n >= int32(len(mgr.cr.Status.Peers)) {
		return false, fmt.Errorf("incorrect Peer got %d length of peer list %d", n, int32(len(mgr.cr.Status.Peers)))
	}
	return mgr.cr.Status.Peers[n].Status == "Up", nil
}

// decommission for indexerClusterPodManager decommissions an indexer pod; it returns true when ready
func (mgr *indexerClusterPodManager) decommission(ctx context.Context, n int32, enforceCounts bool) (bool, error) {
	peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Peers[n].Status {
	case "Up":
		podExecClient := splutil.GetPodExecClient(mgr.c, mgr.cr, getApplicablePodNameForK8Probes(mgr.cr, n))
		err := setProbeLevelOnSplunkPod(ctx, podExecClient, livenessProbeLevelOne)
		if err != nil {
			// Don't return error here. We may be reconciling several times, and the actual Pod status is down, but
			// not yet reflecting on the Cluster Master, in which case, the podExec fails, though the decommission is
			// going fine.
			mgr.log.WarnContext(ctx, "unable to lower the liveness probe level", "peerName", peerName, "enforceCounts", enforceCounts)
		}

		mgr.log.InfoContext(ctx, "decommissioning IndexerCluster peer", "peerName", peerName, "enforceCounts", enforceCounts)
		c := mgr.getClient(ctx, n)
		return false, c.DecommissionIndexerClusterPeer(enforceCounts)

	case "Decommissioning":
		mgr.log.InfoContext(ctx, "waiting for decommission to complete", "peerName", peerName)
		return false, nil

	case "ReassigningPrimaries":
		mgr.log.InfoContext(ctx, "waiting for decommission to complete", "peerName", peerName)
		return false, nil

	case "GracefulShutdown":
		mgr.log.InfoContext(ctx, "decommission complete", "peerName", peerName, "status", mgr.cr.Status.Peers[n].Status)
		return true, nil

	case "Down":
		mgr.log.InfoContext(ctx, "decommission complete", "peerName", peerName, "status", mgr.cr.Status.Peers[n].Status)
		return true, nil

	case "": // this can happen after the peer has been removed from the indexer cluster
		mgr.log.InfoContext(ctx, "peer has empty ID", "peerName", peerName)
		return false, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Peers[n].Status)
}

// getClient for indexerClusterPodManager returns a SplunkClient for the member n
func (mgr *indexerClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	logger := slog.With("func", "indexerClusterPodManager.getClient", "name", mgr.cr.GetName(), "namespace", mgr.cr.GetNamespace())

	// Get Pod Name
	memberName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, splcommon.GetSplunkServiceName(SplunkIndexer, mgr.cr.GetName(), true)))

	// Retrieve admin password from Pod
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, memberName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		logger.WarnContext(ctx, "couldn't retrieve the admin password from pod", "error", err)
	}

	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPwd)
}

// getClusterManagerClient for indexerClusterPodManager returns a SplunkClient for cluster manager
func (mgr *indexerClusterPodManager) getClusterManagerClient(ctx context.Context) *splclient.SplunkClient {
	logger := slog.With("func", "indexerClusterPodManager.getClusterManagerClient", "name", mgr.cr.GetName(), "namespace", mgr.cr.GetNamespace())

	// Retrieve admin password from Pod
	var managerIdxcName string
	var cm InstanceType
	if len(mgr.cr.Spec.ClusterManagerRef.Name) > 0 {
		managerIdxcName = mgr.cr.Spec.ClusterManagerRef.Name
		cm = SplunkClusterManager
	} else if len(mgr.cr.Spec.ClusterMasterRef.Name) > 0 {
		managerIdxcName = mgr.cr.Spec.ClusterMasterRef.Name
		cm = SplunkClusterMaster
	} else {
		mgr.log.InfoContext(ctx, "empty ClusterManager reference")
	}

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(), splcommon.GetSplunkServiceName(cm, managerIdxcName, false))

	// Retrieve admin password for Pod
	podName := fmt.Sprintf("splunk-%s-%s-%s", managerIdxcName, cm, "0")
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, podName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		logger.WarnContext(ctx, "couldn't retrieve the admin password from pod", "error", err.Error())
	}

	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPwd)
}

// getSiteRepFactorOriginCount gets the origin count of the site_replication_factor
func getSiteRepFactorOriginCount(siteRepFactor string) int32 {
	re := regexp.MustCompile(".*origin:(?P<rf>.*),.*")
	match := re.FindStringSubmatch(siteRepFactor)
	siteRF, err := strconv.ParseInt(match[1], 10, 32)
	if err != nil {
		return 0
	}
	return int32(siteRF)
}

// verifyRFPeers verifies the number of peers specified in the replicas section
// of IndexerClsuster CR. If it is less than RF, than we set it to RF.
func (mgr *indexerClusterPodManager) verifyRFPeers(ctx context.Context, c splcommon.ControllerClient) error {
	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, mgr.cr)

	if mgr.c == nil {
		mgr.c = c
	}
	cm := mgr.getClusterManagerClient(ctx)
	clusterInfo, err := cm.GetClusterInfo(false)
	if err != nil {
		return fmt.Errorf("could not get cluster info from cluster manager")
	}
	var replicationFactor int32
	// if it is a multisite indexer cluster, check site_replication_factor
	if clusterInfo.MultiSite == "true" {
		replicationFactor = getSiteRepFactorOriginCount(clusterInfo.SiteReplicationFactor)
	} else { // for single site, check replication factor
		replicationFactor = clusterInfo.ReplicationFactor
	}

	requestedReplicas := mgr.cr.Spec.Replicas
	if requestedReplicas < replicationFactor {
		mgr.log.InfoContext(ctx, "changing number of replicas as it is less than RF number of peers", "replicas", requestedReplicas)
		// Emit event indicating scaling below RF is blocked/adjusted
		if eventPublisher != nil {
			eventPublisher.Warning(ctx, "ScalingBlockedRF",
				fmt.Sprintf("Cannot scale below replication factor: %d replicas required, %d requested. Adjust replicationFactor or replicas.", replicationFactor, requestedReplicas))
		}
		mgr.cr.Spec.Replicas = replicationFactor
	}
	return nil
}

var GetClusterManagerInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager) (*splclient.ClusterManagerInfo, error) {
	c := mgr.getClusterManagerClient(ctx)
	return c.GetClusterManagerInfo()
}

var GetClusterManagerPeersCall = func(ctx context.Context, mgr *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
	c := mgr.getClusterManagerClient(ctx)
	return c.GetClusterManagerPeers()
}

// updateStatus for indexerClusterPodManager uses the REST API to update the status for an IndexerCluster custom resource
func (mgr *indexerClusterPodManager) updateStatus(ctx context.Context, statefulSet *appsv1.StatefulSet) error {
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas

	if mgr.cr.Status.ClusterManagerPhase != enterpriseApi.PhaseReady && mgr.cr.Status.ClusterMasterPhase != enterpriseApi.PhaseReady {
		mgr.cr.Status.Initialized = false
		mgr.cr.Status.IndexingReady = false
		mgr.cr.Status.ServiceReady = false
		mgr.cr.Status.MaintenanceMode = false
		return fmt.Errorf("waiting for cluster manager to become ready")
	}

	oldInitialized := mgr.cr.Status.Initialized
	oldIndexingReady := mgr.cr.Status.IndexingReady

	// get indexer cluster info from cluster manager if it's ready
	clusterInfo, err := GetClusterManagerInfoCall(ctx, mgr)
	if err != nil {
		return err
	}
	mgr.cr.Status.Initialized = clusterInfo.Initialized
	mgr.cr.Status.IndexingReady = clusterInfo.IndexingReady
	mgr.cr.Status.ServiceReady = clusterInfo.ServiceReady
	mgr.cr.Status.MaintenanceMode = clusterInfo.MaintenanceMode

	// get peer information from cluster manager
	peers, err := GetClusterManagerPeersCall(ctx, mgr)
	if err != nil {
		return err
	}
	totalPeerCount := len(peers)
	clusterName := mgr.cr.GetName()
	for n := int32(0); n < statefulSet.Status.Replicas; n++ {
		peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)
		peerStatus := enterpriseApi.IndexerClusterMemberStatus{Name: peerName}
		peerInfo, ok := peers[peerName]
		if ok {
			peerStatus.ID = peerInfo.ID
			peerStatus.Status = peerInfo.Status
			peerStatus.ActiveBundleID = peerInfo.ActiveBundleID
			peerStatus.BucketCount = peerInfo.BucketCount
			peerStatus.Searchable = peerInfo.Searchable
			slog.InfoContext(ctx, "peer registered with ClusterManager",
				"peerName", peerName,
				"clusterName", clusterName,
				"totalPeerCount", totalPeerCount)
		} else {
			mgr.log.InfoContext(ctx, "peer is not known by ClusterManager", "peerName", peerName)
		}
		if n < int32(len(mgr.cr.Status.Peers)) {
			mgr.cr.Status.Peers[n] = peerStatus
		} else {
			mgr.cr.Status.Peers = append(mgr.cr.Status.Peers, peerStatus)
		}
	}

	// truncate any extra peers that we didn't check (leftover from scale down)
	if statefulSet.Status.Replicas < int32(len(mgr.cr.Status.Peers)) {
		mgr.cr.Status.Peers = mgr.cr.Status.Peers[:statefulSet.Status.Replicas]
	}

	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, mgr.cr)

	// Emit events only on state transitions
	if eventPublisher != nil {
		// Compute current available peers for quorum-related events
		var available int32
		totalPeers := len(mgr.cr.Status.Peers)
		for _, p := range mgr.cr.Status.Peers {
			if p.Status == "Up" && p.Searchable {
				available++
			}
		}

		// Cluster just finished initializing when quorum becomes ready
		if !oldIndexingReady && mgr.cr.Status.IndexingReady {
			if !oldInitialized && mgr.cr.Status.Initialized {
				eventPublisher.Normal(ctx, "ClusterInitialized",
					fmt.Sprintf("Cluster '%s' initialized with %d peers", mgr.cr.GetName(), totalPeers))
			}

			// Cluster quorum just restored
			eventPublisher.Normal(ctx, "ClusterQuorumRestored",
				fmt.Sprintf("Cluster quorum restored: %d/%d peers available", available, totalPeers))
		}

		// Cluster quorum lost (transition out of indexing ready)
		if oldIndexingReady && !mgr.cr.Status.IndexingReady {
			eventPublisher.Warning(ctx, "ClusterQuorumLost",
				fmt.Sprintf("Cluster quorum lost: %d/%d peers available. Investigate peer failures immediately.", available, totalPeers))
		}
	}

	return nil
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
	builder, err := splunkconfig.NewSmartBusConfBuilder(&qosCfg.Queue, &qosCfg.OS)
	if err != nil {
		return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
	}

	owner := splcommon.AsOwner(cr, true)

	var configMap resources.DefaultsConfigMap
	if entries := splunkconfig.IndexerConf(builder); len(entries) > 0 {
		configMap, err = configworkflow.EnsureConfigMap(ctx, c, cr, entries, &owner)
		if err != nil {
			return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
		}
	}

	var secret resources.DefaultsSecret
	if entries := splunkconfig.IndexerCredentialsConf(builder, qosCfg.AccessKey, qosCfg.SecretKey); len(entries) > 0 {
		secret, err = configworkflow.EnsureSecret(ctx, c, cr, entries, &owner)
		if err != nil {
			return resources.DefaultsConfigMap{}, resources.DefaultsSecret{}, err
		}
	}

	return configMap, secret, nil
}

// getIndexerStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise indexers.
func getIndexerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, toCertEntries(cr.Spec.Certs))
	if err != nil {
		return nil, err
	}
	// Note: SPLUNK_INDEXER_URL is not used by the indexer pod containers,
	// hence avoided the call to getIndexerExtraEnv.
	// If other indexer CR specific env variables are required:
	// 1. Introduce the new env variables in the function getIndexerExtraEnv
	// 2. Avoid SPLUNK_INDEXER_URL in getIndexerExtraEnv for idxc CR
	// 3. Re-introduce the call to getIndexerExtraEnv here.
	return getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, cr.Spec.Replicas, make([]corev1.EnvVar, 0), certMounts, opts...)
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

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// helper function to get the list of IndexerCluster types in the current namespace
func getIndexerClusterList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []rclient.ListOption) (enterpriseApi.IndexerClusterList, error) {
	objectList := enterpriseApi.IndexerClusterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		return objectList, fmt.Errorf("list IndexerCluster in namespace %s: %w", cr.GetNamespace(), err)
	}

	return objectList, nil
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

func getIndexerClusterSortedSiteList(ctx context.Context, c splcommon.ControllerClient, ref corev1.ObjectReference, indexerList enterpriseApi.IndexerClusterList) (enterpriseApi.IndexerClusterList, error) {

	namespaceList := enterpriseApi.IndexerClusterList{}
	for _, v := range indexerList.Items {
		if v.Spec.ClusterManagerRef == ref {
			namespaceList.Items = append(namespaceList.Items, v)
		}
	}

	sort.SliceStable(namespaceList.Items, func(i, j int) bool {
		return getSiteName(ctx, c, &namespaceList.Items[i]) < getSiteName(ctx, c, &namespaceList.Items[j])
	})

	return namespaceList, nil
}

func getSiteName(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) string {
	defaults := cr.Spec.Defaults
	// site name starts with site:
	pattern := `site:\s+(\w+)`

	// Compile the regular expression pattern
	re := regexp.MustCompile(pattern)

	// Find the first match in the input string
	match := re.FindStringSubmatch(defaults)

	var extractedValue string
	if len(match) > 1 {
		// Extracted value is stored in the second element of the match array
		extractedValue := match[1]
		return extractedValue
	}

	return extractedValue
}

// Tells if there is an image migration from 8.x.x to 9.x.x
func imageUpdatedTo9(previousImage string, currentImage string) bool {
	// If there is no colon, version can't be detected
	if !strings.Contains(previousImage, ":") || !strings.Contains(currentImage, ":") {
		return false
	}
	previousVersion := strings.Split(previousImage, ":")[1]
	currentVersion := strings.Split(currentImage, ":")[1]
	return strings.HasPrefix(previousVersion, "8") && strings.HasPrefix(currentVersion, "9")
}
