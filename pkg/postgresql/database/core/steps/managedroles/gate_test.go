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

package managedroles

import (
	"context"
	"errors"
	"fmt"
	"testing"

	dbpipeline "github.com/splunk/splunk-operator/pkg/postgresql/database/core/pipeline"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type acknowledgementReaderFunc func(context.Context, dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error)

func (f acknowledgementReaderFunc) Read(ctx context.Context, target dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error) {
	return f(ctx, target)
}

func TestAcknowledgementGateMapsDecisionsAndPerDatabaseMessages(t *testing.T) {
	input := validGateInput()
	tests := []struct {
		name             string
		acknowledgement  dbtypes.ManagedRoleAcknowledgement
		reason           string
		phase            string
		message          string
		databaseMessages map[string]string
	}{
		{
			name:    "unpublished",
			reason:  string(reconciliationTypes.ReasonWaitingForCNPG),
			phase:   string(reconciliationTypes.PhaseProvisioning),
			message: "Waiting for cluster to publish managed role status",
			databaseMessages: map[string]string{
				"payments": "Waiting for cluster to publish managed role status",
				"audit":    "Waiting for cluster to publish managed role status",
			},
		},
		{
			name: "provider failure",
			acknowledgement: dbtypes.ManagedRoleAcknowledgement{
				Published: true, Failed: map[string]string{"audit_admin": "provider rejected role"},
			},
			reason:  string(reconciliationTypes.ReasonRoleReconcileFailed),
			phase:   string(reconciliationTypes.PhaseFailed),
			message: "Role reconciliation failed for PostgresDatabase tenant: role audit_admin failed to reconcile: provider rejected role",
			databaseMessages: map[string]string{
				"payments": "blocked by role gate on database \"audit\"",
				"audit":    "role audit_admin failed to reconcile: provider rejected role",
			},
		},
		{
			name: "conflict",
			acknowledgement: dbtypes.ManagedRoleAcknowledgement{
				Published: true,
				Conflicts: []dbtypes.ManagedRoleConflict{{Role: "audit_rw", AttemptedBy: input.Owner}},
			},
			reason:  string(reconciliationTypes.ReasonRoleConflict),
			phase:   string(reconciliationTypes.PhaseFailed),
			message: "Role conflict in PostgresDatabase tenant: role audit_rw is already claimed",
			databaseMessages: map[string]string{
				"payments": "blocked by role gate on database \"audit\"",
				"audit":    "role audit_rw is already claimed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := NewAcknowledgementGate(acknowledgementReaderFunc(func(_ context.Context, target dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error) {
				assert.Equal(t, input.Target, target)
				return tt.acknowledgement, nil
			}), input)

			outcome, err := gate.Observe(t.Context(), dbpipeline.NewContracts(), nil)

			require.NoError(t, err)
			assert.Equal(t, reconciliationTypes.ModeWaiting, outcome.Mode())
			assert.Equal(t, string(reconciliationTypes.ConditionRolesReady), outcome.Condition())
			assert.Equal(t, tt.reason, outcome.Reason())
			assert.Equal(t, tt.phase, outcome.Phase())
			assert.Equal(t, tt.message, outcome.Message())
			assert.Equal(t, tt.databaseMessages, gate.Decision().DatabaseMessages)
		})
	}
}

func TestAcknowledgementGatePublishesReadyOnlyForExactSuccess(t *testing.T) {
	input := validGateInput()
	roles := orderedRoles(input.Databases)
	owners := make(map[string]dbtypes.ManagedRoleParticipant, len(roles))
	for _, role := range roles {
		owners[role] = input.Owner
	}
	gate := NewAcknowledgementGate(acknowledgementReaderFunc(func(context.Context, dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error) {
		return dbtypes.ManagedRoleAcknowledgement{Published: true, Reconciled: roles, Owners: owners}, nil
	}), input)
	contracts := dbpipeline.NewContracts()

	outcome, err := gate.Observe(t.Context(), contracts, nil)

	require.NoError(t, err)
	assert.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	assert.Equal(t, reconciliationTypes.StatusPersistAndContinue, outcome.StatusAction())
	assert.Equal(t, string(reconciliationTypes.ReasonRolesAvailable), outcome.Reason())
	assert.Equal(t, "Roles reconciled: 4 active", outcome.Message())
	assert.NotNil(t, contracts.ManagedRolesReady)
	assert.Equal(t, GateProceed, gate.Decision().State)
	assert.Nil(t, gate.Decision().DatabaseMessages)
}

func TestAcknowledgementGateReadFailurePreservesPriorDecisionAndStatus(t *testing.T) {
	input := validGateInput()
	readErr := fmt.Errorf("%w: apiserver unavailable", dbtypes.ErrManagedRoleAcknowledgementRead)
	reads := 0
	gate := NewAcknowledgementGate(acknowledgementReaderFunc(func(context.Context, dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error) {
		reads++
		if reads == 1 {
			return dbtypes.ManagedRoleAcknowledgement{}, nil
		}
		return dbtypes.ManagedRoleAcknowledgement{}, readErr
	}), input)

	_, err := gate.Observe(t.Context(), dbpipeline.NewContracts(), nil)
	require.NoError(t, err)
	before := gate.Decision()

	outcome, err := gate.Observe(t.Context(), dbpipeline.NewContracts(), nil)

	require.NoError(t, err)
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.Equal(t, reconciliationTypes.StatusNone, outcome.StatusAction())
	assert.ErrorIs(t, outcome.Err(), dbtypes.ErrManagedRoleAcknowledgementRead)
	assert.Equal(t, before, gate.Decision())
}

func TestAcknowledgementGateRequiresPublicationAndConnectionMetadata(t *testing.T) {
	gate := NewAcknowledgementGate(nil, validGateInput())
	assert.Equal(t, []dbpipeline.ContractKey{
		dbpipeline.ContractDatabaseManagedRoleIntentPublished,
		dbpipeline.ContractDatabaseConnectionMetadataReady,
	}, gate.Requires())
	assert.Equal(t, []dbpipeline.ContractKey{dbpipeline.ContractDatabaseManagedRolesReady}, gate.Provides())
}

func TestAcknowledgementGateRejectsInvalidInputBeforeReading(t *testing.T) {
	readCalls := 0
	input := validGateInput()
	input.Owner.UID = ""
	gate := NewAcknowledgementGate(acknowledgementReaderFunc(func(context.Context, dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error) {
		readCalls++
		return dbtypes.ManagedRoleAcknowledgement{}, errors.New("must not run")
	}), input)

	outcome, err := gate.Observe(t.Context(), dbpipeline.NewContracts(), nil)

	require.NoError(t, err)
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.ErrorIs(t, outcome.Err(), errInvalidGateInput)
	assert.Zero(t, readCalls)
}

func validGateInput() GateInput {
	return GateInput{
		Target:    dbtypes.ManagedRoleAcknowledgementTarget{Namespace: "dbs", ClusterName: "primary"},
		Owner:     dbtypes.ManagedRoleParticipant{Name: "tenant", UID: "database-uid"},
		Databases: managedRoleDatabases(),
	}
}

func managedRoleDatabases() []Database {
	return []Database{
		{Name: "payments", Roles: []Role{{Name: "payments_admin"}, {Name: "payments_rw"}}},
		{Name: "audit", Roles: []Role{{Name: "audit_admin"}, {Name: "audit_rw"}}},
	}
}
