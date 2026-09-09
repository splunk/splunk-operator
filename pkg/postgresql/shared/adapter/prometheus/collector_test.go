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

package prometheus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

type fleetRecorder struct {
	mu sync.Mutex

	clusterPhases        map[string]float64
	databasePhases       map[string]float64
	managedUsers         map[string]float64
	poolerEnabled        float64
	clusterPhaseUpdates  int
	databasePhaseUpdates int
	signals              chan string
}

func newFleetRecorder() *fleetRecorder {
	return &fleetRecorder{signals: make(chan string, 20)}
}

func (*fleetRecorder) IncStatusTransition(string, string, string, string) {}
func (*fleetRecorder) ObserveProvisioningDuration(string, float64)        {}

func (r *fleetRecorder) SetClusterPhases(phases map[string]float64) {
	r.mu.Lock()
	r.clusterPhases = cloneMetricValues(phases)
	r.clusterPhaseUpdates++
	r.mu.Unlock()
	r.signals <- "cluster"
}

func (r *fleetRecorder) SetPoolerEnabledClusters(count float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.poolerEnabled = count
}

func (r *fleetRecorder) SetDatabasePhases(phases map[string]float64) {
	r.mu.Lock()
	r.databasePhases = cloneMetricValues(phases)
	r.databasePhaseUpdates++
	r.mu.Unlock()
	r.signals <- "database"
}

func (r *fleetRecorder) SetManagedUsers(_ string, states map[string]float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.managedUsers = cloneMetricValues(states)
}

func (r *fleetRecorder) snapshot() (map[string]float64, map[string]float64, map[string]float64, float64, int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneMetricValues(r.clusterPhases), cloneMetricValues(r.databasePhases), cloneMetricValues(r.managedUsers),
		r.poolerEnabled, r.clusterPhaseUpdates, r.databasePhaseUpdates
}

func cloneMetricValues(values map[string]float64) map[string]float64 {
	cloned := make(map[string]float64, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func fleetMetricsScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	return scheme
}

func TestFleetCollectorCalculatesExistingGauges(t *testing.T) {
	ready := "Ready"
	objects := []client.Object{
		&platformv1alpha1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "ready"},
			Status: platformv1alpha1.PostgresClusterStatus{
				Phase:                  &ready,
				ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true},
				ManagedRolesStatus: &platformv1alpha1.ManagedRolesStatus{
					RoleOwners: map[string]platformv1alpha1.RoleOwnerReference{
						"app_admin": {Name: "app", UID: "app-uid"},
						"app_rw":    {Name: "app", UID: "app-uid"},
					},
					Reconciled: []string{"app_admin"},
					Pending:    []string{"app_rw", "reporting_rw"},
					Failed:     map[string]string{"failed_role": "failed"},
					Conflicts: []platformv1alpha1.RoleConflict{
						{Role: "contested"},
						{Role: "contested"},
						{Role: "other"},
					},
				},
			},
		},
		&platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "unknown"}},
		&platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "ready-db"}, Status: platformv1alpha1.PostgresDatabaseStatus{Phase: &ready}},
		&platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "unknown-db"}},
	}
	recorder := newFleetRecorder()
	collector := NewFleetCollector(fake.NewClientBuilder().WithScheme(fleetMetricsScheme(t)).WithObjects(objects...).Build(), recorder)

	require.NoError(t, collector.collectClusterMetrics(t.Context()))
	require.NoError(t, collector.collectDatabaseMetrics(t.Context()))

	clusterPhases, databasePhases, managedUsers, poolerEnabled, _, _ := recorder.snapshot()
	assert.Equal(t, map[string]float64{"Ready": 1, "Unknown": 1}, clusterPhases)
	assert.Equal(t, map[string]float64{"Ready": 1, "Unknown": 1}, databasePhases)
	assert.Equal(t, float64(1), poolerEnabled)
	assert.Equal(t, map[string]float64{
		"desired": 4, "reconciled": 1, "pending": 2, "failed": 1, "conflicts": 3,
	}, managedUsers)
}

