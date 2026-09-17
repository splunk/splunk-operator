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
	"testing"

	dbpipeline "github.com/splunk/splunk-operator/pkg/postgresql/database/core/pipeline"
	connectionmetadata "github.com/splunk/splunk-operator/pkg/postgresql/database/core/steps/connectionmetadata"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pipelineEndpointResolver struct {
	events *[]string
}

func (r pipelineEndpointResolver) Resolve(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
	*r.events = append(*r.events, "connection-metadata")
	return pgconninfo.Endpoints{RWHost: "rw", ROHost: "ro", RHost: "r"}, nil
}

type pipelineMetadataPublisher struct{}

func (pipelineMetadataPublisher) Apply(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error {
	return nil
}

type readyContractProvider struct {
	events *[]string
}

func (s *readyContractProvider) Name() string                       { return "credentials" }
func (s *readyContractProvider) Requires() []dbpipeline.ContractKey { return nil }
func (s *readyContractProvider) Provides() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{dbpipeline.ContractDatabaseCredentialsReady}
}
func (s *readyContractProvider) Observe(
	_ context.Context,
	contracts *dbpipeline.Contracts,
	_ error,
) (reconciliationTypes.Outcome, error) {
	*s.events = append(*s.events, "credentials")
	contracts.CredentialsReady = &dbpipeline.CredentialsReadyContract{}
	return reconciliationTypes.Converged(), nil
}

type managedRolesConsumer struct {
	called bool
	events *[]string
}

func (s *managedRolesConsumer) Name() string { return "cnpg-databases" }
func (s *managedRolesConsumer) Requires() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{dbpipeline.ContractDatabaseManagedRolesReady}
}
func (s *managedRolesConsumer) Provides() []dbpipeline.ContractKey { return nil }
func (s *managedRolesConsumer) Observe(
	_ context.Context,
	_ *dbpipeline.Contracts,
	_ error,
) (reconciliationTypes.Outcome, error) {
	s.called = true
	*s.events = append(*s.events, "cnpg-databases")
	return reconciliationTypes.Converged(), nil
}

func TestManagedRoleUnitsPreserveFacadeInterleaving(t *testing.T) {
	var events []string
	publication := NewPublicationStep(
		intentPublisherFunc(func(context.Context, dbtypes.ManagedRolePublication) error {
			events = append(events, "managed-role-intent")
			return nil
		}),
		validPublicationInput(),
	)
	metadata := connectionmetadata.New(
		pipelineEndpointResolver{events: &events},
		pipelineMetadataPublisher{},
		connectionmetadata.Input{
			Target: dbtypes.ConnectionMetadataTarget{Name: "tenant", Namespace: "dbs", UID: "database-uid"},
			Databases: []connectionmetadata.Database{{
				Name: "payments", ConfigMapName: "tenant-payments-config", AdminUser: "payments_admin", RWUser: "payments_rw",
			}},
		},
	)
	gateInput := GateInput{
		Target: dbtypes.ManagedRoleAcknowledgementTarget{Namespace: "dbs", ClusterName: "primary"},
		Owner:  dbtypes.ManagedRoleParticipant{Name: "tenant", UID: "database-uid"},
		Databases: []Database{{
			Name: "payments", Roles: []Role{{Name: "payments_admin"}, {Name: "payments_rw"}},
		}},
	}
	gate := NewAcknowledgementGate(
		acknowledgementReaderFunc(func(context.Context, dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error) {
			events = append(events, "managed-role-acknowledgement")
			return dbtypes.ManagedRoleAcknowledgement{
				Published:  true,
				Reconciled: []string{"payments_admin", "payments_rw"},
				Owners: map[string]dbtypes.ManagedRoleParticipant{
					"payments_admin": gateInput.Owner,
					"payments_rw":    gateInput.Owner,
				},
			}, nil
		}),
		gateInput,
	)
	consumer := &managedRolesConsumer{events: &events}

	outcome, err := dbpipeline.Run(
		t.Context(),
		[]dbpipeline.Step{
			&readyContractProvider{events: &events},
			publication,
			metadata,
			gate,
			consumer,
		},
		func(context.Context, reconciliationTypes.Outcome) error { return nil },
	)

	require.NoError(t, err)
	assert.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	assert.True(t, consumer.called)
	assert.Equal(t, []string{
		"credentials",
		"managed-role-intent",
		"connection-metadata",
		"managed-role-acknowledgement",
		"cnpg-databases",
	}, events)
}

