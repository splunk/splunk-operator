/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

type accessSQLStub struct {
	prepareCalls  int
	retainedCalls int
	fenceReady    bool
	openReady     bool
	verifyError   error
}

func (s *accessSQLStub) PrepareControl(context.Context, *cnpgv1.Cluster, []declaredDatabase, cutoverIdentity) error {
	s.prepareCalls++
	return nil
}
func (s *accessSQLStub) VerifyControl(context.Context, *cnpgv1.Cluster, cutoverIdentity) error {
	return s.verifyError
}
func (s *accessSQLStub) VerifyFence(context.Context, *cnpgv1.Cluster, cutoverIdentity, []string) (bool, error) {
	return s.fenceReady, s.verifyError
}
func (s *accessSQLStub) VerifyRetainedSourceFence(context.Context, *cnpgv1.Cluster, cutoverIdentity, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	s.retainedCalls++
	return s.fenceReady, s.verifyError
}
func (s *accessSQLStub) VerifyOpen(context.Context, *cnpgv1.Cluster, cutoverIdentity, []platformv1alpha1.BlueGreenRoleLoginStatus) (bool, error) {
	return s.openReady, s.verifyError
}

func accessFixture(t *testing.T) (*blueGreenAccess, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, *accessSQLStub) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	owner := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default", UID: "owner-uid"}
	blue := &cnpgv1.Cluster{
		Name: "orders-blue", Namespace: owner.Namespace, UID: "blue-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))},
		Spec: cnpgv1.ClusterSpec{
			PostgresConfiguration: cnpgv1.PostgresConfiguration{PgHBA: []string{"hostssl all all all scram-sha-256"}},
			Managed:               &cnpgv1.ManagedConfiguration{Roles: []cnpgv1.RoleConfiguration{{Name: "admin", Login: true, PasswordSecret: &cnpgv1.LocalObjectReference{Name: "admin-secret"}}, {Name: "writer", Login: false}}},
		},
	}
	green := blue.DeepCopy()
	green.Name, green.UID = "orders-green", "green-uid"
	green.Labels = map[string]string{blueGreenAttemptLabel: "attempt-1"}
	green.Spec.PostgresConfiguration.PgHBA = []string{"hostssl all all 10.0.0.0/8 scram-sha-256"}
	green.Spec.Managed.Roles[1].Login = true
	databases := []declaredDatabase{{name: "orders", definition: platformv1alpha1.DatabaseDefinition{Name: "orders", AdminRoleName: "admin", RWRoleName: "writer"}}}
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt-1", Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1},
		Blue:  &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Namespace: blue.Namespace, Name: blue.Name, UID: blue.UID}},
		Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Namespace: green.Namespace, Name: green.Name, UID: green.UID}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, blue, green).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if secret, ok := obj.(*corev1.Secret); ok && secret.UID == "" {
				secret.UID = "control-secret-uid"
			}
			return c.Create(ctx, obj, opts...)
		},
	}).Build()
	sql := &accessSQLStub{fenceReady: true, openReady: true}
	access, err := newBlueGreenAccess(c, c, client.ObjectKeyFromObject(owner), owner, sql)
	require.NoError(t, err)
	return access, blue, green, databases, status, sql
}

func TestAccessSnapshotIsIndependentPerEnvironmentAndDoesNotFence(t *testing.T) {
	a, blue, green, databases, status, sql := accessFixture(t)
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
	assert.Equal(t, blue.Spec.PostgresConfiguration.PgHBA, *status.Switchover.OriginalHBA)
	assert.Equal(t, green.Spec.PostgresConfiguration.PgHBA, *status.Switchover.GreenOriginalHBA)
	assert.False(t, status.Switchover.OriginalRoleLogins[1].Login)
	assert.True(t, status.Switchover.GreenOriginalRoleLogins[1].Login)
	assert.Zero(t, sql.prepareCalls)
	stored := &cnpgv1.Cluster{}
	require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(blue), stored))
	assert.Equal(t, blue.Spec, stored.Spec)
	secret := &corev1.Secret{}
	require.NoError(t, a.client.Get(t.Context(), client.ObjectKey{Namespace: a.key.Namespace, Name: status.Switchover.ControlIdentity.SecretRef.Name}, secret))
	assert.Equal(t, corev1.SecretTypeBasicAuth, secret.Type)
	assert.True(t, *secret.Immutable)
	assert.True(t, metav1.IsControlledBy(secret, a.owner))
	assert.NotEmpty(t, secret.Data["password"])
	identity, err := a.Credentials(t.Context(), status)
	require.NoError(t, err)
	assert.Equal(t, cutoverRoleName(status.AttemptID), identity.Role)
	assert.NotEmpty(t, identity.Password)
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
	repeated, err := a.Credentials(t.Context(), status)
	require.NoError(t, err)
	assert.Equal(t, identity, repeated)
}

