/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package majorupgradeadapter

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAccessFingerprintVariantsPreserveCompleteSQLContract(t *testing.T) {
	// Captured from the four pre-refactor queries. This covers the entire SQL,
	// including branches that substring-only assertions could accidentally miss.
	for _, test := range []struct {
		name, query, digest string
		cutover, green      bool
	}{
		{"source preparation", accessFingerprintSQL, "8a6d8f7ee0152227e1e426fbbd6cb00c7046e01d98d165ee4c2a2abe7a951f12", false, false},
		{"green preparation", greenAccessFingerprintSQL, "7269228d6684141ace305256c3357c8a48989448eaa15d49e89e747f9e476817", false, true},
		{"source cutover", cutoverAccessFingerprintSQL(sideBlue), "5fe8951ee205e95b996dd9d5475ebcbf57772219edc3ab65a096275077494d13", true, false},
		{"green cutover", cutoverAccessFingerprintSQL(sideGreen), "7ed18d766da3e4b1c9b48f29983b059d0ebe136027c6a74858e6fc4c5a63b1d8", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.digest, fmt.Sprintf("%x", sha256.Sum256([]byte(test.query))))
			for _, contribution := range []string{"database-acl:", "role:", "attempt-role-drift:", "membership:", "schema-acl:", "relation-acl:", "column-acl:", "default-acl:", "function-acl:"} {
				assert.Contains(t, test.query, contribution)
			}
			assert.Equal(t, test.green, strings.Contains(test.query, "attempt-role-missing:"))
			assert.Equal(t, test.cutover, strings.Contains(test.query, "control_role AS"))
			if test.cutover {
				assert.Equal(t, 2, strings.Count(test.query, fingerprintOriginalLoginSQL))
				assert.Equal(t, 3, strings.Count(test.query, fingerprintControlGrantSQL))
			}
		})
	}
}
