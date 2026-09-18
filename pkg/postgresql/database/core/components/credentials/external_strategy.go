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

// externalCredentialStrategy reads and validates externally managed Secrets.
// It deliberately has no create or adoption operations in its policy flow.
type externalCredentialStrategy struct {
	reader SecretReader
}

func (s externalCredentialStrategy) Reconcile(ctx context.Context, intent Intent) Result {
	facts, err := s.reader.Read(ctx, intent.Ref)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			message := fmt.Sprintf("external secret %q is missing", intent.Ref.Name)
			return terminal(ReasonExternalSecretMissing, message, fmt.Errorf("%s: %w", message, err))
		}
		return retryable(ReasonSecretsCreationFailed,
			fmt.Sprintf("failed to read Secret %s: %v", intent.Ref.Name, err), err)
	}
	return validateExternal(intent, facts)
}

func validateExternal(intent Intent, facts ObservedSecret) Result {
	switch {
	case !facts.DataDefined:
		return drift(ReasonExternalSecretMissingData,
			fmt.Sprintf("external secret %q is missing data", intent.Ref.Name))
	case !facts.PasswordPresent || !facts.UsernamePresent:
		return drift(ReasonExternalSecretMissingKeys,
			fmt.Sprintf("external secret %q is missing required keys", intent.Ref.Name))
	case facts.Username != intent.Role:
		return drift(ReasonExternalSecretInvalid,
			fmt.Sprintf("external secret %q username does not match PostgreSQL role %q", intent.Ref.Name, intent.Role))
	case !facts.ReloadEnabled:
		return drift(ReasonExternalSecretMissingLabel,
			fmt.Sprintf("external secret %q is missing the cnpg.io/reload=\"true\" label", intent.Ref.Name))
	default:
		return Result{Outcome: reconciliationTypes.Converged()}
	}
}

// ValidateExternal applies the read-only external credential contract to
// already-observed, non-sensitive Secret facts. Admission uses the same policy
// as reconciliation without gaining permission to mutate the Secret. Callers
// must accept only ModeConverged: Waiting is invalid external drift even though
// Outcome.Err is nil.
func ValidateExternal(ref SecretRef, role string, facts ObservedSecret) Result {
	intent := Intent{Ref: ref, Role: role, Source: SourceExternal, Continuity: ContinuityNew}
	if failures := validateIntents([]Intent{intent}); len(failures) > 0 {
		return selectFailure(failures)
	}
	return validateExternal(intent, facts)
}
