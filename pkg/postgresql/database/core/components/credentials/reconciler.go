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
package credentials

import (
	"context"
	"errors"
	"fmt"
	"strings"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
)

// Reconciler applies credential policy through SecretOperations. It does not
// write PostgresDatabase status or emit events; those transition-aware concerns
// remain with the reconciliation facade.
type Reconciler struct {
	external  credentialStrategy
	generated credentialStrategy
}

// New returns a credential policy reconciler with a required operations port.
// A facade must handle construction failure before entering reconciliation so a
// wiring defect never becomes customer-visible Secret status.
func New(operations SecretOperations) (Reconciler, error) {
	if operations == nil {
		return Reconciler{}, errors.New("credential Secret operations are not configured")
	}
	owner := operations.OwnerIdentity()
	if owner.Name == "" || owner.UID == "" {
		return Reconciler{}, errors.New("credential Secret owner name and UID are not configured")
	}
	return Reconciler{
		external:  externalCredentialStrategy{reader: operations},
		generated: generatedCredentialStrategy{operations: operations, owner: owner},
	}, nil
}

// Reconcile evaluates all credentials for one database. It deliberately visits
// both admin and RW credentials before returning a decision so a pair cannot be
// left half-observed by the first deterministic failure.
func (r Reconciler) Reconcile(ctx context.Context, intents []Intent) Result {
	if failures := validateIntents(intents); len(failures) > 0 {
		return selectFailure(failures)
	}
	if r.external == nil || r.generated == nil {
		cause := errors.New("credential Secret operations are not configured")
		return retryable(ReasonSecretOperationsNotConfigured, cause.Error(), cause)
	}

	failures := make([]Result, 0, len(intents))
	retainedReadopted := false
	for _, intent := range intents {
		result := r.strategyFor(intent.Source).Reconcile(ctx, intent)
		retainedReadopted = retainedReadopted || result.RetainedReadopted
		if result.Outcome.Mode() != reconciliationTypes.ModeConverged {
			failures = append(failures, result)
		}
	}
	if len(failures) == 0 {
		return Result{Outcome: reconciliationTypes.Converged(), RetainedReadopted: retainedReadopted}
	}
	selected := selectFailure(failures)
	selected.RetainedReadopted = selected.RetainedReadopted || retainedReadopted
	return selected
}

func (r Reconciler) strategyFor(source Source) credentialStrategy {
	switch source {
	case SourceExternal:
		return r.external
	case SourceGenerated:
		return r.generated
	default:
		return nil
	}
}

// Ready returns the status-bearing success result for the future facade after
// every database credential has converged. CPI-2165 will consume this result
// when it links the dormant component into the production lifecycle.
func Ready(databaseCount int) Result {
	return Result{Outcome: reconciliationTypes.ConvergedStatus(
		ConditionSecretsReady,
		ReasonSecretsCreated,
		fmt.Sprintf("All secrets provisioned for %d databases", databaseCount),
		PhaseProvisioning,
	)}
}

func validateIntents(intents []Intent) []Result {
	if len(intents) == 0 {
		cause := errors.New("at least one credential intent is required")
		return []Result{terminal(ReasonInvalidCredentialIntent, cause.Error(), cause)}
	}

	failures := make([]Result, 0, len(intents))
	for index, intent := range intents {
		if err := validateIntent(intent); err != nil {
			reason := ReasonInvalidCredentialIntent
			if intent.Source == SourceExternal && intent.Ref.Name == "" {
				reason = ReasonExternalSecretInvalid
			}
			message := fmt.Sprintf("credential intent %d is invalid: %v", index, err)
			failures = append(failures, terminal(reason, message, fmt.Errorf("%s: %w", message, err)))
		}
	}
	return failures
}

func validateIntent(intent Intent) error {
	if intent.Ref.Name == "" {
		return errors.New("Secret reference name is empty")
	}
	if intent.Role == "" {
		return errors.New("role is empty")
	}
	switch intent.Source {
	case SourceGenerated, SourceExternal:
	default:
		return fmt.Errorf("unknown source %q", intent.Source)
	}
	switch intent.Continuity {
	case ContinuityNew, ContinuityPublished:
	default:
		return fmt.Errorf("unknown continuity %q", intent.Continuity)
	}
	return nil
}

func selectFailure(failures []Result) Result {
	selected := failures[0]
	for _, reason := range []string{ReasonExternalSecretMissing, ReasonManagedSecretMissing} {
		for _, failure := range failures {
			if failure.Outcome.Reason() == reason {
				selected = failure
				return withFailureDiagnostics(selected, failures)
			}
		}
	}
	// Preserve legacy selection semantics: classified credential policy outcomes
	// outrank transient infrastructure failures. Mode makes equal-reason policy
	// outcomes deterministic: terminal validation errors must win over waiting
	// drift regardless of intent order.
	for _, mode := range []reconciliationTypes.Mode{
		reconciliationTypes.ModeTerminalError,
		reconciliationTypes.ModeWaiting,
		reconciliationTypes.ModeRetryableRequeue,
		reconciliationTypes.ModeDeferred,
	} {
		for _, failure := range failures {
			if failure.Outcome.Mode() == mode {
				selected = failure
				return withFailureDiagnostics(selected, failures)
			}
		}
	}
	return selected
}

func withFailureDiagnostics(selected Result, failures []Result) Result {
	if len(failures) == 1 {
		return selected
	}

	messages := make([]string, 0, len(failures))
	for _, failure := range failures {
		messages = append(messages, failure.Outcome.Message())
	}
	message := strings.Join(messages, "; ")
	switch selected.Outcome.Mode() {
	case reconciliationTypes.ModeTerminalError:
		return terminal(selected.Outcome.Reason(), message, joinOutcomeErrors(failures))
	case reconciliationTypes.ModeWaiting:
		return waiting(selected.Outcome.Reason(), message)
	case reconciliationTypes.ModeRetryableRequeue:
		return retryable(selected.Outcome.Reason(), message, joinOutcomeErrors(failures))
	}
	return selected
}

func joinOutcomeErrors(failures []Result) error {
	errs := make([]error, 0, len(failures))
	for _, failure := range failures {
		if err := failure.Outcome.Err(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func drift(reason, message string) Result {
	return waiting(reason, message)
}

func waiting(reason, message string) Result {
	return Result{Outcome: reconciliationTypes.Waiting(
		ConditionSecretsReady, reason, message, PhaseProvisioning, reconciliationTypes.ReadinessRetryDelay,
	)}
}

func deferOnConflict() Result {
	return Result{Outcome: reconciliationTypes.Deferred(reconciliationTypes.ReadinessRetryDelay)}
}

func retryable(reason, message string, cause error) Result {
	return Result{Outcome: reconciliationTypes.RetryableRequeue(
		ConditionSecretsReady, reason, message, PhaseProvisioning, cause,
	)}
}

func terminal(reason, message string, cause error) Result {
	return Result{Outcome: reconciliationTypes.TerminalError(
		ConditionSecretsReady, reason, message, PhaseFailed, cause,
	)}
}
