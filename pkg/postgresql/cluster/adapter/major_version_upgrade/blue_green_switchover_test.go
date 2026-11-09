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
	"slices"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"

	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"

	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
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

type cutoverTestEvents struct{}

func (cutoverTestEvents) EmitNormalEvent(client.Object, string, string)  {}
func (cutoverTestEvents) EmitWarningEvent(client.Object, string, string) {}

type cutoverFixture struct {
	t         *testing.T
	runtime   *BlueGreenSwitchover
	access    *cutoverAccessFake
	sql       *cutoverSQLFake
	endpoints *cutoverEndpointsFake
	clock     time.Time
	calls     []string
}

func newCutoverFixture(t *testing.T) *cutoverFixture {
	t.Helper()
	f := &cutoverFixture{t: t, clock: time.Now().UTC().Truncate(time.Second)}
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default", UID: "parent-uid", Generation: 4}
	owner := *metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))
	blue := &cnpgv1.Cluster{Name: "blue", Namespace: parent.Namespace, UID: "blue-uid", OwnerReferences: []metav1.OwnerReference{owner},
		Spec: cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "blue-superuser"}}}
	green := blue.DeepCopy()
	green.Name, green.UID = "green", "green-uid"
	green.Spec.SuperuserSecret = &cnpgv1.LocalObjectReference{Name: "green-superuser"}
	green.Labels = map[string]string{blueGreenAttemptLabel: "attempt"}
	child := &platformv1alpha1.PostgresDatabase{Name: "orders-database", Namespace: parent.Namespace, UID: "database-uid", Generation: 1,
		Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}}}
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	now := metav1.NewTime(f.clock)
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt",
		Blue:                &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: blue.Name, Namespace: blue.Namespace, UID: blue.UID}},
		Green:               &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: green.Name, Namespace: green.Namespace, UID: green.UID}},
		Gates:               &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: parent.Generation, ConsumedAt: &now}},
		Switchover:          &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1},
		DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), DatabaseName: "orders"}},
	}
	parent.Status.ProvisionerRef = status.Blue.Ref.DeepCopy()
	parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Strategy: &strategy, SourcePgVersion: new("17"), TargetPgVersion: new("18"), Phase: new(string(mvutypes.SwitchingOver)), BlueGreen: status,
	}}
	objects := []client.Object{parent, blue, green, child,
		&corev1.Secret{Name: "blue-superuser", Namespace: parent.Namespace, UID: "blue-secret", Data: map[string][]byte{"password": []byte("source")}},
		&corev1.Secret{Name: "green-superuser", Namespace: parent.Namespace, UID: "green-secret", Data: map[string][]byte{"password": []byte("target")}},
		&corev1.Secret{Name: replicationSecretName(parent.Name, status.AttemptID), Namespace: parent.Namespace, UID: "replication-secret", Data: map[string][]byte{"password": []byte("replication")}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if event, ok := obj.(*corev1.Event); ok && event.UID == "" {
				event.UID = "publication-event"
			}
			return c.Create(ctx, obj, opts...)
		},
	}).Build()
	f.access = &cutoverAccessFake{f: f, ready: true}
	f.sql = &cutoverSQLFake{f: f, parity: true, drained: true}
	f.endpoints = &cutoverEndpointsFake{f: f, prepared: true, published: true}
	f.sql.targetDurable = true
	f.runtime = &BlueGreenSwitchover{client: c, reader: c, key: client.ObjectKeyFromObject(parent), owner: parent,
		access: f.access, sql: f.sql, endpoints: f.endpoints, now: func() time.Time { return f.clock }}
	declarations, err := listManagedDatabases(t.Context(), c, f.runtime.key)
	require.NoError(t, err)
	baseline := sourceBaseline{client: c, key: f.runtime.key, cluster: parent}
	fingerprint, err := baseline.configurationFingerprint(blue, declarations)
	require.NoError(t, err)
	credentials, err := baseline.credentialFingerprints(t.Context(), blue, declarations, replicationSecretName(parent.Name, status.AttemptID))
	require.NoError(t, err)
	f.mutate(func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		bg.SourceConfigurationFingerprint, bg.CredentialFingerprints = &fingerprint, credentials
	})
	return f
}

func (f *cutoverFixture) mutate(change func(*platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus)) {
	f.t.Helper()
	parent := new(platformv1alpha1.PostgresCluster)
	require.NoError(f.t, f.runtime.reader.Get(f.t.Context(), f.runtime.key, parent))
	change(parent, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen)
	require.NoError(f.t, f.runtime.client.Status().Update(f.t.Context(), parent))
}

