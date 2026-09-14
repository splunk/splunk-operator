// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package standalone

import (
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// validateRemoteVolumeSpec validates SmartStore remote volumes.
func validateRemoteVolumeSpec(volList []enterpriseApi.VolumeSpec) error {
	duplicateChecker := make(map[string]bool)
	for i, volume := range volList {
		if duplicateChecker[volume.Name] {
			return fmt.Errorf("duplicate volume name detected: %s. Remove the duplicate entry and reapply the configuration", volume.Name)
		}
		duplicateChecker[volume.Name] = true
		if volume.Name == "" {
			return fmt.Errorf("volume name is missing for volume at : %d", i)
		}
		if volume.Endpoint == "" {
			return fmt.Errorf("volume Endpoint URI is missing")
		}
		if volume.Path == "" {
			return fmt.Errorf("volume Path is missing")
		}
	}
	return nil
}

// validateSmartstoreIndexesSpec validates SmartStore index entries.
func validateSmartstoreIndexesSpec(smartstore *enterpriseApi.SmartStoreSpec) error {
	duplicateChecker := make(map[string]bool)
	for i, index := range smartstore.IndexList {
		if index.Name == "" {
			return fmt.Errorf("index name is missing for index at: %d", i)
		}
		if duplicateChecker[index.Name] {
			return fmt.Errorf("duplicate index name detected: %s.Remove the duplicate entry and reapply the configuration", index.Name)
		}
		duplicateChecker[index.Name] = true
		if index.VolName == "" && smartstore.Defaults.VolName == "" {
			return fmt.Errorf("volumeName is missing for index: %s", index.Name)
		}
		if index.VolName != "" {
			if _, err := splutil.CheckIfVolumeExists(smartstore.VolList, index.VolName); err != nil {
				return fmt.Errorf("invalid configuration for index: %s. %s", index.Name, err)
			}
		}
	}
	return nil
}

// validateSmartstoreSpec validates the SmartStore configuration.
func validateSmartstoreSpec(smartstore *enterpriseApi.SmartStoreSpec) error {
	if !resources.IsSmartstoreConfigured(smartstore) {
		return nil
	}
	if len(smartstore.IndexList) > 0 && len(smartstore.VolList) == 0 {
		return fmt.Errorf("volume configuration is missing. Num. of indexes = %d. Num. of Volumes = %d", len(smartstore.IndexList), len(smartstore.VolList))
	}
	if err := validateRemoteVolumeSpec(smartstore.VolList); err != nil {
		return err
	}
	if smartstore.Defaults.VolName != "" {
		if _, err := splutil.CheckIfVolumeExists(smartstore.VolList, smartstore.Defaults.VolName); err != nil {
			return fmt.Errorf("invalid configuration for defaults volume. %s", err)
		}
	}
	return validateSmartstoreIndexesSpec(smartstore)
}
