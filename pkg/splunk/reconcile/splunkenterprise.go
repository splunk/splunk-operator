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

// ReconcileSplunkEnterprise reconciles the state of a SplunkEnterprise deployment.
func ReconcileSplunkEnterprise(client ControllerClient, cr *enterprisev1.SplunkEnterprise) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateSplunkEnterpriseSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		_, err := CheckSplunkDeletion(cr, client)
		return err
	}

	// create a spark cluster if EnableDFS == true
	if cr.Spec.EnableDFS {
		err = applySpark(client, cr)
		if err != nil {
			return err
		}
	}

	// create standalone instances when > 0
	if cr.Spec.Topology.Standalones > 0 {
		err = applyStandalone(client, cr)
		if err != nil {
			return err
		}
	}

	// create a cluster when at least 1 search head and 1 indexer
	if cr.Spec.Topology.Indexers > 0 && cr.Spec.Topology.SearchHeads > 0 {
		// create or update license master if we have a licenseURL
		if cr.Spec.LicenseURL != "" {
			err = applyLicenseMaster(client, cr)
			if err != nil {
				return err
			}
		}

		// create or update indexers
		err = applyIndexer(client, cr)
		if err != nil {
			return err
		}

		// create or update search heads
		err = applySearchHead(client, cr)
		if err != nil {
			return err
		}
	}

	return nil
}