func TestAccessSnapshotRejectsForeignOrReplacementControlSecrets(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*corev1.Secret)
	}{
		{name: "another owner", change: func(secret *corev1.Secret) { secret.OwnerReferences[0].UID = "different-owner" }},
		{name: "another attempt", change: func(secret *corev1.Secret) { secret.Labels[blueGreenAttemptLabel] = "attempt-2" }},
		{name: "mutable secret", change: func(secret *corev1.Secret) { secret.Immutable = new(false) }},
		{name: "missing credential", change: func(secret *corev1.Secret) { delete(secret.Data, "password") }},
		{name: "wrong role", change: func(secret *corev1.Secret) { secret.Data["username"] = []byte("postgres") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, blue, green, databases, status, _ := accessFixture(t)
			require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
			secret := &corev1.Secret{}
			require.NoError(t, a.client.Get(t.Context(), client.ObjectKey{Namespace: a.key.Namespace, Name: status.Switchover.ControlIdentity.SecretRef.Name}, secret))
			test.change(secret)
			require.NoError(t, a.client.Update(t.Context(), secret))
			_, err := a.Credentials(t.Context(), status)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
		})
	}
	t.Run("recorded UID replaced", func(t *testing.T) {
		a, blue, green, databases, status, _ := accessFixture(t)
		require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
		secret := &corev1.Secret{}
		require.NoError(t, a.client.Get(t.Context(), client.ObjectKey{Namespace: a.key.Namespace, Name: status.Switchover.ControlIdentity.SecretRef.Name}, secret))
		require.NoError(t, a.client.Delete(t.Context(), secret))
		secret.UID, secret.ResourceVersion = "replacement-uid", ""
		require.NoError(t, a.client.Create(t.Context(), secret))
		_, err := a.Credentials(t.Context(), status)
		require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	})
	t.Run("recorded secret missing is never regenerated", func(t *testing.T) {
		a, blue, green, databases, status, _ := accessFixture(t)
		require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
		secret := &corev1.Secret{Namespace: a.key.Namespace, Name: status.Switchover.ControlIdentity.SecretRef.Name}
		require.NoError(t, a.client.Delete(t.Context(), secret))
		_, err := a.Credentials(t.Context(), status)
		require.Error(t, err)
		var secrets corev1.SecretList
		require.NoError(t, a.client.List(t.Context(), &secrets))
		assert.Empty(t, secrets.Items)
	})
}

