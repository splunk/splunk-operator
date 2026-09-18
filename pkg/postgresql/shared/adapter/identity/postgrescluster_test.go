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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestClusterInputFromPostgresCluster(t *testing.T) {
	tests := []struct {
		name    string
		cluster *platformv1alpha1.PostgresCluster
		want    []identitytypes.Environment
		wantErr string
	}{
		{
			name:    "conventional fallback",
			cluster: testPostgresCluster(),
			want: []identitytypes.Environment{
				testEnvironment("orders", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeConventional, ""),
			},
		},
		{
			name: "unrecorded nonconventional provider receives explicit resource scope",
			cluster: withProvisionerRef(testPostgresCluster(), corev1.ObjectReference{
				Name: "orders-provider", UID: "provider-uid",
			}),
			want: []identitytypes.Environment{
				testEnvironment("orders-provider", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "provider-uid"),
			},
		},
		{
			name: "blue green cutover preserves blue and scopes green",
			cluster: withProvisionerRef(&platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres", UID: "logical-uid"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ProvisionerRef: &corev1.ObjectReference{Name: "orders-green", UID: "green-uid"},
					PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{
						BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
							Blue:  &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "orders", UID: "blue-uid"}},
							Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "orders-green", UID: "green-uid"}},
						},
					}},
				},
			}, corev1.ObjectReference{Name: "orders-green", UID: "green-uid"}),
			want: []identitytypes.Environment{
				testEnvironment("orders-green", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "green-uid"),
				testEnvironment("orders", identitytypes.EnvironmentRoleRetained, identitytypes.NamingScopeConventional, "blue-uid"),
			},
		},
		{
			name: "cleaning retained environment becomes retirable",
			cluster: withProvisionerRef(&platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres", UID: "logical-uid"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ProvisionerRef: &corev1.ObjectReference{Name: "orders-green", UID: "green-uid"},
					PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{
						BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
							Blue:    &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "orders", UID: "blue-uid"}},
							Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaning},
						},
					}},
				},
			}, corev1.ObjectReference{Name: "orders-green", UID: "green-uid"}),
			want: []identitytypes.Environment{
				testEnvironment("orders-green", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "green-uid"),
				testEnvironment("orders", identitytypes.EnvironmentRoleRetirable, identitytypes.NamingScopeConventional, "blue-uid"),
			},
		},
		{
			name:    "rejects foreign provider namespace",
			cluster: withProvisionerRef(testPostgresCluster(), corev1.ObjectReference{Name: "orders", Namespace: "other"}),
			wantErr: "provider environment namespace \"other\" does not match PostgresCluster namespace \"postgres\"",
		},
		{
			name:    "rejects unsupported provider kind",
			cluster: withProvisionerRef(testPostgresCluster(), corev1.ObjectReference{Name: "orders", Kind: "Pooler"}),
			wantErr: "unsupported provider environment kind \"Pooler\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClusterInputFromPostgresCluster(tt.cluster)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Environments)
		})
	}
}

func TestClusterInputFromPostgresClusterNormalizesProviderReference(t *testing.T) {
	cluster := withProvisionerRef(testPostgresCluster(), corev1.ObjectReference{
		APIVersion: cnpgv1.SchemeGroupVersion.String(),
		Kind:       "Cluster",
		Name:       "orders-green",
		Namespace:  "postgres",
		UID:        "green-uid",
	})

	input, err := ClusterInputFromPostgresCluster(cluster)

	require.NoError(t, err)
	assert.Equal(t, platformv1alpha1.GroupVersion.String(), input.Logical.APIVersion)
	assert.Equal(t, "PostgresCluster", input.Logical.Kind)
	assert.Equal(t, types.UID("logical-uid"), input.Logical.UID)
	assert.Equal(t, testEnvironment("orders-green", identitytypes.EnvironmentRoleAuthoritative, identitytypes.NamingScopeEnvironment, "green-uid"), input.Environments[0])
}

func testPostgresCluster() *platformv1alpha1.PostgresCluster {
	return &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres", UID: "logical-uid"},
	}
}

func withProvisionerRef(cluster *platformv1alpha1.PostgresCluster, ref corev1.ObjectReference) *platformv1alpha1.PostgresCluster {
	cluster.Status.ProvisionerRef = &ref
	return cluster
}
