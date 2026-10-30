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

package util

import (
	"strconv"
	"strings"
	"testing"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePostgresDatabaseMounts(t *testing.T) {
	valid := PostgresDatabaseMountInput{Name: "mount", ReferenceName: "database", Database: "app"}
	for _, tt := range []struct {
		name        string
		change      func(*PostgresDatabaseMountInput)
		volumeNames []string
		duplicate   bool
		want        string
	}{
		{name: "empty name", change: func(s *PostgresDatabaseMountInput) { s.Name = "" }, want: "invalid PostgresDatabase mount name"},
		{name: "path traversal", change: func(s *PostgresDatabaseMountInput) { s.Name = "../etc" }, want: "invalid PostgresDatabase mount name"},
		{name: "long name", change: func(s *PostgresDatabaseMountInput) { s.Name = strings.Repeat("a", 61) }, want: "invalid PostgresDatabase mount name"},
		{name: "empty reference", change: func(s *PostgresDatabaseMountInput) { s.ReferenceName = "" }, want: "requires a valid postgresDatabaseRef.name"},
		{name: "invalid reference", change: func(s *PostgresDatabaseMountInput) { s.ReferenceName = "other/namespace" }, want: "requires a valid postgresDatabaseRef.name"},
		{name: "empty database", change: func(s *PostgresDatabaseMountInput) { s.Database = "" }, want: "requires a valid database name"},
		{name: "invalid database", change: func(s *PostgresDatabaseMountInput) { s.Database = "app_data" }, want: "requires a valid database name"},
		{name: "long database", change: func(s *PostgresDatabaseMountInput) { s.Database = strings.Repeat("a", 31) }, want: "requires a valid database name"},
		{name: "reserved volume", volumeNames: []string{common.PostgresDatabaseVolumeName}, want: "is reserved for PostgresDatabase bundles"},
		{name: "duplicate", duplicate: true, want: "duplicate PostgresDatabase mount name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			if tt.change != nil {
				tt.change(&input)
			}
			inputs := []PostgresDatabaseMountInput{input}
			if tt.duplicate {
				inputs = append(inputs, input)
			}
			require.ErrorContains(t, ValidatePostgresDatabaseMounts(inputs, tt.volumeNames), tt.want)
		})
	}
	require.NoError(t, ValidatePostgresDatabaseMounts([]PostgresDatabaseMountInput{valid}, []string{"other-volume"}))
	other := valid
	other.Name = "other"
	require.ErrorContains(t, ValidatePostgresDatabaseMounts([]PostgresDatabaseMountInput{valid, other}, nil), "conflicting")
	assert.NoError(t, ValidatePostgresDatabaseMounts(nil, []string{common.PostgresDatabaseVolumeName}))

	var maximum []PostgresDatabaseMountInput
	for i := range 10 {
		maximum = append(maximum, PostgresDatabaseMountInput{Name: "mount" + strconv.Itoa(i), ReferenceName: "database", Database: "db" + strconv.Itoa(i)})
	}
	require.NoError(t, ValidatePostgresDatabaseMounts(maximum, nil))
	require.ErrorContains(t, ValidatePostgresDatabaseMounts(append(maximum, valid), nil), "at most 10 entries")
}
