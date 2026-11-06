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

import (
	"fmt"
	"regexp"
	"strings"
)

var kvServiceTenantNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,29}$`)

// ValidateKVServiceTenantName checks the database naming contract shared by consumers and provisioners.
func ValidateKVServiceTenantName(tenant string) error {
	if !kvServiceTenantNamePattern.MatchString(tenant) {
		return fmt.Errorf("invalid tenant %q: must match ^[a-z][a-z0-9]{0,29}$", tenant)
	}
	return nil
}

// KVServiceTenantDatabaseName returns the retained tenant database resource name.
func KVServiceTenantDatabaseName(tenant string) string { return tenant + "-database" }

// KVServiceNameFromInstance recognizes the operator's existing workload-instance label.
func KVServiceNameFromInstance(instance string) (string, bool) {
	name, prefix := strings.CutPrefix(instance, "splunk-")
	name, suffix := strings.CutSuffix(name, "-kvservice")
	return name, prefix && suffix && name != ""
}