func TestManagedRoleGateBlocksDownstreamUntilExactAcknowledgement(t *testing.T) {
	var events []string
	input := validGateInput()
	gate := NewAcknowledgementGate(
		acknowledgementReaderFunc(func(context.Context, dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error) {
			return dbtypes.ManagedRoleAcknowledgement{Published: true}, nil
		}),
		input,
	)
	consumer := &managedRolesConsumer{events: &events}
	prerequisites := &pipelinePrerequisites{}

	outcome, err := dbpipeline.Run(
		t.Context(),
		[]dbpipeline.Step{prerequisites, gate, consumer},
		func(context.Context, reconciliationTypes.Outcome) error { return nil },
	)

	require.NoError(t, err)
	assert.Equal(t, reconciliationTypes.ModeWaiting, outcome.Mode())
	assert.False(t, consumer.called)
}

func TestManagedRoleIntentFailureStopsMetadataAcknowledgementAndProvisioning(t *testing.T) {
	var events []string
	writeErr := errors.New("status write failed")
	publication := NewPublicationStep(
		intentPublisherFunc(func(context.Context, dbtypes.ManagedRolePublication) error {
			events = append(events, "managed-role-intent")
			return writeErr
		}),
		validPublicationInput(),
	)
	metadata := connectionmetadata.New(
		pipelineEndpointResolver{events: &events},
		pipelineMetadataPublisher{},
		connectionmetadata.Input{
			Target: dbtypes.ConnectionMetadataTarget{Name: "tenant", Namespace: "dbs", UID: "database-uid"},
			Databases: []connectionmetadata.Database{{
				Name: "payments", ConfigMapName: "tenant-payments-config", AdminUser: "payments_admin", RWUser: "payments_rw",
			}},
		},
	)
	acknowledgementReads := 0
	gate := NewAcknowledgementGate(
		acknowledgementReaderFunc(func(context.Context, dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error) {
			acknowledgementReads++
			return dbtypes.ManagedRoleAcknowledgement{}, nil
		}),
		validGateInput(),
	)
	consumer := &managedRolesConsumer{events: &events}
	statusWrites := 0

	outcome, err := dbpipeline.Run(
		t.Context(),
		[]dbpipeline.Step{
			&readyContractProvider{events: &events},
			publication,
			metadata,
			gate,
			consumer,
		},
		func(context.Context, reconciliationTypes.Outcome) error {
			statusWrites++
			return nil
		},
	)

	require.ErrorIs(t, err, writeErr)
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.Equal(t, []string{"credentials", "managed-role-intent"}, events)
	assert.Zero(t, acknowledgementReads)
	assert.False(t, consumer.called)
	assert.Zero(t, statusWrites)
}

type pipelinePrerequisites struct{}

func (s *pipelinePrerequisites) Name() string                       { return "prerequisites" }
func (s *pipelinePrerequisites) Requires() []dbpipeline.ContractKey { return nil }
func (s *pipelinePrerequisites) Provides() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{
		dbpipeline.ContractDatabaseManagedRoleIntentPublished,
		dbpipeline.ContractDatabaseConnectionMetadataReady,
	}
}
func (s *pipelinePrerequisites) Observe(
	_ context.Context,
	contracts *dbpipeline.Contracts,
	_ error,
) (reconciliationTypes.Outcome, error) {
	contracts.ManagedRoleIntentPublished = &dbpipeline.ManagedRoleIntentPublishedContract{}
	contracts.ConnectionMetadataReady = &dbpipeline.ConnectionMetadataReadyContract{}
	return reconciliationTypes.Converged(), nil
}
