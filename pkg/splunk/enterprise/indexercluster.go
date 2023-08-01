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
	"regexp"
	"strconv"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	"github.com/go-logr/logr"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NewSplunkClientFunc funciton pointer type
type NewSplunkClientFunc func(managementURI, username, password string) *splclient.SplunkClient

// ApplyIndexerClusterManager reconciles the state of a Splunk Enterprise indexer cluster.
func ApplyIndexerClusterManager(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyIndexerClusterManager").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(client, cr)

	// validate and updates defaults for CR
	err := validateIndexerClusterSpec(ctx, client, cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterpriseApi.PhaseError
	cr.Status.ClusterManagerPhase = enterpriseApi.PhaseError
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

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr)

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      cr.Spec.ClusterManagerRef.Name,
	}
	managerIdxCluster := &enterpriseApi.ClusterManager{}
	err = client.Get(context.TODO(), namespacedName, managerIdxCluster)
	if err == nil {
		// when user creates both cluster manager and index cluster yaml file at the same time
		// cluser manager status is not yet set so it will be blank
		if managerIdxCluster.Status.Phase == "" {
			cr.Status.ClusterManagerPhase = enterpriseApi.PhasePending
		} else {
			cr.Status.ClusterManagerPhase = managerIdxCluster.Status.Phase
		}
	} else {
		scopedLog.Error(nil, "The configured clusterMasterRef doesn't exist", "clusterManagerRef", cr.Spec.ClusterManagerRef.Name)
		cr.Status.ClusterManagerPhase = enterpriseApi.PhaseError
	}

	mgr := newIndexerClusterPodManager(scopedLog, cr, namespaceScopedSecret, splclient.NewSplunkClient)
	// Check if we have configured enough number(<= RF) of replicas
	if mgr.cr.Status.ClusterManagerPhase == enterpriseApi.PhaseReady {
		err = VerifyRFPeers(ctx, mgr, client)
		if err != nil {
			eventPublisher.Warning(ctx, "verifyRFPeers", fmt.Sprintf("verify RF peer failed %s", err.Error()))
			return result, err
		}
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		DeleteOwnerReferencesForResources(ctx, client, cr, nil, SplunkIndexer)
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
			cr.Status.ClusterManagerPhase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, "Delete", fmt.Sprintf("delete custom resource failed %s", err.Error()))
		}
		return result, err
	}
	// create or update a headless service for indexer cluster
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update headless service for indexer cluster failed %s", err.Error()))
		return result, err
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update service for indexer cluster failed %s", err.Error()))
		return result, err
	}

	// create or update statefulset for the indexers
	statefulSet, err := getIndexerStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "getIndexerStatefulSet", fmt.Sprintf("get indexer stateful set failed %s", err.Error()))
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
				if v.Spec.Containers[0].Image != cr.Spec.Image {
					// image do not match that means its image upgrade
					versionUpgrade = true
					break
				}
			}
		}
	}

	// check if version upgrade is set
	if !versionUpgrade {
		phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("update statefulset failed %s", err.Error()))
			return result, err
		}
	} else {
		// check if the IndexerCluster is ready for version upgrade
		continueReconcile, err := isIndexerClusterReadyForUpgrade(ctx, client, cr)
		if err != nil || !continueReconcile {
			return result, err
		}
		// Delete the statefulset and recreate new one
		err = client.Delete(ctx, statefulSet)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("version mitmatch for indexer clustre and indexer container, delete statefulset failed %s", err.Error()))
			eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("%s-%s, %s-%s", "indexer-image", cr.Spec.Image, "container-image", statefulSet.Spec.Template.Spec.Containers[0].Image))
			return result, err
		}
		time.Sleep(1 * time.Second)
		// since we are creating new statefulset, setting resourceVersion to ""
		statefulSet.ResourceVersion = ""
		phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("update statefulset failed %s", err.Error()))
			return result, err
		}
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//update MC
		//Retrieve monitoring  console ref from CM Spec
		cmMonitoringConsoleConfigRef, err := RetrieveCMSpec(ctx, client, cr)
		if err != nil {
			eventPublisher.Warning(ctx, "RetrieveCMSpec", fmt.Sprintf("retrive cluster manager spec failed %s", err.Error()))
			return result, err
		}
		if cmMonitoringConsoleConfigRef != "" {
			namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cmMonitoringConsoleConfigRef)}
			_, err := splctrl.GetStatefulSetByName(ctx, client, namespacedName)
			//if MC pod already exists
			if err == nil {
				c := mgr.getMonitoringConsoleClient(cr, cmMonitoringConsoleConfigRef)
				err := c.AutomateMCApplyChanges()
				if err != nil {
					eventPublisher.Warning(ctx, "AutomateMCApplyChanges", fmt.Sprintf("get monitoring console client failed %s", err.Error()))
					return result, err
				}
			}
			if len(cr.Spec.MonitoringConsoleRef.Name) > 0 && (cr.Spec.MonitoringConsoleRef.Name != cmMonitoringConsoleConfigRef) {
				scopedLog.Info("Indexer Cluster CR should not specify monitoringConsoleRef and if specified, should be similar to cluster manager spec")
			}
		}
		if len(cr.Status.IndexerSecretChanged) > 0 {
			var managerIdxcName string
			if len(cr.Spec.ClusterManagerRef.Name) > 0 {
				managerIdxcName = cr.Spec.ClusterManagerRef.Name
			} else {
				return result, errors.New("empty cluster manager reference")
			}
			cmPodName := fmt.Sprintf("splunk-%s-cluster-manager-%s", managerIdxcName, "0")
			podExecClient := splutil.GetPodExecClient(client, cr, cmPodName)
			// Disable maintenance mode
			err = SetClusterMaintenanceMode(ctx, client, cr, false, cmPodName, podExecClient)
			if err != nil {
				eventPublisher.Warning(ctx, "SetClusterMaintenanceMode", fmt.Sprintf("set cluster maintainance mode failed %s", err.Error()))
				return result, err
			}
		}

		// Reset idxc secret changed and namespace secret revision
		cr.Status.IndexerSecretChanged = []bool{}
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion
		cr.Status.IdxcPasswordChangedSecrets = make(map[string]bool)

		result.Requeue = false
		// Set indexer cluster CR as owner reference for clustermanager
		scopedLog.Info("Setting indexer cluster as owner for cluster manager")
		if len(cr.Spec.ClusterManagerRef.Name) > 0 {
			namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkClusterManager, cr.Spec.ClusterManagerRef.Name)}
		}
		err = splctrl.SetStatefulSetOwnerRef(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, "SetStatefulSetOwnerRef", fmt.Sprintf("set stateful set owner reference failed %s", err.Error()))
			result.Requeue = true
			return result, err
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
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyIndexerCluster")
	eventPublisher, _ := newK8EventPublisher(client, cr)

	// validate and updates defaults for CR
	err := validateIndexerClusterSpec(ctx, client, cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterpriseApi.PhaseError
	cr.Status.ClusterMasterPhase = enterpriseApi.PhaseError
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

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr)

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      cr.Spec.ClusterMasterRef.Name,
	}
	managerIdxCluster := &enterpriseApiV3.ClusterMaster{}
	err = client.Get(context.TODO(), namespacedName, managerIdxCluster)
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

	mgr := newIndexerClusterPodManager(scopedLog, cr, namespaceScopedSecret, splclient.NewSplunkClient)
	// Check if we have configured enough number(<= RF) of replicas
	if mgr.cr.Status.ClusterMasterPhase == enterpriseApi.PhaseReady {
		err = VerifyRFPeers(ctx, mgr, client)
		if err != nil {
			eventPublisher.Warning(ctx, "verifyRFPeers", fmt.Sprintf("verify RF peer failed %s", err.Error()))
			return result, err
		}
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		DeleteOwnerReferencesForResources(ctx, client, cr, nil, SplunkIndexer)
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
			cr.Status.ClusterMasterPhase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, "Delete", fmt.Sprintf("delete custom resource failed %s", err.Error()))
		}
		return result, err
	}

	// create or update a headless service for indexer cluster
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, true))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update headless service for indexer cluster failed %s", err.Error()))
		return result, err
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, false))
	if err != nil {
		eventPublisher.Warning(ctx, "ApplyService", fmt.Sprintf("create/update service for indexer cluster failed %s", err.Error()))
		return result, err
	}

	// create or update statefulset for the indexers
	statefulSet, err := getIndexerStatefulSet(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "getIndexerStatefulSet", fmt.Sprintf("get indexer stateful set failed %s", err.Error()))
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
				previousImage := v.Spec.Containers[0].Image
				currentImage := cr.Spec.Image
				// get the pod image name
				if strings.HasPrefix(previousImage, "8") &&
					strings.HasPrefix(currentImage, "9") {
					// image do not match that means its image upgrade
					versionUpgrade = true
					break
				}
			}
		}
	}

	// check if version upgrade is set
	if !versionUpgrade {
		phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("update statefulset failed %s", err.Error()))
			return result, err
		}
	} else {
		// Delete the statefulset and recreate new one
		err = client.Delete(ctx, statefulSet)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("version mitmatch for indexer clustre and indexer container, delete statefulset failed %s", err.Error()))
			eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("%s-%s, %s-%s", "indexer-image", cr.Spec.Image, "container-image", statefulSet.Spec.Template.Spec.Containers[0].Image))
			return result, err
		}
		time.Sleep(1 * time.Second)
		// since we are creating new statefulset, setting resourceVersion to ""
		statefulSet.ResourceVersion = ""
		phase, err = mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
		if err != nil {
			eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("update statefulset failed %s", err.Error()))
			return result, err
		}
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//update MC
		//Retrieve monitoring  console ref from CM Spec
		cmMonitoringConsoleConfigRef, err := RetrieveCMSpec(ctx, client, cr)
		if err != nil {
			eventPublisher.Warning(ctx, "RetrieveCMSpec", fmt.Sprintf("retrive cluster master spec failed %s", err.Error()))
			return result, err
		}
		if cmMonitoringConsoleConfigRef != "" {
			namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cmMonitoringConsoleConfigRef)}
			_, err := splctrl.GetStatefulSetByName(ctx, client, namespacedName)
			//if MC pod already exists
			if err == nil {
				c := mgr.getMonitoringConsoleClient(cr, cmMonitoringConsoleConfigRef)
				err := c.AutomateMCApplyChanges()
				if err != nil {
					eventPublisher.Warning(ctx, "AutomateMCApplyChanges", fmt.Sprintf("get monitoring console client failed %s", err.Error()))
					return result, err
				}
			}
			if len(cr.Spec.MonitoringConsoleRef.Name) > 0 && (cr.Spec.MonitoringConsoleRef.Name != cmMonitoringConsoleConfigRef) {
				scopedLog.Info("Indexer Cluster CR should not specify monitoringConsoleRef and if specified, should be similar to cluster master spec")
			}
		}
		if len(cr.Status.IndexerSecretChanged) > 0 {
			var managerIdxcName string
			if len(cr.Spec.ClusterMasterRef.Name) > 0 {
				managerIdxcName = cr.Spec.ClusterMasterRef.Name
			} else {
				return result, errors.New("empty cluster master reference")
			}
			cmPodName := fmt.Sprintf("splunk-%s-cluster-master-%s", managerIdxcName, "0")
			podExecClient := splutil.GetPodExecClient(client, cr, cmPodName)
			// Disable maintenance mode
			err = SetClusterMaintenanceMode(ctx, client, cr, false, cmPodName, podExecClient)
			if err != nil {
				eventPublisher.Warning(ctx, "SetClusterMaintenanceMode", fmt.Sprintf("set cluster maintainance mode failed %s", err.Error()))
				return result, err
			}
		}

		// Reset idxc secret changed and namespace secret revision
		cr.Status.IndexerSecretChanged = []bool{}
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion
		cr.Status.IdxcPasswordChangedSecrets = make(map[string]bool)

		result.Requeue = false
		// Set indexer cluster CR as owner reference for clustermaster
		scopedLog.Info("Setting indexer cluster as owner for cluster master")
		namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkClusterMaster, cr.Spec.ClusterMasterRef.Name)}
		err = splctrl.SetStatefulSetOwnerRef(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, "SetStatefulSetOwnerRef", fmt.Sprintf("set stateful set owner reference failed %s", err.Error()))
			result.Requeue = true
			return result, err
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
	log             logr.Logger
	cr              *enterpriseApi.IndexerCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// newIndexerClusterPodManager function to create pod manager this is added to write unit test case
