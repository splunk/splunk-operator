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
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	monadapter "github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/custom_metrics"
	cnpgmonitoring "github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/custom_metrics/cnpg"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const validQueryYAML = `
pg_active:
  type: gauge
  help: Active sessions
  query: SELECT count(*) AS value FROM pg_stat_activity
  value: value
`

func queryCount(cfg mtypes.AggregatedConfig) int {
	count := len(cfg.ClusterQueries)
	for _, queries := range cfg.DatabaseQueries {
		count += len(queries)
	}
	return count
}

type stubDataRepository struct {
	configMaps map[string]string // "namespace/name/key" → content
	fetchErr   error
	listErr    error
	snapshot   mtypes.DatabaseContributionSnapshot
}

func (s *stubDataRepository) set(namespace, name, key, content string) {
	if s.configMaps == nil {
		s.configMaps = map[string]string{}
	}
	s.configMaps[namespace+"/"+name+"/"+key] = content
}

func (s *stubDataRepository) ListDatabaseContributions(_ context.Context, _, _ string) (mtypes.DatabaseContributionSnapshot, error) {
	return s.snapshot, s.listErr
}

func (s *stubDataRepository) FetchConfigMap(_ context.Context, namespace, name, key string) ([]byte, error) {
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	v, ok := s.configMaps[namespace+"/"+name+"/"+key]
	if !ok {
		return nil, mtypes.ErrSourceNotFound
	}
	return []byte(v), nil
}

type recordingProvisioner struct {
	applied     []mtypes.AggregatedConfig
	saveChanged bool
	saveErr     error
	err         error
	observeErr  error
	observation mtypes.Observation
	rollback    mtypes.RollbackResult
	rollbackErr error
	rollbacks   int
	saved       []mtypes.ConfirmedState
}

func (c *recordingProvisioner) Apply(_ context.Context, cfg mtypes.AggregatedConfig) (mtypes.ExpectedState, error) {
	c.applied = append(c.applied, cfg)
	return mtypes.ExpectedState{
		Revision:   "revision",
		Enabled:    queryCount(cfg) > 0,
		QueryCount: queryCount(cfg),
	}, c.err
}

func (c *recordingProvisioner) Observe(_ context.Context, expected mtypes.ExpectedState) (mtypes.Observation, error) {
	if c.observeErr != nil {
		return mtypes.Observation{}, c.observeErr
	}
	if c.observation.State == mtypes.ObservationPending && c.observation.Message == "" {
		return mtypes.Observation{
			State: mtypes.ObservationReady,
			Confirmed: &mtypes.ConfirmedState{
				Revision:   expected.Revision,
				Enabled:    expected.Enabled,
				QueryCount: expected.QueryCount,
			},
		}, nil
	}
	if c.observation.State == mtypes.ObservationReady && c.observation.Confirmed == nil {
		c.observation.Confirmed = &mtypes.ConfirmedState{
			Revision:   expected.Revision,
			Enabled:    expected.Enabled,
			QueryCount: expected.QueryCount,
		}
	}
	return c.observation, nil
}

func (c *recordingProvisioner) Save(_ context.Context, confirmed mtypes.ConfirmedState) (mtypes.SaveResult, error) {
	c.saved = append(c.saved, confirmed)
	return mtypes.SaveResult{Changed: c.saveChanged}, c.saveErr
}

func (c *recordingProvisioner) Rollback(context.Context) (mtypes.RollbackResult, error) {
	c.rollbacks++
	return c.rollback, c.rollbackErr
}

func selector(name string) corev1.ConfigMapKeySelector {
	return corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  "queries.yaml",
	}
}

func clusterWithSources(names ...string) *platformv1alpha1.PostgresCluster {
	refs := make([]corev1.ConfigMapKeySelector, 0, len(names))
	for _, name := range names {
		refs = append(refs, selector(name))
	}
	return &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns"},
		Spec: platformv1alpha1.PostgresClusterSpec{
			Monitoring: &platformv1alpha1.PostgresClusterMonitoring{CustomQueriesConfigMap: refs},
		},
	}
}

