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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

func TestPostgresDatabaseAdmissionValidation(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	for _, tt := range []struct {
		name     string
		validate func([]enterpriseApi.PostgresDatabaseMountSpec, []corev1.Volume) field.ErrorList
	}{
		{"Standalone", func(mounts []enterpriseApi.PostgresDatabaseMountSpec, volumes []corev1.Volume) field.ErrorList {
			return ValidateStandaloneCreate(&enterpriseApi.Standalone{Spec: enterpriseApi.StandaloneSpec{
				PostgresDatabaseMounts: mounts, Volumes: volumes,
			}})
		}},
		{"SearchHeadCluster", func(mounts []enterpriseApi.PostgresDatabaseMountSpec, volumes []corev1.Volume) field.ErrorList {
			return ValidateSearchHeadClusterCreate(&enterpriseApi.SearchHeadCluster{Spec: enterpriseApi.SearchHeadClusterSpec{
				Replicas: 3, PostgresDatabaseMounts: mounts, Volumes: volumes,
			}})
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			valid := enterpriseApi.PostgresDatabaseMountSpec{Name: "mount", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "database"}, Database: "app"}
			assert.Empty(t, tt.validate([]enterpriseApi.PostgresDatabaseMountSpec{valid}, nil))
			for _, change := range []func(*enterpriseApi.PostgresDatabaseMountSpec){
				func(s *enterpriseApi.PostgresDatabaseMountSpec) { s.Name = "../invalid" },
				func(s *enterpriseApi.PostgresDatabaseMountSpec) { s.PostgresDatabaseRef.Name = "" },
				func(s *enterpriseApi.PostgresDatabaseMountSpec) { s.Database = "invalid_database" },
			} {
				invalid := valid
				change(&invalid)
				errors := tt.validate([]enterpriseApi.PostgresDatabaseMountSpec{invalid}, nil)
				assert.NotEmpty(t, errors)
				for _, err := range errors {
					assert.Equal(t, "spec.postgresDatabaseMounts", err.Field)
				}
			}
			assert.NotEmpty(t, tt.validate([]enterpriseApi.PostgresDatabaseMountSpec{valid}, []corev1.Volume{{Name: common.PostgresDatabaseVolumeName}}))
		})
	}
}

func TestPostgresDatabaseAdmissionFeatureGate(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, false)
	valid := []enterpriseApi.PostgresDatabaseMountSpec{{Name: "mount", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "database"}, Database: "app"}}
	errors := validatePostgresDatabaseMounts(valid, nil)
	assert.Len(t, errors, 1)
	if len(errors) == 1 {
		assert.Equal(t, field.ErrorTypeForbidden, errors[0].Type)
	}
	assert.Empty(t, validatePostgresDatabaseMounts(nil, []corev1.Volume{{Name: common.PostgresDatabaseVolumeName}}))
}