func TestFleetCollectorPreservesSnapshotsWhenListsFail(t *testing.T) {
	wantErr := errors.New("list failed")
	ready := "Ready"
	var failClusters atomic.Bool
	var failDatabases atomic.Bool
	base := fake.NewClientBuilder().
		WithScheme(fleetMetricsScheme(t)).
		WithObjects(
			&platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "ready"},
				Status: platformv1alpha1.PostgresClusterStatus{
					Phase:                  &ready,
					ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true},
				},
			},
			&platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "ready"},
				Status:     platformv1alpha1.PostgresDatabaseStatus{Phase: &ready},
			},
		).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				switch list.(type) {
				case *platformv1alpha1.PostgresClusterList:
					if failClusters.Load() {
						return wantErr
					}
				case *platformv1alpha1.PostgresDatabaseList:
					if failDatabases.Load() {
						return wantErr
					}
				}
				return c.List(ctx, list, opts...)
			},
		}).
		Build()
	recorder := newFleetRecorder()
	collector := NewFleetCollector(base, recorder)
	require.NoError(t, collector.collectClusterMetrics(t.Context()))
	require.NoError(t, collector.collectDatabaseMetrics(t.Context()))

	beforeCluster, beforeDatabase, beforeManaged, beforePooler, clusterUpdates, databaseUpdates := recorder.snapshot()
	failClusters.Store(true)
	failDatabases.Store(true)
	assert.ErrorIs(t, collector.collectClusterMetrics(t.Context()), wantErr)
	assert.ErrorIs(t, collector.collectDatabaseMetrics(t.Context()), wantErr)

	afterCluster, afterDatabase, afterManaged, afterPooler, afterClusterUpdates, afterDatabaseUpdates := recorder.snapshot()
	assert.Equal(t, beforeCluster, afterCluster)
	assert.Equal(t, beforeDatabase, afterDatabase)
	assert.Equal(t, beforeManaged, afterManaged)
	assert.Equal(t, beforePooler, afterPooler)
	assert.Equal(t, clusterUpdates, afterClusterUpdates)
	assert.Equal(t, databaseUpdates, afterDatabaseUpdates)
}

func TestFleetCollectorDatabaseFailureDoesNotSuppressClusterCollection(t *testing.T) {
	var failDatabase atomic.Bool
	failDatabase.Store(true)
	base := fake.NewClientBuilder().
		WithScheme(fleetMetricsScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*platformv1alpha1.PostgresDatabaseList); ok && failDatabase.Load() {
					return errors.New("database list failed")
				}
				return c.List(ctx, list, opts...)
			},
		}).
		Build()
	recorder := newFleetRecorder()
	collector := NewFleetCollector(base, recorder)

	collector.collect(t.Context())
	_, _, _, _, clusterUpdates, databaseUpdates := recorder.snapshot()
	assert.Equal(t, 1, clusterUpdates)
	assert.Zero(t, databaseUpdates)

	failDatabase.Store(false)
	collector.collect(t.Context())
	_, _, _, _, clusterUpdates, databaseUpdates = recorder.snapshot()
	assert.Equal(t, 2, clusterUpdates)
	assert.Equal(t, 1, databaseUpdates)
}

func TestFleetCollectorRunnableLifecycle(t *testing.T) {
	fakeClock := clocktesting.NewFakeClock(time.Now())
	recorder := newFleetRecorder()
	collector := newFleetCollector(
		fake.NewClientBuilder().WithScheme(fleetMetricsScheme(t)).Build(),
		recorder,
		fakeClock,
		fleetCollectionInterval,
	)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- collector.Start(ctx) }()

	requireSignal(t, recorder.signals, "cluster")
	requireSignal(t, recorder.signals, "database")
	require.Eventually(t, fakeClock.HasWaiters, time.Second, time.Millisecond)
	fakeClock.Step(fleetCollectionInterval)
	requireSignal(t, recorder.signals, "cluster")
	requireSignal(t, recorder.signals, "database")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("collector did not stop after context cancellation")
	}
	assert.True(t, collector.NeedLeaderElection())
}

func TestFleetCollectorRetriesIndependentFailuresWithoutOverlapping(t *testing.T) {
	wantErr := errors.New("cluster list failed")
	fakeClock := clocktesting.NewFakeClock(time.Now())
	var clusterLists atomic.Int32
	secondClusterStarted := make(chan struct{})
	releaseSecondCluster := make(chan struct{})
	base := fake.NewClientBuilder().
		WithScheme(fleetMetricsScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*platformv1alpha1.PostgresClusterList); ok {
					call := clusterLists.Add(1)
					if call == 1 {
						return wantErr
					}
					if call == 2 {
						close(secondClusterStarted)
						select {
						case <-releaseSecondCluster:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
				return c.List(ctx, list, opts...)
			},
		}).
		Build()
	recorder := newFleetRecorder()
	collector := newFleetCollector(base, recorder, fakeClock, fleetCollectionInterval)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSecondCluster) }) }
	t.Cleanup(release)
	done := make(chan error, 1)
	go func() { done <- collector.Start(ctx) }()

	// The cluster failure does not suppress database collection in the same cycle.
	requireSignal(t, recorder.signals, "database")
	require.Eventually(t, fakeClock.HasWaiters, time.Second, time.Millisecond)
	fakeClock.Step(fleetCollectionInterval)
	select {
	case <-secondClusterStarted:
	case <-time.After(time.Second):
		t.Fatal("collector did not retry cluster collection")
	}

	// Another tick cannot start a third collection while the second one is blocked.
	fakeClock.Step(fleetCollectionInterval)
	assert.Equal(t, int32(2), clusterLists.Load())
	release()
	requireSignal(t, recorder.signals, "cluster")
	requireSignal(t, recorder.signals, "database")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("collector did not stop after context cancellation")
	}
}

func requireSignal(t *testing.T, signals <-chan string, want string) {
	t.Helper()
	select {
	case got := <-signals:
		require.Equal(t, want, got)
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s collection", want)
	}
}

var _ ports.Recorder = (*fleetRecorder)(nil)
