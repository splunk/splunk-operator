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

package identity

// EnvironmentRole states why an environment remains relevant to one logical
// PostgresCluster. It is explicit so consumers never infer lifecycle policy
// from an environment name or the number of environments.
type EnvironmentRole string

const (
	// EnvironmentRoleAuthoritative identifies the single environment used by
	// ordinary cluster and database reconciliation.
	EnvironmentRoleAuthoritative EnvironmentRole = "Authoritative"
	// EnvironmentRoleCandidate identifies an environment being prepared but
	// not yet selected for ordinary reconciliation.
	EnvironmentRoleCandidate EnvironmentRole = "Candidate"
	// EnvironmentRoleRetained identifies a previous environment kept for a
	// rollback window.
	EnvironmentRoleRetained EnvironmentRole = "Retained"
	// EnvironmentRoleRetirable identifies an environment whose lifecycle
	// policy has authorized retirement.
	EnvironmentRoleRetirable EnvironmentRole = "Retirable"
)

// NamingScope determines whether generated provider resources include the
// provider-environment name. The scope is resolved explicitly instead of being
// inferred from a difference between logical and provider names.
type NamingScope string

const (
	// NamingScopeConventional preserves the conventional generated-resource
	// name for a single-environment cluster.
	NamingScopeConventional NamingScope = "Conventional"
	// NamingScopeEnvironment includes the provider-environment name in a
	// resource-specific generated name.
	NamingScopeEnvironment NamingScope = "Environment"
)

// Environment combines a provider object identity with its lifecycle role and
// generated-resource naming scope.
type Environment struct {
	// Identity identifies the provider environment object.
	Identity ObjectIdentity
	// Role states the environment lifecycle role for this reconciliation state.
	Role EnvironmentRole
	// Scope controls generated-resource naming for this environment.
	Scope NamingScope
}

// ClusterInput is the provider-neutral status snapshot from which an identity
// resolver derives a ClusterCard. Adapters translate API-specific status into
// this input before core reconciliation consumes it.
type ClusterInput struct {
	// Logical identifies the logical PostgresCluster API object.
	Logical ObjectIdentity
	// Environments is the adapter-translated provider environment inventory.
	Environments []Environment
}

// ClusterCard is the immutable resolved identity of one logical
// PostgresCluster. Authoritative is also present in Managed exactly once.
type ClusterCard struct {
	// Logical identifies the logical PostgresCluster API object.
	Logical ObjectIdentity
	// Authoritative identifies the environment ordinary reconciliation uses.
	Authoritative Environment
	// Managed contains the de-duplicated environments relevant to lifecycle work.
	// It includes Authoritative exactly once.
	Managed []Environment
}
