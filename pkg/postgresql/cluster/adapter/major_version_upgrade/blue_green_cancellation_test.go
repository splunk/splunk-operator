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
	"maps"
	"slices"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5/pgconn"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	majorupgrade "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	clusterk8s "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/k8s"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRestoreForCancellationSkipsUntouchedBlue(t *testing.T) {
	f := newCutoverFixture(t)
	status := f.status()
	status.Switchover = nil
	restored, err := f.runtime.RestoreForCancellation(t.Context(), "attempt", status)
	require.NoError(t, err)
	assert.True(t, restored)
	assert.Empty(t, f.calls)
}

func TestRestoreForCancellationRestoresAnOpenWindow(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPreparation != nil })
	f.calls = nil

	status := f.status()
	restored, err := f.runtime.RestoreForCancellation(t.Context(), "attempt", status)
	require.NoError(t, err)
	assert.False(t, restored, "clearing endpoint preparation is confirmed by a later pass")
	assert.Equal(t, []string{"restore-canonical-blue", "cancel-preparation"}, f.calls)

	status = f.status()
	restored, err = f.runtime.RestoreForCancellation(t.Context(), "attempt", status)
	require.NoError(t, err)
	assert.True(t, restored)
	assert.Contains(t, f.calls, "restore-blue")
	assert.NotNil(t, status.Switchover.SourceFenceRestoredAt)
	assert.NotContains(t, f.calls, "open-green")

	f.calls = nil
	restored, err = f.runtime.RestoreForCancellation(t.Context(), "attempt", status)
	require.NoError(t, err)
	assert.True(t, restored)
	assert.Empty(t, f.calls, "a restored window needs no further restoration")
}

func TestRestoreForCancellationWaitsForVerifiedBlueAccess(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.FenceVerifiedAt != nil
	})
	f.access.ready = false
	status := f.status()
	restored, err := f.runtime.RestoreForCancellation(t.Context(), "attempt", status)
	require.NoError(t, err)
	assert.False(t, restored)
	assert.Nil(t, status.Switchover.SourceFenceRestoredAt)
}

type candidateProbeFake struct {
	fenced bool
	roles  [][]string
}

func (p *candidateProbeFake) VerifyCandidateFence(_ context.Context, _ *cnpgv1.Cluster, _ string, roles []string) (bool, error) {
	p.roles = append(p.roles, roles)
	return p.fenced, nil
}

func newCandidateFenceFixture(t *testing.T) (*teardownFixture, *BlueGreenCancellation, *candidateProbeFake) {
	t.Helper()
	f := newTeardownFixture(t)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	green.Spec.Managed = &cnpgv1.ManagedConfiguration{Roles: []cnpgv1.RoleConfiguration{
		{Name: "orders_rw", Login: true, PasswordSecret: &cnpgv1.LocalObjectReference{Name: "orders-rw"}},
		{Name: "orders_owner", Login: false},
	}}
	green.Spec.PostgresConfiguration.PgHBA = []string{"hostssl all all all scram-sha-256"}
	require.NoError(t, f.client.Update(t.Context(), green))
	probe := &candidateProbeFake{fenced: true}
	return f, &BlueGreenCancellation{BlueGreenReplicationTeardown: f.teardown, client: f.client, probe: probe}, probe
}

func TestFenceCandidateAppliesOneWayFenceThenProbes(t *testing.T) {
	f, cancellation, probe := newCandidateFenceFixture(t)

	fenced, err := cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, fenced, "the fence is verified by a later pass")
	assert.Empty(t, probe.roles)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	assert.Equal(t, []string{candidateRejectAll, "hostssl all all all scram-sha-256"}, green.Spec.PostgresConfiguration.PgHBA)
	for _, role := range green.Spec.Managed.Roles {
		assert.False(t, role.Login, role.Name)
	}

	green.Status.ManagedRolesStatus.ByStatus = map[cnpgv1.RoleStatus][]string{cnpgv1.RoleStatusPendingReconciliation: {"orders_rw"}}
	require.NoError(t, f.client.Update(t.Context(), green))
	fenced, err = cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, fenced, "CNPG must apply the role change before the probe")
	assert.Empty(t, probe.roles)

	green.Status.ManagedRolesStatus.ByStatus = nil
	require.NoError(t, f.client.Update(t.Context(), green))
	fenced, err = cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	assert.True(t, fenced)
	assert.Equal(t, [][]string{{"postgres", "orders_rw"}}, probe.roles, "roles without a credential cannot be probed")

	require.True(t, f.exists(green))
	before := slices.Clone(green.Spec.PostgresConfiguration.PgHBA)
	_, err = cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	require.True(t, f.exists(green))
	assert.Equal(t, before, green.Spec.PostgresConfiguration.PgHBA, "the fence is applied once")
}

