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
	"time"

	dbpipeline "github.com/splunk/splunk-operator/pkg/postgresql/database/core/pipeline"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type intentPublisherFunc func(context.Context, dbtypes.ManagedRolePublication) error

func (f intentPublisherFunc) Publish(ctx context.Context, publication dbtypes.ManagedRolePublication) error {
	return f(ctx, publication)
}

func TestBuildPublicationPreservesDesiredOrderAndAddsDatabaseAndRoleRemovalTombstones(t *testing.T) {
	input := validPublicationInput()
	input.Databases[0].Roles = input.Databases[0].Roles[:1]
	input.Databases = append(input.Databases, Database{
		Name: "audit",
		Roles: []Role{
			{Name: "audit_owner", SecretName: "tenant-audit-owner"},
			{Name: "audit_reader", SecretName: "tenant-audit-reader"},
		},
	})
	input.PreviouslyPublished = []dbtypes.DatabaseRoleIntent{
		{Database: "payments", Roles: []dbtypes.ManagedRoleIntent{
			{Name: "payments_admin", SecretName: "tenant-payments-admin", Exists: true},
			{Name: "payments_rw", SecretName: "tenant-payments-rw", Exists: true},
		}},
		{Database: "removed", Roles: []dbtypes.ManagedRoleIntent{
			{Name: "removed_admin", SecretName: "tenant-removed-admin", Exists: true},
			{Name: "removed_rw", SecretName: "tenant-removed-rw", Exists: true},
		}},
	}

	publication, err := buildPublication(input)
	require.NoError(t, err)
	require.Len(t, publication.Databases, 3)
	assert.Equal(t, []string{"payments", "audit", "removed"}, []string{
		publication.Databases[0].Database,
		publication.Databases[1].Database,
		publication.Databases[2].Database,
	})
	assert.Equal(t, []dbtypes.ManagedRoleIntent{
		{Name: "payments_admin", SecretName: "tenant-payments-admin", Exists: true},
		{Name: "payments_rw", SecretName: "tenant-payments-rw", Exists: false},
	}, publication.Databases[0].Roles)
	assert.Equal(t, []dbtypes.ManagedRoleIntent{
		{Name: "removed_admin", SecretName: "tenant-removed-admin", Exists: false},
		{Name: "removed_rw", SecretName: "tenant-removed-rw", Exists: false},
	}, publication.Databases[2].Roles)
}

func TestBuildPublicationRejectsInvalidInternalInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PublicationInput)
	}{
		{name: "missing target", mutate: func(input *PublicationInput) { input.Target.UID = "" }},
		{name: "no databases", mutate: func(input *PublicationInput) { input.Databases = nil }},
		{name: "duplicate database", mutate: func(input *PublicationInput) { input.Databases = append(input.Databases, input.Databases[0]) }},
		{name: "missing role", mutate: func(input *PublicationInput) { input.Databases[0].Roles[0].Name = "" }},
		{name: "missing secret", mutate: func(input *PublicationInput) { input.Databases[0].Roles[0].SecretName = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validPublicationInput()
			tt.mutate(&input)
			_, err := buildPublication(input)
			assert.ErrorIs(t, err, errInvalidPublicationInput)
		})
	}
}

func TestPublicationStepPublishesCurrentPassContractAndCopiesResult(t *testing.T) {
	var got dbtypes.ManagedRolePublication
	step := NewPublicationStep(intentPublisherFunc(func(_ context.Context, publication dbtypes.ManagedRolePublication) error {
		got = publication
		return nil
	}), validPublicationInput())
	contracts := dbpipeline.NewContracts()

	reconcileErr := step.Reconcile(t.Context(), contracts)
	outcome, observeErr := step.Observe(t.Context(), contracts, reconcileErr)

	require.NoError(t, reconcileErr)
	require.NoError(t, observeErr)
	assert.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	assert.NotNil(t, contracts.ManagedRoleIntentPublished)
	assert.Equal(t, got, step.Publication())
	got.Databases[0].Roles[0].Name = "mutated"
	assert.Equal(t, "payments_admin", step.Publication().Databases[0].Roles[0].Name)
}

func TestPublicationStepClassifiesWriteFailuresWithoutStatus(t *testing.T) {
	t.Run("conflict", func(t *testing.T) {
		writeErr := fmt.Errorf("%w: write conflict", dbtypes.ErrManagedRoleIntentConflict)
		step := NewPublicationStep(intentPublisherFunc(func(context.Context, dbtypes.ManagedRolePublication) error {
			return writeErr
		}), validPublicationInput())
		contracts := dbpipeline.NewContracts()

		reconcileErr := step.Reconcile(t.Context(), contracts)
		outcome, err := step.Observe(t.Context(), contracts, reconcileErr)

		require.NoError(t, err)
		assert.Equal(t, reconciliationTypes.ModeImmediateRequeue, outcome.Mode())
		assert.Equal(t, reconciliationTypes.StatusNone, outcome.StatusAction())
		assert.ErrorIs(t, outcome.Err(), dbtypes.ErrManagedRoleIntentConflict)
		assert.Nil(t, contracts.ManagedRoleIntentPublished)
	})

	t.Run("other API failure", func(t *testing.T) {
		writeErr := errors.New("apiserver unavailable")
		step := NewPublicationStep(intentPublisherFunc(func(context.Context, dbtypes.ManagedRolePublication) error {
			return writeErr
		}), validPublicationInput())
		contracts := dbpipeline.NewContracts()

		reconcileErr := step.Reconcile(t.Context(), contracts)
		outcome, err := step.Observe(t.Context(), contracts, reconcileErr)

		require.NoError(t, err)
		assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
		assert.Equal(t, reconciliationTypes.StatusNone, outcome.StatusAction())
		assert.ErrorIs(t, outcome.Err(), writeErr)
		assert.Nil(t, contracts.ManagedRoleIntentPublished)
	})
}

func TestPublicationStepDoesNotRunWithoutCurrentPassCredentials(t *testing.T) {
	publishCalls := 0
	step := NewPublicationStep(intentPublisherFunc(func(context.Context, dbtypes.ManagedRolePublication) error {
		publishCalls++
		return nil
	}), validPublicationInput())

	outcome, err := dbpipeline.Run(t.Context(), []dbpipeline.Step{
		&testContractStep{name: "credentials", provides: []dbpipeline.ContractKey{dbpipeline.ContractDatabaseCredentialsReady}},
		step,
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, reconciliationTypes.ModeDeferred, outcome.Mode())
	assert.Zero(t, publishCalls)
}

func validPublicationInput() PublicationInput {
	return PublicationInput{
		Target: dbtypes.ManagedRolePublicationTarget{
			Name: "tenant", Namespace: "dbs", UID: "database-uid", Generation: 4,
		},
		Databases: []Database{{
			Name: "payments",
			Roles: []Role{
				{Name: "payments_admin", SecretName: "tenant-payments-admin"},
				{Name: "payments_rw", SecretName: "tenant-payments-rw"},
			},
		}},
	}
}

type testContractStep struct {
	name     string
	provides []dbpipeline.ContractKey
}

func (s *testContractStep) Name() string                       { return s.name }
func (s *testContractStep) Requires() []dbpipeline.ContractKey { return nil }
func (s *testContractStep) Provides() []dbpipeline.ContractKey { return s.provides }
func (s *testContractStep) Observe(context.Context, *dbpipeline.Contracts, error) (reconciliationTypes.Outcome, error) {
	return reconciliationTypes.Deferred(time.Second), nil
}
