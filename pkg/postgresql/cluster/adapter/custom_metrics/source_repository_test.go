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

package custom_metrics

import (
	"testing"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestListDatabaseContributionsUsesCommittedStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))

	db := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "ns", UID: types.UID("uid"), Generation: 3},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "pg"},
			Databases: []enterprisev4.DatabaseDefinition{{
				Name: "orders",
				Monitoring: &enterprisev4.DatabaseMonitoring{
					CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
						LocalObjectReference: corev1.LocalObjectReference{Name: "spec-only"},
						Key:                  "queries.yaml",
					}},
				},
			}},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			CustomMetricsPublication: &enterprisev4.PostgresDatabaseCustomMetricsPublication{
				ObservedGeneration: 3,
				Contributions: []enterprisev4.DatabaseCustomMetricsContribution{{
					DatabaseName: "orders",
					Revision:     "revision",
					Exists:       true,
					CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
						LocalObjectReference: corev1.LocalObjectReference{Name: "published"},
						Key:                  "queries.yaml",
					}},
				}},
			},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(db).
		WithIndex(&enterprisev4.PostgresDatabase{}, enterprisev4.PostgresDatabaseClusterRefNameField, func(obj client.Object) []string {
			return []string{obj.(*enterprisev4.PostgresDatabase).Spec.ClusterRef.Name}
		}).
		Build()

	snapshot, err := NewDataRepository(c).ListDatabaseContributions(t.Context(), "ns", "pg")

	require.NoError(t, err)
	require.Len(t, snapshot.Contributions, 1)
	assert.Empty(t, snapshot.Unpublished)
	assert.Equal(t, "published", snapshot.Contributions[0].Selectors[0].ConfigMapName)
	assert.Equal(t, "revision", snapshot.Contributions[0].Revision)
}

func TestListDatabaseContributionsTreatsMissingOrStalePublicationAsUnpublished(t *testing.T) {
	// A DB with monitoring intent but a missing or stale publication must block
	// the cluster (Unpublished) until it publishes a current generation.
	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	for _, publication := range []*enterprisev4.PostgresDatabaseCustomMetricsPublication{
		nil,
		{ObservedGeneration: 2},
	} {
		db := &enterprisev4.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "ns", UID: types.UID("uid"), Generation: 3},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "pg"},
				Databases: []enterprisev4.DatabaseDefinition{{
					Name: "orders",
					Monitoring: &enterprisev4.DatabaseMonitoring{
						CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
							LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
							Key:                  "queries.yaml",
						}},
					},
				}},
			},
			Status: enterprisev4.PostgresDatabaseStatus{
				CustomMetricsPublication: publication,
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(db).Build()

		snapshot, err := NewDataRepository(c).ListDatabaseContributions(t.Context(), "ns", "pg")

		require.NoError(t, err)
		assert.Empty(t, snapshot.Contributions)
		require.Len(t, snapshot.Unpublished, 1)
		assert.Empty(t, snapshot.Unpublished[0].DatabaseName)
	}
}

func TestListDatabaseContributionsUsesPublishedNonParticipationInsteadOfDatabaseSpec(t *testing.T) {
	// The database controller publishes participation before unrelated
	// provisioning gates. The cluster consumes that status and does not inspect
	// the producer's monitoring spec across the controller boundary.
	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	db := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "ns", UID: types.UID("uid"), Generation: 3},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "pg"},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			CustomMetricsPublication: &enterprisev4.PostgresDatabaseCustomMetricsPublication{
				ObservedGeneration: 3,
				Contributions: []enterprisev4.DatabaseCustomMetricsContribution{{
					DatabaseName: "orders",
					Revision:     "disabled",
					Exists:       false,
				}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(db).Build()

	snapshot, err := NewDataRepository(c).ListDatabaseContributions(t.Context(), "ns", "pg")

	require.NoError(t, err)
	require.Len(t, snapshot.Contributions, 1)
	assert.False(t, snapshot.Contributions[0].Exists)
	assert.Empty(t, snapshot.Unpublished)
}

func TestListDatabaseContributionsDoesNotInferParticipationFromDatabaseSpec(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	db := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "ns", UID: types.UID("uid"), Generation: 3},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "pg"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(db).Build()

	snapshot, err := NewDataRepository(c).ListDatabaseContributions(t.Context(), "ns", "pg")

	require.NoError(t, err)
	assert.Empty(t, snapshot.Contributions)
	require.Len(t, snapshot.Unpublished, 1)
	assert.Equal(t, "owner", snapshot.Unpublished[0].PostgresDatabaseName)
}

func TestListDatabaseContributionsAcceptsExplicitNonParticipation(t *testing.T) {
	// A DB with monitoring intent that has published Exists=false for a database
	// is accepted as a non-participating contribution (not unpublished).
	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	db := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "ns", UID: types.UID("uid"), Generation: 3},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "pg"},
			Databases: []enterprisev4.DatabaseDefinition{{
				Name: "orders",
				Monitoring: &enterprisev4.DatabaseMonitoring{
					CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
						Key:                  "queries.yaml",
					}},
				},
			}},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			CustomMetricsPublication: &enterprisev4.PostgresDatabaseCustomMetricsPublication{
				ObservedGeneration: 3,
				Contributions: []enterprisev4.DatabaseCustomMetricsContribution{{
					DatabaseName: "orders",
					Revision:     "disabled",
					Exists:       false,
				}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(db).Build()

	snapshot, err := NewDataRepository(c).ListDatabaseContributions(t.Context(), "ns", "pg")

	require.NoError(t, err)
	require.Len(t, snapshot.Contributions, 1)
	assert.False(t, snapshot.Contributions[0].Exists)
	assert.Empty(t, snapshot.Unpublished)
}
