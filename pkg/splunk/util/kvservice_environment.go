// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

var kvServiceManagedEnvironment = map[string]string{
	"KVSERVICE_SINGLE_TENANT_K8S_MODE":            "Postgres",
	"KVSERVICE_SINGLE_TENANT_PASS4_SYMM_KEY":      "Postgres",
	"KVSERVICE_CMPK_SERVICE_DATABASE":             "Postgres",
	"KVSERVICE_CMPK_TENANT_MAP_DIR":               "Postgres",
	"KVSERVICE_CMPK_TENANT_PASSWORD_DIR":          "Postgres",
	"KVSERVICE_CMPK_TENANT_VERIFICATION_KEYS_DIR": "Postgres",
	"KVSERVICE_DB_HOST":                           "Postgres",
	"KVSERVICE_DB_PORT":                           "Postgres",
	"KVSERVICE_DB_ROOT_USER":                      "Postgres",
	"KVSERVICE_DB_SERVICE_USER":                   "Postgres",
	"KVSERVICE_DB_PASSWORD":                       "Postgres",
	"KVSERVICE_DB_SERVICE_PASSWORD":               "Postgres",
	"KVSERVICE_SINGLE_TENANT_PRIMARY_HOST":        "Postgres",
	"KVSERVICE_SINGLE_TENANT_PRIMARY_PORT":        "Postgres",
	"KVSERVICE_SINGLE_TENANT_REPLICA_HOST":        "Postgres",
	"KVSERVICE_SINGLE_TENANT_REPLICA_PORT":        "Postgres",
	"TLS_CERTIFICATE":                             "TLS",
	"TLS_PRIVATE_KEY":                             "TLS",
	"TLS_HOST":                                    "TLS",
	"TLS_PORT":                                    "TLS",
}

// KVServiceManagedEnvironmentCategory identifies variables reserved by the operator.
// TLS variables are reserved only when the operator configures certificates.
func KVServiceManagedEnvironmentCategory(name string, certsConfigured bool) (category string, managed bool) {
	category, managed = kvServiceManagedEnvironment[name]
	if category == "TLS" && !certsConfigured {
		return "", false
	}
	return
}
