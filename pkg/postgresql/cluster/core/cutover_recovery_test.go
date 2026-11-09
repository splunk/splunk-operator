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

package core

import (
	"context"
	"errors"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	custommetrics "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/custom_metrics"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	dbadapter "github.com/splunk/splunk-operator/pkg/postgresql/database/adapter"
	dbreadiness "github.com/splunk/splunk-operator/pkg/postgresql/database/core/components/clusterreadiness"
	dbreconciliation "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	identityadapter "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/identity"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

type recoveryPriorityUseCase struct {
	act         func(context.Context) (reconciliationtypes.Report, error)
	active      bool
	blocksCalls int
}

func (*recoveryPriorityUseCase) Prerequisites(context.Context) error      { return nil }
func (u *recoveryPriorityUseCase) Schedule(context.Context) (bool, error) { return u.active, nil }
func (u *recoveryPriorityUseCase) BlocksComponents() []string {
	u.blocksCalls++
	return nil
}

func (u *recoveryPriorityUseCase) Act(ctx context.Context) (reconciliationtypes.Report, error) {
	return u.act(ctx)
}

func TestCutoverRecoveryPrecedesOrdinaryPrerequisites(t *testing.T) {
	for _, test := range []struct {
		name          string
		addClass      bool
		staleInformer bool
	}{
		{"class deleted after fencing", false, false},
		{"invalid class configuration and absent authoritative source", true, false},
		{"informer predates consumed gate", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := newTestScheme()
			parent := newTestCluster("orders", "default")
			parent.Spec.Class = "deleted-class"
			parent.Spec.PasswordConfig = &platformv1alpha1.SuperuserPasswordConfig{SuperuserExternalSecretRef: corev1.LocalObjectReference{Name: "unlabelled-superuser"}}
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1}}}}
			cached := parent.DeepCopy()
			if test.staleInformer {
				cached.Status.PostgresMajorUpgradeStatus = nil
			}
			objects := []client.Object{parent}
			if test.addClass {
				objects = append(objects, &platformv1alpha1.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: parent.Spec.Class}})
			}
			live := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent).WithObjects(objects...).Build()
			secretReads := 0
			cachedClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent).WithObjects(cached).WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok {
					secretReads++
					return errors.New("ordinary Secret validation failed")
				}
				return c.Get(ctx, key, obj, opts...)
			}}).Build()
			acts := 0
			uc := &recoveryPriorityUseCase{active: true, act: func(context.Context) (reconciliationtypes.Report, error) {
				acts++
				return reconciliationtypes.Report{Name: mvutypes.UseCaseName, Retry: true, Sleep: new(1)}, nil
			}}
			factoryCalls := 0
			rc := &ReconcileContext{Client: cachedClient, APIReader: live, Scheme: scheme, Recorder: record.NewFakeRecorder(5), UseCaseRegistryProvider: func(_ types.NamespacedName, got *platformv1alpha1.PostgresCluster, merged *MergedConfig) map[string]usecases.Factory {
				factoryCalls++
				require.True(t, cutoverStage(got).OwnsRecovery())
				if !test.addClass {
					assert.Nil(t, merged)
				}
				return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) { return uc, nil }}
			}}
			result, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, nil, nil, nil)
			require.NoError(t, err)
			assert.Equal(t, time.Second, result.RequeueAfter)
			assert.Equal(t, 1, acts)
			assert.Equal(t, 1, factoryCalls)
			assert.Zero(t, secretReads, "ordinary credential validation cannot precede cutover restoration")
		})
	}
}

