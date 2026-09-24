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

package testenv

import (
	"os"
	"testing"
)

func TestCertManagerVersion_Default(t *testing.T) {
	os.Unsetenv("CERT_MANAGER_VERSION")
	if got := certManagerVersion(); got != DefaultCertManagerVersion {
		t.Errorf("certManagerVersion() = %q, want %q", got, DefaultCertManagerVersion)
	}
}

func TestCertManagerVersion_EnvOverride(t *testing.T) {
	os.Setenv("CERT_MANAGER_VERSION", "v1.99.0")
	defer os.Unsetenv("CERT_MANAGER_VERSION")
	if got := certManagerVersion(); got != "v1.99.0" {
		t.Errorf("certManagerVersion() = %q, want %q", got, "v1.99.0")
	}
}
