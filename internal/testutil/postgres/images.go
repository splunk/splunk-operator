// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

// Package postgres supplies synthetic image references for tests that never pull images.
package postgres

import (
	"crypto/sha256"
	"fmt"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
)

// ImageForVersion returns a deterministic, pinned fixture, not a published image.
func ImageForVersion(version string) string {
	return fmt.Sprintf("registry.example.invalid/postgresql:%s@sha256:%x", version, sha256.Sum256([]byte("postgres-fixture-"+version)))
}

// Images returns a fresh customer mapping for supported majors and pooling.
func Images() *platformv1alpha1.PostgresClusterImages {
	return &platformv1alpha1.PostgresClusterImages{
		PostgreSQL: map[string]platformv1alpha1.PinnedImageReference{
			platformv1alpha1.PostgresMajorVersion17: platformv1alpha1.PinnedImageReference(ImageForVersion(platformv1alpha1.PostgresMajorVersion17)),
			platformv1alpha1.PostgresMajorVersion18: platformv1alpha1.PinnedImageReference(ImageForVersion(platformv1alpha1.PostgresMajorVersion18)),
		},
		PgBouncer: platformv1alpha1.PinnedImageReference(fmt.Sprintf("registry.example.invalid/pgbouncer:fixture@sha256:%x", sha256.Sum256([]byte("pgbouncer-fixture")))),
	}
}
