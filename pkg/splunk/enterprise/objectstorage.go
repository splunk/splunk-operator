/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package enterprise

import (
	"context"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyObjectStorage reconciles the state of an IngestorCluster custom resource
func ApplyObjectStorage(ctx context.Context, client client.Client, cr *enterpriseApi.ObjectStorage) (reconcile.Result, error) {
	var err error

	// Unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	eventPublisher, _ := newK8EventPublisher(client, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	cr.Kind = "ObjectStorage"

	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// Check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil {
			cr.Status.Phase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	cr.Status.Phase = enterpriseApi.PhaseReady

	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}
