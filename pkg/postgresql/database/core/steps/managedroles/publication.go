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

	dbpipeline "github.com/splunk/splunk-operator/pkg/postgresql/database/core/pipeline"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
)

const publicationStepName = "managed-role-intent-publication"

var (
	errIntentPublisherNotConfigured = errors.New("managed-role intent publisher is not configured")
	errInvalidPublicationInput      = errors.New("invalid managed-role publication input")
)

// IntentPublisher commits credential-ready role intent to database status.
type IntentPublisher interface {
	Publish(context.Context, dbtypes.ManagedRolePublication) error
}

// Role contains an already-resolved role and credential Secret identity.
type Role struct {
	Name       string
	SecretName string
}

// Database groups the roles managed for one logical database.
type Database struct {
	Name  string
	Roles []Role
}

// PublicationInput contains the credential-ready facts needed to publish role
// intent. PreviouslyPublished supplies removal tombstones for live spec edits.
type PublicationInput struct {
	Target              dbtypes.ManagedRolePublicationTarget
	Databases           []Database
	PreviouslyPublished []dbtypes.DatabaseRoleIntent
}

// PublicationStep commits role intent before connection metadata is produced.
type PublicationStep struct {
	publisher   IntentPublisher
	input       PublicationInput
	publication dbtypes.ManagedRolePublication
}

// NewPublicationStep returns a dormant role-intent publication unit.
func NewPublicationStep(publisher IntentPublisher, input PublicationInput) *PublicationStep {
	return &PublicationStep{publisher: publisher, input: input}
}

func (s *PublicationStep) Name() string { return publicationStepName }

func (s *PublicationStep) Requires() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{dbpipeline.ContractDatabaseCredentialsReady}
}

func (s *PublicationStep) Provides() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{dbpipeline.ContractDatabaseManagedRoleIntentPublished}
}

func (s *PublicationStep) Reconcile(ctx context.Context, _ *dbpipeline.Contracts) error {
	publication, err := buildPublication(s.input)
	if err != nil {
		return err
	}
	if s.publisher == nil {
		return errIntentPublisherNotConfigured
	}
	if err := s.publisher.Publish(ctx, publication); err != nil {
		return err
	}
	s.publication = clonePublication(publication)
	return nil
}

func (s *PublicationStep) Observe(
	_ context.Context,
	contracts *dbpipeline.Contracts,
	reconcileErr error,
) (reconciliationTypes.Outcome, error) {
	if reconcileErr == nil {
		contracts.ManagedRoleIntentPublished = &dbpipeline.ManagedRoleIntentPublishedContract{}
		return reconciliationTypes.Converged(), nil
	}
	if errors.Is(reconcileErr, dbtypes.ErrManagedRoleIntentConflict) {
		return reconciliationTypes.ImmediateRequeue(reconcileErr), nil
	}
	return reconciliationTypes.RetryableError(reconcileErr), nil
}

// Publication returns a copy of the last successfully committed intent.
func (s *PublicationStep) Publication() dbtypes.ManagedRolePublication {
	return clonePublication(s.publication)
}

func buildPublication(input PublicationInput) (dbtypes.ManagedRolePublication, error) {
	if input.Target.Name == "" || input.Target.Namespace == "" || input.Target.UID == "" || input.Target.Generation <= 0 {
		return dbtypes.ManagedRolePublication{}, fmt.Errorf("%w: target identity and generation are required", errInvalidPublicationInput)
	}
	if len(input.Databases) == 0 {
		return dbtypes.ManagedRolePublication{}, fmt.Errorf("%w: at least one database is required", errInvalidPublicationInput)
	}

	publication := dbtypes.ManagedRolePublication{Target: input.Target}
	desiredRoles := make(map[string]map[string]struct{}, len(input.Databases))
	publicationIndexes := make(map[string]int, len(input.Databases))
	for _, database := range input.Databases {
		if database.Name == "" || len(database.Roles) == 0 {
			return dbtypes.ManagedRolePublication{}, fmt.Errorf("%w: database and role identities are required", errInvalidPublicationInput)
		}
		if _, duplicate := publicationIndexes[database.Name]; duplicate {
			return dbtypes.ManagedRolePublication{}, fmt.Errorf("%w: duplicate database %q", errInvalidPublicationInput, database.Name)
		}
		desiredRoles[database.Name] = make(map[string]struct{}, len(database.Roles))

		intent := dbtypes.DatabaseRoleIntent{Database: database.Name, Roles: make([]dbtypes.ManagedRoleIntent, 0, len(database.Roles))}
		for _, role := range database.Roles {
			if role.Name == "" || role.SecretName == "" {
				return dbtypes.ManagedRolePublication{}, fmt.Errorf("%w: role and Secret identities are required for database %q", errInvalidPublicationInput, database.Name)
			}
			intent.Roles = append(intent.Roles, dbtypes.ManagedRoleIntent{
				Name: role.Name, SecretName: role.SecretName, Exists: true,
			})
			desiredRoles[database.Name][role.Name] = struct{}{}
		}
		publicationIndexes[database.Name] = len(publication.Databases)
		publication.Databases = append(publication.Databases, intent)
	}

	for _, previous := range input.PreviouslyPublished {
		if len(previous.Roles) == 0 {
			continue
		}
		if index, stillDesired := publicationIndexes[previous.Database]; stillDesired {
			for _, role := range previous.Roles {
				if _, stillDesired := desiredRoles[previous.Database][role.Name]; stillDesired {
					continue
				}
				publication.Databases[index].Roles = append(publication.Databases[index].Roles, dbtypes.ManagedRoleIntent{
					Name: role.Name, SecretName: role.SecretName, Exists: false,
				})
			}
			continue
		}
		tombstone := dbtypes.DatabaseRoleIntent{Database: previous.Database, Roles: make([]dbtypes.ManagedRoleIntent, 0, len(previous.Roles))}
		for _, role := range previous.Roles {
			tombstone.Roles = append(tombstone.Roles, dbtypes.ManagedRoleIntent{
				Name: role.Name, SecretName: role.SecretName, Exists: false,
			})
		}
		publication.Databases = append(publication.Databases, tombstone)
	}
	return publication, nil
}

func clonePublication(publication dbtypes.ManagedRolePublication) dbtypes.ManagedRolePublication {
	result := publication
	result.Databases = make([]dbtypes.DatabaseRoleIntent, len(publication.Databases))
	for i := range publication.Databases {
		result.Databases[i] = publication.Databases[i]
		result.Databases[i].Roles = append([]dbtypes.ManagedRoleIntent(nil), publication.Databases[i].Roles...)
	}
	return result
}

var _ dbpipeline.MutatingStep = (*PublicationStep)(nil)
