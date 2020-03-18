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

package reconcile

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// ApplyIndexerCluster reconciles the state of a Splunk Enterprise indexer cluster.
func ApplyIndexerCluster(client ControllerClient, cr *enterprisev1.IndexerCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplyIndexerCluster").WithValues("name", cr.GetIdentifier(), "namespace", cr.GetNamespace())

	// validate and updates defaults for CR
	err := enterprise.ValidateIndexerClusterSpec(&cr.Spec)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterprisev1.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-indexer", cr.GetIdentifier())
	defer func() {
		client.Status().Update(context.TODO(), cr)
	}()

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := CheckSplunkDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterprisev1.PhaseTerminating
			cr.Status.ClusterMasterPhase = enterprisev1.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update general config resources
	secrets, err := ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, enterprise.SplunkIndexer)
	if err != nil {
		return result, err
	}

	// create or update a headless service for indexer cluster
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkIndexer, true))
	if err != nil {
		return result, err
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkIndexer, false))
	if err != nil {
		return result, err
	}

	// create or update a regular service for the cluster master
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkClusterMaster, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the cluster master
	statefulSet, err := enterprise.GetClusterMasterStatefulSet(cr)
	if err != nil {
		return result, err
	}
	cr.Status.ClusterMasterPhase, err = ApplyStatefulSet(client, statefulSet)
	if err == nil && cr.Status.Phase == enterprisev1.PhaseReady {
		mgr := DefaultStatefulSetPodManager{}
		cr.Status.ClusterMasterPhase, err = UpdateStatefulSetPods(client, statefulSet, &mgr, 1)
	}
	if err != nil {
		cr.Status.ClusterMasterPhase = enterprisev1.PhaseError
		return result, err
	}

	// create or update statefulset for the indexers
	statefulSet, err = enterprise.GetIndexerStatefulSet(cr)
	if err != nil {
		return result, err
	}
	cr.Status.Phase, err = ApplyStatefulSet(client, statefulSet)
	if err != nil {
		return result, err
	}

	// update CR status with SHC information
	mgr := IndexerClusterPodManager{client: client, log: scopedLog, cr: cr, secrets: secrets}
	err = mgr.updateStatus(statefulSet)
	if err != nil || cr.Status.ReadyReplicas == 0 || !cr.Status.Initialized || !cr.Status.IndexingReady || !cr.Status.ServiceReady {
		scopedLog.Error(err, "Indexer cluster is not ready")
		cr.Status.Phase = enterprisev1.PhasePending
		return result, nil
	}

	// manage scaling and updates
	cr.Status.Phase, err = UpdateStatefulSetPods(client, statefulSet, &mgr, cr.Spec.Replicas)
	if err != nil {
		cr.Status.Phase = enterprisev1.PhaseError
		return result, err
	}

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterprisev1.PhaseReady {
		result.Requeue = false
	}
	return result, nil
}

// IndexerClusterPodManager is used to manage the pods within a search head cluster
type IndexerClusterPodManager struct {
	client  ControllerClient
	log     logr.Logger
	cr      *enterprisev1.IndexerCluster
	secrets *corev1.Secret
}

// PrepareScaleDown for IndexerClusterPodManager prepares indexer pod to be removed via scale down event; it returns true when ready
func (mgr *IndexerClusterPodManager) PrepareScaleDown(n int32) (bool, error) {
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

// PrepareRecycle for IndexerClusterPodManager prepares indexer pod to be recycled for updates; it returns true when ready
func (mgr *IndexerClusterPodManager) PrepareRecycle(n int32) (bool, error) {
	return mgr.decommission(n, false)
}

// FinishRecycle for IndexerClusterPodManager completes recycle event for indexer pod; it returns true when complete
func (mgr *IndexerClusterPodManager) FinishRecycle(n int32) (bool, error) {
	return mgr.cr.Status.Peers[n].Status == "Up", nil
}

// decommission for IndexerClusterPodManager decommissions an indexer pod; it returns true when ready
func (mgr *IndexerClusterPodManager) decommission(n int32, enforceCounts bool) (bool, error) {
	peerName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkIndexer, mgr.cr.GetIdentifier(), n)

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

// getClient for IndexerClusterPodManager returns a SplunkClient for the member n
func (mgr *IndexerClusterPodManager) getClient(n int32) *enterprise.SplunkClient {
	memberName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkIndexer, mgr.cr.GetIdentifier(), n)
	fqdnName := resources.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, enterprise.GetSplunkServiceName(enterprise.SplunkIndexer, mgr.cr.GetIdentifier(), true)))
	return enterprise.NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// getClusterMasterClient for IndexerClusterPodManager returns a SplunkClient for cluster master
func (mgr *IndexerClusterPodManager) getClusterMasterClient() *enterprise.SplunkClient {
	fqdnName := resources.GetServiceFQDN(mgr.cr.GetNamespace(), enterprise.GetSplunkServiceName(enterprise.SplunkClusterMaster, mgr.cr.GetIdentifier(), false))
	return enterprise.NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// updateStatus for IndexerClusterPodManager uses the REST API to update the status for a SearcHead custom resource
func (mgr *IndexerClusterPodManager) updateStatus(statefulSet *appsv1.StatefulSet) error {
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	mgr.cr.Status.Peers = []enterprisev1.IndexerClusterMemberStatus{}

	if mgr.cr.Status.ClusterMasterPhase != enterprisev1.PhaseReady {
		mgr.cr.Status.Phase = enterprisev1.PhasePending
		mgr.cr.Status.Initialized = false
		mgr.cr.Status.IndexingReady = false
		mgr.cr.Status.ServiceReady = false
		mgr.cr.Status.MaintenanceMode = false
		mgr.log.Info("Waiting for cluster master to become ready")
		return nil
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
		peerName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkIndexer, mgr.cr.GetIdentifier(), n)
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
		mgr.cr.Status.Peers = append(mgr.cr.Status.Peers, peerStatus)
	}

	return nil
}
