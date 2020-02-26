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

// ReconcileIndexer reconciles the state of a Splunk Enterprise indexer cluster.
func ReconcileIndexer(client ControllerClient, cr *enterprisev1.Indexer) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateIndexerSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		_, err := CheckSplunkDeletion(cr, client)
		return err
	}

	// create or update general config resources
	err = ReconcileSplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, enterprise.SplunkIndexer)
	if err != nil {
		return err
	}

	// create or update a headless service for indexer cluster
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkIndexer, true))
	if err != nil {
		return err
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkIndexer, false))
	if err != nil {
		return err
	}

	// create or update a regular service for the cluster master
	err = ApplyService(client, enterprise.GetSplunkService(cr, cr.Spec.CommonSpec, enterprise.SplunkClusterMaster, false))
	if err != nil {
		return err
	}

	// create or update statefulset for the cluster master
	statefulSet, err := enterprise.GetClusterMasterStatefulSet(cr)
	if err != nil {
		return err
	}
	err = ApplyStatefulSet(client, statefulSet)
	if err != nil {
		return err
	}

	// create or update statefulset for the indexers
	statefulSet, err = enterprise.GetIndexerStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}
