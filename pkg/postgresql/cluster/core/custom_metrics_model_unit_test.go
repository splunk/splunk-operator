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

package core

import (
	"errors"
	"testing"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	mon "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/custom_metrics"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMonitoringModel_OversizedConfigConditionIsActionable(t *testing.T) {
	m := &customMetricsModel{outcome: mon.Outcome{
		Invalid:       mon.InvalidConfigTooLarge,
		InvalidDetail: "generated ConfigMap data is 1049000 bytes; maximum is 1048576 bytes",
	}}

	health, err := m.computeHealth(nil)

	require.NoError(t, err)
	assert.Equal(t, customMetricsReady, health.Condition)
	assert.Equal(t, reasonCustomMetricsConfigTooLarge, health.Reason)
	assert.Equal(t, pgcConstants.Ready, health.State, "custom-metrics degradation must not make the parent cluster unavailable")
	require.NotNil(t, health.ConditionStatus)
	assert.Equal(t, metav1.ConditionFalse, *health.ConditionStatus)
	assert.Empty(t, health.Phase)
	assert.Contains(t, health.Message, "1049000 bytes")
	assert.Contains(t, health.Message, "Reduce the number or size")
	assert.Contains(t, health.Message, "previous complete configuration remains active")
}

func TestCustomMetricsModel_ConflictStaysConfiguringAndRetryable(t *testing.T) {
	conflict := apierrors.NewConflict(
		schema.GroupResource{Group: "", Resource: "configmaps"},
		"pg-metrics",
		assert.AnError,
	)
	m := &customMetricsModel{}

	health, err := m.computeHealth(newReconcileFailure(reasonCustomMetricsApplyFailed, conflict))

	assert.ErrorIs(t, err, conflict)
	assert.Equal(t, pgcConstants.Configuring, health.State)
	assert.Equal(t, configuringClusterPhase, health.Phase)
	assert.Equal(t, reasonCustomMetricsApplyRetrying, health.Reason)
	require.NotNil(t, health.ConditionStatus)
	assert.Equal(t, metav1.ConditionFalse, *health.ConditionStatus)
	assert.NotZero(t, health.Result.RequeueAfter)
}

func TestCustomMetricsModel_ConfirmedResourceDisappearanceStaysConfiguringAndRetryable(t *testing.T) {
	m := &customMetricsModel{}

	health, err := m.computeHealth(newReconcileFailure(
		reasonCustomMetricsApplyFailed,
		mtypes.ErrConfirmedResourceUnavailable,
	))

	assert.ErrorIs(t, err, mtypes.ErrConfirmedResourceUnavailable)
	assert.Equal(t, pgcConstants.Configuring, health.State)
	assert.Equal(t, reasonCustomMetricsApplyRetrying, health.Reason)
	assert.NotZero(t, health.Result.RequeueAfter)
}

func TestCustomMetricsModel_NotFoundDuringConfirmationStaysConfiguringAndRetryable(t *testing.T) {
	notFound := apierrors.NewNotFound(
		schema.GroupResource{Resource: "configmaps"},
		"pg-metrics",
	)
	m := &customMetricsModel{}

	health, err := m.computeHealth(newReconcileFailure(reasonCustomMetricsApplyFailed, notFound))

	assert.ErrorIs(t, err, notFound)
	assert.Equal(t, pgcConstants.Configuring, health.State)
	assert.Equal(t, reasonCustomMetricsApplyRetrying, health.Reason)
	assert.NotZero(t, health.Result.RequeueAfter)
}

func TestCustomMetricsModel_NonRetryableApplyErrorRemainsFailed(t *testing.T) {
	deterministic := errors.New("generated ConfigMap is foreign-owned")
	m := &customMetricsModel{
		events:  noopEventEmitter{},
		cluster: &enterprisev4.PostgresCluster{},
	}

	health, err := m.computeHealth(newReconcileFailure(reasonCustomMetricsApplyFailed, deterministic))

	assert.ErrorIs(t, err, deterministic)
	assert.Equal(t, pgcConstants.Failed, health.State)
	assert.Equal(t, failedClusterPhase, health.Phase)
	assert.Equal(t, reasonCustomMetricsApplyFailed, health.Reason)
}

func TestCustomMetricsModel_OwnershipConflictIsDegraded(t *testing.T) {
	m := &customMetricsModel{outcome: mon.Outcome{
		Invalid:       mon.InvalidOwnershipConflict,
		InvalidDetail: "ConfigMap ns/pg-metrics is foreign",
	}}

	health, err := m.computeHealth(nil)

	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Ready, health.State)
	assert.Equal(t, reasonCustomMetricsOwnershipConflict, health.Reason)
	require.NotNil(t, health.ConditionStatus)
	assert.Equal(t, metav1.ConditionFalse, *health.ConditionStatus)
	assert.Contains(t, health.Message, "Remove or rename")
}

