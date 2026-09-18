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

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
)

// generatedCredentialStrategy owns generated Secret creation and ownership
// continuity. All Secret mutations are contained in this strategy.
type generatedCredentialStrategy struct {
	operations SecretOperations
	owner      OwnerIdentity
}

func (s generatedCredentialStrategy) Reconcile(ctx context.Context, intent Intent) Result {
	facts, err := s.operations.Read(ctx, intent.Ref)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return s.reconcileMissing(ctx, intent)
		}
		return retryable(ReasonSecretsCreationFailed,
			fmt.Sprintf("failed to read Secret %s: %v", intent.Ref.Name, err), err)
	}
	return s.reconcileObserved(ctx, intent, facts)
}

func (s generatedCredentialStrategy) reconcileMissing(ctx context.Context, intent Intent) Result {
	if intent.Continuity == ContinuityPublished {
		return drift(ReasonManagedSecretMissing,
			fmt.Sprintf("Managed Secret %s is missing for previously provisioned role %s; restore the Secret with the original credential data", intent.Ref.Name, intent.Role))
	}
	if err := s.operations.CreateGenerated(ctx, intent.Ref, intent.Role); err != nil {
		if errors.Is(err, ErrSecretAlreadyExists) {
			return s.reconcileCreateRace(ctx, intent, err)
		}
		return retryable(ReasonSecretsCreationFailed,
			fmt.Sprintf("failed to create Secret %s: %v", intent.Ref.Name, err), err)
	}
	return Result{Outcome: reconciliationTypes.Converged()}
}

func (s generatedCredentialStrategy) reconcileCreateRace(ctx context.Context, intent Intent, createErr error) Result {
	facts, err := s.operations.Read(ctx, intent.Ref)
	if err != nil {
		return retryable(ReasonSecretsCreationFailed,
			fmt.Sprintf("Secret %s was created concurrently but could not be read: %v", intent.Ref.Name, err),
			errors.Join(createErr, err))
	}
	return s.reconcileObserved(ctx, intent, facts)
}

func (s generatedCredentialStrategy) reconcileObserved(ctx context.Context, intent Intent, facts ObservedSecret) Result {
	retainedReadopted := facts.RetainedFrom != "" && facts.RetainedFrom == s.owner.Name
	// Retained-resource adoption takes precedence over an existing owner
	// reference. A retained Secret may still carry the previous controller
	// reference, and Adopt removes the retention marker before restoring
	// ownership so a later Retain deletion cannot garbage-collect it.
	if retainedReadopted {
		if err := s.operations.Adopt(ctx, intent.Ref, facts.ResourceVersion); err != nil {
			if errors.Is(err, ErrSecretConflict) {
				return deferOnConflict()
			}
			return retryable(ReasonSecretsCreationFailed,
				fmt.Sprintf("failed to adopt Secret %s: %v", intent.Ref.Name, err), err)
		}
		return Result{Outcome: reconciliationTypes.Converged(), RetainedReadopted: true}
	}
	if sameOwner(facts.Controller, s.owner) {
		return Result{Outcome: reconciliationTypes.Converged()}
	}
	if facts.Controller != nil {
		message := fmt.Sprintf("Managed Secret %s is controlled by %s; remove the conflicting owner or restore operator ownership", intent.Ref.Name, describeOwner(*facts.Controller))
		return drift(ReasonManagedSecretOwnershipConflict, message)
	}

	// An unowned Secret is adopted to preserve the established database behavior
	// for interrupted earlier reconciles. Existing Secret data is not changed.
	if err := s.operations.Adopt(ctx, intent.Ref, facts.ResourceVersion); err != nil {
		if errors.Is(err, ErrSecretConflict) {
			return deferOnConflict()
		}
		return retryable(ReasonSecretsCreationFailed,
			fmt.Sprintf("failed to adopt Secret %s: %v", intent.Ref.Name, err), err)
	}
	return Result{Outcome: reconciliationTypes.Converged()}
}

func sameOwner(actual *OwnerIdentity, expected OwnerIdentity) bool {
	return actual != nil && actual.Name == expected.Name && actual.UID == expected.UID
}

func describeOwner(owner OwnerIdentity) string {
	if owner.Kind == "" {
		return owner.Name
	}
	return fmt.Sprintf("%s %s", owner.Kind, owner.Name)
}
