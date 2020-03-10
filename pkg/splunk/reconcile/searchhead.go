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

package deploy

import (
	"context"
	"fmt"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// ReconcileSearchHeadCluster reconciles the state for a Splunk Enterprise search head cluster.
func ReconcileSearchHeadCluster(client ControllerClient, cr *enterprisev1.SearchHeadCluster) error {
	scopedLog := log.WithName("ReconcileSearchHeadCluster").WithValues("name", cr.GetIdentifier(), "namespace", cr.GetNamespace())

	// validate and updates defaults for CR
	err := enterprise.ValidateSearchHeadClusterSpec(&cr.Spec)
	if err != nil {
		return err
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
		}
		return err
	}

	// create or update general config resources
	secrets, err := ReconcileSplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, enterprise.SplunkSearchHead)
	if err != nil {
		return err
	}

	// create or update a headless search head cluster service
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkSearchHead, true))
	if err != nil {
		return err
	}

	// create or update a regular search head cluster service
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkSearchHead, false))
	if err != nil {
		return err
	}

	// create or update a deployer service
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkDeployer, false))
	if err != nil {
		return err
	}

	// create or update statefulset for the deployer
	statefulSet, err := enterprise.GetDeployerStatefulSet(cr)
	if err != nil {
		return err
	}
	cr.Status.DeployerPhase, err = ApplyStatefulSet(client, statefulSet)
	if err == nil && cr.Status.DeployerPhase == enterprisev1.PhaseReady {
		cr.Status.DeployerPhase, err = ReconcileStatefulSetPods(client, statefulSet, statefulSet.Status.ReadyReplicas, 1, nil)
	}
	if err != nil {
		cr.Status.DeployerPhase = enterprisev1.PhaseError
		return err
	}

	// create or update statefulset for the search heads
	statefulSet, err = enterprise.GetSearchHeadStatefulSet(cr)
	if err != nil {
		return err
	}
	cr.Status.Phase, err = ApplyStatefulSet(client, statefulSet)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if cr.Status.ReadyReplicas > 0 {
		err = enterprise.UpdateSearchHeadClusterStatus(cr, secrets)
		if err != nil {
			scopedLog.Error(err, "Failed to update status")
		}
	}
	if err == nil && cr.Status.Phase == enterprisev1.PhaseReady {
		cr.Status.Phase, err = ReconcileStatefulSetPods(client, statefulSet, cr.Status.ReadyReplicas, cr.Spec.Replicas, nil)
	}
	if err != nil {
		cr.Status.Phase = enterprisev1.PhaseError
		return err
	}

	return nil
}
