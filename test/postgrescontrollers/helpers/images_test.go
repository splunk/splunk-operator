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
	platform "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	fixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	"strings"
	"testing"
)

func TestPostgresImages(t *testing.T) {
	pg17 := fixture.ImageForVersion(platform.PostgresMajorVersion17)
	pg18 := fixture.ImageForVersion(platform.PostgresMajorVersion18)
	for _, tc := range []struct {
		name                        string
		versions                    []string
		pg17, pg18, pooler, secrets string
		wantCount                   int
		wantError                   string
	}{
		{name: "default PG17", pg17: pg17, wantCount: 1},
		{name: "explicit PG18", versions: []string{platform.PostgresMajorVersion18}, pg18: pg18, wantCount: 1},
		{name: "major and minor selectors", versions: []string{platform.PostgresMajorVersion17 + ".1", platform.PostgresMajorVersion18}, pg17: pg17, pg18: pg18, wantCount: 2},
		{name: "missing default", wantError: "TEST_PG17_IMAGE"},
		{name: "missing target", versions: []string{platform.PostgresMajorVersion17, platform.PostgresMajorVersion18}, pg17: pg17, wantError: "TEST_PG18_IMAGE"},
		{name: "optional pooling and Secret inputs", pg17: pg17, pooler: string(fixture.Images().PgBouncer), secrets: " reader-one, ,reader-two ", wantCount: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_PG17_IMAGE", tc.pg17)
			t.Setenv("TEST_PG18_IMAGE", tc.pg18)
			t.Setenv("TEST_PGBOUNCER_IMAGE", tc.pooler)
			t.Setenv("TEST_PG_IMAGE_PULL_SECRETS", tc.secrets)
			images, err := PostgresImages(tc.versions...)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || images != nil {
					t.Fatalf("want missing-input error for %s and nil images, got %v, %v", tc.wantError, images, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(images.PostgreSQL) != tc.wantCount {
				t.Fatalf("unexpected mappings: %v", images.PostgreSQL)
			}
			for major, image := range images.PostgreSQL {
				expected := pg17
				if major == platform.PostgresMajorVersion18 {
					expected = pg18
				}
				if string(image) != expected {
					t.Fatalf("reference changed: %q != %q", image, expected)
				}
			}
			if string(images.PgBouncer) != tc.pooler {
				t.Fatalf("PgBouncer reference changed: %q", images.PgBouncer)
			}
			if tc.secrets == "" {
				if len(images.ImagePullSecrets) != 0 {
					t.Fatal("unexpected default Secret")
				}
			} else if len(images.ImagePullSecrets) != 2 || images.ImagePullSecrets[0].Name != "reader-one" || images.ImagePullSecrets[1].Name != "reader-two" {
				t.Fatalf("unexpected Secret references: %v", images.ImagePullSecrets)
			}
		})
	}
}