func (f *cutoverFixture) status() *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	f.t.Helper()
	_, status, err := f.runtime.readAttempt(f.t.Context(), "attempt")
	require.NoError(f.t, err)
	return status
}

func (f *cutoverFixture) step() (bluegreen.Outcome, error) {
	f.t.Helper()
	parent := new(platformv1alpha1.PostgresCluster)
	require.NoError(f.t, f.runtime.reader.Get(f.t.Context(), f.runtime.key, parent))
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt", Generation: parent.Generation,
		SwitchoverTimeout: time.Minute, State: parent.Status.PostgresMajorUpgradeStatus}
	outcome, err := f.runtime.AdvanceSwitchover(f.t.Context(), intent, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen)
	if outcome.BlueGreen != nil {
		store := NewMajorUpgradeStateStore(newTestClusterStateStore(f.t, f.runtime.client, f.runtime.key))
		require.NoError(f.t, progressError(store.SaveMajorUpgradeProgress(f.t.Context(), intent, mvutypes.Progress{BlueGreen: outcome.BlueGreen, Report: outcome.Report})))
	}
	return outcome, err
}

func (f *cutoverFixture) until(predicate func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool) {
	f.t.Helper()
	for range 30 {
		if predicate(f.status()) {
			return
		}
		_, err := f.step()
		require.NoError(f.t, err)
		f.clock = f.clock.Add(time.Second)
	}
	f.t.Fatal("cutover did not reach the requested durable stage")
}

type cutoverAccessFake struct {
	f         *cutoverFixture
	ready     bool
	openDelay time.Duration
}

func (a *cutoverAccessFake) Snapshot(_ context.Context, blue, green *cnpgv1.Cluster, _ []declaredDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	a.f.calls = append(a.f.calls, "snapshot")
	bg.Switchover.OriginalHBA, bg.Switchover.GreenOriginalHBA = new(append([]string{}, blue.Spec.PostgresConfiguration.PgHBA...)), new(append([]string{}, green.Spec.PostgresConfiguration.PgHBA...))
	fingerprint, err := candidateConfigurationFingerprint(green)
	if err != nil {
		return err
	}
	bg.Switchover.GreenConfigurationFingerprint = &fingerprint
	bg.Switchover.ControlIdentity = &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: "control", SecretRef: corev1.ObjectReference{Name: "control-secret", UID: "control-uid"}}
	return nil
}
func (a *cutoverAccessFake) PrepareControl(_ context.Context, _, _ *cnpgv1.Cluster, _ []declaredDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	a.f.calls = append(a.f.calls, "prepare-control")
	bg.Switchover.ControlIdentity.PreparedAt = new(metav1.NewTime(a.f.clock))
	return nil
}
func (*cutoverAccessFake) Credentials(context.Context, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (cutoverIdentity, error) {
	return cutoverIdentity{Role: "control", Password: "operator-only", AttemptID: "attempt"}, nil
}
func (a *cutoverAccessFake) ApplyFence(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	a.f.calls = append(a.f.calls, "apply-fence")
	return nil
}
func (a *cutoverAccessFake) VerifyFences(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	a.f.calls = append(a.f.calls, "verify-fences")
	return a.ready, nil
}
func (a *cutoverAccessFake) FenceBlue(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	a.f.calls = append(a.f.calls, "fence-blue")
	return a.ready, nil
}
func (a *cutoverAccessFake) RestoreBlue(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	a.f.calls = append(a.f.calls, "restore-blue")
	return a.ready, nil
}
func (a *cutoverAccessFake) OpenGreen(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	a.f.calls = append(a.f.calls, "open-green")
	a.f.clock = a.f.clock.Add(a.openDelay)
	return a.ready, nil
}

func TestGreenAccessVerificationRecordsDeadlineCrossedDuringProbe(t *testing.T) {
	for _, completion := range []time.Duration{-time.Nanosecond, 0, time.Second} {
		t.Run(completion.String(), func(t *testing.T) {
			f := newCutoverFixture(t)
			f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
				return bg.Switchover.BlueSessionsTerminatedAt != nil
			})
			deadline := f.status().Switchover.DeadlineAt.Time
			f.clock = deadline.Add(-time.Second)
			f.access.openDelay = time.Second + completion
			outcome, err := f.step()
			require.NoError(t, err)
			status := f.status()
			require.NotNil(t, status.Switchover.TargetAvailableAt)
			assert.Equal(t, !status.Switchover.TargetAvailableAt.Before(&metav1.Time{Time: deadline}), status.Switchover.TimedOutAt != nil)
			assert.Equal(t, string(mvutypes.SwitchingOver), outcome.Report.Phase)
			assert.NotContains(t, f.calls, "restore-blue")
			f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
				return bg.Switchover.EndpointPublicationEventRef != nil
			})
		})
	}
}