func TestAvailableCutoverRunsOnceBeforeOrdinaryErrorOnEveryReconcile(t *testing.T) {
	parent, green, _ := availableTargetFixture()
	parent.Spec.Class = "orders-class"
	parent.Finalizers = []string{PostgresClusterFinalizerName}
	parent.Status.Phase = new(string(readyClusterPhase))
	scheme := newTestScheme()
	green.Spec.Instances = 1
	green.Status = cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: 1, ReadyInstances: 1, CurrentPrimary: "green-1", InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"green-1"}}}
	secret := &corev1.Secret{Name: green.GetSuperuserSecretName(), Namespace: parent.Namespace, Data: map[string][]byte{secretKeyPassword: []byte("test-password")}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent).WithObjects(parent, green, secret, &platformv1alpha1.PostgresClusterClass{Name: parent.Spec.Class}).Build()
	acts := 0
	uc := &recoveryPriorityUseCase{active: true, act: func(context.Context) (reconciliationtypes.Report, error) {
		acts++
		return reconciliationtypes.Report{Name: mvutypes.UseCaseName, Retry: true, Sleep: new(1)}, nil
	}}
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), ClusterCardResolver: identityadapter.NewIdentityResolver(), ClusterInputFactory: identityadapter.ClusterInputFromPostgresCluster, EnvironmentNamer: identityadapter.NewIdentityResolver(), UseCaseRegistryProvider: func(types.NamespacedName, *platformv1alpha1.PostgresCluster, *MergedConfig) map[string]usecases.Factory {
		return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) { return uc, nil }}
	}}
	for i := 1; i <= 2; i++ {
		result, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, nil, func(string, monitoring.Target) (*custommetrics.Model, error) {
			assert.Equal(t, i, acts)
			return nil, errors.New("ordinary metrics factory failed")
		}, nil)
		require.ErrorContains(t, err, "ordinary metrics factory failed")
		assert.Equal(t, time.Second, result.RequeueAfter)
		assert.Equal(t, i, acts, "the same runtime must execute exactly once each pass")
	}
}

func TestPriorityCutoverRejectsReplacementParent(t *testing.T) {
	parent := newTestCluster("orders", "default")
	live := parent.DeepCopy()
	live.UID = "replacement"
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(live).Build()
	err := refreshCutoverParent(t.Context(), c, parent)
	require.ErrorContains(t, err, "identity changed")
}