func TestFenceCandidatePrecedesAnExistingSwitchoverFence(t *testing.T) {
	f, cancellation, _ := newCandidateFenceFixture(t)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	switchoverFence := fencedHBA(f.status, []string{"hostssl all all all scram-sha-256"})
	green.Spec.PostgresConfiguration.PgHBA = switchoverFence
	require.NoError(t, f.client.Update(t.Context(), green))

	_, err := cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	require.True(t, f.exists(green))
	assert.Equal(t, append([]string{candidateRejectAll}, switchoverFence...), green.Spec.PostgresConfiguration.PgHBA)
}

func TestFenceCandidateDisablesLoginsWithoutRepeatingTheRule(t *testing.T) {
	f, cancellation, _ := newCandidateFenceFixture(t)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	green.Spec.PostgresConfiguration.PgHBA = []string{candidateRejectAll, "hostssl all all all scram-sha-256"}
	require.NoError(t, f.client.Update(t.Context(), green))

	_, err := cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	require.True(t, f.exists(green))
	assert.Equal(t, []string{candidateRejectAll, "hostssl all all all scram-sha-256"}, green.Spec.PostgresConfiguration.PgHBA)
	assert.False(t, green.Spec.Managed.Roles[0].Login)
}

func TestFenceCandidateWaitsForEffectiveDenial(t *testing.T) {
	f, cancellation, probe := newCandidateFenceFixture(t)
	_, err := cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	probe.fenced = false
	fenced, err := cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, fenced)
}

func TestFenceCandidateWithoutGreen(t *testing.T) {
	f, cancellation, probe := newCandidateFenceFixture(t)
	f.status.Green = nil
	fenced, err := cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	assert.True(t, fenced)
	assert.Empty(t, probe.roles)
}

func TestFenceCandidateRejectsForeignGreen(t *testing.T) {
	f, cancellation, _ := newCandidateFenceFixture(t)
	f.status.Green.Ref.UID = "replaced-green"
	_, err := cancellation.FenceCandidate(t.Context(), f.status)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestVerifyCandidateFenceRequiresIntentionalDenial(t *testing.T) {
	denied := &pgconn.PgError{Code: "28000", Message: `pg_hba.conf rejects connection for host "10.0.0.2"`}
	badPassword := &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}
	for _, test := range []struct {
		name    string
		connect error
		fenced  bool
		wantErr bool
	}{
		{name: "denied", connect: denied, fenced: true},
		{name: "unreachable instance", connect: &pgconn.PgError{Code: "57P03"}, wantErr: true},
		{name: "network unreachable", connect: syscall.ENETUNREACH, wantErr: true},
		{name: "login accepted", connect: nil},
		{name: "wrong password", connect: badPassword, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newTeardownFixture(t)
			green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
			require.True(t, f.exists(green))
			green.Spec.SuperuserSecret = &cnpgv1.LocalObjectReference{Name: "green-superuser"}
			green.Status.Certificates.ServerCASecret = "green-ca"
			pod := &corev1.Pod{Name: "green-1", Namespace: "default", UID: "green-1-uid", Labels: map[string]string{"cnpg.io/cluster": "green"},
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(green, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))},
				Status:          corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2"}}
			for _, object := range []client.Object{pod,
				&corev1.Secret{Name: "green-superuser", Namespace: "default", Data: map[string][]byte{"password": []byte("secret")}},
				&corev1.Secret{Name: "green-ca", Namespace: "default", Data: map[string][]byte{"ca.crt": []byte("ca")}},
			} {
				require.NoError(t, f.client.Create(t.Context(), object))
			}
			var hosts []string
			sql := &postgresSwitchoverSQL{client: f.client, connect: func(_ context.Context, host, _, user, _, _ string, _ []byte) (pginfra.Conn, error) {
				hosts = append(hosts, host+"/"+user)
				if test.connect != nil {
					return nil, test.connect
				}
				return &teardownSQLTestConn{db: &teardownSQLDatabase{}}, nil
			}}
			fenced, err := sql.VerifyCandidateFence(t.Context(), green, "attempt", []string{"postgres"})
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.fenced, fenced)
			assert.Equal(t, []string{"10.0.0.2/postgres"}, hosts)
		})
	}
}

