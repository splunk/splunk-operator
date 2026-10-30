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

package validation

import (
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func validatePostgresDatabaseMounts(specs []enterpriseApi.PostgresDatabaseMountSpec, volumes []corev1.Volume) field.ErrorList {
	path := field.NewPath("spec", "postgresDatabaseMounts")
	var errors field.ErrorList
	if len(specs) > 0 && !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		errors = append(errors, field.Forbidden(path, "requires the PostgresController feature gate"))
	}
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
	if err := util.ValidatePostgresDatabaseMounts(inputs, volumeNames); err != nil {
		errors = append(errors, field.Invalid(path, nil, err.Error()))
	}
	return errors
}
