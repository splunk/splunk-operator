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
	"fmt"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
)

// ContractKey identifies a narrow per-pass fact required or provided by a step.
type ContractKey string

type CNPGDatabasesReadyContract struct{}

type RWPrivilegesReadyContract struct{}

// Contracts carries typed per-pass facts that earlier steps publish for later
// steps. It is intentionally not a general data bus; nil fields mean those
// facts are absent in this pass.
type Contracts struct {
	CNPGDatabasesReady *CNPGDatabasesReadyContract
	RWPrivilegesReady  *RWPrivilegesReadyContract

	useCaseStepResults map[*useCaseStep]useCaseStepResult
}

// NewContracts returns empty per-pass contracts.
func NewContracts() *Contracts {
	return &Contracts{}
}

// Has reports whether key has been published this cycle.
func (c *Contracts) Has(key ContractKey) bool {
	if c == nil {
		return false
	}
	switch key {
	case ContractDatabaseCNPGDatabasesReady:
		return c.CNPGDatabasesReady != nil
	case ContractDatabaseRWPrivilegesReady:
		return c.RWPrivilegesReady != nil
	default:
		return false
	}
}

// ValidateStepOrder verifies that every required contract has an earlier provider.
func ValidateStepOrder(steps []Step) error {
	provided := map[ContractKey]bool{}
	for i, s := range steps {
		for _, req := range s.Requires() {
			if !provided[req] {
				return fmt.Errorf("step %q (index %d) requires contract %q but no earlier step provides it", s.Name(), i, req)
			}
		}
		for _, prov := range s.Provides() {
			provided[prov] = true
		}
	}
	return nil
}

func missingContracts(required []ContractKey, contracts *Contracts) []ContractKey {
	var missing []ContractKey
	for _, req := range required {
		if !contracts.Has(req) {
			missing = append(missing, req)
		}
	}
	return missing
}

type useCaseStepResult struct {
	deferred  bool
	scheduled bool
	outcome   reconciliationTypes.Outcome
}

func (c *Contracts) setUseCaseStepResult(step *useCaseStep, result useCaseStepResult) {
	if c.useCaseStepResults == nil {
		c.useCaseStepResults = map[*useCaseStep]useCaseStepResult{}
	}
	c.useCaseStepResults[step] = result
}

func (c *Contracts) useCaseStepResult(step *useCaseStep) (useCaseStepResult, bool) {
	if c == nil || c.useCaseStepResults == nil {
		return useCaseStepResult{}, false
	}
	result, ok := c.useCaseStepResults[step]
	return result, ok
}
