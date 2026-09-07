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

package v4

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var apiServerClient client.Client

func TestMain(m *testing.M) {
	code, err := runTests(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runTests(m *testing.M) (int, error) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		return m.Run(), nil
	}

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		return 0, fmt.Errorf("register enterprise v4 scheme: %w", err)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	restConfig, err := testEnv.Start()
	if err != nil {
		return 0, fmt.Errorf("start envtest: %w", err)
	}
	defer func() {
		if stopErr := testEnv.Stop(); stopErr != nil {
			fmt.Fprintf(os.Stderr, "stop envtest: %v\n", stopErr)
		}
	}()

	if apiServerClient, err = client.New(restConfig, client.Options{Scheme: scheme}); err != nil {
		return 0, fmt.Errorf("create envtest client: %w", err)
	}

	return m.Run(), nil
}

func requireAPIServer(t *testing.T) client.Client {
	t.Helper()
	if apiServerClient == nil {
		t.Skip("KUBEBUILDER_ASSETS is not set; run via `make test` or `make setup-envtest`")
	}
	return apiServerClient
}
