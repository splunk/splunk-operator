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

// ReconcileStandalone reconciles the StatefulSet for N standalone instances of Splunk Enterprise.
func ReconcileStandalone(client ControllerClient, cr *enterprisev1.Standalone) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateStandaloneSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// updates status after function completes
	cr.Status.Phase = enterprisev1.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-standalone", cr.GetIdentifier())
	defer func() {
		client.Status().Update(context.TODO(), cr)
	}()

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := CheckSplunkDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterprisev1.PhaseTerminating
			client.Status().Update(context.TODO(), cr)
		}
		return err
	}

	// create or update general config resources
	err = ReconcileSplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, enterprise.SplunkStandalone)
	if err != nil {
		return err
	}

	// create or update a headless service (this is required by DFS for Spark->standalone comms, possibly other things)
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkStandalone, true))
	if err != nil {
		return err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetStandaloneStatefulSet(cr)
	if err != nil {
		return err
	}
	cr.Status.Phase, err = ApplyStatefulSet(client, statefulSet)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err == nil && cr.Status.Phase == enterprisev1.PhaseReady {
		cr.Status.Phase, err = ReconcileStatefulSetPods(client, statefulSet, cr.Status.ReadyReplicas, cr.Spec.Replicas, nil)
	}
	if err != nil {
		cr.Status.Phase = enterprisev1.PhaseError
		return err
	}

	return nil
}
