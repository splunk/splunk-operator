// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
)

// ApplyIndexerCluster reconciles the state of a Splunk Enterprise indexer cluster.
func ApplyIndexerCluster(client splcommon.ControllerClient, cr *enterprisev1.IndexerCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplyIndexerCluster").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// validate and updates defaults for CR
	err := validateIndexerClusterSpec(cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.ClusterMasterPhase = splcommon.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-indexer", cr.GetName())
	if cr.Status.Peers == nil {
		cr.Status.Peers = []enterprisev1.IndexerClusterMemberStatus{}
	}
	defer func() {
		err = client.Status().Update(context.TODO(), cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := splctrl.CheckForDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
			cr.Status.ClusterMasterPhase = splcommon.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
	if err != nil {
		return result, err
	}

	// create or update a headless service for indexer cluster
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, true))
	if err != nil {
		return result, err
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, false))
	if err != nil {
		return result, err
	}

	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      cr.Spec.ClusterMasterRef.Name,
	}
	masterIdxCluster := &enterprisev1.ClusterMaster{}
	err = client.Get(context.TODO(), namespacedName, masterIdxCluster)
	if err == nil {
		cr.Status.ClusterMasterPhase = masterIdxCluster.Status.Phase
	} else {
		cr.Status.ClusterMasterPhase = splcommon.PhaseError
	}

	// create or update statefulset for the indexers
	statefulSet, err := getIndexerStatefulSet(client, cr)
	if err != nil {
		return result, err
	}
	mgr := indexerClusterPodManager{log: scopedLog, cr: cr, secrets: namespaceScopedSecret, newSplunkClient: splclient.NewSplunkClient}
	phase, err := mgr.Update(client, statefulSet, cr.Spec.Replicas)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		result.Requeue = false
	}
	return result, nil
}

