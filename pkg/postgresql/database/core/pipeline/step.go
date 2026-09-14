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

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
)

// Step is one ordered database reconciliation unit. It may wrap a steady-state
// component, a pure gate, a use-case adapter, or the final status flush point.
type Step interface {
	Name() string
	Requires() []ContractKey
	Provides() []ContractKey

	// Observe classifies the current state after any mutation attempt.
	// Return an error only when the state cannot be classified into an Outcome.
	Observe(ctx context.Context, contracts *Contracts, mutationErr error) (reconciliationTypes.Outcome, error)
}

// MutatingStep actuates desired state before observation. Steps that only
// read or classify state implement Step alone.
type MutatingStep interface {
	Step
	Reconcile(ctx context.Context, contracts *Contracts) error
}
