// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v4

import corev1 "k8s.io/api/core/v1"

// ConditionPostgresDatabaseResolved reports whether database mount dependencies resolved.
const ConditionPostgresDatabaseResolved ConditionType = "PostgresDatabaseResolved"

// PostgresDatabaseMountSpec mounts a database's published connection metadata
// and credentials into the Splunk container. It does not provision a database,
// enable a sidecar, or configure Splunk to use PostgreSQL.
type PostgresDatabaseMountSpec struct {
	// Name identifies this entry and names the directory that holds its files,
	// $SPLUNK_HOME/var/run/supervisor/postgresok/<name>. Give each consumer
	// its own entry. Several entries can reference the same database.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=60
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// PostgresDatabaseRef references a platform.splunk.com PostgresDatabase
	// in the same namespace as this workload.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="postgresDatabaseRef.name must not be empty"
	PostgresDatabaseRef corev1.LocalObjectReference `json:"postgresDatabaseRef"`

	// Database selects one entry in PostgresDatabase.spec.databases by name.
	// Internal hyphens, including repeated hyphens, are preserved.
	// The -bg-<10 lowercase hex digits>- form is reserved for blue/green resources.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=30
	// +kubebuilder:validation:Pattern=`^[a-z]([a-z0-9-]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="!self.matches('-bg-[0-9a-f]{10}-')",message="the -bg-<10 lowercase hex digits>- name form is reserved for blue/green resources"
	Database string `json:"database"`

	// IncludeAdmin also mounts the database-owner password for schema changes.
	// Disabled by default; ordinary consumers receive only the RW password.
	// +optional
	// +kubebuilder:default=false
	IncludeAdmin bool `json:"includeAdmin,omitzero"`
}