func newTestModel(repo DataRepository, provisioner *recordingProvisioner) *Model {
	return NewModel(repo, monadapter.NewParser(), monadapter.NewCollider(), monadapter.NewAggregator(), provisioner)
}

func newCNPGTestModel(repo DataRepository, provisioner *recordingProvisioner) *Model {
	return NewModel(
		repo,
		monadapter.NewParser(),
		monadapter.NewCollider(cnpgmonitoring.RenderIdentity),
		monadapter.NewAggregator(),
		provisioner,
	)
}

func TestReconcile_InvalidSourcePreservesAppliedConfiguration(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "invalid", "queries.yaml", validQueryYAML)
	cfg := &recordingProvisioner{}
	model := newTestModel(repo, cfg)
	cluster := clusterWithSources("invalid")

	out, err := model.Reconcile(t.Context(), cluster, nil)
	require.NoError(t, err)
	assert.Equal(t, InvalidNone, out.Invalid)
	require.Len(t, cfg.applied, 1)

	repo.set("ns", "invalid", "queries.yaml", "pg_bad: [")
	out, err = model.Reconcile(t.Context(), cluster, nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidQuery, out.Invalid)
	assert.Len(t, cfg.applied, 1, "invalid desired state must leave the last complete generated config untouched")
}

func TestReconcile_GeneratedResourceOwnershipConflictIsDegradedWithoutRetry(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", validQueryYAML)
	provisioner := &recordingProvisioner{
		err: fmt.Errorf("%w: ConfigMap ns/pg-metrics is foreign", mtypes.ErrGeneratedResourceOwnershipConflict),
	}
	model := newTestModel(repo, provisioner)

	out, err := model.Reconcile(t.Context(), clusterWithSources("source"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidOwnershipConflict, out.Invalid)
	assert.Contains(t, out.InvalidDetail, "ConfigMap ns/pg-metrics is foreign")
	assert.Zero(t, provisioner.rollbacks, "a foreign active name also prevents safety rollback")
	require.Len(t, out.Events, 1)
	assert.Equal(t, EventOwnershipConflict, out.Events[0].Kind)
}

func TestReconcile_OwnershipConflictTakesPriorityOverCollision(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "alpha", "queries.yaml", validQueryYAML)
	repo.set("ns", "beta", "queries.yaml", validQueryYAML)
	provisioner := &recordingProvisioner{
		err: fmt.Errorf("%w: ConfigMap ns/pg-metrics is foreign", mtypes.ErrGeneratedResourceOwnershipConflict),
	}

	out, err := newTestModel(repo, provisioner).
		Reconcile(t.Context(), clusterWithSources("alpha", "beta"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidOwnershipConflict, out.Invalid)
	assert.Equal(t, "ConfigMap ns/pg-metrics is foreign", out.InvalidDetail)
	require.Len(t, out.Events, 2)
	assert.Equal(t, EventCollision, out.Events[0].Kind)
	assert.Equal(t, EventOwnershipConflict, out.Events[1].Kind)
}

func TestReconcile_CNPGRenderedFamilyCollisionRejectsAtomicPackage(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", `
foo_bar:
  type: gauge
  help: first
  query: SELECT 1 AS baz
  value: baz
foo:
  type: gauge
  help: second
  query: SELECT 1 AS bar_baz
  value: bar_baz
`)
	provisioner := &recordingProvisioner{}
	model := newCNPGTestModel(repo, provisioner)

	out, err := model.Reconcile(t.Context(), clusterWithSources("source"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidCollision, out.Invalid)
	assert.Contains(t, out.InvalidDetail, "cnpg_splunk_operator_cluster_foo_bar_baz")
	require.Len(t, provisioner.applied, 1)
	assert.Empty(t, provisioner.applied[0].ClusterQueries)
}

func TestReconcile_SuccessfulApplyThenInvalidSourceRepairsDeletedOrDriftedOutputFromSafety(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", validQueryYAML)
	cfg := &recordingProvisioner{saveChanged: true}
	model := newTestModel(repo, cfg)
	cluster := clusterWithSources("source")

	out, err := model.Reconcile(t.Context(), cluster, nil)
	require.NoError(t, err)
	assert.Equal(t, InvalidNone, out.Invalid)
	require.Len(t, cfg.saved, 1, "successful observation must save the confirmed revision")

	cfg.rollback = mtypes.RollbackResult{
		Available: true,
		Expected: mtypes.ExpectedState{
			Revision:   cfg.saved[0].Revision,
			Enabled:    cfg.saved[0].Enabled,
			QueryCount: cfg.saved[0].QueryCount,
		},
		Changed: true,
	}
	repo.set("ns", "source", "queries.yaml", "pg_bad: [")

	out, err = model.Reconcile(t.Context(), cluster, nil)
	require.NoError(t, err)
	assert.Equal(t, InvalidQuery, out.Invalid)
	assert.Equal(t, 1, cfg.rollbacks)
	assert.Len(t, cfg.applied, 1, "repair must not depend on applying invalid current sources")
	require.Len(t, out.Events, 2)
	assert.Equal(t, EventInvalidQuery, out.Events[0].Kind)
	assert.Equal(t, EventQueryRepaired, out.Events[1].Kind)
}

func TestReconcile_EmitsRepairEventWhenRestorationAwaitsProviderConsumption(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", "pg_bad: [")
	cfg := &recordingProvisioner{
		observation: mtypes.Observation{
			State:   mtypes.ObservationPending,
			Message: "waiting for CNPG to consume restored ConfigMap",
		},
		rollback: mtypes.RollbackResult{
			Available: true,
			Expected: mtypes.ExpectedState{
				Revision:   "confirmed",
				Enabled:    true,
				QueryCount: 1,
			},
			Changed: true,
		},
	}

	out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources("source"), nil)

	require.NoError(t, err)
	assert.True(t, out.Configuring)
	assert.True(t, out.Requeue)
	require.Len(t, out.Events, 2)
	assert.Equal(t, EventInvalidQuery, out.Events[0].Kind)
	assert.Equal(t, EventQueryRepaired, out.Events[1].Kind)
}

func TestReconcile_MissingSourcePreservesAppliedConfiguration(t *testing.T) {
	cfg := &recordingProvisioner{}
	out, err := newTestModel(&stubDataRepository{}, cfg).
		Reconcile(t.Context(), clusterWithSources("missing"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidConfigMapNotFound, out.Invalid)
	assert.Empty(t, cfg.applied)
}

func TestReconcile_ReportsEveryMissingSourceInConditionDetail(t *testing.T) {
	cfg := &recordingProvisioner{}
	out, err := newTestModel(&stubDataRepository{}, cfg).
		Reconcile(t.Context(), clusterWithSources("missing-a", "missing-b"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidConfigMapNotFound, out.Invalid)
	assert.Equal(t,
		"ns/missing-a/queries.yaml; ns/missing-b/queries.yaml",
		out.InvalidDetail,
	)
	assert.Len(t, out.Events, 2)
	assert.Empty(t, cfg.applied)
}

func TestRunMarkInvalid_DeduplicatesAReusedBrokenPackage(t *testing.T) {
	r := &run{}
	r.markInvalid(InvalidQuery, "ns/shared/queries.yaml: broken")
	r.markInvalid(InvalidQuery, "ns/shared/queries.yaml: broken")

	assert.Equal(t, InvalidQuery, r.invalid)
	assert.Equal(t, "ns/shared/queries.yaml: broken", r.detail())
}

func TestRunMarkContributorInvalid_DeduplicatesARepeatedSelector(t *testing.T) {
	identity := mtypes.ContributorIdentity{
		PostgresDatabaseName: "databases",
		PostgresDatabaseUID:  "uid",
		DatabaseName:         "orders",
	}
	r := &run{invalidContributors: map[string]acknowledgementFailure{}}
	r.markContributorInvalid(&identity, "InvalidQueryDefinition", "ns/shared/queries.yaml: broken")
	r.markContributorInvalid(&identity, "InvalidQueryDefinition", "ns/shared/queries.yaml: broken")

	failure := r.invalidContributors[acknowledgementKey(identity)]
	assert.Equal(t, "ns/shared/queries.yaml: broken", failure.message())
}

func TestSortDatabaseQuerySets_UsesContributorIdentityAsFinalTieBreaker(t *testing.T) {
	timestamp := metav1.Now().Time
	makeSet := func(owner, uid string) mtypes.DatabaseQuerySet {
		identity := mtypes.ContributorIdentity{
			PostgresDatabaseName: owner,
			PostgresDatabaseUID:  uid,
			DatabaseName:         "orders",
		}
		return mtypes.DatabaseQuerySet{
			DatabaseName:      "orders",
			CreationTimestamp: timestamp,
			Contributor:       &identity,
			Queries: []mtypes.ResolvedQuery{{
				PlatformQuery: mtypes.PlatformQuery{Name: "metric", Value: "value"},
				Source: mtypes.QuerySource{
					ConfigMapName: "shared",
					ConfigMapKey:  "queries.yaml",
					Contributor:   &identity,
				},
			}},
		}
	}
	sets := []mtypes.DatabaseQuerySet{makeSet("z-owner", "uid-z"), makeSet("a-owner", "uid-a")}

	sortDatabaseQuerySets(sets)

	require.NotNil(t, sets[0].Contributor)
	assert.Equal(t, "a-owner", sets[0].Contributor.PostgresDatabaseName)
}

func TestJoinDiagnostics_StaysWithinConditionMessageBudget(t *testing.T) {
	detail := strings.Repeat("€", maxDiagnosticDetailBytes)
	for _, details := range [][]string{
		{detail, "not reached"},
		{strings.Repeat("a", maxDiagnosticDetailBytes-1), "overflow"},
	} {
		got := joinDiagnostics(details)
		assert.LessOrEqual(t, len(got), maxDiagnosticDetailBytes)
		assert.True(t, utf8.ValidString(got))
		assert.True(t, strings.HasSuffix(got, diagnosticsOmitted))
	}
}

func TestReconcile_TransientSourceReadIsRetried(t *testing.T) {
	apiErr := &transientError{"temporary API failure"}
	cfg := &recordingProvisioner{}
	_, err := newTestModel(&stubDataRepository{fetchErr: apiErr}, cfg).
		Reconcile(t.Context(), clusterWithSources("source"), nil)

	assert.ErrorIs(t, err, apiErr)
	assert.Empty(t, cfg.applied)
}

type transientError struct{ msg string }

func (e *transientError) Error() string { return e.msg }
func (e *transientError) Is(target error) bool {
	t, ok := target.(*transientError)
	return ok && t.msg == e.msg
}

func TestReconcile_RecoveryAppliesCompleteConfiguration(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", "pg_bad: [")
	cfg := &recordingProvisioner{}
	model := newTestModel(repo, cfg)
	cluster := clusterWithSources("source")

	out, err := model.Reconcile(t.Context(), cluster, nil)
	require.NoError(t, err)
	assert.Equal(t, InvalidQuery, out.Invalid)
	assert.Empty(t, cfg.applied)

	repo.set("ns", "source", "queries.yaml", validQueryYAML)
	out, err = model.Reconcile(t.Context(), cluster, nil)
	require.NoError(t, err)
	assert.Equal(t, InvalidNone, out.Invalid)
	require.Len(t, cfg.applied, 1)
	require.Len(t, cfg.applied[0].ClusterQueries, 1)
	assert.Equal(t, "pg_active", cfg.applied[0].ClusterQueries[0].Name)
}

func TestReconcile_EmitsQueryAppliedOnlyWhenConfirmedSafetyRevisionChanges(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", validQueryYAML)

	for _, test := range []struct {
		name       string
		changed    bool
		eventCount int
	}{
		{name: "changed", changed: true, eventCount: 1},
		{name: "idempotent no-op", changed: false, eventCount: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &recordingProvisioner{saveChanged: test.changed}
			out, err := newTestModel(repo, cfg).
				Reconcile(t.Context(), clusterWithSources("source"), nil)

			require.NoError(t, err)
			require.Len(t, out.Events, test.eventCount)
			if test.eventCount > 0 {
				assert.Equal(t, EventQueryApplied, out.Events[0].Kind)
				assert.Contains(t, out.Events[0].Message, "1 custom metric query definition")
			}
		})
	}
}

func TestReconcile_DoesNotEmitAppliedEventForPartiallyAcceptedCollisionState(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "alpha", "queries.yaml", validQueryYAML)
	repo.set("ns", "beta", "queries.yaml", validQueryYAML)
	cfg := &recordingProvisioner{saveChanged: true}

	out, err := newTestModel(repo, cfg).
		Reconcile(t.Context(), clusterWithSources("alpha", "beta"), nil)

	require.NoError(t, err)
	require.Len(t, out.Events, 1)
	assert.Equal(t, EventCollision, out.Events[0].Kind)
}

func TestReconcile_ProviderPendingRequeuesThenConfirmsWithoutDuplicateApplyEvent(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", validQueryYAML)
	cfg := &recordingProvisioner{
		observation: mtypes.Observation{State: mtypes.ObservationPending, Message: "provider operation pending"},
	}
	model := newTestModel(repo, cfg)
	cluster := clusterWithSources("source")

	out, err := model.Reconcile(t.Context(), cluster, nil)
	require.NoError(t, err)
	assert.True(t, out.Configuring)
	assert.True(t, out.Requeue)
	assert.Empty(t, out.Events)
	assert.Empty(t, cfg.saved)

	cfg.observation = mtypes.Observation{State: mtypes.ObservationReady}
	cfg.saveChanged = true
	out, err = model.Reconcile(t.Context(), cluster, nil)
	require.NoError(t, err)
	require.Len(t, out.Events, 1)
	assert.Equal(t, EventQueryApplied, out.Events[0].Kind)

	cfg.saveChanged = false
	out, err = model.Reconcile(t.Context(), cluster, nil)
	require.NoError(t, err)
	assert.Empty(t, out.Events)
}

func TestReconcile_ObservationRevisionMismatchNeverSavesOrAcknowledges(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", validQueryYAML)
	cfg := &recordingProvisioner{
		observation: mtypes.Observation{
			State: mtypes.ObservationReady,
			Confirmed: &mtypes.ConfirmedState{
				Revision:   "different-revision",
				Enabled:    true,
				QueryCount: 1,
			},
		},
	}

	_, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources("source"), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "without confirming expected")
	assert.Empty(t, cfg.saved)
}

func TestReconcile_ObservationErrorRemainsRetryableWithoutSaving(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", validQueryYAML)
	observationErr := apierrors.NewServerTimeout(
		schema.GroupResource{Resource: "configmaps"},
		"get",
		1,
	)
	cfg := &recordingProvisioner{observeErr: observationErr}

	out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources("source"), nil)

	assert.ErrorIs(t, err, observationErr)
	assert.Contains(t, err.Error(), `observing expected custom-metrics revision "revision"`)
	assert.Empty(t, cfg.saved)
	assert.Empty(t, out.Events)
}

func TestReconcile_ClusterCollisionExcludesLoser(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "alpha", "queries.yaml", validQueryYAML)
	repo.set("ns", "beta", "queries.yaml", validQueryYAML)
	cfg := &recordingProvisioner{}
	out, err := newTestModel(repo, cfg).
		Reconcile(t.Context(), clusterWithSources("alpha", "beta"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidCollision, out.Invalid)
	require.Len(t, cfg.applied, 1)
	require.Len(t, cfg.applied[0].ClusterQueries, 1)
	assert.Equal(t, "alpha", cfg.applied[0].ClusterQueries[0].Source.ConfigMapName)
}

func TestReconcile_ClusterCollisionDropsCompleteSourcePackage(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "alpha", "queries.yaml", validQueryYAML)
	repo.set("ns", "beta", "queries.yaml", validQueryYAML+`
pg_beta_only:
  type: gauge
  help: Beta-only metric
  query: SELECT 1 AS value
  value: value
`)
	cfg := &recordingProvisioner{}
	out, err := newTestModel(repo, cfg).
		Reconcile(t.Context(), clusterWithSources("alpha", "beta"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidCollision, out.Invalid)
	require.Len(t, cfg.applied, 1)
	require.Len(t, cfg.applied[0].ClusterQueries, 1)
	assert.Equal(t, "alpha", cfg.applied[0].ClusterQueries[0].Source.ConfigMapName)
}

func TestReconcile_DatabaseCollisionUsesDeclaredSelectorOrder(t *testing.T) {
	identity := mtypes.ContributorIdentity{
		PostgresDatabaseName: "owner",
		PostgresDatabaseUID:  "uid",
		DatabaseName:         "orders",
		Namespace:            "ns",
	}
	contribution := mtypes.DatabaseContribution{
		Identity:          identity,
		Revision:          "revision",
		Exists:            true,
		CreationTimestamp: metav1.Now().Time,
		Selectors: []mtypes.QuerySelector{
			{ConfigMapName: "z-declared-first", ConfigMapKey: "queries.yaml"},
			{ConfigMapName: "a-declared-second", ConfigMapKey: "queries.yaml"},
		},
	}
	repo := &stubDataRepository{snapshot: mtypes.DatabaseContributionSnapshot{
		Contributions: []mtypes.DatabaseContribution{contribution},
	}}
	repo.set("ns", "z-declared-first", "queries.yaml", validQueryYAML)
	repo.set("ns", "a-declared-second", "queries.yaml", validQueryYAML)
	provisioner := &recordingProvisioner{}

	out, err := newTestModel(repo, provisioner).Reconcile(t.Context(), clusterWithSources(), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidCollision, out.Invalid)
	require.Len(t, provisioner.applied, 1)
	require.Len(t, provisioner.applied[0].DatabaseQueries["orders"], 1)
	assert.Equal(t, "z-declared-first", provisioner.applied[0].DatabaseQueries["orders"][0].Source.ConfigMapName)
}

func TestReconcile_OversizedAggregateIsKnownInvalid(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "source", "queries.yaml", validQueryYAML)
	cfg := &recordingProvisioner{err: mtypes.ErrGeneratedConfigTooLarge}
	out, err := newTestModel(repo, cfg).
		Reconcile(t.Context(), clusterWithSources("source"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidConfigTooLarge, out.Invalid)
	assert.Contains(t, out.InvalidDetail, "too large")
	require.Len(t, out.Events, 1)
	assert.Equal(t, EventConfigTooLarge, out.Events[0].Kind)
}

func TestReconcile_OversizedAggregateTakesConditionPriorityOverCollision(t *testing.T) {
	repo := &stubDataRepository{}
	repo.set("ns", "alpha", "queries.yaml", validQueryYAML)
	repo.set("ns", "beta", "queries.yaml", validQueryYAML)
	cfg := &recordingProvisioner{err: mtypes.ErrGeneratedConfigTooLarge}
	out, err := newTestModel(repo, cfg).
		Reconcile(t.Context(), clusterWithSources("alpha", "beta"), nil)

	require.NoError(t, err)
	assert.Equal(t, InvalidConfigTooLarge, out.Invalid)
	require.Len(t, out.Events, 2)
	assert.Equal(t, EventCollision, out.Events[0].Kind)
	assert.Equal(t, EventConfigTooLarge, out.Events[1].Kind)
}

func TestReconcile_NoReferencesClearsManagedConfiguration(t *testing.T) {
	cfg := &recordingProvisioner{}
	out, err := newTestModel(&stubDataRepository{}, cfg).
		Reconcile(t.Context(), clusterWithSources(), nil)

	require.NoError(t, err)
	assert.True(t, out.Disabled)
	require.Len(t, cfg.applied, 1)
	assert.Empty(t, cfg.applied[0].ClusterQueries)
	assert.Empty(t, cfg.applied[0].DatabaseQueries)
}