func TestAvailableCutoverRestoresOperationalReadinessAfterMetricsAndRolesConverge(t *testing.T) {
	parent, green, _ := availableTargetFixture()
	parent.Spec.Class = "orders-class"
	parent.Spec.Instances = new(int32(1))
	parent.Spec.PostgresVersion = new("18")
	parent.Spec.Storage = new(resource.MustParse("10Gi"))
	parent.Finalizers = []string{PostgresClusterFinalizerName}
	parent.Status.Phase = new(string(readyClusterPhase))
	parent.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}}
	parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Switchover.StartedAt = new(metav1.Now())
	parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Switchover.TargetAvailableAt = nil
	entry := &parent.Status.PostgresMajorUpgradeStatus[0]
	entry.Strategy, entry.SourcePgVersion, entry.TargetPgVersion, entry.Phase = new(mvutypes.MajorUpgradeFlowBlueGreen), new("17"), new("18"), new(string(mvutypes.PostUpgradeBackup))
	green.Spec.Instances = 1
	green.Status = cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: 1, ReadyInstances: 1, CurrentPrimary: "green-1", InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"green-1"}}, ManagedRolesStatus: cnpgv1.ManagedRoles{ByStatus: map[cnpgv1.RoleStatus][]string{cnpgv1.RoleStatusPendingReconciliation: {"app_user"}}}}
	child := postgresDatabaseWithManagedRoles("new-orders", []managedRole{{Name: "app_user", Exists: true}})
	child.Spec.ClusterRef.Name = parent.Name
	class := &platformv1alpha1.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: parent.Spec.Class}, Spec: platformv1alpha1.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: green.GetSuperuserSecretName(), Namespace: parent.Namespace}, Data: map[string][]byte{secretKeyPassword: []byte("test-password")}}
	scheme := newTestScheme()
	c := indexedManagedRolesTestClient(scheme, parent, green, child, class, secret).WithStatusSubresource(parent, green).Build()
	metrics := &availableMetricsFixture{}
	acts := 0
	uc := &recoveryPriorityUseCase{active: true, act: func(context.Context) (reconciliationtypes.Report, error) {
		acts++
		return reconciliationtypes.Report{Name: mvutypes.UseCaseName, Retry: true, Sleep: new(1)}, nil
	}}
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(30), ClusterCardResolver: identityadapter.NewIdentityResolver(), ClusterInputFactory: identityadapter.ClusterInputFromPostgresCluster, EnvironmentNamer: identityadapter.NewIdentityResolver(), UseCaseRegistryProvider: func(types.NamespacedName, *platformv1alpha1.PostgresCluster, *MergedConfig) map[string]usecases.Factory {
		return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) { return uc, nil }}
	}}
	reconcile := func() *platformv1alpha1.PostgresCluster {
		t.Helper()
		result, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, nil, func(string, monitoring.Target) (*custommetrics.Model, error) {
			return custommetrics.NewModel(metrics, metrics, metrics, metrics, metrics), nil
		}, nil)
		require.NoError(t, err)
		assert.NotZero(t, result.RequeueAfter, "runtime canonical repair remains scheduled through handoff")
		fresh := new(platformv1alpha1.PostgresCluster)
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
		assert.Equal(t, string(mvutypes.PostUpgradeBackup), *fresh.Status.PostgresMajorUpgradeStatus[0].Phase)
		assert.Equal(t, green.UID, fresh.Status.ProvisionerRef.UID)
		return fresh
	}
	gate := dbreadiness.New(dbadapter.NewClusterReader(c))
	input := dbreadiness.Input{Namespace: parent.Namespace, Name: parent.Name}
	fresh := reconcile()
	assert.Equal(t, string(configuringClusterPhase), *fresh.Status.Phase, "the priority cutover must leave Ready while production is fenced")
	assert.True(t, meta.IsStatusConditionFalse(fresh.Status.Conditions, "Ready"))
	fresh.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Switchover.TargetAvailableAt = new(metav1.Now())
	require.NoError(t, c.Status().Update(t.Context(), fresh))
	fresh = reconcile()
	assert.Equal(t, string(configuringClusterPhase), *fresh.Status.Phase)
	assert.Equal(t, dbreconciliation.ModeWaiting, gate.Observe(t.Context(), input).Outcome.Mode())
	metrics.ready = true
	fresh = reconcile()
	assert.Contains(t, fresh.Status.ManagedRolesStatus.Pending, "app_user")
	assert.NotEqual(t, string(readyClusterPhase), *fresh.Status.Phase)
	assert.Equal(t, dbreconciliation.ModeWaiting, gate.Observe(t.Context(), input).Outcome.Mode())
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(green), green))
	green.Status.ManagedRolesStatus.ByStatus = map[cnpgv1.RoleStatus][]string{cnpgv1.RoleStatusReconciled: {"app_user"}}
	require.NoError(t, c.Status().Update(t.Context(), green))
	fresh = reconcile()
	assert.Equal(t, string(readyClusterPhase), *fresh.Status.Phase)
	assert.True(t, meta.IsStatusConditionTrue(fresh.Status.Conditions, "Ready"))
	assert.Empty(t, fresh.Status.ManagedRolesStatus.Pending)
	assert.Equal(t, dbreconciliation.ModeConverged, gate.Observe(t.Context(), input).Outcome.Mode(), "new/changed DB work must pass the real parent readiness gate")
	assert.Equal(t, 4, acts)
	// The same live health/component path continues after publication handoff.
	fresh.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Switchover.EndpointPublicationEventRef = &corev1.ObjectReference{Name: "published", UID: "publication-event"}
	require.NoError(t, c.Status().Update(t.Context(), fresh))
	// A failover may first be observed after green is healthy again. Persist
	// the new primary even when all existing conditions remain unchanged.
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(green), green))
	green.Status.CurrentPrimary = "green-2"
	green.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"green-2"}}
	require.NoError(t, c.Status().Update(t.Context(), green))
	fresh = reconcile()
	assert.Equal(t, new("green-2"), fresh.Status.CurrentPrimary)
	assert.True(t, meta.IsStatusConditionTrue(fresh.Status.Conditions, "Ready"))
	// Historical TargetAvailable is insufficient if current green loses its primary.
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(green), green))
	green.Status.CurrentPrimary = "missing-primary"
	require.NoError(t, c.Status().Update(t.Context(), green))
	fresh = reconcile()
	assert.NotEqual(t, string(readyClusterPhase), *fresh.Status.Phase)
	assert.False(t, meta.IsStatusConditionTrue(fresh.Status.Conditions, "Ready"))
	assert.Equal(t, dbreconciliation.ModeWaiting, gate.Observe(t.Context(), input).Outcome.Mode())
	assert.Equal(t, 6, acts, "green health cannot starve canonical repair")
	assert.Zero(t, uc.blocksCalls, "the handoff owns component release; ordinary blocking rules must not participate")
}

