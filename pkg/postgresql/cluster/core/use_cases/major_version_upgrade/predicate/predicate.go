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

package majorversionupgradepredicate

import (
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// Predicate reports whether the major-version upgrade use case is possibly
// relevant for the given spec. It is a NECESSARY (not sufficient) condition —
// it only eliminates the common steady state where the feature is switched off,
// so the reconciler can skip construction and status reads entirely. The use
// case's own Schedule makes the precise decision that needs live CNPG reads.
// When spec is nil it returns true so a missing cluster falls through to Schedule.
func Predicate(spec *enterprisev4.PostgresClusterSpec) bool {
	if spec == nil {
		return true
	}
	cfg := spec.PostgresMajorUpgradeConfig
	return cfg != nil && cfg.Allow != nil && *cfg.Allow
}