var newIndexerClusterPodManager = func(log logr.Logger, cr *enterpriseApi.IndexerCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) indexerClusterPodManager {
	return indexerClusterPodManager{
		log:             log,
		cr:              cr,
		secrets:         secret,
		newSplunkClient: newSplunkClient,
	}
}

// getMonitoringConsoleClient for indexerClusterPodManager returns a SplunkClient for monitoring console
func (mgr *indexerClusterPodManager) getMonitoringConsoleClient(cr *enterpriseApi.IndexerCluster, cmMonitoringConsoleConfigRef string) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkMonitoringConsole, cmMonitoringConsoleConfigRef, false))
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
	// Get namespace scoped secret
	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mgr.c, mgr.cr.GetNamespace())
	if err != nil {
		return err
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyIdxcSecret").WithValues("Desired replicas", replicas, "IdxcSecretChanged", mgr.cr.Status.IndexerSecretChanged, "CrStatusNamespaceSecretResourceVersion", mgr.cr.Status.NamespaceSecretResourceVersion, "NamespaceSecretResourceVersion", namespaceSecret.GetObjectMeta().GetResourceVersion())

	// If namespace scoped secret revision is the same ignore
	if len(mgr.cr.Status.NamespaceSecretResourceVersion) == 0 {
		// First time, set resource version in CR
		scopedLog.Info("Setting CrStatusNamespaceSecretResourceVersion for the first time")
		mgr.cr.Status.NamespaceSecretResourceVersion = namespaceSecret.ObjectMeta.ResourceVersion
		return nil
	} else if mgr.cr.Status.NamespaceSecretResourceVersion == namespaceSecret.ObjectMeta.ResourceVersion {
		// If resource version hasn't changed don't return
		return nil
	}

	scopedLog.Info("Namespaced scoped secret revision has changed")

	// Retrieve idxc_secret password from secret data
	nsIdxcSecret := string(namespaceSecret.Data[splcommon.IdxcSecret])

	// Loop over all indexer pods and get individual pod's idxc password
	for i := int32(0); i <= replicas-1; i++ {
		// Get Indexer's name
		indexerPodName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), i)

		// Retrieve secret from pod
		podSecret, err := splutil.GetSecretFromPod(ctx, mgr.c, indexerPodName, mgr.cr.GetNamespace())
		if err != nil {
			return fmt.Errorf(fmt.Sprintf(splcommon.PodSecretNotFoundError, indexerPodName))
		}

		// Retrieve idxc_secret token
		if indIdxcSecretByte, ok := podSecret.Data[splcommon.IdxcSecret]; ok {
			indIdxcSecret = string(indIdxcSecretByte)
		} else {
			return fmt.Errorf(fmt.Sprintf(splcommon.SecretTokenNotRetrievable, splcommon.IdxcSecret))
		}

		// If idxc secret is different from namespace scoped secret change it
		if indIdxcSecret != nsIdxcSecret {
			scopedLog.Info("idxc Secret different from namespace scoped secret")

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
				scopedLog.Info("Set CM in maintenance mode")
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
				return err
			}
			scopedLog.Info("Changed idxc secret")

			// Restart splunk instance on pod
			err = idxcClient.RestartSplunk()
			if err != nil {
				return err
			}
			scopedLog.Info("Restarted splunk")

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
				podSecret, err := splutil.GetSecretByName(ctx, mgr.c, mgr.cr.GetNamespace(), mgr.cr.GetName(), podSecretName)
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
				scopedLog.Info("idxc password changed on the secret mounted on pod", "Secret on Pod:", podSecretName)

				// Set to false marking the idxc password change in the secret
				mgr.cr.Status.IdxcPasswordChangedSecrets[podSecretName] = false
			}
		}
	}

	return nil
}

