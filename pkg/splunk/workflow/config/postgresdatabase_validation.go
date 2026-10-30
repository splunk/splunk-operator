// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
)

// ValidatePostgresDatabaseMounts adapts workload inputs to the shared stateless
// checks, including runtime callers that do not pass through admission.
func ValidatePostgresDatabaseMounts(specs []enterpriseApi.PostgresDatabaseMountSpec, volumes []corev1.Volume) error {
	inputs := make([]util.PostgresDatabaseMountInput, 0, len(specs))
	for _, spec := range specs {
		inputs = append(inputs, util.PostgresDatabaseMountInput{
			Name: spec.Name, ReferenceName: spec.PostgresDatabaseRef.Name, Database: spec.Database,
		})
	}
	volumeNames := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		volumeNames = append(volumeNames, volume.Name)
	}
	return util.ValidatePostgresDatabaseMounts(inputs, volumeNames)
}