func TestNewBlueGreenCancellationRequiresSwitchover(t *testing.T) {
	f := newTeardownFixture(t)
	_, err := NewBlueGreenCancellation(f.client, f.client, f.teardown.key, f.parent, nil)
	require.Error(t, err)
}

func TestCancellationDuringOpenWindowCompletesThroughPriorityRecovery(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPreparation != nil })
	for _, name := range []string{"blue", "green"} {
		cluster := &cnpgv1.Cluster{}
		require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: name}, cluster))
		cluster.Status.Phase = cnpgv1.PhaseHealthy
		cluster.Status.CurrentPrimary = name + "-1"
		cluster.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {name + "-1"}}
		require.NoError(t, f.runtime.client.Update(t.Context(), cluster))
	}
	parent := new(platformv1alpha1.PostgresCluster)
	require.NoError(t, f.runtime.reader.Get(t.Context(), f.runtime.key, parent))
	secret := &corev1.Secret{}
	require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: replicationSecretName(parent.Name, "attempt")}, secret))
	secret.Labels = map[string]string{blueGreenAttemptLabel: "attempt"}
	secret.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
	require.NoError(t, f.runtime.client.Update(t.Context(), secret))
	parent.Spec.Class = "deleted-class"
	parent.Spec.PostgresMajorUpgradeConfig = &platformv1alpha1.PostgresMajorUpgradeConfig{Allow: new(true), Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen),
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cancel: new(true)}}
	require.NoError(t, f.runtime.client.Update(t.Context(), parent))

	teardownSQL := &teardownSQLFake{slotsGone: true}
	probe := &candidateProbeFake{fenced: true}
	rc := &clustercore.ReconcileContext{Client: f.runtime.client, APIReader: f.runtime.reader, Scheme: f.runtime.client.Scheme(), Recorder: record.NewFakeRecorder(10),
		UseCaseRegistryProvider: func(key types.NamespacedName, p *platformv1alpha1.PostgresCluster, _ *clustercore.MergedConfig) map[string]usecases.Factory {
			return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) {
				stateStore, err := clusterk8s.NewClusterStateStoreWithReader(f.runtime.client, f.runtime.reader, key)
				if err != nil {
					return nil, err
				}
				teardown := &BlueGreenReplicationTeardown{client: f.runtime.client, reader: f.runtime.reader, key: key, owner: p, sql: teardownSQL}
				cancellation := &BlueGreenCancellation{BlueGreenReplicationTeardown: teardown, client: f.runtime.client, switchover: f.runtime, probe: probe}
				return majorupgrade.NewMajorUpgradeUseCase(NewMajorUpgradeStateStoreWithTarget(stateStore, "18"), nil, nil, NewUpgradeNotifier(cutoverTestEvents{}, p), nil, nil, f.runtime).
					WithBlueGreenCanceller(cancellation), nil
			}}
		}}

	phase := func() string {
		parent := new(platformv1alpha1.PostgresCluster)
		require.NoError(t, f.runtime.reader.Get(t.Context(), f.runtime.key, parent))
		return *parent.Status.PostgresMajorUpgradeStatus[0].Phase
	}
	for range 12 {
		if phase() == string(mvutypes.Cancelled) {
			break
		}
		_, err := clustercore.PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: f.runtime.key}, nil, nil, nil, nil)
		require.NoError(t, err)
	}
	require.Equal(t, string(mvutypes.Cancelled), phase())
	status := f.status()
	assert.Nil(t, status.EndpointPreparation)
	assert.Nil(t, status.CommitReceipt)
	assert.NotNil(t, status.Switchover.SourceFenceRestoredAt)
	assert.NotNil(t, status.Gates.Cancellation.CompletedAt)
	assert.Contains(t, f.calls, "restore-blue")
	assert.NotContains(t, f.calls, "commit")
	assert.NotContains(t, f.calls, "open-green")
	assert.Zero(t, f.endpoints.commits)
	assert.Len(t, probe.roles, 1)
	require.NotEmpty(t, teardownSQL.roleCalls)
	for _, call := range teardownSQL.roleCalls {
		assert.Equal(t, "blue", call.cluster, "fenced green is left for cleanup")
	}
	err := f.runtime.reader.Get(t.Context(), client.ObjectKeyFromObject(secret), &corev1.Secret{})
	assert.True(t, apierrors.IsNotFound(err), "the replication Secret is removed")
	cleanup := meta.FindStatusCondition(status.Conditions, mvutypes.ConditionCleanupAvailable)
	require.NotNil(t, cleanup)
	assert.Equal(t, metav1.ConditionTrue, cleanup.Status)
}