func TestRestoredCutoverSchedulesOrdinarySourceHealthPass(t *testing.T) {
	for _, state := range []string{"healthy source", "unhealthy source", "missing class"} {
		t.Run(state, func(t *testing.T) {
			parent, blue, c := convergedClusterFixture(t)
			parent.Spec.PostgresVersion = new("18")
			parent.Spec.PostgresMajorUpgradeConfig.Strategy = new(mvutypes.MajorUpgradeFlowBlueGreen)
			parent.Status.Phase = new(string(configuringClusterPhase))
			meta.SetStatusCondition(&parent.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "BlueGreenSwitchover"})
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen), SourcePgVersion: new("17"), TargetPgVersion: new("18"), Phase: new(string(mvutypes.SwitchingOver)), BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: *parent.Status.ProvisionerRef.DeepCopy()}, Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: new(metav1.Now())}}}}
			status := parent.Status.DeepCopy()
			require.NoError(t, c.Update(t.Context(), parent))
			parent.Status = *status
			require.NoError(t, c.Status().Update(t.Context(), parent))
			acts := 0
			uc := &recoveryPriorityUseCase{active: true, act: func(ctx context.Context) (reconciliationtypes.Report, error) {
				acts++
				fresh := new(platformv1alpha1.PostgresCluster)
				if err := c.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return reconciliationtypes.Report{}, err
				}
				entry := &fresh.Status.PostgresMajorUpgradeStatus[0]
				entry.Phase = new(string(mvutypes.Failed))
				entry.BlueGreen.Switchover.SourceFenceRestoredAt = new(metav1.Now())
				ucReport := reconciliationtypes.Report{Name: mvutypes.UseCaseName, Phase: string(mvutypes.Failed)}
				return ucReport, c.Status().Update(ctx, fresh)
			}}
			rc := &ReconcileContext{Client: c, APIReader: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(30), ClusterCardResolver: identityadapter.NewIdentityResolver(), ClusterInputFactory: identityadapter.ClusterInputFromPostgresCluster, EnvironmentNamer: identityadapter.NewIdentityResolver(), UseCaseRegistryProvider: func(types.NamespacedName, *platformv1alpha1.PostgresCluster, *MergedConfig) map[string]usecases.Factory {
				return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) { return uc, nil }}
			}}
			metrics := &availableMetricsFixture{ready: true}
			reconcile := func() (ctrl.Result, error) {
				return PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, noopBackupBackend{}, func(string, monitoring.Target) (*custommetrics.Model, error) {
					return custommetrics.NewModel(metrics, metrics, metrics, metrics, metrics), nil
				}, nil)
			}
			result, err := reconcile()
			require.NoError(t, err)
			assert.Equal(t, time.Second, result.RequeueAfter, "a terminal status-only restoration must schedule the ownership transfer")
			fresh := new(platformv1alpha1.PostgresCluster)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
			assert.True(t, meta.IsStatusConditionFalse(fresh.Status.Conditions, "Ready"), "the restoration timestamp alone cannot certify source health")
			uc.active = false
			if state == "unhealthy source" {
				blue.Status.ReadyInstances = 0
				require.NoError(t, c.Status().Update(t.Context(), blue))
			}
			if state == "missing class" {
				require.NoError(t, c.Delete(t.Context(), &platformv1alpha1.PostgresClusterClass{Name: parent.Spec.Class}))
			}
			_, err = reconcile()
			if state == "missing class" {
				require.ErrorContains(t, err, "failed to fetch PostgresClusterClass")
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
			assert.Equal(t, state == "healthy source", meta.IsStatusConditionTrue(fresh.Status.Conditions, "Ready"))
			assert.Equal(t, 1, acts, "ordinary ownership must not consume a second interruption")
			current := new(cnpgv1.Cluster)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(blue), current))
			assert.Equal(t, blue.Spec.ImageName, current.Spec.ImageName, "the requested target image must not be applied to restored blue")
		})
	}
}

func TestPostgresClusterServiceRequiresUncachedReader(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	_, err := PostgresClusterService(t.Context(), &ReconcileContext{Client: c}, ctrl.Request{}, nil, nil, nil, nil)
	require.ErrorContains(t, err, "uncached API reader")
}

