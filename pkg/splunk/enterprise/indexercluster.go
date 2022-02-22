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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ApplyIndexerCluster reconciles the state of a Splunk Enterprise indexer cluster.
func ApplyIndexerCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyIndexerCluster").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(client, cr)

	// validate and updates defaults for CR
	err := validateIndexerClusterSpec(ctx, cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.ClusterMasterPhase = splcommon.PhaseError
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
	defer func() {
		err = client.Status().Update(ctx, cr)
		if err != nil {
			eventPublisher.Warning(ctx, "Update", fmt.Sprintf("custom resource update failed %s", err.Error()))
			scopedLog.Error(err, "Status update failed")
		}
	}()

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
	managerIdxCluster := &enterpriseApi.ClusterMaster{}
	err = client.Get(context.TODO(), namespacedName, managerIdxCluster)
	if err == nil {
		// when user creates both cluster manager and index cluster yaml file at the same time
		// cluser master status is not yet set so it will be blank
		if managerIdxCluster.Status.Phase == "" {
			cr.Status.ClusterMasterPhase = splcommon.PhasePending
		} else {
			cr.Status.ClusterMasterPhase = managerIdxCluster.Status.Phase
		}
	} else {
		cr.Status.ClusterMasterPhase = splcommon.PhaseError
	}
	mgr := indexerClusterPodManager{log: scopedLog, cr: cr, secrets: namespaceScopedSecret, newSplunkClient: splclient.NewSplunkClient}
	// Check if we have configured enough number(<= RF) of replicas
	if mgr.cr.Status.ClusterMasterPhase == splcommon.PhaseReady {
		err = mgr.verifyRFPeers(ctx, client)
		if err != nil {
			eventPublisher.Warning(ctx, "verifyRFPeers", fmt.Sprintf("verify RF peer failed %s", err.Error()))
			return result, err
		}
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		DeleteOwnerReferencesForResources(ctx, client, cr, nil)
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
			cr.Status.ClusterMasterPhase = splcommon.PhaseTerminating
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

	phase, err := mgr.Update(ctx, client, statefulSet, cr.Spec.Replicas)
	if err != nil {
		eventPublisher.Warning(ctx, "UpdateManager", fmt.Sprintf("update statefulset failed %s", err.Error()))
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		//update MC
		//Retrieve monitoring  console ref from CM Spec
		cmMonitoringConsoleConfigRef, err := RetrieveCMSpec(ctx, client, cr, cr.Spec.ClusterMasterRef.Name)
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
				err := c.AutomateMCApplyChanges(false)
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
			// Disable maintenance mode
			err = SetClusterMaintenanceMode(ctx, client, cr, false, false)
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
		scopedLog.Info("Setting indexer cluster as owner for cluster manager")
		namespacedName = types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkClusterManager, cr.Spec.ClusterMasterRef.Name)}
		err = splctrl.SetStatefulSetOwnerRef(ctx, client, cr, namespacedName)
		if err != nil {
			eventPublisher.Warning(ctx, "SetStatefulSetOwnerRef", fmt.Sprintf("set stateful set owner reference failed %s", err.Error()))
			result.Requeue = true
			return result, err
		}
	} /*else if cr.Status.Phase == splcommon.PhasePending {
		result.Requeue = false
	} */

	/*if !result.Requeue {
		return reconcile.Result{}, nil
	} */
	return result, nil
}

