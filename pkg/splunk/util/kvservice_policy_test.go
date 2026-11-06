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

package util

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKVServiceManagedEnvironmentCategory(t *testing.T) {
	for _, tc := range []struct {
		name, withoutCerts, withCerts string
	}{
		{"TLS_CERTIFICATE", "", "TLS"}, {"TLS_PRIVATE_KEY", "", "TLS"},
		{"TLS_HOST", "", "TLS"}, {"TLS_PORT", "", "TLS"},
		{"KVSERVICE_DB_PASSWORD", "Postgres", "Postgres"},
		{"KVSERVICE_CMPK_TENANT_MAP_DIR", "Postgres", "Postgres"},
		{"CUSTOM_SETTING", "", ""}, {"TLS_CUSTOM_SETTING", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, policy := range []struct {
				certsConfigured bool
				wantCategory    string
			}{{false, tc.withoutCerts}, {true, tc.withCerts}} {
				category, managed := KVServiceManagedEnvironmentCategory(tc.name, policy.certsConfigured)
				require.Equal(t, policy.wantCategory, category, "certs configured: %t", policy.certsConfigured)
				require.Equal(t, policy.wantCategory != "", managed, "certs configured: %t", policy.certsConfigured)
			}
		})
	}
}

func TestKVServiceAddressParsing(t *testing.T) {
	for _, tc := range []struct{ address, normalized, host, name, namespace string }{
		{"splunk-shared-kvservice-service.backend:8066", "http://splunk-shared-kvservice-service.backend:8066", "splunk-shared-kvservice-service.backend:8066", "shared", "backend"},
		{"https://splunk-kvs2-kvservice-service.default:8066/", "https://splunk-kvs2-kvservice-service.default:8066/", "splunk-kvs2-kvservice-service.default:8066", "kvs2", "default"},
		{"http://splunk-kvs-east-kvservice-service.tenant-1:8066", "http://splunk-kvs-east-kvservice-service.tenant-1:8066", "splunk-kvs-east-kvservice-service.tenant-1:8066", "kvs-east", "tenant-1"},
	} {
		t.Run(tc.address, func(t *testing.T) {
			u, err := ParseKVServiceAddress(tc.address)
			require.NoError(t, err)
			require.Equal(t, tc.normalized, u.String())
			require.Equal(t, tc.host, u.Host)
			require.Equal(t, tc.name, u.KVServiceName)
			require.Equal(t, tc.namespace, u.Namespace)
		})
	}
	for _, address := range []string{
		"", "http://", "ftp://splunk-shared-kvservice-service.backend:8066",
		"https://user:password@splunk-shared-kvservice-service.backend:8066",
		"https://splunk-shared-kvservice-service.backend:8066/path",
		"splunk-shared-kvservice-service.backend:8066?tenant=other",
		"splunk-shared-kvservice-service.backend:8066?", "splunk-shared-kvservice-service.backend:8066#", "splunk-shared-kvservice-service.backend:8066#fragment",
		" invalid host ", "shared.backend:8066", "splunk-kvs-kvservice-service:8066", "splunk-kvs-kvservice-service.backend",
		"splunk-kvs-kvservice-service.backend:9000", "splunk-kvs-kvservice-service.backend:08066",
		"splunk-kvs-kvservice-service.backend.svc:8066", "splunk-kvs-kvservice-service.backend.svc.cluster.local:8066",
		"splunk-kvs-kvservice-service.backend.:8066", "127.0.0.1:8066", "http://[2001:db8::1]:8066",
		"splunk-kvs-kvservice-service.Backend:8066", "splunk-kvs-kvservice-service.bad_namespace:8066",
		"splunk--kvs-kvservice-service.backend:8066", "splunk-kvs--kvservice-service.backend:8066",
		"splunk-" + strings.Repeat("k", 64) + "-kvservice-service.backend:8066",
	} {
		t.Run(address, func(t *testing.T) {
			u, err := ParseKVServiceAddress(address)
			require.Error(t, err)
			require.Nil(t, u)
		})
	}
}

func TestKVServiceTenantNameContract(t *testing.T) {
	for _, name := range []string{"tenant1", "a", "abcdefghijklmnopqrstuvwxyz1234"} {
		require.NoError(t, ValidateKVServiceTenantName(name))
	}
	for _, name := range []string{"", "tenant-1", "Tenant1", "1tenant", "abcdefghijklmnopqrstuvwxyz12345", "tenant_1"} {
		require.Error(t, ValidateKVServiceTenantName(name))
	}
}

func TestKVServiceBackendAddressContract(t *testing.T) {
	for _, tc := range []struct{ name, namespace, want string }{
		{"kvs2", "default", "splunk-kvs2-kvservice-service.default:8066"},
		{"kvs-east", "shared-backend", "splunk-kvs-east-kvservice-service.shared-backend:8066"},
	} {
		require.Equal(t, tc.want, KVServiceBackendAddress(tc.name, tc.namespace))
	}
}
