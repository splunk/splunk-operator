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

package connectionmetadata

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	dbpipeline "github.com/splunk/splunk-operator/pkg/postgresql/database/core/pipeline"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type endpointResolverFunc func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error)

func (f endpointResolverFunc) Resolve(ctx context.Context, request dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
	return f(ctx, request)
}

type publisherFunc func(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error

func (f publisherFunc) Apply(ctx context.Context, target dbtypes.ConnectionMetadataTarget, publication dbtypes.ConnectionMetadataPublication) error {
	return f(ctx, target, publication)
}

func TestStepReconcileBuildsPublicationsInOrder(t *testing.T) {
	request := dbtypes.ConnectionEndpointRequest{ProviderResourceName: "primary", Namespace: "dbs"}
	target := dbtypes.ConnectionMetadataTarget{Name: "tenant", Namespace: "dbs", UID: "database-uid"}
	endpoints := pgconninfo.Endpoints{
		RWHost: "primary-rw.dbs.svc.cluster.local", ROHost: "primary-ro.dbs.svc.cluster.local", RHost: "primary-r.dbs.svc.cluster.local",
	}
	var publications []dbtypes.ConnectionMetadataPublication
	step := New(
		endpointResolverFunc(func(_ context.Context, got dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
			assert.Equal(t, request, got)
			return endpoints, nil
		}),
		publisherFunc(func(_ context.Context, gotTarget dbtypes.ConnectionMetadataTarget, publication dbtypes.ConnectionMetadataPublication) error {
			assert.Equal(t, target, gotTarget)
			publications = append(publications, publication)
			return nil
		}),
		Input{
			Target: target, EndpointRequest: request,
			Databases: []Database{
				{Name: "payments", ConfigMapName: "tenant-payments-config", AdminUser: "payments_owner", RWUser: "payments_writer"},
				{Name: "audit", ConfigMapName: "tenant-audit-config", AdminUser: "audit_admin", RWUser: "audit_rw"},
			},
		},
	)

	contracts := dbpipeline.NewContracts()
	reconcileErr := step.Reconcile(t.Context(), contracts)
	outcome, observeErr := step.Observe(t.Context(), contracts, reconcileErr)

	require.NoError(t, reconcileErr)
	require.NoError(t, observeErr)
	require.Len(t, publications, 2)
	assert.Equal(t, "tenant-payments-config", publications[0].Name)
	assert.Equal(t, "payments", publications[0].Data[dbtypes.ConnectionKeyDatabaseName])
	assert.Equal(t, "payments_owner", publications[0].Data[dbtypes.ConnectionKeyAdminUser])
	assert.Equal(t, "payments_writer", publications[0].Data[dbtypes.ConnectionKeyRWUser])
	assert.Equal(t, "tenant-audit-config", publications[1].Name)
	require.NotNil(t, contracts.ConnectionMetadataReady)
	assert.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	assert.Equal(t, reconciliationTypes.StatusPersistAndContinue, outcome.StatusAction())
	assert.Equal(t, "ConfigMapsReady", outcome.Condition())
	assert.Equal(t, "True", string(outcome.ConditionStatus()))
	assert.Equal(t, "ConfigMapsCreated", outcome.Reason())
	assert.Equal(t, "Provisioning", outcome.Phase())
	assert.Equal(t, "All ConfigMaps provisioned for 2 databases", outcome.Message())
}

func TestStepRequiresCredentialsReady(t *testing.T) {
	step := New(nil, nil, Input{})
	assert.Equal(t, []dbpipeline.ContractKey{dbpipeline.ContractDatabaseCredentialsReady}, step.Requires())
}

func TestPipelineRejectsConnectionMetadataBeforeCredentialsProvider(t *testing.T) {
	step := New(nil, nil, Input{})

	_, err := dbpipeline.Run(t.Context(), []dbpipeline.Step{step}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("status handler must not run for an invalid step order")
		return nil
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, `requires contract "database.credentials.ready"`)
}

func TestStepClassifiesEndpointFailure(t *testing.T) {
	resolveErr := errors.New("write service name is required")
	step := New(endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
		return pgconninfo.Endpoints{}, resolveErr
	}), publisherFunc(func(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error {
		t.Fatal("publisher must not run")
		return nil
	}), validInput())

	contracts := dbpipeline.NewContracts()
	reconcileErr := step.Reconcile(t.Context(), contracts)
	outcome, err := step.Observe(t.Context(), contracts, reconcileErr)

	require.NoError(t, err)
	assert.ErrorIs(t, reconcileErr, resolveErr)
	assert.True(t, IsEndpointResolutionFailure(reconcileErr))
	assert.Nil(t, contracts.ConnectionMetadataReady)
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.Equal(t, "ConfigMapsReady", outcome.Condition())
	assert.Equal(t, "False", string(outcome.ConditionStatus()))
	assert.Equal(t, "ConfigMapsCreationFailed", outcome.Reason())
	assert.Equal(t, "Provisioning", outcome.Phase())
	assert.Equal(t, "Failed to resolve ConfigMap endpoints: write service name is required", outcome.Message())
}