// indexerClusterPodManager is used to manage the pods within an indexer cluster
type indexerClusterPodManager struct {
	log             logr.Logger
	cr              *enterprisev1.IndexerCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// Update for indexerClusterPodManager handles all updates for a statefulset of indexers
func (mgr *indexerClusterPodManager) Update(c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (splcommon.Phase, error) {
	// update statefulset, if necessary
	_, err := splctrl.ApplyStatefulSet(c, statefulSet)
	if err != nil {
		return splcommon.PhaseError, err
	}

	// update CR status with IDXC information
	err = mgr.updateStatus(statefulSet)
	if err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.IndexingReady || !mgr.cr.Status.ServiceReady {
		mgr.log.Error(err, "Indexer cluster is not ready")
		return splcommon.PhasePending, nil
	}

	// manage scaling and updates
	return splctrl.UpdateStatefulSetPods(c, statefulSet, mgr, desiredReplicas)
}

// PrepareScaleDown for indexerClusterPodManager prepares indexer pod to be removed via scale down event; it returns true when ready
func (mgr *indexerClusterPodManager) PrepareScaleDown(n int32) (bool, error) {
	// first, decommission indexer peer with enforceCounts=true; this will rebalance buckets across other peers
	complete, err := mgr.decommission(n, true)
	if err != nil {
		return false, err
	}
	if !complete {
		return false, nil
	}

	// next, remove the peer
	c := mgr.getClusterMasterClient()
	return true, c.RemoveIndexerClusterPeer(mgr.cr.Status.Peers[n].ID)
}

// PrepareRecycle for indexerClusterPodManager prepares indexer pod to be recycled for updates; it returns true when ready
func (mgr *indexerClusterPodManager) PrepareRecycle(n int32) (bool, error) {
	return mgr.decommission(n, false)
}

// FinishRecycle for indexerClusterPodManager completes recycle event for indexer pod; it returns true when complete
func (mgr *indexerClusterPodManager) FinishRecycle(n int32) (bool, error) {
	return mgr.cr.Status.Peers[n].Status == "Up", nil
}

// decommission for indexerClusterPodManager decommissions an indexer pod; it returns true when ready
func (mgr *indexerClusterPodManager) decommission(n int32, enforceCounts bool) (bool, error) {
	peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)

	switch mgr.cr.Status.Peers[n].Status {
	case "Up":
		mgr.log.Info("Decommissioning indexer cluster peer", "peerName", peerName, "enforceCounts", enforceCounts)
		c := mgr.getClient(n)
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
func (mgr *indexerClusterPodManager) getClient(n int32) *splclient.SplunkClient {
	memberName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, GetSplunkServiceName(SplunkIndexer, mgr.cr.GetName(), true)))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// getClusterMasterClient for indexerClusterPodManager returns a SplunkClient for cluster master
func (mgr *indexerClusterPodManager) getClusterMasterClient() *splclient.SplunkClient {
	masterIdxcName := mgr.cr.Spec.ClusterMasterRef.Name
	fqdnName := splcommon.GetServiceFQDN(mgr.cr.GetNamespace(), GetSplunkServiceName(SplunkClusterMaster, masterIdxcName, false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// updateStatus for indexerClusterPodManager uses the REST API to update the status for an IndexerCluster custom resource
func (mgr *indexerClusterPodManager) updateStatus(statefulSet *appsv1.StatefulSet) error {
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas

	if mgr.cr.Status.ClusterMasterPhase != splcommon.PhaseReady {
		mgr.cr.Status.Initialized = false
		mgr.cr.Status.IndexingReady = false
		mgr.cr.Status.ServiceReady = false
		mgr.cr.Status.MaintenanceMode = false
		return fmt.Errorf("Waiting for cluster master to become ready")
	}

	// get indexer cluster info from cluster master if it's ready
	c := mgr.getClusterMasterClient()
	clusterInfo, err := c.GetClusterMasterInfo()
	if err != nil {
		return err
	}
	mgr.cr.Status.Initialized = clusterInfo.Initialized
	mgr.cr.Status.IndexingReady = clusterInfo.IndexingReady
	mgr.cr.Status.ServiceReady = clusterInfo.ServiceReady
	mgr.cr.Status.MaintenanceMode = clusterInfo.MaintenanceMode

	// get peer information from cluster master
	peers, err := c.GetClusterMasterPeers()
	if err != nil {
		return err
	}
	for n := int32(0); n < statefulSet.Status.Replicas; n++ {
		peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), n)
		peerStatus := enterprisev1.IndexerClusterMemberStatus{Name: peerName}
		peerInfo, ok := peers[peerName]
		if ok {
			peerStatus.ID = peerInfo.ID
			peerStatus.Status = peerInfo.Status
			peerStatus.ActiveBundleID = peerInfo.ActiveBundleID
			peerStatus.BucketCount = peerInfo.BucketCount
			peerStatus.Searchable = peerInfo.Searchable
		} else {
			mgr.log.Info("Peer is not known by cluster master", "peerName", peerName)
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
func getIndexerStatefulSet(client splcommon.ControllerClient, cr *enterprisev1.IndexerCluster) (*appsv1.StatefulSet, error) {
	return getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, cr.Spec.Replicas, getIndexerExtraEnv(cr, cr.Spec.Replicas))
}

// validateIndexerClusterSpec checks validity and makes default updates to a IndexerClusterSpec, and returns error if something is wrong.
func validateIndexerClusterSpec(cr *enterprisev1.IndexerCluster) error {
	// We cannot have 0 replicas in IndexerCluster spec, since this refers to number of indexers in an indexer cluster
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}

	// Cannot leave clusterMasterRef field empty or else we cannot connect to CM
	if len(cr.Spec.ClusterMasterRef.Name) == 0 {
		return fmt.Errorf("IndexerCluster spec should refer to ClusterMaster via clusterMasterRef")
	}

	// Multisite / multipart clusters: can't reference a cluster master located in another namespace because of Service and Secret limitations
	if len(cr.Spec.ClusterMasterRef.Namespace) > 0 && cr.Spec.ClusterMasterRef.Namespace != cr.GetNamespace() {
		return fmt.Errorf("Multisite cluster does not support cluster master to be located in a different namespace")
	}
	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}
