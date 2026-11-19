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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChildRetirementCNPGContract(t *testing.T) {
	if os.Getenv("SPLUNK_OPERATOR_TEST_CNPG_CONTRACT") == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_CNPG_CONTRACT=1 to exercise the pinned provider controller")
	}
	path, err := exec.CommandContext(t.Context(), "go", "env", "GOMOD").Output()
	require.NoError(t, err)
	root := filepath.Dir(strings.TrimSpace(string(path)))
	dir := t.TempDir()
	// A child module can import CNPG's internal controller without changing either module.
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		if name == "go.mod" {
			data = []byte(strings.Replace(string(data), "module github.com/splunk/splunk-operator", "module github.com/cloudnative-pg/cloudnative-pg/sok-retirement-contract", 1) + "\nrequire github.com/splunk/splunk-operator v0.0.0\nreplace github.com/splunk/splunk-operator => " + root + "\n")
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0600))
	}
	data, err := os.ReadFile("testdata/cnpg_retirement_contract_test.go.txt")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "retirement_test.go"), data, 0600))
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "-run", "^TestRetainedDatabaseProviderFinalization$", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Log(string(output))
}
