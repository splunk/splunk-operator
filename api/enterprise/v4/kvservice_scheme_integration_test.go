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
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKVServiceSchemeSchema(t *testing.T) {
	c := requireAPIServer(t)
	for _, tt := range []struct {
		name, scheme, expected string
		certs, invalid, omit   bool
	}{
		{name: "default", omit: true, expected: "http"},
		{name: "default with certs", omit: true, certs: true, expected: "http"},
		{name: "explicit empty", invalid: true},
		{name: "http", scheme: "http", expected: "http"},
		{name: "https with certs", scheme: "https", certs: true, expected: "https"},
		{name: "https without certs", scheme: "https", invalid: true},
		{name: "invalid scheme", scheme: "tcp", invalid: true},
		{name: "uppercase", scheme: "HTTPS", certs: true, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cr := &KVService{ObjectMeta: metav1.ObjectMeta{GenerateName: "kvservice-scheme-", Namespace: "default"}, Spec: KVServiceSpec{Scheme: tt.scheme, Postgres: KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}}}}
			if tt.certs {
				cr.Spec.Certs = []CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "serving-cert"}}}
			}
			// Use an unstructured object so omission and an explicit empty value
			// exercise distinct API requests despite Scheme being a required Go string.
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cr)
			require.NoError(t, err)
			if tt.omit {
				unstructured.RemoveNestedField(object, "spec", "scheme")
			}
			input := &unstructured.Unstructured{Object: object}
			input.SetAPIVersion(GroupVersion.String())
			input.SetKind("KVService")
			err = c.Create(t.Context(), input)
			if tt.invalid {
				require.True(t, apierrors.IsInvalid(err), "expected schema rejection, got %v", err)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, c.Delete(context.Background(), input)) })
			stored := &KVService{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(input), stored))
			require.Equal(t, tt.expected, stored.Spec.Scheme)
			stored.Spec.Scheme = ""
			err = c.Update(t.Context(), stored)
			require.True(t, apierrors.IsInvalid(err), "update to an empty scheme must fail: %v", err)
			if !tt.certs {
				stored.Spec.Scheme = "https"
				err := c.Update(t.Context(), stored)
				require.True(t, apierrors.IsInvalid(err), "update to HTTPS without certificates must fail: %v", err)
			}
		})
	}
}