// Update for indexerClusterPodManager handles all updates for a statefulset of indexers
func (mgr *indexerClusterPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {

	var err error

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
		mgr.log.Info("Cluster Manager is not ready yet", "reason ", err)
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
		mgr.log.Info("Indexer cluster is not ready", "reason ", err)
		return enterpriseApi.PhasePending, nil
	}

	// manage scaling and updates
	return splctrl.UpdateStatefulSetPods(ctx, c, statefulSet, mgr, desiredReplicas)
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
	return true, c.RemoveIndexerClusterPeer(mgr.cr.Status.Peers[n].ID)
}

// PrepareRecycle for indexerClusterPodManager prepares indexer pod to be recycled for updates; it returns true when ready
func (mgr *indexerClusterPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	return mgr.decommission(ctx, n, false)
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
			mgr.log.Info("Unable to lower the liveness probe level", "peerName", peerName, "enforceCounts", enforceCounts)
		}

		mgr.log.Info("Decommissioning indexer cluster peer", "peerName", peerName, "enforceCounts", enforceCounts)
		c := mgr.getClient(ctx, n)
		return false, c.DecommissionIndexerClusterPeer(enforceCounts)

	case "Decommissioning":
		mgr.log.Info("Waiting for decommission to complete", "peerName", peerName)
		return false, nil

	case "ReassigningPrimaries":
		mgr.log.Info("Waiting for decommission to complete", "peerName", peerName)
		return false, nil

	case "GracefulShutdown":
		mgr.log.Info("Decommission complete", "peerName", peerName, "Status", mgr.cr.Status.Peers[n].Status)
		return true, nil

	case "Down":
		mgr.log.Info("Decommission complete", "peerName", peerName, "Status", mgr.cr.Status.Peers[n].Status)
		return true, nil

	case "": // this can happen after the peer has been removed from the indexer cluster
		mgr.log.Info("Peer has empty ID", "peerName", peerName)
		return false, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Peers[n].Status)
}

