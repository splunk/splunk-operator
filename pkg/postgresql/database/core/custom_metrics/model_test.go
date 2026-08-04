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
	"testing"

	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type acknowledgementStub struct {
	ack   mtypes.DatabaseAcknowledgement
	found bool
	err   error
}

func (s acknowledgementStub) Find(_ context.Context, _ mtypes.ContributorIdentity) (mtypes.DatabaseAcknowledgement, bool, error) {
	return s.ack, s.found, s.err
}

type acknowledgementsStub map[string]mtypes.DatabaseAcknowledgement

func (s acknowledgementsStub) Find(
	_ context.Context,
	identity mtypes.ContributorIdentity,
) (mtypes.DatabaseAcknowledgement, bool, error) {
	ack, found := s[identity.DatabaseName]
	return ack, found, nil
}

func metricsPublicationInput() PublicationInput {
	return PublicationInput{
		OwnerName: "database-owner",
		OwnerUID:  "uid",
		Namespace: "ns",
		Databases: []DesiredDatabase{{
			Name: "orders",
			Selectors: []mtypes.QuerySelector{{
				ConfigMapName: "orders-metrics",
				ConfigMapKey:  "queries.yaml",
			}},
		}},
	}
}

func TestPlanPublicationBuildsCommittedContribution(t *testing.T) {
	plan := PlanPublication(metricsPublicationInput())

	require.Len(t, plan, 1)
	assert.True(t, plan[0].Exists)
	assert.NotEmpty(t, plan[0].Revision)
	assert.Equal(t, "orders", plan[0].Identity.DatabaseName)
}

func TestPlanPublicationDeclaresExplicitNonParticipation(t *testing.T) {
	input := metricsPublicationInput()
	input.Databases[0].Selectors = nil

	plan := PlanPublication(input)

	require.Len(t, plan, 1)
	assert.Equal(t, "orders", plan[0].Identity.DatabaseName)
	assert.False(t, plan[0].Exists)
	assert.NotEmpty(t, plan[0].Revision)
}

func TestModelRequiresMatchingHealthyAcknowledgement(t *testing.T) {
	contribution := PlanPublication(metricsPublicationInput())[0]
	input := GateInput{
		Contributions: []mtypes.DatabaseContribution{contribution},
		ClusterName:   "postgres-main",
	}

	t.Run("matching applied revision passes", func(t *testing.T) {
		out, reconcileErr := NewModel(acknowledgementStub{
			found: true,
			ack: mtypes.DatabaseAcknowledgement{
				Identity:        contribution.Identity,
				DesiredRevision: contribution.Revision,
				AppliedRevision: contribution.Revision,
				Status:          mtypes.AcknowledgementTrue,
			},
		}).Reconcile(t.Context(), input)
		require.NoError(t, reconcileErr)
		assert.Equal(t, GateReady, out.State)
	})

	t.Run("negative acknowledgement fails even when applied revision matches", func(t *testing.T) {
		out, reconcileErr := NewModel(acknowledgementStub{
			found: true,
			ack: mtypes.DatabaseAcknowledgement{
				Identity:        contribution.Identity,
				DesiredRevision: contribution.Revision,
				AppliedRevision: contribution.Revision,
				Status:          mtypes.AcknowledgementFalse,
				Reason:          "InvalidQueryDefinition",
				Message:         "orders-metrics/queries.yaml is invalid",
			},
		}).Reconcile(t.Context(), input)
		require.NoError(t, reconcileErr)
		assert.Equal(t, GateFailed, out.State)
		assert.Equal(t, "InvalidQueryDefinition", out.Reason)
	})

	t.Run("stale desired revision remains pending", func(t *testing.T) {
		out, reconcileErr := NewModel(acknowledgementStub{
			found: true,
			ack: mtypes.DatabaseAcknowledgement{
				Identity:        contribution.Identity,
				DesiredRevision: "stale",
				AppliedRevision: "stale",
				Status:          mtypes.AcknowledgementTrue,
			},
		}).Reconcile(t.Context(), input)
		require.NoError(t, reconcileErr)
		assert.Equal(t, GatePending, out.State)
		assert.Contains(t, out.Message, `PostgresCluster "postgres-main"`)
	})
}

func TestModelGatesOnlyPendingDisableTombstone(t *testing.T) {
	publication := metricsPublicationInput()
	publication.Databases[0].Selectors = nil
	contribution := PlanPublication(publication)[0]

	out, err := NewModel(acknowledgementStub{}).Reconcile(t.Context(), GateInput{
		Contributions: []mtypes.DatabaseContribution{contribution},
	})
	require.NoError(t, err)
	assert.Equal(t, GateReady, out.State)
	assert.Equal(t, "CustomMetricsDisabled", out.Reason)

	out, err = NewModel(acknowledgementStub{}).Reconcile(t.Context(), GateInput{
		Contributions:                  []mtypes.DatabaseContribution{contribution},
		DisabledAcknowledgementPending: true,
	})
	require.NoError(t, err)
	assert.Equal(t, GatePending, out.State)
}

func TestModelAggregatesAcknowledgementsAcrossDatabases(t *testing.T) {
	publication := PublicationInput{
		OwnerName: "database-owner",
		OwnerUID:  "uid",
		Namespace: "ns",
		Databases: []DesiredDatabase{
			{
				Name: "orders",
				Selectors: []mtypes.QuerySelector{{
					ConfigMapName: "orders-metrics",
					ConfigMapKey:  "queries.yaml",
				}},
			},
			{
				Name: "billing",
				Selectors: []mtypes.QuerySelector{{
					ConfigMapName: "billing-metrics",
					ConfigMapKey:  "queries.yaml",
				}},
			},
		},
	}
	contributions := PlanPublication(publication)
	require.Len(t, contributions, 2)
	input := GateInput{Contributions: contributions}

	ordersAcknowledgement := mtypes.DatabaseAcknowledgement{
		Identity:        contributions[0].Identity,
		DesiredRevision: contributions[0].Revision,
		AppliedRevision: contributions[0].Revision,
		Status:          mtypes.AcknowledgementTrue,
		Reason:          "CustomMetricsReady",
		Message:         "Database custom metrics are applied",
	}

	t.Run("one acknowledged database plus one pending database keeps the gate pending", func(t *testing.T) {
		out, reconcileErr := NewModel(acknowledgementsStub{
			"orders": ordersAcknowledgement,
		}).Reconcile(t.Context(), input)

		require.NoError(t, reconcileErr)
		assert.Equal(t, GatePending, out.State)
		assert.Equal(t, "CustomMetricsPending", out.Reason)
		assert.Contains(t, out.Message, "billing")
	})

	t.Run("a negative acknowledgement fails the gate and identifies its database", func(t *testing.T) {
		billing := contributions[1]
		out, reconcileErr := NewModel(acknowledgementsStub{
			"orders": ordersAcknowledgement,
			"billing": {
				Identity:        billing.Identity,
				DesiredRevision: billing.Revision,
				AppliedRevision: billing.Revision,
				Status:          mtypes.AcknowledgementFalse,
				Reason:          "InvalidQueryDefinition",
				Message:         "query definition is invalid",
			},
		}).Reconcile(t.Context(), input)

		require.NoError(t, reconcileErr)
		assert.Equal(t, GateFailed, out.State)
		assert.Equal(t, "InvalidQueryDefinition", out.Reason)
		assert.Contains(t, out.Message, `"billing"`)
		assert.Contains(t, out.Message, "query definition is invalid")
	})
}
