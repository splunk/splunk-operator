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
	clusteridentity "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/identity"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	corev1 "k8s.io/api/core/v1"
)

// contractKey identifies a shared dependency consumed by a component. Keys are
// matched by validateComponentOrder to enforce that every Requires entry is
// satisfied by an earlier Provides entry or explicit root wiring.
type contractKey string

const (
	contractSecret           contractKey = "Secret"
	contractCNPGCluster      contractKey = "CNPGCluster"
	contractAuthority        contractKey = "Authority"
	contractEnvironmentNamer contractKey = "EnvironmentNamer"
)

// reconcileContracts carries component-published Kubernetes objects and
// reconciliation-wide dependencies supplied by the composition root. A nil
// published object means its producing component has not run successfully this
// cycle; root dependencies must be present before the component pipeline runs.
type reconcileContracts struct {
	CNPGCluster      *cnpgv1.Cluster
	Secret           *corev1.Secret
	Authority        identitytypes.ClusterCard
	EnvironmentNamer clusteridentity.EnvironmentNamer
}

// checkContractsFromRequirements is the single implementation of contract
// validation shared by every model's CheckContracts().
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
		case contractAuthority:
			if contracts.Authority.Authoritative.Identity.Name == "" {
				return false
			}
		case contractEnvironmentNamer:
			if contracts.EnvironmentNamer == nil {
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
func validateComponentOrder(components []component, rootProvidedContracts []contractKey) error {
	provided := make(map[contractKey]bool, len(rootProvidedContracts))
	for _, contract := range rootProvidedContracts {
		provided[contract] = true
	}
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