type cutoverSQLFake struct {
	f               *cutoverFixture
	drained, parity bool
	guardsAbsent    bool
	strictHA        bool
	retainedDrains  int
	targetDurable   bool
}

func (s *cutoverSQLFake) Drain(_ context.Context, cluster *cnpgv1.Cluster, _ cutoverIdentity, terminate bool) (bool, error) {
	name := "drain-" + cluster.Name
	if terminate {
		name += "-terminate"
	}
	s.f.calls = append(s.f.calls, name)
	if s.strictHA && !sourceReadyForFailover(cluster) {
		return false, nil
	}
	return s.drained, nil
}
func (s *cutoverSQLFake) DrainRetainedSource(_ context.Context, cluster *cnpgv1.Cluster, _ cutoverIdentity, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	s.retainedDrains++
	s.f.calls = append(s.f.calls, "drain-"+cluster.Name+"-terminate")
	return s.drained, nil
}

func TestPostcommitDegradedSourceCannotStarveGreenAvailability(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.CommitReceipt != nil })
	blue := new(cnpgv1.Cluster)
	require.NoError(t, f.runtime.client.Get(t.Context(), types.NamespacedName{Namespace: f.runtime.key.Namespace, Name: "blue"}, blue))
	blue.Spec.Instances = 3
	blue.Status.CurrentPrimary = "missing-old-primary"
	blue.Status.ReadyInstances = 1
	blue.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-2"}}
	require.NoError(t, f.runtime.client.Update(t.Context(), blue))
	f.sql.strictHA = true
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.TargetAvailableAt != nil
	})
	assert.Positive(t, f.sql.retainedDrains, "postcommit must use the retained-source drain mode")
	assert.Contains(t, f.calls, "open-green")
	assert.NotContains(t, f.calls, "restore-blue")
}
func (s *cutoverSQLFake) SourceLSN(context.Context, *cnpgv1.Cluster, cutoverIdentity) (string, error) {
	s.f.calls = append(s.f.calls, "source-lsn")
	return "0/100", nil
}
func (s *cutoverSQLFake) VerifyFinalParity(_ context.Context, _, _ *cnpgv1.Cluster, _ []declaredDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ cutoverIdentity) (bool, error) {
	s.f.calls = append(s.f.calls, "parity")
	if s.guardsAbsent && bg.Switchover.CandidateGuardsRemovalStartedAt == nil && bg.Switchover.CandidateGuardsRemovedAt == nil {
		return false, errors.New("missing guard removal authority")
	}
	return s.parity, nil
}
func (s *cutoverSQLFake) SynchronizeSequences(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, cutoverIdentity) error {
	s.f.calls = append(s.f.calls, "sequences")
	return nil
}
func (s *cutoverSQLFake) RemoveGuards(context.Context, *cnpgv1.Cluster, []declaredDatabase, cutoverIdentity) error {
	s.f.calls = append(s.f.calls, "remove-guards")
	s.guardsAbsent = true
	return nil
}

func (s *cutoverSQLFake) VerifyTargetDurability(context.Context, *cnpgv1.Cluster, cutoverIdentity) (bool, error) {
	s.f.calls = append(s.f.calls, "green-durable")
	return s.targetDurable, nil
}

type cutoverEndpointsFake struct {
	f                                                  *cutoverFixture
	committed, prepared, published, loseCommitResponse bool
	commits                                            int
}

