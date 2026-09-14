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
	"fmt"
	"strings"
	"testing"

	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestReconcile_DatabaseContributionAcknowledgements(t *testing.T) {
	identity := mtypes.ContributorIdentity{
		PostgresDatabaseName: "database-owner",
		PostgresDatabaseUID:  "uid",
		DatabaseName:         "orders",
		Namespace:            "ns",
	}
	contribution := mtypes.DatabaseContribution{
		Identity:          identity,
		Revision:          "revision",
		Exists:            true,
		CreationTimestamp: metav1.Now().Time,
		Selectors: []mtypes.QuerySelector{{
			ConfigMapName: "orders-metrics",
			ConfigMapKey:  "queries.yaml",
		}},
	}

	t.Run("successful aggregate acknowledges the applied revision", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{contribution}},
		}
		repo.set("ns", "orders-metrics", "queries.yaml", validQueryYAML)
		cfg := &recordingProvisioner{}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), nil)

		require.NoError(t, err)
		require.Len(t, out.DatabaseContributions, 1)
		assert.Equal(t, mtypes.AcknowledgementTrue, out.DatabaseContributions[0].Status)
		assert.Equal(t, "revision", out.DatabaseContributions[0].AppliedRevision)
		require.Len(t, cfg.applied, 1)
		require.Len(t, cfg.applied[0].DatabaseQueries["orders"], 1)
	})

	t.Run("invalid source rejects only the offending contribution and preserves applied revision", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{contribution}},
		}
		repo.set("ns", "orders-metrics", "queries.yaml", "invalid: [")
		cfg := &recordingProvisioner{}
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: "revision",
			AppliedRevision: "revision",
			Status:          mtypes.AcknowledgementTrue,
		}}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), previous)

		require.NoError(t, err)
		assert.Equal(t, InvalidQuery, out.Invalid)
		require.Len(t, out.DatabaseContributions, 1)
		assert.Equal(t, mtypes.AcknowledgementFalse, out.DatabaseContributions[0].Status)
		assert.Equal(t, "revision", out.DatabaseContributions[0].AppliedRevision)
		assert.Empty(t, cfg.applied)
	})

	t.Run("invalid cluster source preserves only an exact positive acknowledgement", func(t *testing.T) {
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementTrue,
			Reason:          "CustomMetricsReady",
		}}
		tests := []struct {
			name     string
			revision string
			status   mtypes.AcknowledgementStatus
			reason   string
		}{
			{name: "confirmed revision", revision: contribution.Revision, status: mtypes.AcknowledgementTrue, reason: "CustomMetricsReady"},
			{name: "new revision", revision: "updated-revision", status: mtypes.AcknowledgementFalse, reason: "InvalidQueryDefinition"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				current := contribution
				current.Revision = tt.revision
				repo := &stubDataRepository{
					snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{current}},
				}
				repo.set("ns", "orders-metrics", "queries.yaml", validQueryYAML)
				repo.set("ns", "cluster-metrics", "queries.yaml", "invalid: [")

				out, err := newTestModel(repo, &recordingProvisioner{}).
					Reconcile(t.Context(), clusterWithSources("cluster-metrics"), previous)

				require.NoError(t, err)
				assert.Equal(t, InvalidQuery, out.Invalid)
				require.Len(t, out.DatabaseContributions, 1)
				ack := out.DatabaseContributions[0]
				assert.Equal(t, tt.status, ack.Status)
				assert.Equal(t, tt.reason, ack.Reason)
				assert.Equal(t, current.Revision, ack.DesiredRevision)
				assert.Equal(t, contribution.Revision, ack.AppliedRevision)
			})
		}
	})

	t.Run("pending rollback observation downgrades an exact positive acknowledgement", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{contribution}},
		}
		repo.set("ns", "orders-metrics", "queries.yaml", validQueryYAML)
		repo.set("ns", "cluster-metrics", "queries.yaml", "invalid: [")
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementTrue,
			Reason:          "CustomMetricsReady",
		}}
		cfg := &recordingProvisioner{
			observation: mtypes.Observation{
				State:   mtypes.ObservationPending,
				Message: "waiting for CNPG to consume restored ConfigMap",
			},
			rollback: mtypes.RollbackResult{
				Available: true,
				Expected: mtypes.ExpectedState{
					Revision:   contribution.Revision,
					Enabled:    true,
					QueryCount: 1,
				},
			},
		}

		out, err := newTestModel(repo, cfg).
			Reconcile(t.Context(), clusterWithSources("cluster-metrics"), previous)

		require.NoError(t, err)
		assert.True(t, out.Configuring)
		assert.True(t, out.Requeue)
		require.Len(t, out.DatabaseContributions, 1)
		ack := out.DatabaseContributions[0]
		assert.Equal(t, mtypes.AcknowledgementUnknown, ack.Status)
		assert.Equal(t, "CustomMetricsConfiguring", ack.Reason)
		assert.Equal(t, "waiting for CNPG to consume restored ConfigMap", ack.Message)
		assert.Equal(t, contribution.Revision, ack.AppliedRevision)
	})

	t.Run("transient apply error remains retryable with a pending acknowledgement", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{
				Contributions: []mtypes.DatabaseContribution{contribution},
			},
		}
		repo.set("ns", "orders-metrics", "queries.yaml", validQueryYAML)
		conflict := apierrors.NewConflict(
			schema.GroupResource{Resource: "configmaps"},
			"pg-metrics",
			assert.AnError,
		)
		cfg := &recordingProvisioner{err: conflict}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), nil)

		assert.ErrorIs(t, err, conflict)
		require.Len(t, out.DatabaseContributions, 1)
		assert.Equal(t, mtypes.AcknowledgementUnknown, out.DatabaseContributions[0].Status)
		assert.Equal(t, "CustomMetricsApplyFailed", out.DatabaseContributions[0].Reason)
	})

	t.Run("provider pending replaces an older positive acknowledgement with unknown", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{contribution}},
		}
		repo.set("ns", "orders-metrics", "queries.yaml", validQueryYAML)
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementTrue,
			Reason:          "CustomMetricsReady",
		}}
		cfg := &recordingProvisioner{observation: mtypes.Observation{
			State:   mtypes.ObservationPending,
			Message: "waiting for CNPG to consume the source-data update",
		}}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), previous)

		require.NoError(t, err)
		require.Len(t, out.DatabaseContributions, 1)
		ack := out.DatabaseContributions[0]
		assert.Equal(t, mtypes.AcknowledgementUnknown, ack.Status)
		assert.Equal(t, "CustomMetricsConfiguring", ack.Reason)
		assert.Equal(t, contribution.Revision, ack.AppliedRevision)
	})

	t.Run("all invalid sources for one contribution are reported together", func(t *testing.T) {
		multipleSources := contribution
		multipleSources.Selectors = []mtypes.QuerySelector{
			{ConfigMapName: "missing-a", ConfigMapKey: "queries.yaml"},
			{ConfigMapName: "missing-b", ConfigMapKey: "queries.yaml"},
		}
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{multipleSources}},
		}

		out, err := newTestModel(repo, &recordingProvisioner{}).Reconcile(t.Context(), clusterWithSources(), nil)

		require.NoError(t, err)
		require.Len(t, out.DatabaseContributions, 1)
		ack := out.DatabaseContributions[0]
		assert.Equal(t, mtypes.AcknowledgementFalse, ack.Status)
		assert.Equal(t, "CustomMetricsConfigMapNotFound", ack.Reason)
		assert.Contains(t, ack.Message, "ns/missing-a/queries.yaml")
		assert.Contains(t, ack.Message, "ns/missing-b/queries.yaml")
	})

	t.Run("unpublished database status blocks aggregate replacement", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{
				Unpublished: []mtypes.ContributorIdentity{identity},
			},
		}
		cfg := &recordingProvisioner{}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), nil)

		require.NoError(t, err)
		assert.True(t, out.Pending)
		assert.Empty(t, cfg.applied)
	})

	t.Run("ownership loss overrides a prior positive acknowledgement", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{contribution}},
		}
		repo.set("ns", "orders-metrics", "queries.yaml", validQueryYAML)
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementTrue,
			Reason:          "CustomMetricsReady",
		}}
		cfg := &recordingProvisioner{
			err: fmt.Errorf("%w: ConfigMap ns/pg-metrics is foreign", mtypes.ErrGeneratedResourceOwnershipConflict),
		}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), previous)

		require.NoError(t, err)
		require.Len(t, out.DatabaseContributions, 1)
		ack := out.DatabaseContributions[0]
		assert.Equal(t, mtypes.AcknowledgementFalse, ack.Status)
		assert.Equal(t, "GeneratedResourceOwnershipConflict", ack.Reason)
		assert.Equal(t, contribution.Revision, ack.AppliedRevision)
	})

	t.Run("transient source read preserves the previous acknowledgement", func(t *testing.T) {
		repo := &stubDataRepository{
			fetchErr: assert.AnError,
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{contribution}},
		}
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementFalse,
			Reason:          "InvalidQueryDefinition",
			Message:         "previous diagnostic",
		}}

		out, err := newTestModel(repo, &recordingProvisioner{}).
			Reconcile(t.Context(), clusterWithSources(), previous)

		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, previous, out.DatabaseContributions)
	})

	t.Run("transient contribution list failure preserves the complete previous status", func(t *testing.T) {
		repo := &stubDataRepository{listErr: assert.AnError}
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementTrue,
			Reason:          "CustomMetricsReady",
		}}

		out, err := newTestModel(repo, &recordingProvisioner{}).
			Reconcile(t.Context(), clusterWithSources(), previous)

		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, previous, out.DatabaseContributions)
	})

	t.Run("safety save failure publishes unknown without an applied event", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{contribution}},
		}
		repo.set("ns", "orders-metrics", "queries.yaml", validQueryYAML)
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementTrue,
		}}
		cfg := &recordingProvisioner{saveChanged: true, saveErr: assert.AnError}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), previous)

		assert.ErrorIs(t, err, assert.AnError)
		require.Len(t, out.DatabaseContributions, 1)
		assert.Equal(t, mtypes.AcknowledgementUnknown, out.DatabaseContributions[0].Status)
		assert.Equal(t, "CustomMetricsSafetySaveFailed", out.DatabaseContributions[0].Reason)
		assert.Equal(t, contribution.Revision, out.DatabaseContributions[0].AppliedRevision)
		assert.Empty(t, out.Events)
	})

	t.Run("transient safety rollback failure publishes unknown without a repair event", func(t *testing.T) {
		repo := &stubDataRepository{
			snapshot: mtypes.DatabaseContributionSnapshot{Contributions: []mtypes.DatabaseContribution{contribution}},
		}
		repo.set("ns", "orders-metrics", "queries.yaml", "invalid: [")
		previous := []mtypes.DatabaseAcknowledgement{{
			Identity:        identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementTrue,
			Reason:          "CustomMetricsReady",
		}}
		conflict := apierrors.NewConflict(
			schema.GroupResource{Resource: "configmaps"},
			"pg-metrics",
			assert.AnError,
		)
		cfg := &recordingProvisioner{rollbackErr: conflict}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), previous)

		assert.ErrorIs(t, err, conflict)
		assert.Equal(t, InvalidQuery, out.Invalid)
		require.Len(t, out.DatabaseContributions, 1)
		ack := out.DatabaseContributions[0]
		assert.Equal(t, mtypes.AcknowledgementUnknown, ack.Status)
		assert.Equal(t, "CustomMetricsRollbackFailed", ack.Reason)
		assert.Equal(t, contribution.Revision, ack.AppliedRevision)
		require.Len(t, out.Events, 1)
		assert.Equal(t, EventInvalidQuery, out.Events[0].Kind)
	})

	t.Run("rollback ownership loss marks every current contributor false", func(t *testing.T) {
		otherIdentity := mtypes.ContributorIdentity{
			PostgresDatabaseName: "other-owner",
			PostgresDatabaseUID:  "other-uid",
			DatabaseName:         "billing",
			Namespace:            "ns",
		}
		other := mtypes.DatabaseContribution{
			Identity: otherIdentity,
			Revision: "other-revision",
			Exists:   true,
			Selectors: []mtypes.QuerySelector{{
				ConfigMapName: "billing-metrics",
				ConfigMapKey:  "queries.yaml",
			}},
		}
		repo := &stubDataRepository{snapshot: mtypes.DatabaseContributionSnapshot{
			Contributions: []mtypes.DatabaseContribution{contribution, other},
		}}
		repo.set("ns", "orders-metrics", "queries.yaml", "invalid: [")
		repo.set("ns", "billing-metrics", "queries.yaml", validQueryYAML)
		previous := []mtypes.DatabaseAcknowledgement{
			{
				Identity: identity, DesiredRevision: contribution.Revision,
				AppliedRevision: contribution.Revision, Status: mtypes.AcknowledgementTrue,
			},
			{
				Identity: otherIdentity, DesiredRevision: other.Revision,
				AppliedRevision: other.Revision, Status: mtypes.AcknowledgementTrue,
			},
		}
		cfg := &recordingProvisioner{rollbackErr: fmt.Errorf(
			"%w: ConfigMap ns/pg-metrics is foreign",
			mtypes.ErrGeneratedResourceOwnershipConflict,
		)}

		out, err := newTestModel(repo, cfg).Reconcile(t.Context(), clusterWithSources(), previous)

		require.NoError(t, err)
		assert.Equal(t, InvalidOwnershipConflict, out.Invalid)
		require.Len(t, out.DatabaseContributions, 2)
		for _, ack := range out.DatabaseContributions {
			assert.Equal(t, mtypes.AcknowledgementFalse, ack.Status)
			assert.Equal(t, "GeneratedResourceOwnershipConflict", ack.Reason)
			assert.NotEmpty(t, ack.AppliedRevision)
		}
	})
}

