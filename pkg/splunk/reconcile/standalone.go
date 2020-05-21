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

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// ApplyStandalone reconciles the StatefulSet for N standalone instances of Splunk Enterprise.
func ApplyStandalone(client ControllerClient, cr *enterprisev1.Standalone) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	// validate and updates defaults for CR
	err := enterprise.ValidateStandaloneSpec(&cr.Spec)
	if err != nil {
		return result, err
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
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update general config resources
	_, err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, enterprise.SplunkStandalone)
	if err != nil {
		return result, err
	}

	// create or update a headless service (this is required by DFS for Spark->standalone comms, possibly other things)
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkStandalone, true))
	if err != nil {
		return result, err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetStandaloneStatefulSet(cr)
	if err != nil {
		return result, err
	}
	mgr := DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
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
