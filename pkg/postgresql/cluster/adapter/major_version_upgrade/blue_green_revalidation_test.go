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
	"testing"
	"testing/synctest"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func timedOutFixture(t *testing.T) *cutoverFixture {
	t.Helper()
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.FenceVerifiedAt != nil
	})
	f.clock = f.status().Switchover.DeadlineAt.Add(time.Second)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.SourceFenceRestoredAt != nil
	})
	f.calls = nil
	return f
}

func TestTimedOutCandidateIsRevalidatedBeforeAnotherWindow(t *testing.T) {
	f := timedOutFixture(t)
	f.sql.behind = true
	f.retryRestoredWindow()
	assert.Equal(t, []string{"fence-green", "catch-up"}, f.calls)
	assert.Equal(t, int64(1), f.status().Switchover.Sequence, "a lagging green opens no window")

	f.sql.behind = false
	f.retryRestoredWindow()
	assert.Equal(t, int64(2), f.status().Switchover.Sequence)
	assert.Nil(t, f.status().Switchover.StartedAt, "the new window must establish every interruption proof again")
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	assert.Equal(t, 1, f.endpoints.commits)
}

func TestRevalidationRejectsGreenConfigurationDrift(t *testing.T) {
	f := timedOutFixture(t)
	green := &cnpgv1.Cluster{}
	require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "green"}, green))
	green.Spec.Instances = 5
	require.NoError(t, f.runtime.client.Update(t.Context(), green))

	_, err := f.runtime.RevalidateCandidate(t.Context(), mvutypes.Intent{AttemptID: "attempt"}, f.status())
	require.ErrorIs(t, err, mvutypes.ErrCandidateInvalid)
	assert.NotContains(t, f.calls, "catch-up")
}

func TestRevalidationRequiresARestoredPreparedWindow(t *testing.T) {
	f := newCutoverFixture(t)
	_, err := f.runtime.RevalidateCandidate(t.Context(), mvutypes.Intent{AttemptID: "attempt"}, f.status())
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

type stalledRevalidationSQL struct{ cutoverSQL }

func (s stalledRevalidationSQL) VerifyCandidateCatchUp(ctx context.Context, _ *cnpgv1.Cluster, _ *cnpgv1.Cluster, _ []declaredDatabase, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ cutoverIdentity) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

func TestRevalidationTimeoutLeavesWindowRestoredAndCanRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := timedOutFixture(t)
		f.runtime.sql = stalledRevalidationSQL{f.sql}
		status := f.status()
		restored := status.Switchover.SourceFenceRestoredAt.DeepCopy()
		start := time.Now()
		ready, err := f.runtime.RevalidateCandidate(t.Context(), mvutypes.Intent{AttemptID: "attempt"}, status)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.False(t, ready)
		assert.Equal(t, cutoverRecoveryTimeout, time.Since(start))
		assert.Equal(t, restored, status.Switchover.SourceFenceRestoredAt)
		assert.Equal(t, int64(1), status.Switchover.Sequence)
		f.runtime.sql = f.sql
		ready, err = f.runtime.RevalidateCandidate(t.Context(), mvutypes.Intent{AttemptID: "attempt"}, status)
		require.NoError(t, err)
		assert.True(t, ready)
		assert.NotContains(t, f.calls, "apply-fence", "only green is fenced during revalidation")
	})
}

func TestRevalidationWaitsForPoolerRecoveryBeforeAnotherWindow(t *testing.T) {
	f := timedOutFixture(t)
	c := f.runtime.client
	require.NoError(t, appsv1.AddToScheme(c.Scheme()))
	pooler := &cnpgv1.Pooler{Name: "green-pooler-rw", Namespace: f.runtime.key.Namespace, UID: "pooler-uid",
		Spec:   cnpgv1.PoolerSpec{Cluster: cnpgv1.LocalObjectReference{Name: "green"}, Type: cnpgv1.PoolerTypeRW, Instances: new(int32(1))},
		Status: cnpgv1.PoolerStatus{Phase: cnpgv1.PoolerPhaseActive, Instances: 1}}
	selector := map[string]string{"cnpg.io/poolerName": pooler.Name}
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}
	service := &corev1.Service{Name: pooler.Name, Namespace: pooler.Namespace, UID: "pooler-service-uid", OwnerReferences: owner,
		Spec: corev1.ServiceSpec{Selector: selector, Ports: []corev1.ServicePort{{Name: "pgbouncer", Port: 5432, TargetPort: intstr.FromString("pgbouncer"), Protocol: corev1.ProtocolTCP}}}}
	deployment := &appsv1.Deployment{Name: pooler.Name, Namespace: pooler.Namespace, UID: "deployment-uid", Generation: 1, OwnerReferences: owner,
		Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{Labels: selector, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pgbouncer", Image: "pgbouncer:test", Ports: []corev1.ContainerPort{{Name: "pgbouncer", ContainerPort: 5432}}}}}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}}
	for _, object := range []client.Object{pooler, service, deployment} {
		require.NoError(t, c.Create(t.Context(), object))
	}
	f.mutate(func(parent *platformv1alpha1.PostgresCluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		parent.Status.ConnectionPoolerStatus = &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true}
		status.CandidateEndpoints.PoolerService = serviceReference(service)
	})
	ready, err := f.runtime.RevalidateCandidate(t.Context(), mvutypes.Intent{}, f.status())
	require.NoError(t, err)
	require.True(t, ready, "first certify a healthy candidate through the real endpoint proof")
	blue := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: f.runtime.key.Namespace, Name: "blue"}, blue))
	before := blue.Spec.DeepCopy()
	deployment.Status.AvailableReplicas, deployment.Status.ReadyReplicas = 0, 0
	require.NoError(t, c.Status().Update(t.Context(), deployment))
	parent := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, c.Get(t.Context(), f.runtime.key, parent))
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt", Generation: parent.Generation,
		RetryRequestedAt: new(metav1.NewTime(f.clock.Add(time.Second))), State: parent.Status.PostgresMajorUpgradeStatus}
	f.calls = nil
	outcome, err := bluegreen.New(mvutypes.Scheduled, nil, f.candidate, f.runtime).Advance(t.Context(), intent)
	require.ErrorContains(t, err, "Deployment is not ready")
	assert.True(t, outcome.Report.Retry)
	assert.Equal(t, string(mvutypes.Upgrading), outcome.Report.Phase)
	assert.Equal(t, int64(1), outcome.BlueGreen.Switchover.Sequence)
	assert.NotNil(t, outcome.BlueGreen.Switchover.SourceFenceRestoredAt)
	assert.NotContains(t, f.calls, "apply-fence")
	assert.Zero(t, f.endpoints.commits)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(blue), blue))
	assert.Equal(t, *before, blue.Spec, "waiting for the Pooler must leave restored blue writable")
	deployment.Status.AvailableReplicas, deployment.Status.ReadyReplicas = 1, 1
	require.NoError(t, c.Status().Update(t.Context(), deployment))
	outcome, err = bluegreen.New(mvutypes.Upgrading, nil, f.candidate, f.runtime).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.SwitchingOver), outcome.Report.Phase)
	assert.Equal(t, int64(2), outcome.BlueGreen.Switchover.Sequence)
}