func TestAppliedAcknowledgements_AccumulatesContributorCollisionsDeterministically(t *testing.T) {
	identity := mtypes.ContributorIdentity{
		PostgresDatabaseName: "database-owner",
		PostgresDatabaseUID:  "uid",
		DatabaseName:         "orders",
	}
	contribution := mtypes.DatabaseContribution{Identity: identity, Revision: "revision", Exists: true}
	source := mtypes.QuerySource{
		Namespace: "ns", ConfigMapName: "orders", ConfigMapKey: "queries.yaml", Contributor: &identity,
	}
	collisions := []mtypes.CollisionError{
		{
			Key:               mtypes.CollisionKey{Kind: mtypes.CollisionMetricFamily, RenderedName: "z_metric"},
			First:             mtypes.QuerySource{Namespace: "ns", ConfigMapName: "first", ConfigMapKey: "queries.yaml"},
			Second:            source,
			FirstMetricName:   "first",
			FirstValueColumn:  "value",
			SecondMetricName:  "z",
			SecondValueColumn: "value",
		},
		{
			Key:               mtypes.CollisionKey{Kind: mtypes.CollisionMetricFamily, RenderedName: "a_metric"},
			First:             mtypes.QuerySource{Namespace: "ns", ConfigMapName: "first", ConfigMapKey: "queries.yaml"},
			Second:            source,
			FirstMetricName:   "first",
			FirstValueColumn:  "value",
			SecondMetricName:  "a",
			SecondValueColumn: "value",
		},
	}

	acks := appliedAcknowledgements([]mtypes.DatabaseContribution{contribution}, nil, collisions)

	require.Len(t, acks, 1)
	assert.Equal(t, mtypes.AcknowledgementFalse, acks[0].Status)
	aIndex := strings.Index(acks[0].Message, "a_metric")
	zIndex := strings.Index(acks[0].Message, "z_metric")
	assert.GreaterOrEqual(t, aIndex, 0)
	assert.Greater(t, zIndex, aIndex)
}