func TestCancelAfterUnrecordedCanonicalCommitFailsForward(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPreparation != nil })
	// The canonical write landed but its status receipt was lost.
	f.endpoints.committed = true
	f.calls = nil

	parent := new(platformv1alpha1.PostgresCluster)
	require.NoError(t, f.runtime.reader.Get(t.Context(), f.runtime.key, parent))
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt",
		Generation: parent.Generation, CancelRequested: true, State: parent.Status.PostgresMajorUpgradeStatus}
	cancellation := &BlueGreenCancellation{client: f.runtime.client, switchover: f.runtime}
	outcome, err := bluegreen.New(mvutypes.SwitchingOver, nil, nil, f.runtime).WithCanceller(cancellation).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.False(t, mvutypes.BlueGreenCancellationRequested(outcome.BlueGreen), "an observed commit makes cancellation invalid")
	assert.NotEqual(t, string(mvutypes.Cancelling), outcome.Report.Phase)
	assert.NotContains(t, f.calls, "restore-blue")
	assert.NotContains(t, f.calls, "restore-canonical-blue")
}

func TestRestoreForCancellationBoundsEachPass(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newCutoverFixture(t)
		f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
			return bg.Switchover.FenceVerifiedAt != nil
		})
		f.runtime.access = blockedRecoveryAccess{cutoverAccess: f.access, inspect: func(ctx context.Context) {
			deadline, bounded := ctx.Deadline()
			require.True(t, bounded, "cancellation restoration must release its worker")
			assert.Equal(t, cutoverRecoveryTimeout, time.Until(deadline))
			<-ctx.Done()
		}}
		started := time.Now()
		restored, err := f.runtime.RestoreForCancellation(t.Context(), "attempt", f.status())
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.False(t, restored)
		assert.Equal(t, cutoverRecoveryTimeout, time.Since(started))
	})
}

func TestCancellationRecordsUnrecordedGreenBeforeRestoration(t *testing.T) {
	f := newTeardownFixture(t)
	green := healthyTeardownCluster(candidateClusterName(f.parent.Name, f.status.AttemptID), "unrecorded", *metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster")), map[string]string{blueGreenAttemptLabel: f.status.AttemptID})
	require.NoError(t, f.client.Create(t.Context(), green))
	f.status.Green = nil
	cancellation := &BlueGreenCancellation{BlueGreenReplicationTeardown: f.teardown, client: f.client}
	// No switchover runtime: discovery must return before restoration uses it.
	for range 2 {
		replay := f.status.DeepCopy()
		done, err := cancellation.RestoreSource(t.Context(), mvutypes.Intent{AttemptID: f.status.AttemptID}, replay)
		require.NoError(t, err)
		assert.False(t, done)
		assert.Equal(t, green.UID, replay.Green.Ref.UID)
	}
	assert.Empty(t, f.sql.stopCalls)
}

func TestFenceCandidateDiscoversUnrecordedGreen(t *testing.T) {
	f, cancellation, probe := newCandidateFenceFixture(t)
	recorded := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(recorded))
	green := recorded.DeepCopy()
	green.Name = candidateClusterName(f.parent.Name, f.status.AttemptID)
	green.UID = "unrecorded"
	green.ResourceVersion = ""
	require.NoError(t, f.client.Create(t.Context(), green))
	f.status.Green = nil
	fenced, err := cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, fenced, "record the recovered identity before applying the fence")
	require.NotNil(t, f.status.Green)
	assert.Equal(t, green.UID, f.status.Green.Ref.UID)
	fenced, err = cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, fenced, "wait for the applied fence before probing")
	require.True(t, f.exists(&cnpgv1.Cluster{Name: green.Name, Namespace: green.Namespace}))
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(green), green))
	assert.True(t, candidateFenced(green))
	fenced, err = cancellation.FenceCandidate(t.Context(), f.status)
	require.NoError(t, err)
	assert.True(t, fenced)
	assert.NotEmpty(t, probe.roles)
}

