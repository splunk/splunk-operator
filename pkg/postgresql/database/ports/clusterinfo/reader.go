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

// Package clusterinfo defines the database-facing PostgresCluster read contract.
package clusterinfo

import (
	"context"
	"errors"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	corev1 "k8s.io/api/core/v1"
)

// ErrClusterNotFound identifies an expected absent referenced cluster. The
// adapter preserves the Kubernetes error as the wrapped cause.
var ErrClusterNotFound = errors.New("referenced PostgresCluster not found")

// ErrClusterReaderNotConfigured identifies a missing reader dependency. It is
// a controller wiring failure, not a transient Kubernetes read failure.
var ErrClusterReaderNotConfigured = errors.New("cluster reader is not configured")

// Lifecycle is the published lifecycle state needed by database reconciliation.
type Lifecycle string

const (
	// LifecycleReady is the only lifecycle state that lets database work proceed.
	LifecycleReady Lifecycle = "Ready"
)

// Recovery reports whether provider translation identified a recovery or
// failover state that must be handled distinctly from ordinary provisioning.
type Recovery string

const (
	// RecoveryNone is the zero value so absent recovery is represented one way.
	RecoveryNone Recovery = ""
	// RecoveryInProgress identifies a recovery or failover in progress.
	RecoveryInProgress Recovery = "InProgress"
)

// ResolvedClusterFacts is the read-only database view of a referenced
// PostgresCluster. It contains only facts consumed by database reconciliation.
type ResolvedClusterFacts struct {
	Name      string
	Namespace string

	Lifecycle Lifecycle
	// Cluster is the resolved identity used for provider naming and validation.
	Cluster  *identitytypes.ClusterCard
	Recovery Recovery

	// ManagedRolesStatus is consumed by the database role-acknowledgement gate.
	ManagedRolesStatus *platformv1alpha1.ManagedRolesStatus
	// ConnectionPoolerStatus is consumed when building database connection metadata.
	ConnectionPoolerStatus *platformv1alpha1.ConnectionPoolerStatus
	// SuperUserSecretRef is consumed by privilege bootstrap.
	SuperUserSecretRef *corev1.SecretKeySelector
	// CustomMetricsStatus is consumed by the custom-metrics acknowledgement gate.
	CustomMetricsStatus *platformv1alpha1.CustomMetricsStatus
}

// ClusterReader reads the resolved facts database reconciliation requires from
// a referenced PostgresCluster. Implementations belong to adapters.
type ClusterReader interface {
	Read(context.Context, string, string) (ResolvedClusterFacts, error)
}