func TestAccessFenceRequiresPreparedControlAndLiveSQLProof(t *testing.T) {
	a, blue, green, databases, status, sql := accessFixture(t)
	require.Error(t, a.ApplyFence(t.Context(), blue, green, status))
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
	require.Error(t, a.ApplyFence(t.Context(), blue, green, status))
	require.NoError(t, a.PrepareControl(t.Context(), blue, green, databases, status))
	assert.Equal(t, 2, sql.prepareCalls)
	require.NoError(t, a.PrepareControl(t.Context(), blue, green, databases, status))
	assert.Equal(t, 2, sql.prepareCalls, "the preparation receipt prevents repeating mutation")
	require.NoError(t, a.ApplyFence(t.Context(), blue, green, status))
	require.NoError(t, a.ApplyFence(t.Context(), blue, green, status))
	for _, cluster := range []*cnpgv1.Cluster{blue, green} {
		stored := &cnpgv1.Cluster{}
		require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(cluster), stored))
		assert.False(t, stored.Spec.Managed.Roles[0].Login)
		assert.False(t, stored.Spec.Managed.Roles[1].Login)
		assert.Equal(t, "admin-secret", stored.Spec.Managed.Roles[0].PasswordSecret.Name)
		assert.Equal(t, "host all all all reject", stored.Spec.PostgresConfiguration.PgHBA[3])
		assert.Contains(t, stored.Spec.PostgresConfiguration.PgHBA[0], cutoverRoleName(status.AttemptID))
	}
	sql.fenceReady = false
	ready, err := a.VerifyFences(t.Context(), blue, green, status)
	require.NoError(t, err)
	assert.False(t, ready, "a CNPG patch is not effective SQL proof")
	sql.fenceReady = true
	ready, err = a.VerifyFences(t.Context(), blue, green, status)
	require.NoError(t, err)
	assert.True(t, ready)
	sql.verifyError = errors.New("cannot inspect loaded HBA")
	_, err = a.VerifyFences(t.Context(), blue, green, status)
	require.ErrorContains(t, err, "cannot inspect loaded HBA")
}

func TestAccessRestoresOnlySelectedEnvironmentAndWaitsForSQL(t *testing.T) {
	a, blue, green, databases, status, sql := accessFixture(t)
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
	require.NoError(t, a.PrepareControl(t.Context(), blue, green, databases, status))
	require.NoError(t, a.ApplyFence(t.Context(), blue, green, status))
	sql.openReady = false
	ready, err := a.RestoreBlue(t.Context(), blue, status)
	require.NoError(t, err)
	assert.False(t, ready)
	storedBlue, storedGreen := &cnpgv1.Cluster{}, &cnpgv1.Cluster{}
	require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(blue), storedBlue))
	require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(green), storedGreen))
	assert.Equal(t, blue.Spec, storedBlue.Spec)
	assert.True(t, accessMatches(storedGreen, status, sideGreen, fenceApplied), "rollback leaves green fenced")
	sql.openReady = true
	ready, err = a.RestoreBlue(t.Context(), blue, status)
	require.NoError(t, err)
	assert.True(t, ready)
	ready, err = a.OpenGreen(t.Context(), green, status)
	require.NoError(t, err)
	assert.True(t, ready)
	require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(green), storedGreen))
	assert.Equal(t, green.Spec, storedGreen.Spec)
}

func TestAccessFenceRefusesConcurrentAccessEditsAndEnvironmentReplacement(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*cnpgv1.Cluster)
	}{
		{name: "changed HBA", change: func(cluster *cnpgv1.Cluster) {
			cluster.Spec.PostgresConfiguration.PgHBA = []string{"host all all all trust"}
		}},
		{name: "changed login", change: func(cluster *cnpgv1.Cluster) { cluster.Spec.Managed.Roles[1].Login = true }},
		{name: "added role", change: func(cluster *cnpgv1.Cluster) {
			cluster.Spec.Managed.Roles = append(cluster.Spec.Managed.Roles, cnpgv1.RoleConfiguration{Name: "out-of-band", Login: true})
		}},
		{name: "changed UID", change: func(cluster *cnpgv1.Cluster) { cluster.UID = types.UID("replacement-blue") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, blue, green, databases, status, _ := accessFixture(t)
			require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
			require.NoError(t, a.PrepareControl(t.Context(), blue, green, databases, status))
			live := &cnpgv1.Cluster{}
			require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(blue), live))
			test.change(live)
			require.NoError(t, a.client.Update(t.Context(), live))
			err := a.ApplyFence(t.Context(), blue, green, status)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			stored := &cnpgv1.Cluster{}
			require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(blue), stored))
			assert.Equal(t, live.Spec, stored.Spec)
		})
	}
}

