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
	"strings"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
)

const pgBouncerContainerName = "pgbouncer"

// ValidateEffectiveImages checks requirements that depend on class-inherited
// settings. The CRD schema owns image syntax and other intrinsic constraints.
func ValidateEffectiveImages(images *platformv1alpha1.PostgresClusterImages, version *string, poolingEnabled bool) []ConfigValidationError {
	var errs []ConfigValidationError
	if version != nil {
		major, _, _ := strings.Cut(*version, ".")
		if images == nil || images.PostgreSQL[major] == "" {
			errs = append(errs, ConfigValidationError{
				Field:   "spec.images.postgresql[" + major + "]",
				Message: "the selected PostgreSQL major must have an image mapping",
			})
		}
	}
	if poolingEnabled && (images == nil || images.PgBouncer == "") {
		errs = append(errs, ConfigValidationError{
			Field:   "spec.images.pgbouncer",
			Message: "PgBouncer image is required when pooling is enabled",
		})
	}
	return errs
}

// ResolvePostgresImage returns the exact image mapped to the requested major.
// Syntax is enforced by the CRD; missing configuration never selects a fallback.
func ResolvePostgresImage(images *platformv1alpha1.PostgresClusterImages, version string) (string, error) {
	if images == nil {
		return "", fmt.Errorf("spec.images is required")
	}
	major, _, _ := strings.Cut(version, ".")
	image := string(images.PostgreSQL[major])
	if image == "" {
		return "", fmt.Errorf("spec.images.postgresql[%s] is required", major)
	}
	return image, nil
}