func (e *cutoverEndpointsFake) ObserveCommit(context.Context, types.NamespacedName, string) (bool, error) {
	return e.committed, nil
}
func (e *cutoverEndpointsFake) PublishPreparationIntent(context.Context, types.NamespacedName, string) error {
	e.f.calls = append(e.f.calls, "prepare-endpoints")
	e.f.mutate(func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		bg.EndpointIntentRevision = 1
		bg.EndpointPreparation = &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 1, EndpointFingerprint: new("routing")}
	})
	return nil
}
func (e *cutoverEndpointsFake) PreparationComplete(context.Context, types.NamespacedName, string) (bool, error) {
	e.f.calls = append(e.f.calls, "preparation-barrier")
	return e.prepared, nil
}
func (e *cutoverEndpointsFake) CommitCanonical(context.Context, types.NamespacedName, string) error {
	e.f.calls = append(e.f.calls, "commit")
	if !e.committed {
		e.commits++
		e.committed = true
	}
	if e.loseCommitResponse {
		e.loseCommitResponse = false
		return errors.New("canonical response lost")
	}
	e.f.mutate(func(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		now := metav1.NewTime(e.f.clock)
		bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, CanonicalConfigMap: &corev1.ObjectReference{Name: "access", Namespace: parent.Namespace, UID: "canonical"}, Target: bg.Green.Ref.DeepCopy()}
		parent.Status.ProvisionerRef = bg.Green.Ref.DeepCopy()
	})
	return nil
}
func (e *cutoverEndpointsFake) RecoverCanonical(context.Context, types.NamespacedName, string) error {
	e.f.calls = append(e.f.calls, "recover-canonical")
	return nil
}
func (e *cutoverEndpointsFake) RestoreUncommittedCanonical(context.Context, types.NamespacedName, string) error {
	e.f.calls = append(e.f.calls, "restore-canonical-blue")
	return nil
}
func (e *cutoverEndpointsFake) CancelPreparation(context.Context, types.NamespacedName, string, int64) error {
	e.f.calls = append(e.f.calls, "cancel-preparation")
	e.f.mutate(func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		bg.EndpointPreparation = nil
	})
	return nil
}
func (e *cutoverEndpointsFake) PublishPublicationIntent(context.Context, types.NamespacedName, string) error {
	e.f.calls = append(e.f.calls, "publish-endpoints")
	e.f.mutate(func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		bg.EndpointPublication = &platformv1alpha1.BlueGreenEndpointPublicationStatus{Generation: 1, EndpointFingerprint: new("routing")}
	})
	return nil
}
func (e *cutoverEndpointsFake) ReconcilePublicationStatus(context.Context, types.NamespacedName, string) (bool, error) {
	e.f.calls = append(e.f.calls, "publication-barrier")
	return e.published, nil
}

func TestCutoverBootstrapsControlBeforeLatchingInterruption(t *testing.T) {
	f := newCutoverFixture(t)
	_, err := f.step()
	require.NoError(t, err)
	assert.Nil(t, f.status().Switchover.StartedAt)
	assert.NotNil(t, f.status().Switchover.ControlIdentity)
	_, err = f.step()
	require.NoError(t, err)
	assert.NotNil(t, f.status().Switchover.ControlIdentity.PreparedAt)
	assert.Nil(t, f.status().Switchover.StartedAt)
	_, err = f.step()
	require.NoError(t, err)
	if failure := f.status().Switchover.Failure; failure != nil {
		t.Fatalf("unexpected bootstrap failure: %s", *failure)
	}
	require.NotNil(t, f.status().Switchover.LatchedTimeout)
	assert.Equal(t, time.Minute, f.status().Switchover.LatchedTimeout.Duration)
	assert.NotNil(t, f.status().Switchover.DeadlineAt)
	assert.NotContains(t, f.calls, "apply-fence")
	_, err = f.step()
	require.NoError(t, err)
	assert.Contains(t, f.calls, "apply-fence")
}

func TestCutoverOrdersBothDrainsParityBarriersAndGreenAccess(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	for _, step := range []string{"drain-blue-terminate", "drain-green-terminate", "parity", "sequences", "remove-guards", "preparation-barrier", "commit", "publication-barrier", "open-green"} {
		assert.Contains(t, f.calls, step)
	}
	guards, commit, open := slices.Index(f.calls, "remove-guards"), slices.Index(f.calls, "commit"), slices.Index(f.calls, "open-green")
	assert.Less(t, slices.Index(f.calls, "drain-green-terminate"), guards)
	assert.Less(t, slices.Index(f.calls, "drain-blue-terminate"), slices.Index(f.calls, "source-lsn"))
	assert.Less(t, slices.Index(f.calls, "sequences"), guards)
	assert.Less(t, guards, commit)
	assert.Less(t, commit, slices.Index(f.calls, "publish-endpoints"))
	assert.Less(t, slices.Index(f.calls, "publication-barrier"), open)
	assert.Equal(t, []string{"drain-blue-terminate", "drain-green-terminate", "green-durable"}, f.calls[commit-3:commit])
	assert.Equal(t, 1, f.endpoints.commits)
	assert.NotContains(t, f.calls, "restore-blue")
	assert.NotNil(t, f.status().Switchover.TargetAvailableAt)
}