func TestNormalizeFencePreservesOtherSpecChangesForFingerprint(t *testing.T) {
	a, blue, green, databases, status, _ := accessFixture(t)
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
	fenced := blue.DeepCopy()
	setAccess(fenced, status, sideBlue, fenceApplied)
	fenced.Spec.ImageName = "unexpected-image"
	fenced.Spec.Managed.Roles[0].CreateDB = true
	normalized, err := NormalizeFence(fenced, status, sideBlue)
	require.NoError(t, err)
	assert.Equal(t, blue.Spec.PostgresConfiguration.PgHBA, normalized.Spec.PostgresConfiguration.PgHBA)
	assert.True(t, normalized.Spec.Managed.Roles[0].Login)
	assert.True(t, normalized.Spec.Managed.Roles[0].CreateDB, "fingerprinting must still observe unrelated privilege drift")
	assert.Equal(t, "unexpected-image", normalized.Spec.ImageName)
	assert.False(t, fenced.Spec.Managed.Roles[0].Login, "normalization must not mutate live input")
	fenced.Spec.PostgresConfiguration.PgHBA[3] = "host all all all trust"
	_, err = NormalizeFence(fenced, status, sideBlue)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestAccessSnapshotRejectsIncompleteApplicationInventory(t *testing.T) {
	for _, change := range []func(*cnpgv1.Cluster){
		func(cluster *cnpgv1.Cluster) { cluster.Spec.Managed.Roles = cluster.Spec.Managed.Roles[:1] },
		func(cluster *cnpgv1.Cluster) {
			cluster.Spec.Managed.Roles = append(cluster.Spec.Managed.Roles, cnpgv1.RoleConfiguration{Name: "unknown", Login: true})
		},
		func(cluster *cnpgv1.Cluster) { cluster.Spec.Managed.Roles[0].Ensure = cnpgv1.EnsureAbsent },
		func(cluster *cnpgv1.Cluster) { cluster.Spec.Managed.Roles[1].Name = "admin" },
	} {
		a, blue, green, databases, status, _ := accessFixture(t)
		change(blue)
		require.NoError(t, a.client.Update(t.Context(), blue))
		require.ErrorIs(t, a.Snapshot(t.Context(), blue, green, databases, status), mvutypes.ErrUpgradeFlowFailed)
		assert.Nil(t, status.Switchover.ControlIdentity)
	}
}

func TestAccessAcceptsLegacyBlueReferenceWithoutTypeMetadata(t *testing.T) {
	a, blue, green, databases, status, _ := accessFixture(t)
	status.Blue.Ref.Kind, status.Blue.Ref.APIVersion = "", ""
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
	status.Blue.Ref.Kind = "Secret"
	_, err := a.liveEnvironment(t.Context(), blue, status, sideBlue)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestFenceBlueAfterGreenOpenedNeverClosesGreen(t *testing.T) {
	a, blue, green, databases, status, sql := accessFixture(t)
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
	require.NoError(t, a.PrepareControl(t.Context(), blue, green, databases, status))
	require.NoError(t, a.ApplyFence(t.Context(), blue, green, status))
	ready, err := a.OpenGreen(t.Context(), green, status)
	require.NoError(t, err)
	require.True(t, ready)
	ready, err = a.FenceBlue(t.Context(), blue, status)
	require.NoError(t, err)
	require.True(t, ready)
	assert.Equal(t, 1, sql.retainedCalls)
	stored := &cnpgv1.Cluster{}
	require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(green), stored))
	assert.Equal(t, green.Spec, stored.Spec)
	sql.fenceReady = false
	ready, err = a.FenceBlue(t.Context(), blue, status)
	require.NoError(t, err)
	assert.False(t, ready)
}

func TestCommittedSourceFenceDriftIsNotSilentlyRepaired(t *testing.T) {
	a, blue, green, databases, status, sql := accessFixture(t)
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, status))
	require.NoError(t, a.PrepareControl(t.Context(), blue, green, databases, status))
	require.NoError(t, a.ApplyFence(t.Context(), blue, green, status))
	live := new(cnpgv1.Cluster)
	require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(blue), live))
	live.Spec.Managed.Roles[0].Login = true
	require.NoError(t, a.client.Update(t.Context(), live))
	ready, err := a.FenceBlue(t.Context(), blue, status)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.False(t, ready)
	assert.Zero(t, sql.retainedCalls)
	require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(blue), live))
	assert.True(t, live.Spec.Managed.Roles[0].Login, "a contradictory committed source fence requires explicit recovery")
}