// indexerClusterPodManager is used to manage the pods within an indexer cluster
type indexerClusterPodManager struct {
	c               splcommon.ControllerClient
	log             logr.Logger
	cr              *enterpriseApi.IndexerCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

//getMonitoringConsoleClient for indexerClusterPodManager returns a SplunkClient for monitoring console
func (mgr *indexerClusterPodManager) getMonitoringConsoleClient(cr *enterpriseApi.IndexerCluster, cmMonitoringConsoleConfigRef string) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkMonitoringConsole, cmMonitoringConsoleConfigRef, false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// SetClusterMaintenanceMode enables/disables cluster maintenance mode
func SetClusterMaintenanceMode(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, enable bool, mock bool) error {
	// Retrieve admin password from Pod
	var managerIdxcName string
	if len(cr.Spec.ClusterMasterRef.Name) > 0 {
		managerIdxcName = cr.Spec.ClusterMasterRef.Name
	} else {
		return errors.New("Empty cluster manager reference")
	}
	cmPodName := fmt.Sprintf(splcommon.TestClusterManagerID, managerIdxcName, "0")
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
	_, _, err = splutil.PodExecCommand(ctx, c, cmPodName, cr.GetNamespace(), []string{"/bin/sh"}, command, false, false)
	if err != nil {
		if !mock {
			return err
		}
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
func ApplyIdxcSecret(ctx context.Context, mgr *indexerClusterPodManager, replicas int32, mock bool) error {
	var indIdxcSecret string
	// Get namespace scoped secret
	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mgr.c, mgr.cr.GetNamespace())
	if err != nil {
		return err
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyIdxcSecret").WithValues("Desired replicas", replicas, "IdxcSecretChanged", mgr.cr.Status.IndexerSecretChanged, "NamespaceSecretResourceVersion", mgr.cr.Status.NamespaceSecretResourceVersion, "mock", mock)

	// If namespace scoped secret revision is the same ignore
	if len(mgr.cr.Status.NamespaceSecretResourceVersion) == 0 {
		// First time, set resource version in CR
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
				err = SetClusterMaintenanceMode(ctx, mgr.c, mgr.cr, true, mock)
				if err != nil {
					return err
				}
				scopedLog.Info("Set Cm in maintenance mode")
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
				podSecret, err := splutil.GetSecretByName(ctx, mgr.c, mgr.cr, podSecretName)
				if err != nil {
					return fmt.Errorf("Could not read secret %s, reason - %v", podSecretName, err)
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
func (mgr *indexerClusterPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (splcommon.Phase, error) {

	var err error

	// Assign client
	if mgr.c == nil {
		mgr.c = c
	}
	// update statefulset, if necessary
	if mgr.cr.Status.ClusterMasterPhase == splcommon.PhaseReady {
		_, err = splctrl.ApplyStatefulSet(ctx, mgr.c, statefulSet)
		if err != nil {
			return splcommon.PhaseError, err
		}
	} else {
		mgr.log.Error(err, "Cluster Manager is not ready yet")
	}

	// Check if a recycle of idxc pods is necessary(due to idxc_secret mismatch with CM)
	err = ApplyIdxcSecret(ctx, mgr, desiredReplicas, false)
	if err != nil {
		return splcommon.PhaseError, err
	}

	// update CR status with IDXC information
	err = mgr.updateStatus(ctx, statefulSet)
	if err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.IndexingReady || !mgr.cr.Status.ServiceReady {
		mgr.log.Error(err, "Indexer cluster is not ready")
		return splcommon.PhasePending, nil
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
		return false, fmt.Errorf("Incorrect Peer got %d length of peer list %d", n, int32(len(mgr.cr.Status.Peers)))
	}
	return mgr.cr.Status.Peers[n].Status == "Up", nil
}

// decommission for indexerClusterPodManager decommissions an indexer pod; it returns true when ready
func (mgr *indexerClusterPodManager) decommission(ctx context.Context, n int32, enforceCounts bool) (bool, error) {
	peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Peers[n].Status {
	case "Up":
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
	if len(mgr.cr.Spec.ClusterMasterRef.Name) > 0 {
		managerIdxcName = mgr.cr.Spec.ClusterMasterRef.Name
	} else {
		mgr.log.Info("Empty cluster manager reference")
	}

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(), GetSplunkServiceName(SplunkClusterManager, managerIdxcName, false))

	// Retrieve admin password for Pod
	podName := fmt.Sprintf(splcommon.TestClusterManagerID, managerIdxcName, "0")
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
	siteRF, _ := strconv.Atoi(match[1])
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
		return fmt.Errorf("Could not get cluster info from cluster manager")
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

	if mgr.cr.Status.ClusterMasterPhase != splcommon.PhaseReady {
		mgr.cr.Status.Initialized = false
		mgr.cr.Status.IndexingReady = false
		mgr.cr.Status.ServiceReady = false
		mgr.cr.Status.MaintenanceMode = false
		return fmt.Errorf("Waiting for cluster manager to become ready")
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
func validateIndexerClusterSpec(ctx context.Context, cr *enterpriseApi.IndexerCluster) error {
	// We cannot have 0 replicas in IndexerCluster spec, since this refers to number of indexers in an indexer cluster
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}

	// Cannot leave clusterMasterRef field empty or else we cannot connect to CM
	if len(cr.Spec.ClusterMasterRef.Name) == 0 {
		return fmt.Errorf("IndexerCluster spec should refer to ClusterMaster via clusterMasterRef")
	}

	// Multisite / multipart clusters: can't reference a cluster manager located in another namespace because of Service and Secret limitations
	if len(cr.Spec.ClusterMasterRef.Namespace) > 0 && cr.Spec.ClusterMasterRef.Namespace != cr.GetNamespace() {
		return fmt.Errorf("Multisite cluster does not support cluster manager to be located in a different namespace")
	}
	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

//RetrieveCMSpec finds monitoringConsole ref from cm spec
func RetrieveCMSpec(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, clusterMasterRef string) (string, error) {

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: clusterMasterRef}
	var cmCR enterpriseApi.ClusterMaster
	var monitoringConsoleRef string = ""

	err := client.Get(ctx, namespacedName, &cmCR)
	if err == nil {
		monitoringConsoleRef = cmCR.Spec.MonitoringConsoleRef.Name
		return monitoringConsoleRef, err
	}
	return "", err
}
