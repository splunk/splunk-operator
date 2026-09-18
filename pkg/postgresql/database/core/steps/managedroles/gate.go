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

const acknowledgementStepName = "managed-role-acknowledgement"

var (
	errAcknowledgementReaderNotConfigured = errors.New("managed-role acknowledgement reader is not configured")
	errInvalidGateInput                   = errors.New("invalid managed-role acknowledgement input")
)

// AcknowledgementReader obtains the cluster-owned managed-role result.
type AcknowledgementReader interface {
	Read(context.Context, dbtypes.ManagedRoleAcknowledgementTarget) (dbtypes.ManagedRoleAcknowledgement, error)
}

// GateInput contains the current participant and desired roles.
type GateInput struct {
	Target    dbtypes.ManagedRoleAcknowledgementTarget
	Owner     dbtypes.ManagedRoleParticipant
	Databases []Database
}

// AcknowledgementGate unlocks downstream database provisioning only after the
// PostgresCluster acknowledgement confirms every current role and owner.
type AcknowledgementGate struct {
	reader   AcknowledgementReader
	input    GateInput
	decision GateDecision
}

// NewAcknowledgementGate returns a dormant managed-role observation unit.
func NewAcknowledgementGate(reader AcknowledgementReader, input GateInput) *AcknowledgementGate {
	return &AcknowledgementGate{reader: reader, input: input}
}

func (g *AcknowledgementGate) Name() string { return acknowledgementStepName }

func (g *AcknowledgementGate) Requires() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{
		dbpipeline.ContractDatabaseManagedRoleIntentPublished,
		dbpipeline.ContractDatabaseConnectionMetadataReady,
	}
}

func (g *AcknowledgementGate) Provides() []dbpipeline.ContractKey {
	return []dbpipeline.ContractKey{dbpipeline.ContractDatabaseManagedRolesReady}
}

func (g *AcknowledgementGate) Observe(
	ctx context.Context,
	contracts *dbpipeline.Contracts,
	_ error,
) (reconciliationTypes.Outcome, error) {
	if err := validateGateInput(g.input); err != nil {
		return reconciliationTypes.RetryableError(err), nil
	}
	if g.reader == nil {
		return reconciliationTypes.RetryableError(errAcknowledgementReaderNotConfigured), nil
	}
	acknowledgement, err := g.reader.Read(ctx, g.input.Target)
	if err != nil {
		return reconciliationTypes.RetryableError(err), nil
	}

	decision := evaluateAcknowledgement(g.input.Databases, g.input.Owner, acknowledgement)
	if decision.State != GateProceed {
		decision.DatabaseMessages = databaseMessages(g.input.Databases, decision)
	}
	g.decision = cloneDecision(decision)

	switch decision.State {
	case GateConflict:
		return reconciliationTypes.Waiting(
			string(reconciliationTypes.ConditionRolesReady),
			string(reconciliationTypes.ReasonRoleConflict),
			fmt.Sprintf("Role conflict in PostgresDatabase %s: %s", g.input.Owner.Name, decision.Message),
			string(reconciliationTypes.PhaseFailed),
			reconciliationTypes.ReadinessRetryDelay,
		), nil
	case GateFailed:
		return reconciliationTypes.Waiting(
			string(reconciliationTypes.ConditionRolesReady),
			string(reconciliationTypes.ReasonRoleReconcileFailed),
			fmt.Sprintf("Role reconciliation failed for PostgresDatabase %s: %s", g.input.Owner.Name, decision.Message),
			string(reconciliationTypes.PhaseFailed),
			reconciliationTypes.ReadinessRetryDelay,
		), nil
	case GatePending:
		return reconciliationTypes.Waiting(
			string(reconciliationTypes.ConditionRolesReady),
			string(reconciliationTypes.ReasonWaitingForCNPG),
			decision.Message,
			string(reconciliationTypes.PhaseProvisioning),
			reconciliationTypes.ReadinessRetryDelay,
		), nil
	case GateProceed:
		contracts.ManagedRolesReady = &dbpipeline.ManagedRolesReadyContract{}
		return reconciliationTypes.ConvergedStatus(
			string(reconciliationTypes.ConditionRolesReady),
			string(reconciliationTypes.ReasonRolesAvailable),
			fmt.Sprintf("Roles reconciled: %d active", roleCount(g.input.Databases)),
			string(reconciliationTypes.PhaseProvisioning),
		), nil
	default:
		return reconciliationTypes.RetryableError(fmt.Errorf("invalid managed-role gate state %q", decision.State)), nil
	}
}

// Decision returns a copy of the last authoritative acknowledgement decision.
func (g *AcknowledgementGate) Decision() GateDecision {
	return cloneDecision(g.decision)
}

func validateGateInput(input GateInput) error {
	if input.Target.Namespace == "" || input.Target.ClusterName == "" || input.Owner.Name == "" || input.Owner.UID == "" {
		return fmt.Errorf("%w: target and participant identity are required", errInvalidGateInput)
	}
	if len(input.Databases) == 0 {
		return fmt.Errorf("%w: at least one database is required", errInvalidGateInput)
	}
	for _, database := range input.Databases {
		if database.Name == "" || len(database.Roles) == 0 {
			return fmt.Errorf("%w: database and role identities are required", errInvalidGateInput)
		}
		for _, role := range database.Roles {
			if role.Name == "" {
				return fmt.Errorf("%w: role identity is required for database %q", errInvalidGateInput, database.Name)
			}
		}
	}
	return nil
}

func cloneDecision(decision GateDecision) GateDecision {
	result := decision
	if decision.DatabaseMessages != nil {
		result.DatabaseMessages = make(map[string]string, len(decision.DatabaseMessages))
		for database, message := range decision.DatabaseMessages {
			result.DatabaseMessages[database] = message
		}
	}
	return result
}

var _ dbpipeline.Step = (*AcknowledgementGate)(nil)
