/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package core

import (
	"strings"

	cnpgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
)

func EffectiveRoleNames(dbSpec platformv1alpha1.DatabaseDefinition) ports.DatabaseRoleNames {
	roles := ports.DatabaseRoleNames{
		Admin: adminRoleName(dbSpec.Name),
		RW:    rwRoleName(dbSpec.Name),
	}
	if dbSpec.AdminRoleName != "" {
		roles.Admin = dbSpec.AdminRoleName
	}
	if dbSpec.RWRoleName != "" {
		roles.RW = dbSpec.RWRoleName
	}
	return roles
}

// IsReservedRoleName reports whether CNPG or PostgreSQL reserves a role name.
// Lower-casing preserves the operator's case-insensitive admission policy while
// delegating the reserved-name list and prefixes to CNPG.
func IsReservedRoleName(name string) bool {
	return cnpgpostgres.IsRoleReserved(strings.ToLower(name))
}

type databasePrivilegeTarget struct {
	Database string
	Roles    ports.DatabaseRoleNames
}