func TestLaggingGreenReplicaBlocksCommitWithoutReplayingPostcommitMutations(t *testing.T) {
	f := newCutoverFixture(t)
	f.sql.targetDurable = false
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPreparation != nil })
	f.calls = nil
	for range 3 {
		_, err := f.step()
		require.NoError(t, err)
	}
	assert.Contains(t, f.calls, "green-durable")
	assert.NotContains(t, f.calls, "commit")
	assert.NotContains(t, f.calls, "open-green")
	f.sql.targetDurable = true
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.CommitReceipt != nil })
	f.calls = nil
	f.sql.targetDurable = false
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.TargetAvailableAt != nil
	})
	assert.NotContains(t, f.calls, "green-durable")
	assert.NotContains(t, f.calls, "sequences")
	assert.NotContains(t, f.calls, "remove-guards")
}

func TestPrecommitTimeoutClearsPreparationAndVerifiesBlueRestoration(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPreparation != nil })
	f.clock = f.status().Switchover.DeadlineAt.Add(time.Second)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.SourceFenceRestoredAt != nil
	})
	status := f.status()
	assert.NotNil(t, status.Switchover.TimedOutAt)
	assert.Nil(t, status.EndpointPreparation)
	assert.Nil(t, status.CommitReceipt)
	assert.Contains(t, f.calls, "cancel-preparation")
	assert.Contains(t, f.calls, "restore-blue")
	assert.NotContains(t, f.calls, "commit")
	assert.NotContains(t, f.calls, "open-green")
	assert.Less(t, slices.Index(f.calls, "cancel-preparation"), slices.Index(f.calls, "restore-blue"))
}

func TestLostCanonicalResponseAlwaysRecoversForward(t *testing.T) {
	f := newCutoverFixture(t)
	f.endpoints.loseCommitResponse = true
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	assert.Equal(t, 1, f.endpoints.commits)
	assert.NotContains(t, f.calls, "restore-blue")
	assert.NotContains(t, f.calls, "restore-canonical-blue")
	assert.NotNil(t, f.status().CommitReceipt)
}

func TestPublicationBarrierKeepsCommittedGreenFenced(t *testing.T) {
	f := newCutoverFixture(t)
	f.endpoints.published = false
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPublication != nil })
	f.calls = nil
	f.clock = f.status().Switchover.DeadlineAt.Add(time.Second)
	for range 4 {
		_, err := f.step()
		require.NoError(t, err)
	}
	assert.Contains(t, f.calls, "publication-barrier")
	assert.NotContains(t, f.calls, "open-green")
	assert.NotContains(t, f.calls, "restore-blue")
	assert.NotContains(t, f.calls, "sequences")
	assert.NotNil(t, f.status().Switchover.TimedOutAt)
	assert.Nil(t, f.status().Switchover.TargetAvailableAt)
	f.endpoints.published = true
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	assert.NotContains(t, f.calls, "sequences", "postcommit recovery must not resynchronize mutable sequence values")
}

func TestPublicationEventIsAdoptedAfterStatusWriteLoss(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.TargetAvailableAt != nil
	})
	parent := new(platformv1alpha1.PostgresCluster)
	require.NoError(t, f.runtime.reader.Get(t.Context(), f.runtime.key, parent))
	intent := mvutypes.Intent{AttemptID: "attempt"}
	first, err := f.runtime.AdvanceSwitchover(t.Context(), intent, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen)
	require.NoError(t, err)
	require.NotNil(t, first.BlueGreen.Switchover.EndpointPublicationEventRef)
	// Simulate losing the status write after the Event already exists.
	second, err := f.runtime.AdvanceSwitchover(t.Context(), intent, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen)
	require.NoError(t, err)
	assert.Equal(t, first.BlueGreen.Switchover.EndpointPublicationEventRef, second.BlueGreen.Switchover.EndpointPublicationEventRef)
	events := new(corev1.EventList)
	require.NoError(t, f.runtime.reader.List(t.Context(), events, client.InNamespace(f.runtime.key.Namespace)))
	require.Len(t, events.Items, 1)
	assert.Equal(t, int32(1), events.Items[0].Count)
}

