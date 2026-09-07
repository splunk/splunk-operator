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
	"errors"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
)

// ErrPrerequisiteNotReady defers a use case without blocking the pass.
var ErrPrerequisiteNotReady = errors.New("use case prerequisite not ready")

// UseCase is a finite workflow that can be adapted into pipeline order.
type UseCase interface {
	// Prerequisites verifies the workflow can run safely.
	Prerequisites(ctx context.Context, contracts *Contracts) error
	// Schedule decides if there is work this pass.
	Schedule(ctx context.Context, contracts *Contracts) (bool, error)
	// Act advances the workflow by one step.
	// Retryable or terminal errors are classified by the returned Outcome.
	Act(ctx context.Context, contracts *Contracts) (reconciliationTypes.Outcome, error)
}

// useCaseStep adapts a workflow-owned use case into one ordered pipeline position.
type useCaseStep struct {
	name     string
	requires []ContractKey
	provides []ContractKey
	useCase  UseCase
}

// NewUseCaseStep wraps a use case so it can run between other database steps.
func NewUseCaseStep(name string, useCase UseCase, requires, provides []ContractKey) *useCaseStep {
	return &useCaseStep{name: name, useCase: useCase, requires: requires, provides: provides}
}

func (s *useCaseStep) Name() string            { return s.name }
func (s *useCaseStep) Requires() []ContractKey { return s.requires }
func (s *useCaseStep) Provides() []ContractKey { return s.provides }

// Reconcile schedules and acts when the use case has work.
func (s *useCaseStep) Reconcile(ctx context.Context, contracts *Contracts) error {
	if err := s.useCase.Prerequisites(ctx, contracts); err != nil {
		if errors.Is(err, ErrPrerequisiteNotReady) {
			contracts.setUseCaseStepResult(s, useCaseStepResult{deferred: true})
			return nil
		}
		return err
	}

	scheduled, err := s.useCase.Schedule(ctx, contracts)
	if err != nil {
		return err
	}
	if !scheduled {
		contracts.setUseCaseStepResult(s, useCaseStepResult{})
		return nil
	}

	outcome, err := s.useCase.Act(ctx, contracts)
	contracts.setUseCaseStepResult(s, useCaseStepResult{scheduled: true, outcome: outcome})
	return err
}

// Observe implements Step.Observe using the Reconcile result cached in contracts.
func (s *useCaseStep) Observe(_ context.Context, contracts *Contracts, mutationErr error) (reconciliationTypes.Outcome, error) {
	result, ok := contracts.useCaseStepResult(s)
	if ok && result.scheduled {
		if mutationErr != nil && !result.outcome.IsClassifiedError() {
			return reconciliationTypes.Outcome{}, mutationErr
		}
		return result.outcome, nil
	}
	if mutationErr != nil {
		return reconciliationTypes.Outcome{}, mutationErr
	}
	if result.deferred {
		return reconciliationTypes.Deferred(runtimeDependencyRequeueAfter), nil
	}
	return reconciliationTypes.Converged(), nil
}

var _ MutatingStep = (*useCaseStep)(nil)
