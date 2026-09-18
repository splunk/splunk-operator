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

package identity

import (
	"testing"

	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestResolveCluster(t *testing.T) {
	tests := []struct {
		name    string
		input   identitytypes.ClusterInput
		want    identitytypes.ClusterCard
		wantErr string
	}{
		{
			name: "conventional environment",
			input: identitytypes.ClusterInput{
				Logical: testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Environments: []identitytypes.Environment{
					testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, ""),
				},
			},
			want: identitytypes.ClusterCard{
				Logical:       testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Authoritative: testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, ""),
				Managed: []identitytypes.Environment{
					testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, ""),
				},
			},
		},
		{
			name: "authoritative and retained environments",
			input: identitytypes.ClusterInput{
				Logical: testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Environments: []identitytypes.Environment{
					testEnvironment("orders-green", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "green-uid"),
					testEnvironment("orders", identitytypes.EnvironmentRoleRetained, identitytypes.NamingScopeEnvironment, "blue-uid"),
				},
			},
			want: identitytypes.ClusterCard{
				Logical:       testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Authoritative: testEnvironment("orders-green", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "green-uid"),
				Managed: []identitytypes.Environment{
					testEnvironment("orders-green", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "green-uid"),
					testEnvironment("orders", identitytypes.EnvironmentRoleRetained, identitytypes.NamingScopeEnvironment, "blue-uid"),
				},
			},
		},
		{
			name: "duplicate declarations retain observed UID",
			input: identitytypes.ClusterInput{
				Logical: testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Environments: []identitytypes.Environment{
					testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, ""),
					testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, "provider-uid"),
				},
			},
			want: identitytypes.ClusterCard{
				Logical:       testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Authoritative: testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, "provider-uid"),
				Managed: []identitytypes.Environment{
					testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, "provider-uid"),
				},
			},
		},
		{
			name: "missing authoritative environment",
			input: identitytypes.ClusterInput{
				Logical: testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Environments: []identitytypes.Environment{
					testEnvironment("orders-green", identitytypes.EnvironmentRoleCandidate, identitytypes.NamingScopeEnvironment, "green-uid"),
				},
			},
			wantErr: "authoritative environment is required",
		},
		{
			name: "conflicting roles for one environment",
			input: identitytypes.ClusterInput{
				Logical: testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Environments: []identitytypes.Environment{
					testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, "provider-uid"),
					testEnvironment("orders", identitytypes.EnvironmentRoleRetained, identitytypes.NamingScopeConventional, "provider-uid"),
				},
			},
			wantErr: "conflicting environment declarations",
		},
		{
			name: "reused environment name",
			input: identitytypes.ClusterInput{
				Logical: testIdentity("platform.splunk.com/v1alpha1", "PostgresCluster", "orders", "postgres", "logical-uid"),
				Environments: []identitytypes.Environment{
					testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, "old-uid"),
					testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, "new-uid"),
				},
			},
			wantErr: "conflicting environment UIDs",
		},
	}

	resolver := NewIdentityResolver()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.ResolveCluster(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnvironmentNames(t *testing.T) {
	resolver := NewIdentityResolver()
	card := identitytypes.ClusterCard{
		Authoritative: testEnvironment("orders-green", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "green-uid"),
		Managed: []identitytypes.Environment{
			testEnvironment("orders-green", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "green-uid"),
			testEnvironment("orders", identitytypes.EnvironmentRoleRetained, identitytypes.NamingScopeConventional, "blue-uid"),
			testEnvironment("orders-green", identitytypes.EnvironmentRoleRetained, identitytypes.NamingScopeEnvironment, "green-uid"),
		},
	}

	assert.Equal(t, "orders-green", resolver.EnvironmentName("orders", "orders-green"))
	assert.Equal(t, "orders", resolver.EnvironmentName("orders", ""))
	assert.Equal(t, "orders-green", resolver.AuthoritativeEnvironmentName("orders", card))
	assert.Equal(t, []string{"orders-green", "orders"}, resolver.ManagedEnvironmentNames(card))
}

func testIdentity(apiVersion, kind, name, namespace, uid string) identitytypes.ObjectIdentity {
	return identitytypes.ObjectIdentity{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespace,
		UID:        types.UID(uid),
	}
}

func testEnvironment(name string, role identitytypes.EnvironmentRole, scope identitytypes.NamingScope, uid string) identitytypes.Environment {
	return identitytypes.Environment{
		Identity: testIdentity("postgresql.cnpg.io/v1", "Cluster", name, "postgres", uid),
		Role:     role,
		Scope:    scope,
	}
}
