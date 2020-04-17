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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
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
	scopedLog := rconcilelog.WithName("ApplySearchHeadCluster").WithValues("name", cr.GetIdentifier(), "namespace", cr.GetNamespace())

	// validate and updates defaults for CR
	err := enterprise.ValidateSearchHeadClusterSpec(&cr.Spec)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterprisev1.PhaseError
	cr.Status.DeployerPhase = enterprisev1.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-search-head", cr.GetIdentifier())
	if cr.Status.Members == nil {
		cr.Status.Members = []enterprisev1.SearchHeadClusterMemberStatus{}
	}
	defer func() {
		err = client.Status().Update(context.TODO(), cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
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
	deployerManager := DefaultStatefulSetPodManager{}
	phase, err := deployerManager.Update(client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.DeployerPhase = phase

	// create or update statefulset for the search heads
	statefulSet, err = enterprise.GetSearchHeadStatefulSet(cr)
	if err != nil {
		return result, err
	}
	mgr := SearchHeadClusterPodManager{log: scopedLog, cr: cr, secrets: secrets, newSplunkClient: splclient.NewSplunkClient}
	phase, err = mgr.Update(client, statefulSet, cr.Spec.Replicas)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterprisev1.PhaseReady {
		result.Requeue = false
	}
	return result, nil
}

// SearchHeadClusterPodManager is used to manage the pods within a search head cluster
type SearchHeadClusterPodManager struct {
	log             logr.Logger
	cr              *enterprisev1.SearchHeadCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// Update for SearchHeadClusterPodManager handles all updates for a statefulset of search heads
func (mgr *SearchHeadClusterPodManager) Update(c ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterprisev1.ResourcePhase, error) {
	// update statefulset, if necessary
	_, err := ApplyStatefulSet(c, statefulSet)
	if err != nil {
		return enterprisev1.PhaseError, err
	}

	// update CR status with SHC information
	err = mgr.updateStatus(statefulSet)
	if err != nil || mgr.cr.Status.ReadyReplicas == 0 || !mgr.cr.Status.Initialized || !mgr.cr.Status.CaptainReady {
		mgr.log.Error(err, "Search head cluster is not ready")
		return enterprisev1.PhasePending, nil
	}

	// manage scaling and updates
	return UpdateStatefulSetPods(c, statefulSet, mgr, desiredReplicas)
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

	case "": // this can happen after the member has already been recycled and we're just waiting for state to update
		mgr.log.Info("Member has empty Status", "memberName", memberName)
		return false, nil
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
func (mgr *SearchHeadClusterPodManager) getClient(n int32) *splclient.SplunkClient {
	memberName := enterprise.GetSplunkStatefulsetPodName(enterprise.SplunkSearchHead, mgr.cr.GetIdentifier(), n)
	fqdnName := resources.GetServiceFQDN(mgr.cr.GetNamespace(),
		fmt.Sprintf("%s.%s", memberName, enterprise.GetSplunkServiceName(enterprise.SplunkSearchHead, mgr.cr.GetIdentifier(), true)))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// updateStatus for SearchHeadClusterPodManager uses the REST API to update the status for a SearcHead custom resource
func (mgr *SearchHeadClusterPodManager) updateStatus(statefulSet *appsv1.StatefulSet) error {
	// populate members status using REST API to get search head cluster member info
	mgr.cr.Status.Captain = ""
	mgr.cr.Status.CaptainReady = false
	mgr.cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if mgr.cr.Status.ReadyReplicas == 0 {
		return nil
	}
	gotCaptainInfo := false
	for n := int32(0); n < statefulSet.Status.Replicas; n++ {
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
		} else {
			mgr.log.Error(err, "Unable to retrieve search head cluster member info", "memberName", memberName)
		}

		if err == nil && !gotCaptainInfo {
			// try querying captain api; note that this should work on any node
			captainInfo, err := c.GetSearchHeadCaptainInfo()
			if err == nil {
				mgr.cr.Status.Captain = captainInfo.Label
				mgr.cr.Status.CaptainReady = captainInfo.ServiceReady
				mgr.cr.Status.Initialized = captainInfo.Initialized
				mgr.cr.Status.MinPeersJoined = captainInfo.MinPeersJoined
				mgr.cr.Status.MaintenanceMode = captainInfo.MaintenanceMode
				gotCaptainInfo = true
			} else {
				mgr.log.Error(err, "Unable to retrieve captain info", "memberName", memberName)
			}
		}

		if n < int32(len(mgr.cr.Status.Members)) {
			mgr.cr.Status.Members[n] = memberStatus
		} else {
			mgr.cr.Status.Members = append(mgr.cr.Status.Members, memberStatus)
		}
	}

	// truncate any extra members that we didn't check (leftover from scale down)
	if statefulSet.Status.Replicas < int32(len(mgr.cr.Status.Members)) {
		mgr.cr.Status.Members = mgr.cr.Status.Members[:statefulSet.Status.Replicas]
	}

	return nil
}