func TestCutoverRejectsSourceGreenAndCandidateCredentialDriftBeforeFencing(t *testing.T) {
	for _, change := range []string{"blue", "green", "green-superuser"} {
		t.Run(change, func(t *testing.T) {
			f := newCutoverFixture(t)
			f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
				return bg.Switchover.ControlIdentity != nil && bg.Switchover.ControlIdentity.PreparedAt != nil
			})
			if change == "green-superuser" {
				secret := new(corev1.Secret)
				require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: change}, secret))
				secret.Data["password"] = []byte("rotated-during-attempt")
				require.NoError(t, f.runtime.client.Update(t.Context(), secret))
			} else {
				cluster := new(cnpgv1.Cluster)
				require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: change}, cluster))
				cluster.Spec.ImageName = "unapproved-image"
				require.NoError(t, f.runtime.client.Update(t.Context(), cluster))
			}
			_, err := f.step()
			require.NoError(t, err)
			require.NotNil(t, f.status().Switchover.Failure)
			assert.NotContains(t, f.calls, "apply-fence")
			assert.NotContains(t, f.calls, "commit")
			assert.NotContains(t, f.calls, "open-green")
		})
	}
}

func TestPreparationBarrierPreventsCanonicalCommit(t *testing.T) {
	f := newCutoverFixture(t)
	f.endpoints.prepared = false
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.EndpointPreparation != nil })
	for range 3 {
		_, err := f.step()
		require.NoError(t, err)
	}
	assert.NotContains(t, f.calls, "commit")
	assert.NotContains(t, f.calls, "open-green")
	f.endpoints.prepared = true
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
}

func TestAvailableGreenEventFinalizationDoesNotRepeatFrozenDatabaseBarrier(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.TargetAvailableAt != nil
	})
	// Database declarations may change after Target Available releases the
	// schema freeze, even if the Event's status receipt has not yet persisted.
	f.endpoints.published = false
	f.calls = nil
	outcome, err := f.step()
	require.NoError(t, err)
	require.NotNil(t, outcome.BlueGreen.Switchover.EndpointPublicationEventRef)
	assert.Equal(t, string(mvutypes.PostUpgradeBackup), outcome.Report.Phase)
	assert.NotContains(t, f.calls, "publication-barrier")
	assert.NotContains(t, f.calls, "drain-blue-terminate")
}

func TestEmptyHBASnapshotSurvivesAPISerialization(t *testing.T) {
	access, blue, green, databases, status, _ := accessFixture(t)
	blue.Spec.PostgresConfiguration.PgHBA = nil
	green.Spec.PostgresConfiguration.PgHBA = nil
	require.NoError(t, access.client.Update(t.Context(), blue))
	require.NoError(t, access.client.Update(t.Context(), green))
	require.NoError(t, access.Snapshot(t.Context(), blue, green, databases, status))
	encoded, err := runtime.DefaultUnstructuredConverter.ToUnstructured(status)
	require.NoError(t, err)
	decoded := new(platformv1alpha1.PostgresBlueGreenUpgradeStatus)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(encoded, decoded))
	require.NotNil(t, decoded.Switchover.OriginalHBA)
	require.NotNil(t, decoded.Switchover.GreenOriginalHBA)
	assert.Empty(t, *decoded.Switchover.OriginalHBA)
	assert.Empty(t, *decoded.Switchover.GreenOriginalHBA)
}

type cutoverFailOnceReader struct {
	client.Reader
	name   string
	failed bool
}

