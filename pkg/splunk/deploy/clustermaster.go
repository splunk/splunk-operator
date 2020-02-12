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

// ReconcileClusterMaster reconciles the state of a Splunk Enterprise cluster master (used to manage an indexer cluster).
func ReconcileClusterMaster(client ControllerClient, cr *enterprisev1.ClusterMaster) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateClusterMasterSpec(&cr.Spec)
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

	// create or update a service
	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkClusterMaster, false))
	if err != nil {
		return err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetClusterMasterStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}

// applyClusterMaster creates or updates a Splunk Enterprise ClusterMaster custom resource definition (CRD).
func applyClusterMaster(client ControllerClient, cr *enterprisev1.SplunkEnterprise) error {
	scopedLog := log.WithName("applyClusterMaster").WithValues("name", cr.GetIdentifier(), "namespace", cr.GetNamespace())

	revised, err := enterprise.GetClusterMasterResource(cr)
	if err != nil {
		return err
	}
	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetIdentifier()}
	if cr.Spec.LicenseURL != "" {
		revised.Spec.LicenseMasterRef = corev1.ObjectReference{Namespace: revised.GetNamespace(), Name: revised.GetIdentifier()}
	}
	var current enterprisev1.ClusterMaster

	err = client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing ClusterMaster
		if !reflect.DeepEqual(revised.Spec, current.Spec) {
			current.Spec = revised.Spec
			err = UpdateResource(client, &current)
		} else {
			scopedLog.Info("No changes for ClusterMaster")
		}
	} else {
		err = CreateResource(client, revised)
	}

	return err
}
