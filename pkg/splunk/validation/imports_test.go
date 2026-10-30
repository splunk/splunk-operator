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
	"go/build"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdmissionPackageImportLayers(t *testing.T) {
	pkg, err := build.Default.ImportDir(".", 0)
	require.NoError(t, err)
	const prefix = "github.com/splunk/splunk-operator/pkg/splunk/"
	for _, dependency := range pkg.Imports {
		if !strings.HasPrefix(dependency, prefix) {
			continue
		}
		layer, _, _ := strings.Cut(strings.TrimPrefix(dependency, prefix), "/")
		assert.Contains(t, []string{"common", "util"}, layer,
			"admission dependency %q violates the contract in doc.go", dependency)
	}
}