func TestCancellationCommitObservationBeforeGreenIsRecorded(t *testing.T) {
	for _, phase := range []mvutypes.Status{mvutypes.Scheduled, mvutypes.PreUpgradeBackup, mvutypes.Preflight} {
		t.Run(string(phase), func(t *testing.T) {
			f := newCutoverFixture(t)
			f.mutate(func(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
				parent.Status.PostgresMajorUpgradeStatus[0].Phase = new(string(phase))
				bg.Green, bg.Switchover, bg.Gates = nil, nil, nil
			})
			coordinator, err := NewEndpointCoordinator(f.runtime.client, f.runtime.reader)
			require.NoError(t, err)
			f.runtime.endpoints = coordinator
			cancellation := &BlueGreenCancellation{switchover: f.runtime}
			parent, status, err := f.runtime.readAttempt(t.Context(), "attempt")
			require.NoError(t, err)
			intent := mvutypes.Intent{AttemptID: "attempt", Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", CancelRequested: true, State: parent.Status.PostgresMajorUpgradeStatus}
			outcome, err := bluegreen.New(phase, nil, nil, f.runtime).WithCanceller(cancellation).Advance(t.Context(), intent)
			require.NoError(t, err)
			assert.Nil(t, status.Green)
			assert.True(t, mvutypes.BlueGreenCancellationRequested(outcome.BlueGreen))
			assert.Equal(t, string(mvutypes.Cancelling), outcome.Report.Phase)
		})
	}
}

func TestSwitchoverDefersFreshCancellationWithStaleIntent(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPreparation != nil })
	parent, status, err := f.runtime.readAttempt(t.Context(), "attempt")
	require.NoError(t, err)
	parent.Spec.PostgresMajorUpgradeConfig = &platformv1alpha1.PostgresMajorUpgradeConfig{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cancel: new(true)}}
	require.NoError(t, f.runtime.client.Update(t.Context(), parent))
	f.calls = nil
	outcome, err := f.runtime.AdvanceSwitchover(t.Context(), mvutypes.Intent{AttemptID: "attempt", CancelRequested: false}, status)
	require.NoError(t, err)
	assert.Contains(t, outcome.Report.Message, "cancellation request arrived")
	assert.Empty(t, f.calls)
	assert.Zero(t, f.endpoints.commits)
}

func TestCancellationStartsWithoutClassBeforeLatch(t *testing.T) {
	f := newCutoverFixture(t)
	f.mutate(func(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		parent.Status.PostgresMajorUpgradeStatus[0].Phase = new(string(mvutypes.ReadyForSwitchover))
		bg.Gates, bg.Switchover = nil, nil
		// The deleted class formerly authorized this jump. Cancellation does
		// not need to authorize it again using the default policy.
		parent.Status.PostgresMajorUpgradeStatus[0].TargetPgVersion = new("19")
	})
	parent, _, err := f.runtime.readAttempt(t.Context(), "attempt")
	require.NoError(t, err)
	parent.Spec.Class = "deleted-class"
	// Neither an allow gate nor current target configuration may starve recovery.
	parent.Spec.PostgresMajorUpgradeConfig = &platformv1alpha1.PostgresMajorUpgradeConfig{Allow: new(false), Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen), BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cancel: new(true)}}
	require.NoError(t, f.runtime.client.Update(t.Context(), parent))
	rc := &clustercore.ReconcileContext{Client: f.runtime.client, APIReader: f.runtime.reader, Scheme: f.runtime.client.Scheme(), Recorder: record.NewFakeRecorder(10),
		UseCaseRegistryProvider: func(key types.NamespacedName, p *platformv1alpha1.PostgresCluster, merged *clustercore.MergedConfig) map[string]usecases.Factory {
			assert.Nil(t, merged)
			return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) {
				stateStore, err := clusterk8s.NewClusterStateStoreWithReader(f.runtime.client, f.runtime.reader, key)
				require.NoError(t, err)
				return majorupgrade.NewMajorUpgradeUseCase(NewMajorUpgradeStateStore(stateStore), nil, nil, NewUpgradeNotifier(cutoverTestEvents{}, p), nil, nil, f.runtime).
					WithBlueGreenCanceller(&BlueGreenCancellation{switchover: f.runtime}), nil
			}}
		}}
	_, err = clustercore.PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: f.runtime.key}, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, f.runtime.reader.Get(t.Context(), f.runtime.key, parent))
	assert.Equal(t, string(mvutypes.Cancelling), *parent.Status.PostgresMajorUpgradeStatus[0].Phase)
	assert.True(t, mvutypes.BlueGreenCancellationRequested(parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen))
	assert.Empty(t, f.calls)
}

