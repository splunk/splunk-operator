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

// Package identity resolves provider-neutral PostgreSQL identity inputs.
package identity

import (
	"fmt"

	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
)

// IdentityResolver derives immutable cluster identity cards and provider names
// from adapter-translated status snapshots.
type IdentityResolver struct{}

// NewIdentityResolver constructs a resolver with no infrastructure
// dependencies.
func NewIdentityResolver() *IdentityResolver {
	return &IdentityResolver{}
}

// ResolveCluster validates and de-duplicates the input environments, then
// returns the single authoritative environment and the full managed set.
func (r *IdentityResolver) ResolveCluster(input identitytypes.ClusterInput) (identitytypes.ClusterCard, error) {
	if err := validateObjectIdentity("logical cluster", input.Logical); err != nil {
		return identitytypes.ClusterCard{}, err
	}

	managed := make([]identitytypes.Environment, 0, len(input.Environments))
	byName := make(map[environmentKey]int, len(input.Environments))
	authoritative := -1
	for _, environment := range input.Environments {
		if err := validateEnvironment(environment); err != nil {
			return identitytypes.ClusterCard{}, err
		}

		key := newEnvironmentKey(environment.Identity)
		if index, found := byName[key]; found {
			if err := mergeEnvironment(&managed[index], environment); err != nil {
				return identitytypes.ClusterCard{}, err
			}
			if managed[index].Role == identitytypes.EnvironmentRoleAuthoritative {
				authoritative = index
			}
			continue
		}

		byName[key] = len(managed)
		managed = append(managed, environment)
		if environment.Role == identitytypes.EnvironmentRoleAuthoritative {
			if authoritative >= 0 {
				return identitytypes.ClusterCard{}, fmt.Errorf("multiple authoritative environments")
			}
			authoritative = len(managed) - 1
		}
	}
	if authoritative < 0 {
		return identitytypes.ClusterCard{}, fmt.Errorf("authoritative environment is required")
	}

	return identitytypes.ClusterCard{
		Logical:       input.Logical,
		Authoritative: managed[authoritative],
		Managed:       managed,
	}, nil
}

// EnvironmentName returns the observed provider name when present, otherwise
// the conventional logical cluster name.
func (r *IdentityResolver) EnvironmentName(logicalName, observedName string) string {
	if observedName != "" {
		return observedName
	}
	return logicalName
}

// AuthoritativeEnvironmentName returns the provider name selected by card,
// falling back to the conventional logical cluster name.
func (r *IdentityResolver) AuthoritativeEnvironmentName(logicalName string, card identitytypes.ClusterCard) string {
	return r.EnvironmentName(logicalName, card.Authoritative.Identity.Name)
}

// ManagedEnvironmentNames returns unique provider names in card order.
func (r *IdentityResolver) ManagedEnvironmentNames(card identitytypes.ClusterCard) []string {
	names := make([]string, 0, len(card.Managed))
	seen := make(map[string]struct{}, len(card.Managed))
	for _, environment := range card.Managed {
		name := environment.Identity.Name
		if name == "" {
			continue
		}
		if _, found := seen[name]; found {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

type environmentKey struct {
	apiVersion string
	kind       string
	name       string
	namespace  string
}

func newEnvironmentKey(identity identitytypes.ObjectIdentity) environmentKey {
	return environmentKey{
		apiVersion: identity.APIVersion,
		kind:       identity.Kind,
		name:       identity.Name,
		namespace:  identity.Namespace,
	}
}

func validateEnvironment(environment identitytypes.Environment) error {
	if err := validateObjectIdentity("environment", environment.Identity); err != nil {
		return err
	}
	switch environment.Role {
	case identitytypes.EnvironmentRoleAuthoritative,
		identitytypes.EnvironmentRoleCandidate,
		identitytypes.EnvironmentRoleRetained,
		identitytypes.EnvironmentRoleRetirable:
	default:
		return fmt.Errorf("unsupported environment role %q", environment.Role)
	}
	switch environment.Scope {
	case identitytypes.NamingScopeConventional, identitytypes.NamingScopeEnvironment:
		return nil
	default:
		return fmt.Errorf("unsupported environment naming scope %q", environment.Scope)
	}
}

func validateObjectIdentity(description string, identity identitytypes.ObjectIdentity) error {
	if identity.APIVersion == "" || identity.Kind == "" || identity.Name == "" || identity.Namespace == "" {
		return fmt.Errorf("%s apiVersion, kind, name, and namespace are required", description)
	}
	return nil
}

func mergeEnvironment(current *identitytypes.Environment, incoming identitytypes.Environment) error {
	if current.Role != incoming.Role || current.Scope != incoming.Scope {
		return fmt.Errorf("conflicting environment declarations for %s %s/%s", current.Identity.Kind, current.Identity.Namespace, current.Identity.Name)
	}
	if current.Identity.UID != "" && incoming.Identity.UID != "" && current.Identity.UID != incoming.Identity.UID {
		return fmt.Errorf("conflicting environment UIDs for %s %s/%s", current.Identity.Kind, current.Identity.Namespace, current.Identity.Name)
	}
	if current.Identity.UID == "" {
		current.Identity.UID = incoming.Identity.UID
	}
	return nil
}
