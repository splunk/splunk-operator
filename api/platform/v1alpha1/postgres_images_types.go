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

package v1alpha1

import corev1 "k8s.io/api/core/v1"

const (
	// PostgresMajorVersion17 is the oldest supported PostgreSQL major.
	PostgresMajorVersion17 = "17"
	// PostgresMajorVersion18 is the newest supported PostgreSQL major.
	PostgresMajorVersion18 = "18"
	// DefaultPostgresVersion is the default major for a cluster class.
	DefaultPostgresVersion = PostgresMajorVersion17
)

// PinnedImageReference identifies an image by both a tag and a SHA-256 digest.
// +kubebuilder:validation:MaxLength=2048
// +kubebuilder:validation:Pattern=`^(([a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*|\[[a-fA-F0-9:]+\])(:[0-9]+)?/)?[a-z0-9]+(([._]|__|-+)[a-z0-9]+)*(/[a-z0-9]+(([._]|__|-+)[a-z0-9]+)*)*:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}@sha256:[a-f0-9]{64}$`
// +kubebuilder:validation:XValidation:rule="!self.contains('://')",message="image must be a container image reference, not a URL"
// +kubebuilder:validation:XValidation:rule="self.split('@')[0].split(':')[self.split('@')[0].split(':').size() - 1] != 'latest'",message="image must not use the latest tag"
type PinnedImageReference string

// PostgresClusterImages is the complete image policy for one PostgresCluster.
// Images are never inherited from a class or defaulted to a public registry.
type PostgresClusterImages struct {
	// PostgreSQL maps each supported major to a pinned image with a matching numeric-leading tag.
	// Only the selected major must be supplied. Additional entries prepare a future upgrade.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=2
	// +kubebuilder:validation:XValidation:rule="self.all(major, major in ['17', '18'])",message="PostgreSQL image mappings support only majors 17 and 18"
	// +kubebuilder:validation:XValidation:rule="self.all(major, self[major].split('@')[0].split(':')[self[major].split('@')[0].split(':').size() - 1].matches('^' + major + '([^0-9].*)?$'))",message="PostgreSQL image tag must match its mapped major"
	PostgreSQL map[string]PinnedImageReference `json:"postgresql"`

	// PgBouncer is required whenever the effective connection pooler is enabled.
	// +optional
	PgBouncer PinnedImageReference `json:"pgbouncer,omitempty"`

	// ImagePullSecrets names registry Secrets in the PostgresCluster namespace.
	// The same references are applied to PostgreSQL and PgBouncer pods.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self.all(x, has(x.name) && x.name.size() > 0)",message="imagePullSecrets.name must not be empty"
	// +kubebuilder:validation:XValidation:rule="self.all(x, x.name.size() <= 253)",message="imagePullSecrets.name must contain at most 253 characters"
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}
