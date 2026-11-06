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

package v4

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The package envtest installs CRDs without admission webhooks. Use unstructured
// payloads so omitted fields, explicit nulls, and malformed references reach the
// API server exactly as supplied by a manifest.
func TestKVServicePostgresSchemaAdmission(t *testing.T) {
	apiClient := requireAPIServer(t)
	for i, tc := range []struct {
		name      string
		postgres  string
		wantError string
		omitSpec  bool
		nullSpec  bool
	}{
		{name: "missing spec", omitSpec: true, wantError: "spec"},
		{name: "null spec", nullSpec: true, wantError: "spec"},
		{name: "missing postgres", wantError: "spec.postgres"},
		{name: "null postgres", postgres: `null`, wantError: "spec.postgres"},
		{name: "missing clusterRef", postgres: `{}`, wantError: "spec.postgres.clusterRef"},
		{name: "null clusterRef", postgres: `{"clusterRef":null}`, wantError: "spec.postgres.clusterRef"},
		{name: "missing name", postgres: `{"clusterRef":{}}`, wantError: "clusterRef.name must not be empty"},
		{name: "empty name", postgres: `{"clusterRef":{"name":""}}`, wantError: "clusterRef.name must not be empty"},
		{name: "null name", postgres: `{"clusterRef":{"name":null}}`, wantError: "clusterRef.name must not be empty"},
		{name: "malformed postgres", postgres: `"pg"`, wantError: "spec.postgres"},
		{name: "malformed clusterRef", postgres: `{"clusterRef":"pg"}`, wantError: "spec.postgres.clusterRef"},
		{name: "malformed name", postgres: `{"clusterRef":{"name":42}}`, wantError: "spec.postgres.clusterRef.name"},
		{name: "valid", postgres: `{"clusterRef":{"name":"pg"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, operation := range []string{"create", "update"} {
				t.Run(operation, func(t *testing.T) {
					obj := &unstructured.Unstructured{Object: map[string]any{
						"apiVersion": GroupVersion.String(),
						"kind":       "KVService",
						"metadata": map[string]any{
							"name": fmt.Sprintf("kvs-postgres-%d-%s", i, operation), "namespace": "default",
						},
						"spec": map[string]any{
							"livenessInitialDelaySeconds":  int64(0),
							"readinessInitialDelaySeconds": int64(0),
						},
					}}
					if operation == "update" {
						require.NoError(t, unstructured.SetNestedMap(obj.Object, map[string]any{
							"clusterRef": map[string]any{"name": "pg"},
						}, "spec", "postgres"))
						require.NoError(t, apiClient.Create(t.Context(), obj))
						created := obj.DeepCopy()
						t.Cleanup(func() { require.NoError(t, apiClient.Delete(context.Background(), created)) })
					}

					if tc.omitSpec {
						unstructured.RemoveNestedField(obj.Object, "spec")
					} else if tc.nullSpec {
						require.NoError(t, unstructured.SetNestedField(obj.Object, nil, "spec"))
					} else if tc.postgres == "" {
						unstructured.RemoveNestedField(obj.Object, "spec", "postgres")
					} else {
						var postgres any
						require.NoError(t, json.Unmarshal([]byte(tc.postgres), &postgres))
						require.NoError(t, unstructured.SetNestedField(obj.Object, postgres, "spec", "postgres"))
					}
					var err error
					if operation == "create" {
						err = apiClient.Create(t.Context(), obj)
						if err == nil {
							t.Cleanup(func() { require.NoError(t, apiClient.Delete(context.Background(), obj)) })
						}
					} else {
						err = apiClient.Update(t.Context(), obj)
					}
					if tc.wantError == "" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
						require.True(t, apierrors.IsInvalid(err), "expected schema rejection, got %v", err)
						require.ErrorContains(t, err, tc.wantError)
					}
				})
			}
		})
	}
}
