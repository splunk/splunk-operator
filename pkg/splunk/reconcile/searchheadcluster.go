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

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// ApplySearchHeadCluster reconciles the state for a Splunk Enterprise search head cluster.
func ApplySearchHeadCluster(client ControllerClient, cr *enterprisev1.SearchHeadCluster) (reconcile.Result, error) {
	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplySearchHeadCluster").WithValues("name", cr.GetIdentifier(), "namespace", cr.GetNamespace())

	// validate and updates defaults for CR
	err := enterprise.ValidateSearchHeadClusterSpec(&cr.Spec)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterprisev1.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-search-head", cr.GetIdentifier())
	defer func() {
		client.Status().Update(context.TODO(), cr)
	}()

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := CheckSplunkDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterprisev1.PhaseTerminating
			cr.Status.DeployerPhase = enterprisev1.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update general config resources
	secrets, err := ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, enterprise.SplunkSearchHead)
	if err != nil {
		return result, err
	}

	// create or update a headless search head cluster service
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkSearchHead, true))
	if err != nil {
		return result, err
	}

	// create or update a regular search head cluster service
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkSearchHead, false))
	if err != nil {
		return result, err
	}

	// create or update a deployer service
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkDeployer, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the deployer
	statefulSet, err := enterprise.GetDeployerStatefulSet(cr)
	if err != nil {
		return result, err
	}
	cr.Status.DeployerPhase, err = ApplyStatefulSet(client, statefulSet)
	if err == nil && cr.Status.DeployerPhase == enterprisev1.PhaseReady {
		mgr := DefaultStatefulSetPodManager{}
		cr.Status.DeployerPhase, err = UpdateStatefulSetPods(client, statefulSet, &mgr, 1)
	}
	if err != nil {
		cr.Status.DeployerPhase = enterprisev1.PhaseError
		return result, err
	}

	// create or update statefulset for the search heads
	statefulSet, err = enterprise.GetSearchHeadStatefulSet(cr)
	if err != nil {
		return result, err
	}
	cr.Status.Phase, err = ApplyStatefulSet(client, statefulSet)
	if err != nil {
		return result, err
	}

	// update CR status with SHC information
	mgr := SearchHeadClusterPodManager{client: client, log: scopedLog, cr: cr, secrets: secrets}
	err = mgr.updateStatus(statefulSet)
	if err != nil || cr.Status.ReadyReplicas == 0 || !cr.Status.Initialized || !cr.Status.CaptainReady {
		scopedLog.Error(err, "Search head cluster is not ready")
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

// SearchHeadClusterPodManager is used to manage the pods within a search head cluster
type SearchHeadClusterPodManager struct {
	client  ControllerClient
	log     logr.Logger
	cr      *enterprisev1.SearchHeadCluster
	secrets *corev1.Secret
}

// PrepareScaleDown for SearchHeadClusterPodManager prepares search head pod to be removed via scale down event; it returns true when ready
func (mgr *SearchHeadClusterPodManager) PrepareScaleDown(n int32) (bool, error) {
	// start by quarantining the pod
	result, err := mgr.PrepareRecycle(n)
	if err != nil || !result {
		return result, err
	}

	// pod is quarantined; decommission it
	memberName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkSearchHead, mgr.cr.GetIdentifier(), n)
	mgr.log.Info("Removing member from search head cluster", "memberName", memberName)
	c := mgr.getClient(n)
	err = c.RemoveSearchHeadClusterMember()
	if err != nil {
		return false, err
	}

	// delete PVCs used by the pod so that a future scale up will have clean state
	for _, vol := range []string{"pvc-etc", "pvc-var"} {
		namespacedName := types.NamespacedName{
			Namespace: mgr.cr.GetNamespace(),
			Name:      fmt.Sprintf("%s-%s", vol, memberName),
		}
		var pvc corev1.PersistentVolumeClaim
		err := mgr.client.Get(context.TODO(), namespacedName, &pvc)
		if err != nil {
			return false, err
		}

		log.Info("Deleting PVC", "name", pvc.ObjectMeta.Name)
		err = mgr.client.Delete(context.Background(), &pvc)
		if err != nil {
			return false, err
		}
	}

	// all done -> ok to scale down the statefulset
	return true, nil
}

