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
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	identityadapter "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/identity"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var testEnvironmentNamer = identityadapter.NewIdentityResolver()

func TestResolveAuthoritativeCNPGCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef: &corev1.ObjectReference{
				APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Name: "orders-green", Namespace: "postgres",
			},
		},
	}
	green := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "orders-green", Namespace: "postgres"}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(green).Build()

	got, conventionalFallback, err := resolveAuthoritativeCNPGCluster(context.Background(), client, cluster, testClusterCard(cluster, "orders-green", ""))
	require.NoError(t, err)
	assert.False(t, conventionalFallback)
	assert.Equal(t, "orders-green", got.Name)
}

func TestResolveAuthoritativeCNPGClusterDoesNotFallBackFromPresentReference(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef: &corev1.ObjectReference{Name: "orders-green", Namespace: "postgres"},
		},
	}
	conventional := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres"}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(conventional).Build()

	got, conventionalFallback, err := resolveAuthoritativeCNPGCluster(context.Background(), client, cluster, testClusterCard(cluster, "orders-green", ""))
	require.Error(t, err)
	assert.Nil(t, got)
	assert.False(t, conventionalFallback)
}

func TestResolveAuthoritativeCNPGClusterRejectsReusedName(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef: &corev1.ObjectReference{Name: "orders-green", Namespace: "postgres", UID: "expected-uid"},
		},
	}
	green := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "orders-green", Namespace: "postgres", UID: "reused-uid"}}

	got, fallback, err := resolveAuthoritativeCNPGCluster(context.Background(), fake.NewClientBuilder().WithScheme(scheme).WithObjects(green).Build(), cluster, testClusterCard(cluster, "orders-green", "expected-uid"))

	require.Error(t, err)
	assert.Nil(t, got)
	assert.False(t, fallback)
}

func TestValidateAuthoritativeCNPGEnvironmentRejectsReusedName(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres"},
	}
	green := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "orders-green", Namespace: "postgres", UID: "reused-uid"}}

	err := validateAuthoritativeCNPGEnvironment(
		context.Background(),
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(green).Build(),
		testClusterCard(cluster, "orders-green", "expected-uid"),
	)

	require.EqualError(t, err, "resolved CNPG Cluster UID \"expected-uid\" does not match \"orders-green\" UID \"reused-uid\"")
}

func TestManagedEnvironmentNamesIncludesRetainedBlueGreenEnvironments(t *testing.T) {
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef: &corev1.ObjectReference{Name: "orders-green"},
			PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{
				BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
					Blue:  &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "orders-blue"}},
					Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "orders-green"}},
				},
			}},
		},
	}

	assert.Equal(t, []string{"orders", "orders-green", "orders-blue"}, testEnvironmentNamer.ManagedEnvironmentNames(identitytypes.ClusterCard{
		Logical:       conventionalClusterCard(cluster).Logical,
		Authoritative: testEnvironment(cluster, "orders-green", "", identitytypes.EnvironmentRoleAuthoritative),
		Managed: []identitytypes.Environment{
			testEnvironment(cluster, "orders", "", identitytypes.EnvironmentRoleRetained),
			testEnvironment(cluster, "orders-green", "", identitytypes.EnvironmentRoleAuthoritative),
			testEnvironment(cluster, "orders-blue", "", identitytypes.EnvironmentRoleRetained),
		},
	}))
}

func testClusterCard(cluster *platformv1alpha1.PostgresCluster, authoritativeName string, authoritativeUID types.UID) identitytypes.ClusterCard {
	authoritative := testEnvironment(cluster, authoritativeName, authoritativeUID, identitytypes.EnvironmentRoleAuthoritative)
	return identitytypes.ClusterCard{
		Logical:       conventionalClusterCard(cluster).Logical,
		Authoritative: authoritative,
		Managed:       []identitytypes.Environment{authoritative},
	}
}

func testEnvironment(cluster *platformv1alpha1.PostgresCluster, name string, uid types.UID, role identitytypes.EnvironmentRole) identitytypes.Environment {
	return identitytypes.Environment{
		Identity: identitytypes.ObjectIdentity{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       name,
			Namespace:  cluster.Namespace,
			UID:        uid,
		},
		Role:  role,
		Scope: identitytypes.NamingScopeEnvironment,
	}
}
