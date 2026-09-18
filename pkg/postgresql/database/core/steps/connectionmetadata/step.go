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

	dbpipeline "github.com/splunk/splunk-operator/pkg/postgresql/database/core/pipeline"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
)

const (
	stepName                 = "connection-metadata"
	conditionConfigMapsReady = "ConfigMapsReady"
	reasonConfigMapsCreated  = "ConfigMapsCreated"
	reasonConfigMapsFailed   = "ConfigMapsCreationFailed"
	phaseProvisioning        = "Provisioning"
)

var (
	errEndpointResolverNotConfigured = errors.New("connection endpoint resolver is not configured")
	errPublisherNotConfigured        = errors.New("connection metadata publisher is not configured")
	errNoDatabases                   = errors.New("connection metadata requires at least one database")
)

// EndpointResolver translates provider facts into the shared connection
// endpoint contract.
type EndpointResolver interface {
	Resolve(context.Context, dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error)
}

// Publisher applies one desired connection ConfigMap.
type Publisher interface {
	Apply(context.Context, dbtypes.ConnectionMetadataTarget, dbtypes.ConnectionMetadataPublication) error
}

// Database identifies one logical database and the role names published to its
// consumers. ConfigMapName is the desired generated ConfigMap name.
type Database struct {
	Name          string
	ConfigMapName string
	AdminUser     string
	RWUser        string
}

// Input contains the committed facts needed for one connection-metadata pass.
type Input struct {
	Target          dbtypes.ConnectionMetadataTarget
	EndpointRequest dbtypes.ConnectionEndpointRequest
	Databases       []Database
}

type failureKind string

const (
	failureEndpoint    failureKind = "endpoint"
	failurePublication failureKind = "publication"
)

type stepError struct {
	kind failureKind
	err  error
}

func (e *stepError) Error() string { return e.err.Error() }
func (e *stepError) Unwrap() error { return e.err }

// IsEndpointResolutionFailure reports whether err was produced while resolving
// endpoints rather than while building or applying a ConfigMap.
func IsEndpointResolutionFailure(err error) bool {
	var stepErr *stepError
	return errors.As(err, &stepErr) && stepErr.kind == failureEndpoint
}

// Step reconciles one connection-metadata unit.
type Step struct {
	resolver  EndpointResolver
	publisher Publisher
	input     Input
}

// New returns a dormant connection-metadata step ready for later pipeline
// composition.
func New(resolver EndpointResolver, publisher Publisher, input Input) *Step {
	return &Step{resolver: resolver, publisher: publisher, input: input}
}

func (s *Step) Name() string { return stepName }

func (s *Step) Requires() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{
		dbpipeline.ContractDatabaseCredentialsReady,
		dbpipeline.ContractDatabaseManagedRoleIntentPublished,
	}
}

func (s *Step) Provides() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{dbpipeline.ContractDatabaseConnectionMetadataReady}
}

// Reconcile resolves endpoints and applies each database publication in spec
// order. Earlier successful publications remain valid if a later one fails.
func (s *Step) Reconcile(ctx context.Context, _ *dbpipeline.Contracts) error {
	if len(s.input.Databases) == 0 {
		return &stepError{kind: failurePublication, err: errNoDatabases}
	}
	if s.resolver == nil {
		return &stepError{kind: failureEndpoint, err: errEndpointResolverNotConfigured}
	}
	endpoints, err := s.resolver.Resolve(ctx, s.input.EndpointRequest)
	if err != nil {
		return &stepError{kind: failureEndpoint, err: err}
	}
	if s.publisher == nil {
		return &stepError{kind: failurePublication, err: errPublisherNotConfigured}
	}

	for _, database := range s.input.Databases {
		data, _, err := pgconninfo.BuildConfigMapData(endpoints, withDatabaseIdentity(database))
		if err != nil {
			return &stepError{kind: failurePublication, err: fmt.Errorf(
				"reconciling ConfigMap %s: building ConfigMap data for database %s: %w",
				database.ConfigMapName, database.Name, err,
			)}
		}
		err = s.publisher.Apply(ctx, s.input.Target, dbtypes.ConnectionMetadataPublication{
			Name: database.ConfigMapName,
			Data: data,
		})
		if err != nil {
			return &stepError{kind: failurePublication, err: err}
		}
	}
	return nil
}

// Observe classifies the mutation and publishes readiness only after every
// desired ConfigMap converges.
func (s *Step) Observe(_ context.Context, contracts *dbpipeline.Contracts, reconcileErr error) (reconciliationTypes.Outcome, error) {
	if reconcileErr == nil {
		contracts.ConnectionMetadataReady = &dbpipeline.ConnectionMetadataReadyContract{}
		return reconciliationTypes.ConvergedStatus(
			conditionConfigMapsReady,
			reasonConfigMapsCreated,
			fmt.Sprintf("All ConfigMaps provisioned for %d databases", len(s.input.Databases)),
			phaseProvisioning,
		), nil
	}
	if errors.Is(reconcileErr, dbtypes.ErrConnectionMetadataConflict) {
		return reconciliationTypes.ImmediateRequeue(reconcileErr), nil
	}
	if errors.Is(reconcileErr, dbtypes.ErrConnectionEndpointProviderRead) {
		return reconciliationTypes.RetryableError(reconcileErr), nil
	}

	message := fmt.Sprintf("Failed to reconcile ConfigMaps: %v", reconcileErr)
	if IsEndpointResolutionFailure(reconcileErr) {
		message = fmt.Sprintf("Failed to resolve ConfigMap endpoints: %v", reconcileErr)
	}
	return reconciliationTypes.RetryableRequeue(
		conditionConfigMapsReady,
		reasonConfigMapsFailed,
		message,
		phaseProvisioning,
		reconcileErr,
	), nil
}

func withDatabaseIdentity(database Database) pgconninfo.Option {
	return func(builder *pgconninfo.Builder) {
		builder.SetRequired(dbtypes.ConnectionKeyDatabaseName, database.Name)
		builder.SetRequired(dbtypes.ConnectionKeyAdminUser, database.AdminUser)
		builder.SetRequired(dbtypes.ConnectionKeyRWUser, database.RWUser)
	}
}

var _ dbpipeline.MutatingStep = (*Step)(nil)
