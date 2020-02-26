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

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		_, err := CheckSplunkDeletion(cr, client)
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
	return ApplyStatefulSet(client, statefulSet)
}