// PrepareRecycle for SearchHeadClusterPodManager prepares search head pod to be recycled for updates; it returns true when ready
func (mgr *SearchHeadClusterPodManager) PrepareRecycle(n int32) (bool, error) {
	memberName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkSearchHead, mgr.cr.GetIdentifier(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// Detain search head
		mgr.log.Info("Detaining search head cluster member", "memberName", memberName)
		c := mgr.getClient(n)
		return false, c.SetSearchHeadDetention(true)

	case "ManualDetention":
		// Wait until active searches have drained
		searchesComplete := mgr.cr.Status.Members[n].ActiveHistoricalSearchCount+mgr.cr.Status.Members[n].ActiveRealtimeSearchCount == 0
		if searchesComplete {
			mgr.log.Info("Detention complete", "memberName", memberName)
		} else {
			mgr.log.Info("Waiting for active searches to complete", "memberName", memberName)
		}
		return searchesComplete, nil
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// FinishRecycle for SearchHeadClusterPodManager completes recycle event for search head pod; it returns true when complete
func (mgr *SearchHeadClusterPodManager) FinishRecycle(n int32) (bool, error) {
	memberName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkSearchHead, mgr.cr.GetIdentifier(), n)

	switch mgr.cr.Status.Members[n].Status {
	case "Up":
		// not in detention
		return true, nil

	case "ManualDetention":
		// release from detention
		mgr.log.Info("Releasing search head cluster member from detention", "memberName", memberName)
		c := mgr.getClient(n)
		return false, c.SetSearchHeadDetention(false)
	}

	// unhandled status
	return false, fmt.Errorf("Status=%s", mgr.cr.Status.Members[n].Status)
}

// getClient for SearchHeadClusterPodManager returns a SplunkClient for the member n
func (mgr *SearchHeadClusterPodManager) getClient(n int32) *enterprise.SplunkClient {
	memberName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkSearchHead, mgr.cr.GetIdentifier(), n)
	fqdnName := resources.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, enterprise.GetSplunkServiceName(enterprise.SplunkSearchHead, mgr.cr.GetIdentifier(), true)))
	return enterprise.NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// updateStatus for SearchHeadClusterPodManager uses the REST API to update the status for a SearcHead custom resource
func (mgr *SearchHeadClusterPodManager) updateStatus(statefulSet *appsv1.StatefulSet) error {
	// populate members status using REST API to get search head cluster member info
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if mgr.cr.Status.ReadyReplicas == 0 {
		return nil
	}
	gotCaptainInfo := false
	mgr.cr.Status.Members = []enterprisev1.SearchHeadClusterMemberStatus{}
	for n := int32(0); n < mgr.cr.Status.ReadyReplicas; n++ {
		c := mgr.getClient(n)
		memberName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkSearchHead, mgr.cr.GetIdentifier(), n)
		memberStatus := enterprisev1.SearchHeadClusterMemberStatus{Name: memberName}
		memberInfo, err := c.GetSearchHeadClusterMemberInfo()
		if err == nil {
			memberStatus.Status = memberInfo.Status
			memberStatus.Adhoc = memberInfo.Adhoc
			memberStatus.Registered = memberInfo.Registered
			memberStatus.ActiveHistoricalSearchCount = memberInfo.ActiveHistoricalSearchCount
			memberStatus.ActiveRealtimeSearchCount = memberInfo.ActiveRealtimeSearchCount
			if !gotCaptainInfo {
				// try querying captain api; note that this should work on any node
				captainInfo, err := c.GetSearchHeadCaptainInfo()
				if err == nil {
					mgr.cr.Status.Captain = captainInfo.Label
					mgr.cr.Status.CaptainReady = captainInfo.ServiceReady
					mgr.cr.Status.Initialized = captainInfo.Initialized
					mgr.cr.Status.MinPeersJoined = captainInfo.MinPeersJoined
					mgr.cr.Status.MaintenanceMode = captainInfo.MaintenanceMode
					gotCaptainInfo = true
				}
			}
		} else if n < statefulSet.Status.Replicas {
			// ignore error if pod was just terminated for scale down event (n >= Replicas)
			mgr.log.Error(err, "Unable to retrieve search head cluster member info", "memberName", memberName)
			return err
		}
		mgr.cr.Status.Members = append(mgr.cr.Status.Members, memberStatus)
	}

	return nil
}
