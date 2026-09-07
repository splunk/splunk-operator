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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNoahClusterXValidation(t *testing.T) {
	t.Run("Endpoint", func(t *testing.T) {
		testCases := []struct {
			desc     string
			endpoint string
			expected string
		}{
			{
				desc:     "should accept an https endpoint with a port",
				endpoint: "https://noah.test:8080",
			},
			{
				desc:     "should accept an http endpoint",
				endpoint: "http://noah.test:8080",
			},
			{
				desc:     "should accept an in-cluster service endpoint",
				endpoint: "https://noah.splunk-operator.svc.cluster.local:8080",
			},
			{
				desc:     "should accept a single trailing slash",
				endpoint: "https://noah.test:8080/",
			},
			{
				desc:     "should error if the endpoint is empty",
				endpoint: "",
				expected: "should be at least 1 chars long",
			},
			{
				desc:     "should error if the endpoint has no scheme",
				endpoint: "noah.test:8443",
				expected: "endpoint scheme must be http or https",
			},
			{
				desc:     "should error if the endpoint scheme is not http or https",
				endpoint: "file:///noah",
				expected: "endpoint scheme must be http or https",
			},
			{
				desc:     "should error if the endpoint has no host",
				endpoint: "https://",
				expected: "endpoint must include a host",
			},
			{
				desc:     "should error if the endpoint contains credentials",
				endpoint: "https://user:nope@noah.test",
				expected: "endpoint must not contain credentials",
			},
			{
				desc:     "should error if the endpoint contains a path",
				endpoint: "https://noah.test/api",
				expected: "endpoint must not contain a path",
			},
			{
				desc:     "should error if the endpoint contains an empty query marker",
				endpoint: "https://noah.test?",
				expected: "endpoint must not contain a query",
			},
			{
				desc:     "should error if the endpoint contains a query",
				endpoint: "https://noah.test?tenant=other",
				expected: "endpoint must not contain a query",
			},
			{
				desc:     "should error if the endpoint contains a fragment",
				endpoint: "https://noah.test#fragment",
				expected: "endpoint must not contain a fragment",
			},
			{
				desc:     "should error if the endpoint is not a URL",
				endpoint: "not a url",
				expected: "endpoint must be a valid URL",
			},
			{
				desc:     "should error if the endpoint is a relative path",
				endpoint: "/noah",
				expected: "endpoint scheme must be http or https",
			},
		}

		for index, tC := range testCases {
			t.Run(tC.desc, func(t *testing.T) {
				noahCluster := &NoahCluster{
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("endpoint-%d", index), Namespace: "default"},
					Spec: NoahClusterSpec{
						Endpoint:      tC.endpoint,
						Tenant:        "tenant",
						AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
					},
				}

				err := requireAPIServer(t).Create(t.Context(), noahCluster)

				if tC.expected == "" {
					assert.NoError(t, err)
					return
				}

				assert.ErrorContains(t, err, tC.expected)
			})
		}
	})

	t.Run("Tenant", func(t *testing.T) {
		testCases := []struct {
			desc     string
			tenant   string
			expected string
		}{
			{
				desc:   "should accept a simple tenant",
				tenant: "skaffold-e2e",
			},
			{
				desc:   "should accept a tenant containing whitespace in the middle",
				tenant: "one two",
			},
			{
				desc:     "should error if the tenant is empty",
				tenant:   "",
				expected: "should be at least 1 chars long",
			},
			{
				desc:     "should error if the tenant is only whitespace",
				tenant:   " ",
				expected: "tenant must not have leading or trailing whitespace",
			},
			{
				desc:     "should error if the tenant has leading whitespace",
				tenant:   " tenant",
				expected: "tenant must not have leading or trailing whitespace",
			},
			{
				desc:     "should error if the tenant has trailing whitespace",
				tenant:   "tenant ",
				expected: "tenant must not have leading or trailing whitespace",
			},
		}

		for index, tC := range testCases {
			t.Run(tC.desc, func(t *testing.T) {
				noahCluster := &NoahCluster{
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("tenant-%d", index), Namespace: "default"},
					Spec: NoahClusterSpec{
						Endpoint:      "https://noah.test:8080",
						Tenant:        tC.tenant,
						AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
					},
				}

				err := requireAPIServer(t).Create(t.Context(), noahCluster)

				if tC.expected == "" {
					assert.NoError(t, err)
					return
				}

				assert.ErrorContains(t, err, tC.expected)
			})
		}
	})

	t.Run("AuthSecretRef", func(t *testing.T) {
		testCases := []struct {
			desc     string
			name     string
			expected string
		}{
			{
				desc: "should accept a simple secret name",
				name: "noah-auth",
			},
			{
				desc: "should accept a dotted secret name",
				name: "noah.auth.example",
			},
			{
				desc:     "should error if the name is empty",
				name:     "",
				expected: "authSecretRef.name must not be empty",
			},
			{
				desc:     "should error if the name is only whitespace",
				name:     " ",
				expected: "authSecretRef.name must be a valid DNS-1123 subdomain",
			},
			{
				desc:     "should error if the name has surrounding whitespace",
				name:     " noah-auth ",
				expected: "authSecretRef.name must be a valid DNS-1123 subdomain",
			},
			{
				desc:     "should error if the name contains an underscore",
				name:     "noah_auth",
				expected: "authSecretRef.name must be a valid DNS-1123 subdomain",
			},
			{
				desc:     "should error if the name contains uppercase characters",
				name:     "NoahAuth",
				expected: "authSecretRef.name must be a valid DNS-1123 subdomain",
			},
			{
				desc:     "should error if the name has a trailing hyphen",
				name:     "noah-auth-",
				expected: "authSecretRef.name must be a valid DNS-1123 subdomain",
			},
		}

		for index, tC := range testCases {
			t.Run(tC.desc, func(t *testing.T) {
				noahCluster := &NoahCluster{
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("auth-secret-ref-%d", index), Namespace: "default"},
					Spec: NoahClusterSpec{
						Endpoint:      "https://noah.test:8080",
						Tenant:        "tenant",
						AuthSecretRef: corev1.LocalObjectReference{Name: tC.name},
					},
				}

				err := requireAPIServer(t).Create(t.Context(), noahCluster)

				if tC.expected == "" {
					assert.NoError(t, err)
					return
				}

				assert.ErrorContains(t, err, tC.expected)
			})
		}
	})
}