func TestStepStopsAfterPublicationFailure(t *testing.T) {
	applyErr := errors.New("apiserver unavailable")
	var applied []string
	step := New(endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
		return pgconninfo.Endpoints{RWHost: "rw", ROHost: "ro", RHost: "r"}, nil
	}), publisherFunc(func(_ context.Context, _ dbtypes.ConnectionMetadataTarget, publication dbtypes.ConnectionMetadataPublication) error {
		applied = append(applied, publication.Name)
		if publication.Name == "second" {
			return applyErr
		}
		return nil
	}), Input{Databases: []Database{
		{Name: "one", ConfigMapName: "first", AdminUser: "one_admin", RWUser: "one_rw"},
		{Name: "two", ConfigMapName: "second", AdminUser: "two_admin", RWUser: "two_rw"},
		{Name: "three", ConfigMapName: "third", AdminUser: "three_admin", RWUser: "three_rw"},
	}})

	contracts := dbpipeline.NewContracts()
	reconcileErr := step.Reconcile(t.Context(), contracts)
	outcome, err := step.Observe(t.Context(), contracts, reconcileErr)

	require.NoError(t, err)
	assert.ErrorIs(t, reconcileErr, applyErr)
	assert.False(t, IsEndpointResolutionFailure(reconcileErr))
	assert.Equal(t, []string{"first", "second"}, applied)
	assert.Nil(t, contracts.ConnectionMetadataReady)
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.Equal(t, "Failed to reconcile ConfigMaps: apiserver unavailable", outcome.Message())
}

func TestPipelineRequeuesConnectionMetadataConflictWithoutPublishingFailureStatus(t *testing.T) {
	conflict := errors.New("write conflict")
	publicationErr := fmt.Errorf("%w: %w", dbtypes.ErrConnectionMetadataConflict, conflict)
	publishCalls := 0
	step := New(endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
		return pgconninfo.Endpoints{RWHost: "rw", ROHost: "ro", RHost: "r"}, nil
	}), publisherFunc(func(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error {
		publishCalls++
		return publicationErr
	}), Input{Databases: []Database{{
		Name: "payments", ConfigMapName: "tenant-payments-config", AdminUser: "payments_admin", RWUser: "payments_rw",
	}}})
	downstream := &managedRoleGate{}
	persistCalls := 0

	outcome, err := dbpipeline.Run(t.Context(), []dbpipeline.Step{
		&credentialsReadyProvider{ready: true},
		step,
		downstream,
	}, func(context.Context, reconciliationTypes.Outcome) error {
		persistCalls++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, reconciliationTypes.ModeImmediateRequeue, outcome.Mode())
	assert.Equal(t, reconciliationTypes.StatusNone, outcome.StatusAction())
	assert.True(t, outcome.Result().Requeue)
	assert.ErrorIs(t, outcome.Err(), publicationErr)
	assert.Equal(t, 1, publishCalls)
	assert.Zero(t, persistCalls)
	assert.False(t, downstream.called)
}

func TestStepRejectsIncompleteResolvedEndpointsBeforePublishing(t *testing.T) {
	step := New(endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
		return pgconninfo.Endpoints{RWHost: "rw", ROHost: "ro"}, nil
	}), publisherFunc(func(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error {
		t.Fatal("publisher must not run")
		return nil
	}), Input{Databases: []Database{{
		Name: "payments", ConfigMapName: "tenant-payments-config", AdminUser: "payments_admin", RWUser: "payments_rw",
	}}})

	contracts := dbpipeline.NewContracts()
	reconcileErr := step.Reconcile(t.Context(), contracts)
	outcome, err := step.Observe(t.Context(), contracts, reconcileErr)

	require.NoError(t, err)
	require.Error(t, reconcileErr)
	assert.ErrorContains(t, reconcileErr, "reconciling ConfigMap tenant-payments-config")
	assert.ErrorContains(t, reconcileErr, "building ConfigMap data for database payments")
	assert.ErrorContains(t, reconcileErr, "RHost is required")
	assert.False(t, IsEndpointResolutionFailure(reconcileErr))
	assert.Nil(t, contracts.ConnectionMetadataReady)
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.Equal(t, "ConfigMapsCreationFailed", outcome.Reason())
}

func TestStepRejectsEmptyDatabaseInputBeforeExternalCalls(t *testing.T) {
	resolveCalls := 0
	publishCalls := 0
	step := New(endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
		resolveCalls++
		return pgconninfo.Endpoints{}, nil
	}), publisherFunc(func(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error {
		publishCalls++
		return nil
	}), Input{})

	contracts := dbpipeline.NewContracts()
	reconcileErr := step.Reconcile(t.Context(), contracts)
	outcome, err := step.Observe(t.Context(), contracts, reconcileErr)

	require.NoError(t, err)
	assert.ErrorIs(t, reconcileErr, errNoDatabases)
	assert.Zero(t, resolveCalls)
	assert.Zero(t, publishCalls)
	assert.Nil(t, contracts.ConnectionMetadataReady)
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.Equal(t, "ConfigMapsCreationFailed", outcome.Reason())
}

