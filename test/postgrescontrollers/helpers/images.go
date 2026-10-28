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
package helpers

import (
	"fmt"
	"os"
	"strings"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// PostgresImages reads explicit registry inputs only while a live spec is running.
// Omitted versions select the class default; upgrade specs supply both majors.
func PostgresImages(versions ...string) (*platformv1alpha1.PostgresClusterImages, error) {
	if len(versions) == 0 {
		versions = []string{platformv1alpha1.DefaultPostgresVersion}
	}
	images := &platformv1alpha1.PostgresClusterImages{PostgreSQL: make(map[string]platformv1alpha1.PinnedImageReference)}
	for _, version := range versions {
		major, _, _ := strings.Cut(version, ".")
		variable := fmt.Sprintf("TEST_PG%s_IMAGE", major)
		value := os.Getenv(variable)
		if value == "" {
			return nil, fmt.Errorf("%s must contain a numeric-leading tag@sha256 reference", variable)
		}
		images.PostgreSQL[major] = platformv1alpha1.PinnedImageReference(value)
	}
	images.PgBouncer = platformv1alpha1.PinnedImageReference(os.Getenv("TEST_PGBOUNCER_IMAGE"))
	for _, name := range strings.Split(os.Getenv("TEST_PG_IMAGE_PULL_SECRETS"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			images.ImagePullSecrets = append(images.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}
	return images, nil
}
