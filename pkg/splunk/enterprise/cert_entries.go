// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package enterprise

import (
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
)

// toCertEntries converts a []CertSpec into the []CertEntry type expected by
// workflow/certs, keeping that package decoupled from CRD API types.
func toCertEntries(specs []enterpriseApi.CertSpec) []certs.CertEntry {
	if len(specs) == 0 {
		return nil
	}
	entries := make([]certs.CertEntry, len(specs))
	for i, s := range specs {
		entries[i] = certs.CertEntry{
			SecretName: s.SecretRef.Name,
			Role:       string(s.Role),
		}
	}
	return entries
}