func TestStepProviderReadFailureDoesNotChangeConfigMapStatus(t *testing.T) {
	providerErr := fmt.Errorf("%w: apiserver unavailable", dbtypes.ErrConnectionEndpointProviderRead)
	step := New(endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
		return pgconninfo.Endpoints{}, providerErr
	}), publisherFunc(func(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error {
		t.Fatal("publisher must not run")
		return nil
	}), validInput())

	downstream := &managedRoleGate{}
	statusCalls := 0
	outcome, err := dbpipeline.Run(t.Context(), []dbpipeline.Step{
		&credentialsReadyProvider{ready: true},
		step,
		downstream,
	}, func(context.Context, reconciliationTypes.Outcome) error {
		statusCalls++
		return nil
	})

	assert.ErrorIs(t, err, providerErr)
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.Equal(t, reconciliationTypes.StatusNone, outcome.StatusAction())
	assert.Empty(t, outcome.Condition())
	assert.ErrorIs(t, outcome.Err(), providerErr)
	assert.Zero(t, statusCalls)
	assert.False(t, downstream.called)
}

func TestStepReportsMissingDependencies(t *testing.T) {
	t.Run("resolver", func(t *testing.T) {
		step := New(nil, nil, validInput())
		err := step.Reconcile(t.Context(), dbpipeline.NewContracts())
		assert.ErrorIs(t, err, errEndpointResolverNotConfigured)
		assert.True(t, IsEndpointResolutionFailure(err))
	})

	t.Run("publisher", func(t *testing.T) {
		step := New(endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
			return pgconninfo.Endpoints{}, nil
		}), nil, validInput())
		err := step.Reconcile(t.Context(), dbpipeline.NewContracts())
		assert.ErrorIs(t, err, errPublisherNotConfigured)
		assert.False(t, IsEndpointResolutionFailure(err))
	})
}

func TestStepRunsBetweenCredentialsAndManagedRoleGate(t *testing.T) {
	endpoints := pgconninfo.Endpoints{RWHost: "rw", ROHost: "ro", RHost: "r"}
	step := New(
		endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
			return endpoints, nil
		}),
		publisherFunc(func(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error {
			return nil
		}),
		validInput(),
	)
	consumer := &managedRoleGate{}
	provider := &credentialsReadyProvider{ready: true}

	_, err := dbpipeline.Run(t.Context(), []dbpipeline.Step{provider, step, consumer}, func(context.Context, reconciliationTypes.Outcome) error {
		return nil
	})

	require.NoError(t, err)
	assert.True(t, consumer.called)
}

func validInput() Input {
	return Input{Databases: []Database{{
		Name: "payments", ConfigMapName: "tenant-payments-config", AdminUser: "payments_admin", RWUser: "payments_rw",
	}}}
}

type managedRoleGate struct {
	called bool
}

func (c *managedRoleGate) Name() string { return "managed-roles" }

func (c *managedRoleGate) Requires() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{dbpipeline.ContractDatabaseConnectionMetadataReady}
}

func (c *managedRoleGate) Provides() []dbpipeline.ContractKey { return nil }

func (c *managedRoleGate) Observe(
	_ context.Context,
	contracts *dbpipeline.Contracts,
	_ error,
) (reconciliationTypes.Outcome, error) {
	c.called = contracts.ConnectionMetadataReady != nil
	return reconciliationTypes.Converged(), nil
}

func TestStepDoesNotRunUntilCredentialsAreReady(t *testing.T) {
	resolveCalls := 0
	publishCalls := 0
	step := New(
		endpointResolverFunc(func(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
			resolveCalls++
			return pgconninfo.Endpoints{}, nil
		}),
		publisherFunc(func(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error {
			publishCalls++
			return nil
		}),
		Input{},
	)

	outcome, err := dbpipeline.Run(t.Context(), []dbpipeline.Step{
		&credentialsReadyProvider{ready: false},
		step,
	}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("status must not be persisted while the prerequisite is deferred")
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, reconciliationTypes.ModeDeferred, outcome.Mode())
	assert.Zero(t, resolveCalls)
	assert.Zero(t, publishCalls)
}

type credentialsReadyProvider struct {
	ready bool
}

func (p *credentialsReadyProvider) Name() string { return "credentials" }
func (p *credentialsReadyProvider) Requires() []dbpipeline.ContractKey {
	return nil
}
func (p *credentialsReadyProvider) Provides() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{dbpipeline.ContractDatabaseCredentialsReady}
}
func (p *credentialsReadyProvider) Observe(
	_ context.Context,
	contracts *dbpipeline.Contracts,
	_ error,
) (reconciliationTypes.Outcome, error) {
	if !p.ready {
		return reconciliationTypes.Deferred(time.Second), nil
	}
	contracts.CredentialsReady = &dbpipeline.CredentialsReadyContract{}
	return reconciliationTypes.Converged(), nil
}