// getClient for indexerClusterPodManager returns a SplunkClient for the member n
func (mgr *indexerClusterPodManager) getClient(ctx context.Context, n int32) *splclient.SplunkClient {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("indexerClusterPodManager.getClient").WithValues("name", mgr.cr.GetName(), "namespace", mgr.cr.GetNamespace())

	// Get Pod Name
	memberName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkIndexer, mgr.cr.GetName(), true)))

	// Retrieve admin password from Pod
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, memberName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		scopedLog.Error(err, "Couldn't retrieve the admin password from pod")
	}

	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPwd)
}

// getClusterManagerClient for indexerClusterPodManager returns a SplunkClient for cluster manager
func (mgr *indexerClusterPodManager) getClusterManagerClient(ctx context.Context) *splclient.SplunkClient {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("indexerClusterPodManager.getClusterManagerClient")

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
		mgr.log.Info("Empty cluster manager reference")
	}

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(), GetSplunkServiceName(cm, managerIdxcName, false))

	// Retrieve admin password for Pod
	podName := fmt.Sprintf("splunk-%s-%s-%s", managerIdxcName, cm, "0")
	adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.c, podName, mgr.cr.GetNamespace(), "password")
	if err != nil {
		scopedLog.Error(err, "Couldn't retrieve the admin password from pod")
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

	if mgr.cr.Spec.Replicas < replicationFactor {
		mgr.log.Info("Changing number of replicas as it is less than RF number of peers", "replicas", mgr.cr.Spec.Replicas)
		mgr.cr.Spec.Replicas = replicationFactor
	}
	return nil
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

	// get indexer cluster info from cluster manager if it's ready
	c := mgr.getClusterManagerClient(ctx)
	clusterInfo, err := c.GetClusterManagerInfo()
	if err != nil {
		return err
	}
	mgr.cr.Status.Initialized = clusterInfo.Initialized
	mgr.cr.Status.IndexingReady = clusterInfo.IndexingReady
	mgr.cr.Status.ServiceReady = clusterInfo.ServiceReady
	mgr.cr.Status.MaintenanceMode = clusterInfo.MaintenanceMode

	// get peer information from cluster manager
	peers, err := c.GetClusterManagerPeers()
	if err != nil {
		return err
	}
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
		} else {
			mgr.log.Info("Peer is not known by cluster manager", "peerName", peerName)
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

	return nil
}

// getIndexerStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise indexers.
func getIndexerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (*appsv1.StatefulSet, error) {
	// Note: SPLUNK_INDEXER_URL is not used by the indexer pod containers,
	// hence avoided the call to getIndexerExtraEnv.
	// If other indexer CR specific env variables are required:
	// 1. Introduce the new env variables in the function getIndexerExtraEnv
	// 2. Avoid SPLUNK_INDEXER_URL in getIndexerExtraEnv for idxc CR
	// 3. Re-introduce the call to getIndexerExtraEnv here.
	return getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, cr.Spec.Replicas, make([]corev1.EnvVar, 0))
}

// validateIndexerClusterSpec checks validity and makes default updates to a IndexerClusterSpec, and returns error if something is wrong.
func validateIndexerClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) error {
	// We cannot have 0 replicas in IndexerCluster spec, since this refers to number of indexers in an indexer cluster
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
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
func getIndexerClusterList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.IndexerClusterList, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getIndexerClusterList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.IndexerClusterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	if err != nil {
		scopedLog.Error(err, "IndexerCluster types not found in namespace", "namsespace", cr.GetNamespace())
		return objectList, err
	}

	return objectList, nil
}

// RetrieveCMSpec finds monitoringConsole ref from cm spec
func RetrieveCMSpec(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (string, error) {
	var monitoringConsoleRef string = ""

	if len(cr.Spec.ClusterMasterRef.Name) > 0 && len(cr.Spec.ClusterManagerRef.Name) == 0 {
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.ClusterMasterRef.Name}
		var cmCR enterpriseApiV3.ClusterMaster
		err := client.Get(ctx, namespacedName, &cmCR)
		if err == nil {
			monitoringConsoleRef = cmCR.Spec.MonitoringConsoleRef.Name
			return monitoringConsoleRef, err
		}
	} else if len(cr.Spec.ClusterManagerRef.Name) > 0 && len(cr.Spec.ClusterMasterRef.Name) == 0 {
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.ClusterManagerRef.Name}
		var cmCR enterpriseApi.ClusterManager
		err := client.Get(ctx, namespacedName, &cmCR)
		if err == nil {
			monitoringConsoleRef = cmCR.Spec.MonitoringConsoleRef.Name
			return monitoringConsoleRef, err
		}
	}

	return "", nil
}

