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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestPostgresDatabaseRuntimeValidation(t *testing.T) {
	valid := enterpriseApi.PostgresDatabaseMountSpec{Name: "mount", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "database"}, Database: "app"}
	require.NoError(t, ValidatePostgresDatabaseMounts([]enterpriseApi.PostgresDatabaseMountSpec{valid}, nil))
	for _, change := range []func(*enterpriseApi.PostgresDatabaseMountSpec){
		func(s *enterpriseApi.PostgresDatabaseMountSpec) { s.Name = "../invalid" },
		func(s *enterpriseApi.PostgresDatabaseMountSpec) { s.PostgresDatabaseRef.Name = "" },
		func(s *enterpriseApi.PostgresDatabaseMountSpec) { s.Database = "invalid_database" },
	} {
		invalid := valid
		change(&invalid)
		require.Error(t, ValidatePostgresDatabaseMounts([]enterpriseApi.PostgresDatabaseMountSpec{invalid}, nil))
		// Invalid references fail before any dependency lookup.
		_, err := ResolvePostgresDatabaseMounts(t.Context(), nil, "namespace", []enterpriseApi.PostgresDatabaseMountSpec{invalid})
		require.Error(t, err)
	}
	require.ErrorContains(t, ValidatePostgresDatabaseMounts([]enterpriseApi.PostgresDatabaseMountSpec{valid}, []corev1.Volume{{Name: common.PostgresDatabaseVolumeName}}), "reserved")
}
