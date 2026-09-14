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
package clusterreadiness

import (
	"errors"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// ErrClusterNotFound identifies an expected absent referenced cluster. The
// adapter preserves the Kubernetes error as the wrapped cause.
var ErrClusterNotFound = errors.New("referenced PostgresCluster not found")

// ErrClusterReaderNotConfigured identifies a missing gate dependency. It is a
// controller wiring failure, not a transient Kubernetes read failure.
var ErrClusterReaderNotConfigured = errors.New("cluster reader is not configured")

// Lifecycle is the published lifecycle state needed by the database readiness
// policy. It is intentionally narrower than the PostgresCluster component
// health model.
type Lifecycle string

const (
	// LifecycleReady is the only lifecycle state that lets database work proceed.
	LifecycleReady Lifecycle = "Ready"
)

// ProviderKind identifies the provisioner named by a cluster status reference.
// The database gate only requires a reference; provider-specific operations are
// performed later by the facade and its adapters.
type ProviderKind string

const (
	// ProviderCNPG is the CloudNativePG provider currently used by PostgresCluster.
	ProviderCNPG ProviderKind = "CNPG"
)

// ProviderReference is the database-facing identity of the provider resource.
// It deliberately does not expose a provider Kubernetes object.
type ProviderReference struct {
	Kind      ProviderKind
	Name      string
	Namespace string
}

// Recovery reports whether the provider translation identified a recovery or
// failover state that must be presented to database consumers distinctly from
// ordinary provisioning.
type Recovery string

const (
	// RecoveryNone is the zero value so absent recovery is represented one way.
	RecoveryNone       Recovery = ""
	RecoveryInProgress Recovery = "InProgress"
)

// ResolvedClusterFacts is the read-only database view of the referenced
// cluster. It contains only status facts consumed by the current database
// facade; it is not a mirror of PostgresCluster and never carries a provider
// object.
type ResolvedClusterFacts struct {
	Name      string
	Namespace string

	Lifecycle Lifecycle
	Provider  *ProviderReference
	Recovery  Recovery

	// ManagedRolesStatus is consumed by the database role-acknowledgement gate.
	ManagedRolesStatus *platformv1alpha1.ManagedRolesStatus
	// ConnectionPoolerStatus is consumed when building database connection metadata.
	ConnectionPoolerStatus *platformv1alpha1.ConnectionPoolerStatus
	// SuperUserSecretRef is consumed by privilege bootstrap.
	SuperUserSecretRef *corev1.SecretKeySelector
	// CustomMetricsStatus is consumed by the custom-metrics acknowledgement gate.
	CustomMetricsStatus *platformv1alpha1.CustomMetricsStatus
}

// Input contains database-owned context needed to classify the current facts.
// PreviousClusterReadyReason is intentionally just a condition reason: events
// and full status objects remain facade concerns.
type Input struct {
	Namespace string
	Name      string

	WasReady                   bool
	PreviousClusterReadyReason string
}