func (r *cutoverFailOnceReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if key.Name == r.name && !r.failed {
		r.failed = true
		return errors.New("temporary credential read failure")
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func (f *cutoverFixture) retryRestoredWindow() {
	f.t.Helper()
	parent := new(platformv1alpha1.PostgresCluster)
	require.NoError(f.t, f.runtime.reader.Get(f.t.Context(), f.runtime.key, parent))
	f.clock = f.clock.Add(time.Second)
	retryAt := metav1.NewTime(f.clock)
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt", Generation: parent.Generation, RetryRequestedAt: &retryAt, State: parent.Status.PostgresMajorUpgradeStatus}
	outcome, err := bluegreen.New(mvutypes.Scheduled, nil, nil).Advance(f.t.Context(), intent)
	require.NoError(f.t, err)
	store := NewMajorUpgradeStateStore(newTestClusterStateStore(f.t, f.runtime.client, f.runtime.key))
	require.NoError(f.t, progressError(store.SaveMajorUpgradeProgress(f.t.Context(), intent, mvutypes.Progress{BlueGreen: outcome.BlueGreen, Report: outcome.Report})))
}

func TestPartialControlSnapshotCanCompleteOnExplicitRetry(t *testing.T) {
	f := newCutoverFixture(t)
	f.runtime.reader = &cutoverFailOnceReader{Reader: f.runtime.reader, name: "green-superuser"}
	_, err := f.step()
	require.NoError(t, err)
	require.NotNil(t, f.status().Switchover.ControlIdentity)
	require.Empty(t, f.status().Switchover.GreenCredentialFingerprints)
	require.NotNil(t, f.status().Switchover.Failure)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.SourceFenceRestoredAt != nil
	})
	f.retryRestoredWindow()
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	assert.NotEmpty(t, f.status().Switchover.GreenCredentialFingerprints)
	assert.Equal(t, int64(2), f.status().Switchover.Sequence)
	assert.Equal(t, 1, f.endpoints.commits)
}

func TestGuardRemovalIntentSurvivesLostStatusAndTimeoutRetry(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.CandidateGuardsRemovalStartedAt != nil
	})
	_, status, err := f.runtime.readAttempt(t.Context(), "attempt")
	require.NoError(t, err)
	_, err = f.runtime.AdvanceSwitchover(t.Context(), mvutypes.Intent{AttemptID: "attempt"}, status)
	require.NoError(t, err)
	// SQL removed the guards, but the returned status update is lost.
	assert.True(t, f.sql.guardsAbsent)
	require.Nil(t, f.status().Switchover.CandidateGuardsRemovedAt)
	intentTime := f.status().Switchover.CandidateGuardsRemovalStartedAt.DeepCopy()
	f.clock = f.status().Switchover.DeadlineAt.Add(time.Second)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.SourceFenceRestoredAt != nil
	})
	f.retryRestoredWindow()
	assert.Equal(t, intentTime, f.status().Switchover.CandidateGuardsRemovalStartedAt)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	assert.Equal(t, int64(2), f.status().Switchover.Sequence)
	assert.Equal(t, 1, f.endpoints.commits)
	removals := 0
	for _, call := range f.calls {
		if call == "remove-guards" {
			removals++
		}
	}
	assert.Equal(t, 2, removals)
}

func TestCutoverDurableStepsUseExplicitRequeueDelay(t *testing.T) {
	f := newCutoverFixture(t)
	for range 40 {
		outcome, err := f.step()
		require.NoError(t, err)
		require.True(t, outcome.Report.Retry)
		require.NotNil(t, outcome.Report.Sleep, "successful cutover steps must not accumulate controller failure backoff")
		assert.Equal(t, 1, *outcome.Report.Sleep)
		if f.status().Switchover.EndpointPublicationEventRef != nil {
			return
		}
	}
	t.Fatal("cutover did not reach its publication receipt")
}

func TestBlueGreenSwitchoverValidatesConstruction(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid"}}
	key := client.ObjectKeyFromObject(parent)
	for _, test := range []struct {
		name   string
		writer client.Client
		reader client.Reader
		key    types.NamespacedName
		parent *platformv1alpha1.PostgresCluster
	}{
		{"nil writer", nil, c, key, parent},
		{"missing UID", c, c, key, &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default"}},
		{"nil reader", c, nil, key, parent},
		{"nil parent", c, c, key, nil},
		{"different name", c, c, types.NamespacedName{Namespace: key.Namespace, Name: "other"}, parent},
		{"different namespace", c, c, types.NamespacedName{Namespace: "other", Name: key.Name}, parent},
		{"empty identity", c, c, types.NamespacedName{}, new(platformv1alpha1.PostgresCluster)},
	} {
		t.Run(test.name, func(t *testing.T) {
			switchover, err := NewBlueGreenSwitchover(test.writer, test.reader, test.key, test.parent, BlueGreenCandidatePlan{})
			require.Error(t, err)
			require.Nil(t, switchover)
		})
	}
	switchover, err := NewBlueGreenSwitchover(c, c, key, parent, BlueGreenCandidatePlan{})
	require.NoError(t, err)
	require.NotNil(t, switchover.endpoints)
}
