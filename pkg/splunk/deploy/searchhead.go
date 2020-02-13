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
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// ReconcileSearchHead reconciles the state for a Splunk Enterprise search head cluster.
func ReconcileSearchHead(client ControllerClient, cr *enterprisev1.SearchHead) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateSearchHeadSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		_, err := CheckSplunkDeletion(cr, client)
		return err
	}

	// create or update general config resources
	err = ReconcileSplunkConfig(client, cr, cr.Spec.CommonSplunkSpec)
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
	err = ApplyStatefulSet(client, statefulSet)
	if err != nil {
		return err
	}

	// create or update statefulset for the search heads
	statefulSet, err = enterprise.GetSearchHeadStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}

// applySearchHead creates or updates a Splunk Enterprise SearchHead custom resource definition (CRD).
func applySearchHead(client ControllerClient, cr *enterprisev1.SplunkEnterprise) error {
	scopedLog := log.WithName("applySearchHead").WithValues("name", cr.GetIdentifier(), "namespace", cr.GetNamespace())

	revised, err := enterprise.GetSearchHeadResource(cr)
	if err != nil {
		return err
	}
	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetIdentifier()}
	if cr.Spec.LicenseURL != "" {
		revised.Spec.LicenseMasterRef = corev1.ObjectReference{Namespace: revised.GetNamespace(), Name: revised.GetIdentifier()}
	}
	revised.Spec.IndexerRef = corev1.ObjectReference{Namespace: revised.GetNamespace(), Name: revised.GetIdentifier()}
	var current enterprisev1.SearchHead

	err = client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing SearchHead
		if !reflect.DeepEqual(revised.Spec, current.Spec) {
			current.Spec = revised.Spec
			err = UpdateResource(client, &current)
		} else {
			scopedLog.Info("No changes for SearchHead")
		}
	} else {
		err = CreateResource(client, revised)
	}

	return err
}