func TestRetainDeletionPreservesUnfinishedCutoverOwnership(t *testing.T) {
	for _, stage := range []string{"preparation", "fenced", "committed", "handoff"} {
		t.Run(stage, func(t *testing.T) {
			parent, blue, c := convergedClusterFixture(t)
			parent.Spec.ClusterDeletionPolicy = new(clusterDeletionPolicyRetain)
			bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: *parent.Status.ProvisionerRef.DeepCopy()}}
			if stage != "preparation" {
				bg.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: new(metav1.Now()), FenceAppliedAt: new(metav1.Now())}
			}
			if stage == "committed" || stage == "handoff" {
				bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: new(metav1.Now())}
			}
			if stage == "handoff" {
				bg.Switchover.TargetAvailableAt = new(metav1.Now())
			}
			require.NoError(t, c.Update(t.Context(), parent))
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen), BlueGreen: bg}}
			require.NoError(t, c.Status().Update(t.Context(), parent))
			control := &corev1.Secret{Name: "attempt-control", Namespace: parent.Namespace, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}, Data: map[string][]byte{"password": []byte("recovery-password")}}
			require.NoError(t, c.Create(t.Context(), control))
			require.NoError(t, c.Delete(t.Context(), parent))
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(blue), blue))
			rc := &ReconcileContext{Client: c, APIReader: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(10), EnvironmentNamer: identityadapter.NewIdentityResolver()}
			_, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, noopBackupBackend{}, nil, nil)
			require.ErrorContains(t, err, "requires verified replication teardown and source restoration")
			fresh := new(platformv1alpha1.PostgresCluster)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
			assert.Contains(t, fresh.Finalizers, PostgresClusterFinalizerName)
			condition := meta.FindStatusCondition(fresh.Status.Conditions, "Ready")
			require.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionFalse, condition.Status)
			assert.Equal(t, string(reasonClusterDeleteFailed), condition.Reason)
			currentBlue := new(cnpgv1.Cluster)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(blue), currentBlue))
			assert.Equal(t, blue.Spec, currentBlue.Spec)
			assert.Equal(t, blue.OwnerReferences, currentBlue.OwnerReferences)
			currentSecret := new(corev1.Secret)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(control), currentSecret))
			assert.Equal(t, control.Data, currentSecret.Data)
			assert.Equal(t, control.OwnerReferences, currentSecret.OwnerReferences)
		})
	}
}

func TestRetainFinalizationAllowsOrdinaryAndCleanedHistory(t *testing.T) {
	for _, bg := range []*platformv1alpha1.PostgresBlueGreenUpgradeStatus{nil, {AttemptID: "scheduled"}, {AttemptID: "cleaned", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{}, Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1}, Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned}}} {
		parent := newTestCluster("orders", "default")
		parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}}
		require.NoError(t, requireSafeRetainFinalization(parent))
	}
}

func TestAvailableCutoverClearsReadinessWhenSafetyEvidenceFails(t *testing.T) {
	for _, scenario := range []string{"missing green", "missing credential", "runtime error", "failed green"} {
		t.Run(scenario, func(t *testing.T) {
			parent, green, _ := availableTargetFixture()
			green.Spec.Instances = 1
			green.Status = cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: 1, ReadyInstances: 1, CurrentPrimary: "green-1", InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"green-1"}}}
			if scenario == "failed green" {
				green.Status.Phase = cnpgv1.PhaseUnrecoverable
			}
			parent.Spec.Class = "orders-class"
			parent.Status.Phase = new(string(readyClusterPhase))
			parent.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}}
			objects := []client.Object{parent, &platformv1alpha1.PostgresClusterClass{Name: parent.Spec.Class}}
			if scenario != "missing green" {
				objects = append(objects, green)
			}
			if scenario != "missing credential" {
				objects = append(objects, &corev1.Secret{Name: green.GetSuperuserSecretName(), Namespace: parent.Namespace, Data: map[string][]byte{secretKeyPassword: []byte("pw")}})
			}
			c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithStatusSubresource(parent).WithObjects(objects...).Build()
			uc := &recoveryPriorityUseCase{active: true, act: func(context.Context) (reconciliationtypes.Report, error) {
				report := reconciliationtypes.Report{Name: mvutypes.UseCaseName, Retry: true, Sleep: new(1)}
				if scenario == "runtime error" {
					return report, errors.New("canonical ownership contradiction")
				}
				return report, nil
			}}
			rc := &ReconcileContext{Client: c, APIReader: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(10), ClusterCardResolver: identityadapter.NewIdentityResolver(), ClusterInputFactory: identityadapter.ClusterInputFromPostgresCluster, EnvironmentNamer: identityadapter.NewIdentityResolver(), UseCaseRegistryProvider: func(types.NamespacedName, *platformv1alpha1.PostgresCluster, *MergedConfig) map[string]usecases.Factory {
				return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) { return uc, nil }}
			}}
			_, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, nil, nil, nil)
			require.Error(t, err)
			fresh := new(platformv1alpha1.PostgresCluster)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
			assert.False(t, meta.IsStatusConditionTrue(fresh.Status.Conditions, "Ready"), "Ready must be cleared when required production evidence disappears")
			assert.NotEqual(t, string(readyClusterPhase), *fresh.Status.Phase)
		})
	}
}
