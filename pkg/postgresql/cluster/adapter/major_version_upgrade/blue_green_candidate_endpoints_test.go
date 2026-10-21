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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCandidateEndpointsWaitForDirectServiceAndRemainStable(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	preparer := &BlueGreenCandidatePreparer{client: c}
	source := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: "default"}}
	candidate := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "default"}}
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Green: &platformv1alpha1.BlueGreenEnvironmentStatus{}}

	ready, err := preparer.reconcileCandidateEndpoints(t.Context(), source, candidate, "attempt-1", status)
	require.NoError(t, err)
	assert.False(t, ready)
	assert.Nil(t, status.CandidateEndpoints)
	assert.Nil(t, status.Green.AvailableAt)

	direct := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: candidate.GetServiceReadWriteName(), Namespace: candidate.Namespace, UID: "direct-uid"}}
	require.NoError(t, c.Create(t.Context(), direct))
	ready, err = preparer.reconcileCandidateEndpoints(t.Context(), source, candidate, "attempt-1", status)
	require.NoError(t, err)
	assert.True(t, ready)
	require.NotNil(t, status.CandidateEndpoints)
	assert.Equal(t, serviceReference(direct), status.CandidateEndpoints.DirectService)
	assert.Nil(t, status.CandidateEndpoints.PoolerService)
	require.NotNil(t, status.Green.AvailableAt)
	availableAt := status.Green.AvailableAt.DeepCopy()

	ready, err = preparer.reconcileCandidateEndpoints(t.Context(), source, candidate, "attempt-1", status)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.Equal(t, availableAt, status.Green.AvailableAt, "a retry must preserve the first availability time")
}

func TestCandidateEndpointsWaitForClonedPoolerAndServingPods(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid"}}
	source := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: parent.Namespace}}
	candidate := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: parent.Namespace}}
	sourcePooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: "blue-pooler-rw", Namespace: parent.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}},
		Spec: cnpgv1.PoolerSpec{Cluster: cnpgv1.LocalObjectReference{Name: source.Name}, Type: cnpgv1.PoolerTypeRW,
			PgBouncer: &cnpgv1.PgBouncerSpec{Parameters: map[string]string{"max_client_conn": "100"}}},
	}
	direct := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: candidate.GetServiceReadWriteName(), Namespace: candidate.Namespace, UID: "direct-uid"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sourcePooler, direct).Build()
	preparer := &BlueGreenCandidatePreparer{client: c, cluster: parent}
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Green: &platformv1alpha1.BlueGreenEnvironmentStatus{}}

	ready, err := preparer.reconcileCandidateEndpoints(t.Context(), source, candidate, "attempt-1", status)
	require.NoError(t, err)
	assert.False(t, ready, "the candidate Pooler must be created before its endpoint is advertised")
	assert.Nil(t, status.CandidateEndpoints)
	pooler := &cnpgv1.Pooler{}
	poolerKey := types.NamespacedName{Name: candidatePoolerName(candidate.Name, string(cnpgv1.PoolerTypeRW)), Namespace: parent.Namespace}
	require.NoError(t, c.Get(t.Context(), poolerKey, pooler))
	assert.Equal(t, candidate.Name, pooler.Spec.Cluster.Name)
	assert.Equal(t, sourcePooler.Spec.PgBouncer.Parameters, pooler.Spec.PgBouncer.Parameters)
	assert.Equal(t, "attempt-1", pooler.Labels[blueGreenAttemptLabel])
	assert.True(t, metav1.IsControlledBy(pooler, parent))

	ready, err = preparer.reconcileCandidateEndpoints(t.Context(), source, candidate, "attempt-1", status)
	require.NoError(t, err)
	assert.False(t, ready, "a Pooler without its Service is still pending")
	assert.Nil(t, status.CandidateEndpoints)

	pooler.UID = "green-pooler-uid"
	pooler.Status.Phase = cnpgv1.PoolerPhaseActive
	pooler.Status.Instances = 1
	require.NoError(t, c.Update(t.Context(), pooler))
	poolerService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: pooler.Name, Namespace: pooler.Namespace, UID: "pooler-service-uid",
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}},
		Spec: corev1.ServiceSpec{Selector: map[string]string{cnpgutils.PgbouncerNameLabel: pooler.Name}}}
	require.NoError(t, c.Create(t.Context(), poolerService))
	ready, err = preparer.reconcileCandidateEndpoints(t.Context(), source, candidate, "attempt-1", status)
	require.NoError(t, err)
	assert.False(t, ready, "a Service alone does not prove the Pooler is serving")
	assert.Nil(t, status.CandidateEndpoints)

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "green-pooler-1", Namespace: parent.Namespace,
		Labels: map[string]string{cnpgutils.PgbouncerNameLabel: pooler.Name}},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	require.NoError(t, c.Create(t.Context(), pod))
	ready, err = preparer.reconcileCandidateEndpoints(t.Context(), source, candidate, "attempt-1", status)
	require.NoError(t, err)
	assert.True(t, ready)
	require.NotNil(t, status.CandidateEndpoints)
	assert.Equal(t, serviceReference(direct), status.CandidateEndpoints.DirectService)
	assert.Equal(t, serviceReference(poolerService), status.CandidateEndpoints.PoolerService)
	require.NotNil(t, status.Green.AvailableAt)

	pooler.Labels[blueGreenAttemptLabel] = "another-attempt"
	require.NoError(t, c.Update(t.Context(), pooler))
	ready, err = preparer.reconcileCandidateEndpoints(t.Context(), source, candidate, "attempt-1", status)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.False(t, ready, "a foreign Pooler must never be treated as a ready candidate endpoint")
}
