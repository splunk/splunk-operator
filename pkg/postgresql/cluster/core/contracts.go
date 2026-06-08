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
package core

import (
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
)

// contractKey identifies a shared object that one component produces and
// another consumes. Keys are matched by validateComponentOrder to enforce
// that every Requires entry is satisfied by an earlier Provides entry.
type contractKey string

const (
	contractSecret      contractKey = "Secret"
	contractCNPGCluster contractKey = "CNPGCluster"
)

// reconcileContracts carries the live k8s objects that components publish for
// downstream components to consume. A nil field means the producing component
// has not yet run successfully this cycle.
type reconcileContracts struct {
	CNPGCluster *cnpgv1.Cluster
	Secret      *corev1.Secret
}

// checkContractsFromRequirements is the single implementation of the nil-check
// logic shared by every model's CheckContracts().
func checkContractsFromRequirements(requires []contractKey, contracts *reconcileContracts) bool {
	for _, req := range requires {
		switch req {
		case contractSecret:
			if contracts.Secret == nil {
				return false
			}
		case contractCNPGCluster:
			if contracts.CNPGCluster == nil {
				return false
			}
		}
	}
	return true
}

// validateComponentOrder verifies that every component's Requires keys are
// satisfied by an earlier component's Provides keys. A failure here is a
// programming error, not a transient condition — surface it loudly rather
// than silently requeue-looping in production.
func validateComponentOrder(components []component) error {
	provided := map[contractKey]bool{}
	for i, c := range components {
		for _, req := range c.Requires() {
			if !provided[req] {
				return fmt.Errorf("component %q (index %d) requires contract %q but no earlier component provides it", c.Name(), i, req)
			}
		}
		for _, prov := range c.Provides() {
			provided[prov] = true
		}
	}
	return nil
}
