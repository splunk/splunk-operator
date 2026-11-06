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

package majorupgradeadapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestBlueGreenCandidatePreparerCreatesInitialCopySubscriptions(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: types.UID("parent-uid")}}
	database := declaredDatabase{name: "orders"}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
	preparer := NewBlueGreenCandidatePreparer(client, types.NamespacedName{Name: parent.Name, Namespace: parent.Namespace}, parent, BlueGreenCandidatePlan{})
	imported := true
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: database.name, SchemaImported: &imported}}}
	candidate := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "orders-bg-12345", Namespace: parent.Namespace}}

	ready, err := preparer.reconcileSubscriptions(t.Context(), candidate, []declaredDatabase{database}, "attempt-1", status)
	require.NoError(t, err)
	assert.False(t, ready)

	subscription := &cnpgv1.Subscription{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: subscriptionName(parent.Name, database.name), Namespace: parent.Namespace}, subscription))
	assert.Equal(t, candidate.Name, subscription.Spec.ClusterRef.Name)
	assert.Equal(t, publicationName(parent.Name, database.name), subscription.Spec.PublicationName)
	assert.Equal(t, candidateSourceExternalClusterName, subscription.Spec.ExternalClusterName)
	assert.Equal(t, "true", subscription.Spec.Parameters["copy_data"])
	assert.Equal(t, "true", subscription.Spec.Parameters["failover"])
	assert.Equal(t, replicationSlotName(parent.Name, database.name), subscription.Spec.Parameters["slot_name"])
	assert.NotEqual(t, subscription.Name, subscription.Spec.Parameters["slot_name"])
	assert.Equal(t, "attempt-1", subscription.Labels[blueGreenAttemptLabel])
	subscription.Spec.Parameters["failover"] = "false"
	desired := preparer.buildCandidateSubscription(candidate.Name, database.name, subscription.Name, "attempt-1")
	require.ErrorIs(t, preparer.validateSubscription(subscription, desired, "attempt-1"), mvutypes.ErrUpgradeFlowFailed)
	subscription.Spec.Parameters["failover"] = "true"
	subscription.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimRetain
	require.ErrorIs(t, preparer.validateSubscription(subscription, desired, "attempt-1"), mvutypes.ErrUpgradeFlowFailed)
}

func TestReplicationSlotNameUsesPostgresAlphabet(t *testing.T) {
	for _, cluster := range []string{"orders", "123-orders", strings.Repeat("a", 63)} {
		t.Run(cluster, func(t *testing.T) {
			name := replicationSlotName(cluster, "orders-data")
			assert.Regexp(t, `^[a-z][a-z0-9_]*$`, name)
			assert.LessOrEqual(t, len(name), postgresIdentifierMaxLength)
			assert.Equal(t, name, replicationSlotName(cluster, "orders-data"))
			assert.NotEqual(t, name, replicationSlotName(cluster, "other"))
			assert.NotEqual(t, name, replicationSlotName(cluster+"x", "orders-data"))
			assert.NotEqual(t, subscriptionName(cluster, "orders-data"), name)
		})
	}
}

func TestCandidateSubscriptionsRejectSpecDriftWithoutMutation(t *testing.T) {
	for _, field := range []string{"slot name", "failover", "reclaim policy"} {
		t.Run(field, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, cnpgv1.AddToScheme(scheme))
			parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid"}}
			key := client.ObjectKeyFromObject(parent)
			candidate := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "orders-green", Namespace: parent.Namespace}}
			database := declaredDatabase{name: "orders"}
			preparer := NewBlueGreenCandidatePreparer(fake.NewClientBuilder().WithScheme(scheme).Build(), key, parent, BlueGreenCandidatePlan{})
			subscription := preparer.buildCandidateSubscription(candidate.Name, database.name, subscriptionName(parent.Name, database.name), "attempt-1")
			switch field {
			case "slot name":
				subscription.Spec.Parameters["slot_name"] = "different_slot"
			case "failover":
				subscription.Spec.Parameters["failover"] = "false"
			case "reclaim policy":
				subscription.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimRetain
			}
			writes := 0
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(subscription).WithInterceptorFuncs(interceptor.Funcs{
				Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
					writes++
					return errors.New("unexpected create")
				},
				Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
					writes++
					return errors.New("unexpected update")
				},
				Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					writes++
					return errors.New("unexpected patch")
				},
			}).Build()
			preparer.client = c
			before := &cnpgv1.Subscription{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(subscription), before))
			status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: database.name, SchemaImported: new(true)}}}
			ready, err := preparer.reconcileSubscriptions(t.Context(), candidate, []declaredDatabase{database}, "attempt-1", status)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			assert.False(t, ready)
			assert.Zero(t, writes, "drift must fail before any resource mutation")
			after := &cnpgv1.Subscription{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(subscription), after))
			assert.Equal(t, before, after)
			assert.Nil(t, status.DatabaseReplication[0].SubscriptionHealthy)
		})
	}
}