func TestCustomMetricsModel_InvalidSourceKeepsDegradedConditionWhileRepairRequeues(t *testing.T) {
	m := &customMetricsModel{outcome: mon.Outcome{
		Invalid:       mon.InvalidQuery,
		InvalidDetail: "invalid source; waiting for restored revision",
		Requeue:       true,
	}}

	health, err := m.computeHealth(nil)

	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Configuring, health.State)
	assert.Equal(t, configuringClusterPhase, health.Phase)
	require.NotNil(t, health.ConditionStatus)
	assert.Equal(t, metav1.ConditionFalse, *health.ConditionStatus)
	assert.Equal(t, reasonCustomMetricsInvalidQuery, health.Reason)
	assert.NotZero(t, health.Result.RequeueAfter)
}

func TestCustomMetricsModel_UnpublishedContributionReasonWinsWhileRollbackConfigures(t *testing.T) {
	m := &customMetricsModel{outcome: mon.Outcome{
		Pending:       true,
		Configuring:   true,
		InvalidDetail: "waiting for database publication; waiting for restored revision",
		Requeue:       true,
	}}

	health, err := m.computeHealth(nil)

	require.NoError(t, err)
	assert.Equal(t, reasonCustomMetricsPending, health.Reason)
	assert.Equal(t, pgcConstants.Configuring, health.State)
	assert.NotZero(t, health.Result.RequeueAfter)
}

func TestCustomMetricsAcknowledgementStatusRoundTrip(t *testing.T) {
	acknowledgements := []mtypes.DatabaseAcknowledgement{{
		Identity: mtypes.ContributorIdentity{
			PostgresDatabaseName: "owner",
			PostgresDatabaseUID:  "uid",
			DatabaseName:         "orders",
		},
		DesiredRevision: "desired",
		AppliedRevision: "applied",
		Status:          mtypes.AcknowledgementFalse,
		Reason:          "InvalidQueryDefinition",
		Message:         "invalid source",
	}}

	status := customMetricsStatusFromAcknowledgements(acknowledgements)
	require.NotNil(t, status)
	roundTrip := databaseAcknowledgementsFromStatus(status)

	require.Len(t, roundTrip, 1)
	assert.Equal(t, acknowledgements[0], roundTrip[0])
}

func TestCustomMetricsAcknowledgementStatusSortsObjectIncarnationsByUID(t *testing.T) {
	acknowledgements := []mtypes.DatabaseAcknowledgement{
		{
			Identity: mtypes.ContributorIdentity{
				PostgresDatabaseName: "owner",
				PostgresDatabaseUID:  "uid-b",
				DatabaseName:         "orders",
			},
		},
		{
			Identity: mtypes.ContributorIdentity{
				PostgresDatabaseName: "owner",
				PostgresDatabaseUID:  "uid-a",
				DatabaseName:         "orders",
			},
		},
	}

	status := customMetricsStatusFromAcknowledgements(acknowledgements)

	require.Len(t, status.DatabaseContributions, 2)
	assert.Equal(t, "uid-a", status.DatabaseContributions[0].PostgresDatabaseUID)
	assert.Equal(t, "uid-b", status.DatabaseContributions[1].PostgresDatabaseUID)
}
