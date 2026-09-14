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
package pipeline

import (
	"context"
	"testing"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	"github.com/stretchr/testify/require"
)

type privilegeBootstrapUseCase struct {
	events *[]string
}

func (u privilegeBootstrapUseCase) Prerequisites(_ context.Context, contracts *Contracts) error {
	*u.events = append(*u.events, "rw-privileges-prerequisites")
	if !contracts.Has(ContractDatabaseCNPGDatabasesReady) {
		return ErrPrerequisiteNotReady
	}
	return nil
}

func (u privilegeBootstrapUseCase) Schedule(context.Context, *Contracts) (bool, error) {
	*u.events = append(*u.events, "rw-privileges-schedule")
	return true, nil
}

func (u privilegeBootstrapUseCase) Act(_ context.Context, contracts *Contracts) (reconciliationTypes.Outcome, error) {
	*u.events = append(*u.events, "rw-privileges-act")
	contracts.RWPrivilegesReady = &RWPrivilegesReadyContract{}
	return reconciliationTypes.ConvergedApply("PrivilegesReady", "Granted", "RW role privileges granted", "Ready"), nil
}

func TestRun_DatabaseOrderAllowsPrivilegeBootstrapBetweenSteadyStateSteps(t *testing.T) {
	var events []string

	databases := &fakeStep{
		name:     "cnpg-databases",
		provides: []ContractKey{ContractDatabaseCNPGDatabasesReady},
		observeFunc: func(c *Contracts, _ error) (reconciliationTypes.Outcome, error) {
			events = append(events, "cnpg-databases")
			c.CNPGDatabasesReady = &CNPGDatabasesReadyContract{}
			return reconciliationTypes.Converged(), nil
		},
	}
	privileges := NewUseCaseStep(
		"rw-privilege-bootstrap",
		privilegeBootstrapUseCase{events: &events},
		[]ContractKey{ContractDatabaseCNPGDatabasesReady},
		[]ContractKey{ContractDatabaseRWPrivilegesReady},
	)
	customMetrics := &fakeStep{
		name:     "custom-metrics",
		requires: []ContractKey{ContractDatabaseRWPrivilegesReady},
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			events = append(events, "custom-metrics")
			return reconciliationTypes.ConvergedApply("CustomMetricsReady", "Ready", "custom metrics acknowledged", "Ready"), nil
		},
	}
	readyFlush := &fakeStep{
		name:     "ready-flush",
		requires: []ContractKey{ContractDatabaseRWPrivilegesReady},
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			events = append(events, "ready-flush")
			return reconciliationTypes.ConvergedFlush("Ready", "AllConverged", "database is ready", "Ready"), nil
		},
	}

	var statusActions []reconciliationTypes.Outcome
	outcome, err := Run(
		context.Background(),
		[]Step{databases, privileges, customMetrics, readyFlush},
		func(_ context.Context, o reconciliationTypes.Outcome) error {
			statusActions = append(statusActions, o)
			return nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	require.Equal(t, reconciliationTypes.StatusPersistAndStop, outcome.StatusAction())
	require.Equal(t, []string{
		"cnpg-databases",
		"rw-privileges-prerequisites",
		"rw-privileges-schedule",
		"rw-privileges-act",
		"custom-metrics",
		"ready-flush",
	}, events)
	require.Len(t, statusActions, 3)
	require.Equal(t, "PrivilegesReady", statusActions[0].Condition())
	require.Equal(t, reconciliationTypes.StatusApplyAndContinue, statusActions[0].StatusAction())
	require.Equal(t, "CustomMetricsReady", statusActions[1].Condition())
	require.Equal(t, reconciliationTypes.StatusApplyAndContinue, statusActions[1].StatusAction())
	require.Equal(t, "Ready", statusActions[2].Condition())
	require.Equal(t, reconciliationTypes.StatusPersistAndStop, statusActions[2].StatusAction())
}