func TestCancellationLateCanonicalCommitRepairsReceiptWithRealCoordinator(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPreparation != nil })
	routing := map[string]string{pgconninfo.KeyClusterRWEndpoint: "green-rw.default.svc.cluster.local", pgconninfo.KeyClusterROEndpoint: "green-ro.default.svc.cluster.local", pgconninfo.KeyClusterREndpoint: "green-r.default.svc.cluster.local", pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort}
	fingerprint := pgconninfo.FingerprintEndpointData(routing)
	f.mutate(func(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "access"}}
		bg.EndpointPreparation.Routing = maps.Clone(routing)
		bg.EndpointPreparation.EndpointFingerprint = &fingerprint
		bg.EndpointPreparation.Acknowledgements = []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{{PostgresDatabaseName: "orders-database", PostgresDatabaseUID: "database-uid", PostgresDatabaseGeneration: 1, DatabaseName: "orders", EndpointFingerprint: &fingerprint, ValidatedAt: new(metav1.NewTime(f.clock))}}
	})
	coordinator, err := NewEndpointCoordinator(f.runtime.client, f.runtime.reader)
	require.NoError(t, err)
	f.runtime.endpoints = coordinator
	cancellation := &BlueGreenCancellation{switchover: f.runtime}
	advance := func() bluegreen.Outcome {
		t.Helper()
		parent, _, err := f.runtime.readAttempt(t.Context(), "attempt")
		require.NoError(t, err)
		intent := mvutypes.Intent{AttemptID: "attempt", Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", CancelRequested: true, State: parent.Status.PostgresMajorUpgradeStatus}
		outcome, err := bluegreen.New(mvutypes.Cancelling, nil, nil, f.runtime).WithCanceller(cancellation).Advance(t.Context(), intent)
		require.NoError(t, err)
		f.mutate(func(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			parent.Status.PostgresMajorUpgradeStatus[0].Phase = &outcome.Report.Phase
			*bg = *outcome.BlueGreen.DeepCopy()
		})
		return outcome
	}
	// Cancellation sees no commit and durably latches its request.
	outcome := advance()
	require.True(t, mvutypes.BlueGreenCancellationRequested(outcome.BlueGreen))
	// The earlier ConfigMap update completes after the latch, without its receipt.
	parent, _, err := f.runtime.readAttempt(t.Context(), "attempt")
	require.NoError(t, err)
	canonical := pgconninfo.NewClusterConfigMap(parent, "access", maps.Clone(routing))
	canonical.UID = "canonical-uid"
	canonical.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green", pgconninfo.AnnotationEndpointAttemptID: "attempt", pgconninfo.AnnotationEndpointIntentGeneration: "1", pgconninfo.AnnotationEndpointFingerprint: fingerprint}
	require.NoError(t, f.runtime.client.Create(t.Context(), canonical))
	f.calls = nil
	outcome = advance()
	require.NotNil(t, outcome.BlueGreen.CommitReceipt, "the real coordinator must repair the lost commit receipt")
	assert.Equal(t, canonical.UID, outcome.BlueGreen.CommitReceipt.CanonicalConfigMap.UID)
	assert.Equal(t, string(mvutypes.SwitchingOver), outcome.Report.Phase)
	assert.Nil(t, outcome.BlueGreen.Gates.Cancellation.SourceAccessRestoredAt)
	assert.NotContains(t, f.calls, "restore-blue")
	// A later pass remains in fail-forward recovery and maintains blue's fence.
	outcome = advance()
	assert.Contains(t, f.calls, "fence-blue")
	assert.NotContains(t, f.calls, "restore-blue")
	assert.NotEqual(t, string(mvutypes.Cancelling), outcome.Report.Phase)
}