// isIndexerClusterReadyForUpgrade checks if IndexerCluster can be upgraded if a version upgrade is in-progress
// No-operation otherwise; returns bool, err accordingly
func isIndexerClusterReadyForUpgrade(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isIndexerClusterReadyForUpgrade").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)

	// get the clusterManagerRef attached to the instance
	clusterManagerRef := cr.Spec.ClusterManagerRef

	// check if a search head cluster exists with the same ClusterManager instance attached
	searchHeadClusterInstance := enterpriseApi.SearchHeadCluster{}
	opts := []rclient.ListOption{
		rclient.InNamespace(cr.GetNamespace()),
	}
	searchHeadList, err := getSearchHeadClusterList(ctx, c, cr, opts)
	if err != nil {
		if err.Error() == "NotFound" {
			return true, nil
		}
		return false, err
	}
	if len(searchHeadList.Items) == 0 {
		return true, nil
	}

	// check if Search Head instance has the required ClusterManagerRef
	for _, shc := range searchHeadList.Items {
		if shc.Spec.ClusterManagerRef.Name == clusterManagerRef.Name {
			searchHeadClusterInstance = shc
			break
		}
	}
	if len(searchHeadClusterInstance.GetName()) == 0 {
		return true, nil
	}

	shcImage, err := getCurrentImage(ctx, c, &searchHeadClusterInstance, SplunkSearchHead)
	if err != nil {
		eventPublisher.Warning(ctx, "isIndexerClusterReadyForUpgrade", fmt.Sprintf("Could not get the Search Head Cluster Image. Reason %v", err))
		scopedLog.Error(err, "Unable to get SearchHeadCluster current image")
		return false, err
	}

	idxImage, err := getCurrentImage(ctx, c, cr, SplunkIndexer)
	if err != nil {
		eventPublisher.Warning(ctx, "isIndexerClusterReadyForUpgrade", fmt.Sprintf("Could not get the Indexer Cluster Image. Reason %v", err))
		scopedLog.Error(err, "Unable to get IndexerCluster current image")
		return false, err
	}

	// check if an image upgrade is happening and whether SHC has finished updating yet, return false to stop
	// further reconcile operations on IDX until SHC is ready
	if (cr.Spec.Image != idxImage) && (searchHeadClusterInstance.Status.Phase != enterpriseApi.PhaseReady || shcImage != cr.Spec.Image) {
		return false, nil
	}
	return true, nil
}
