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
	"errors"
	"fmt"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyBusConfiguration reconciles the state of an IngestorCluster custom resource
func ApplyBusConfiguration(ctx context.Context, client client.Client, cr *enterpriseApi.BusConfiguration) (reconcile.Result, error) {
	var err error

	// Unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyBusConfiguration")

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	eventPublisher, _ := newK8EventPublisher(client, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	cr.Kind = "BusConfiguration"

	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// Validate and updates defaults for CR
	err = validateBusConfigurationSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateBusConfigurationSpec", fmt.Sprintf("validate bus configuration spec failed %s", err.Error()))
		scopedLog.Error(err, "Failed to validate bus configuration spec")
		return result, err
	}

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

// validateBusConfigurationSpec checks validity and makes default updates to a BusConfigurationSpec and returns error if something is wrong
func validateBusConfigurationSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.BusConfiguration) error {
	return validateBusConfigurationInputs(cr)
}

func validateBusConfigurationInputs(cr *enterpriseApi.BusConfiguration) error {
	// sqs_smartbus type is supported for now
	if cr.Spec.Type != "sqs_smartbus" {
		return errors.New("only sqs_smartbus type is supported in bus configuration")
	}

	// Cannot be empty fields check
	cannotBeEmptyFields := []string{}
	if cr.Spec.SQS.QueueName == "" {
		cannotBeEmptyFields = append(cannotBeEmptyFields, "queueName")
	}

	if cr.Spec.SQS.AuthRegion == "" {
		cannotBeEmptyFields = append(cannotBeEmptyFields, "authRegion")
	}

	if cr.Spec.SQS.DeadLetterQueueName == "" {
		cannotBeEmptyFields = append(cannotBeEmptyFields, "deadLetterQueueName")
	}

	if len(cannotBeEmptyFields) > 0 {
		return errors.New("bus configuration sqs " + strings.Join(cannotBeEmptyFields, ", ") + " cannot be empty")
	}

	// Have to start with https:// or s3:// checks
	haveToStartWithHttps := []string{}
	if !strings.HasPrefix(cr.Spec.SQS.Endpoint, "https://") {
		haveToStartWithHttps = append(haveToStartWithHttps, "endpoint")
	}

	if !strings.HasPrefix(cr.Spec.SQS.LargeMessageStoreEndpoint, "https://") {
		haveToStartWithHttps = append(haveToStartWithHttps, "largeMessageStoreEndpoint")
	}

	if len(haveToStartWithHttps) > 0 {
		return errors.New("bus configuration sqs " + strings.Join(haveToStartWithHttps, ", ") + " must start with https://")
	}

	if !strings.HasPrefix(cr.Spec.SQS.LargeMessageStorePath, "s3://") {
		return errors.New("bus configuration sqs largeMessageStorePath must start with s3://")
	}

	return nil
}
