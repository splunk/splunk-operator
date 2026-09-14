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

// Package types defines provider-neutral data exchanged by the
// PostgresDatabase bounded context and its adapters.
package types

type ReclaimPolicy string

const (
	ReclaimDelete ReclaimPolicy = "Delete"
	ReclaimRetain ReclaimPolicy = "Retain"
)

// DatabaseCreationOptions contains PostgreSQL properties fixed when a logical
// database is created.
type DatabaseCreationOptions struct {
	Template         string
	Encoding         string
	Locale           string
	LocaleProvider   string
	LocaleCollate    string
	LocaleCType      string
	ICULocale        string
	ICURules         string
	BuiltinLocale    string
	CollationVersion string
}

// DatabaseMutableOptions contains PostgreSQL properties that remain desired
// state after database creation.
type DatabaseMutableOptions struct {
	IsTemplate       *bool
	AllowConnections *bool
	ConnectionLimit  *int32
	Tablespace       string
}

// DesiredDatabase is the provider-neutral desired state for one logical
// database. ResourceName is its Kubernetes identity; Name is its PostgreSQL
// identity.
type DesiredDatabase struct {
	ResourceName string
	Name         string
	Owner        string
	Reclaim      ReclaimPolicy
	Extensions   []string
	Creation     DatabaseCreationOptions
	Mutable      DatabaseMutableOptions
}

// ProvisionerTarget identifies the SOK owner and provider cluster without
// exposing provider resources to core.
type ProvisionerTarget struct {
	Namespace            string
	PostgresDatabaseName string
	PostgresDatabaseUID  string
	ProviderClusterName  string
}

// DatabaseIdentity correlates a logical database with its provider resource.
type DatabaseIdentity struct {
	Name         string
	ResourceName string
}

// ExpectedDatabase identifies the provider resource generation produced by
// Apply and required by Observe.
type ExpectedDatabase struct {
	Name         string
	ResourceName string
	Generation   int64
}

type ApplyResult struct {
	Expected []ExpectedDatabase
	Adopted  []string
}

// ObservedDatabase reports provider convergence for one expected database.
// Applied is authoritative only when ObservedGeneration equals the expected
// generation.
type ObservedDatabase struct {
	Name               string
	ResourceName       string
	UID                string
	Found              bool
	Applied            bool
	ObservedGeneration int64
	Message            string
}

type Observation struct {
	Databases []ObservedDatabase
}
