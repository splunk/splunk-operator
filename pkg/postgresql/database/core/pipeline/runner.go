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
	"fmt"
	"time"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
)

const runtimeDependencyRequeueAfter = 15 * time.Second

// HandleStatus applies or persists the status represented by an outcome.
type HandleStatus func(context.Context, reconciliationTypes.Outcome) error

// Run executes steps in order using fresh contracts for this pass.
func Run(ctx context.Context, steps []Step, handleStatus HandleStatus) (reconciliationTypes.Outcome, error) {
	if err := ValidateStepOrder(steps); err != nil {
		return reconciliationTypes.Outcome{}, err
	}

	contracts := NewContracts()
	statusDirty := false
	runtimeIncomplete := false
	var deferredOutcome reconciliationTypes.Outcome
	hasDeferredOutcome := false
	for _, s := range steps {
		if missing := missingContracts(s.Requires(), contracts); len(missing) > 0 {
			// Missing runtime contracts skip only the dependent step; the pass
			// remains incomplete so it cannot finish as Ready.
			runtimeIncomplete = true
			continue
		}

		mutating, isMutatingStep := s.(MutatingStep)

		var mutationErr error
		if isMutatingStep {
			mutationErr = mutating.Reconcile(ctx, contracts)
		}

		outcome, err := s.Observe(ctx, contracts, mutationErr)
		if err != nil {
			return outcome, fmt.Errorf("%s observe: %w", s.Name(), err)
		}
		if err := outcome.Validate(s.Name()); err != nil {
			return outcome, err
		}

		switch outcome.Mode() {
		case reconciliationTypes.ModeConverged:
			if err := validateProvidedContracts(s, contracts); err != nil {
				return outcome, err
			}
			if runtimeIncomplete && outcome.StatusAction() == reconciliationTypes.StatusPersistAndStop {
				return outcome, fmt.Errorf("%s: final converged status cannot be persisted after incomplete runtime dependencies", s.Name())
			}
			if err := handleStatusOutcome(ctx, handleStatus, s.Name(), outcome); err != nil {
				return outcome, err
			}
			statusDirty = trackStatusDirty(statusDirty, outcome.StatusAction())
			if outcome.StatusAction() == reconciliationTypes.StatusPersistAndStop {
				return outcome, nil
			}
			continue
		case reconciliationTypes.ModeDeferred:
			runtimeIncomplete = true
			deferredOutcome, hasDeferredOutcome = selectDeferredOutcome(deferredOutcome, hasDeferredOutcome, outcome)
			continue
		case reconciliationTypes.ModeSilentStop:
			if statusDirty {
				return outcome, unflushedStatusError(s.Name())
			}
			return outcome, nil
		case reconciliationTypes.ModeWaiting, reconciliationTypes.ModeRetryableRequeue, reconciliationTypes.ModeTerminalError:
			if err := handleStatusOutcome(ctx, handleStatus, s.Name(), outcome); err != nil {
				return outcome, err
			}
			statusDirty = trackStatusDirty(statusDirty, outcome.StatusAction())
			if statusDirty {
				return outcome, unflushedStatusError(s.Name())
			}
			return outcome, outcome.Err()
		default:
			return outcome, fmt.Errorf("%s: unknown outcome mode %q", s.Name(), outcome.Mode())
		}
	}

	if statusDirty {
		return reconciliationTypes.Converged(), unflushedStatusError("pipeline")
	}
	if runtimeIncomplete {
		if hasDeferredOutcome {
			return deferredOutcome, nil
		}
		return runtimeDependenciesWaiting(), nil
	}
	return reconciliationTypes.Converged(), nil
}

func selectDeferredOutcome(current reconciliationTypes.Outcome, hasCurrent bool, candidate reconciliationTypes.Outcome) (reconciliationTypes.Outcome, bool) {
	if !hasCurrent || candidate.Result().RequeueAfter < current.Result().RequeueAfter {
		return candidate, true
	}
	return current, true
}

func trackStatusDirty(dirty bool, action reconciliationTypes.StatusAction) bool {
	switch action {
	case reconciliationTypes.StatusApplyAndContinue:
		return true
	case reconciliationTypes.StatusPersistAndContinue, reconciliationTypes.StatusPersistAndStop:
		return false
	default:
		return dirty
	}
}

func unflushedStatusError(stepName string) error {
	return fmt.Errorf("%s: status applied in memory but not persisted", stepName)
}

func runtimeDependenciesWaiting() reconciliationTypes.Outcome {
	return reconciliationTypes.Deferred(runtimeDependencyRequeueAfter)
}

func validateProvidedContracts(s Step, contracts *Contracts) error {
	for _, provided := range s.Provides() {
		if !contracts.Has(provided) {
			return fmt.Errorf("step %q converged without providing declared contract %q", s.Name(), provided)
		}
	}
	return nil
}

func handleStatusOutcome(ctx context.Context, handleStatus HandleStatus, stepName string, outcome reconciliationTypes.Outcome) error {
	if outcome.StatusAction() == reconciliationTypes.StatusNone {
		return nil
	}
	if handleStatus == nil {
		return fmt.Errorf("%s: status action requested but status handler is nil", stepName)
	}
	if err := handleStatus(ctx, outcome); err != nil {
		return fmt.Errorf("%s status action: %w", stepName, err)
	}
	return nil
}
