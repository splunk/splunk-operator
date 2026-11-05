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

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalRunMetricsNamespace(t *testing.T) {
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is required to check the local run entry point")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// Observe the real recipe's environment without starting an operator or
	// contacting the developer's kubeconfig. Parse-time Go queries still work.
	spy := "#!/bin/sh\nif [ \"$1\" = run ]; then\n  printf 'RUN_POD_NAMESPACE=%s\\n' \"${POD_NAMESPACE-}\"\n  exit 0\nfi\nexec \"$TEST_REAL_GO\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(spy), 0o755); err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "POD_NAMESPACE", "NAMESPACE", "WATCH_NAMESPACE", "PATH", "MAKEFLAGS", "MFLAGS", "MAKEOVERRIDES":
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TEST_REAL_GO="+goPath)
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "default namespace", want: "splunk-operator"},
		{name: "installation namespace", args: []string{"NAMESPACE=operator-a"}, want: "operator-a"},
		{name: "independent of watch list", args: []string{"NAMESPACE=operator-a", "WATCH_NAMESPACE=tenant-a,tenant-b"}, want: "operator-a"},
		{name: "explicit authorization namespace", args: []string{"NAMESPACE=operator-a", "POD_NAMESPACE=local-metrics"}, want: "local-metrics"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"--no-print-directory", "-C", "../..", "-o", "manifests", "-o", "generate", "-o", "fmt", "-o", "vet", "run"}
			cmd := exec.Command(makePath, append(args, tt.args...)...)
			cmd.Env = env
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("make run: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), "RUN_POD_NAMESPACE="+tt.want+"\n") {
				t.Errorf("want namespace %q in run environment:\n%s", tt.want, output)
			}
		})
	}
}
