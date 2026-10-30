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

package util

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"k8s.io/apimachinery/pkg/util/validation"
)

// PostgresDatabaseMountInput is the CRD-independent input to shared mount
// validation. Callers translate their own API types at their package boundary.
type PostgresDatabaseMountInput struct {
	Name          string
	ReferenceName string
	Database      string
}

var postgresDatabaseNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,29}$`)

// ValidatePostgresDatabaseMounts checks names, reference syntax, database-based
// file collisions, and the reserved workload volume without constructing objects.
func ValidatePostgresDatabaseMounts(mounts []PostgresDatabaseMountInput, volumeNames []string) error {
	if len(mounts) > 10 {
		return fmt.Errorf("postgresDatabaseMounts supports at most 10 entries")
	}
	if len(mounts) == 0 {
		return nil
	}
	if slices.Contains(volumeNames, common.PostgresDatabaseVolumeName) {
		return fmt.Errorf("volume %q is reserved for PostgresDatabase bundles", common.PostgresDatabaseVolumeName)
	}
	names, databases := make(map[string]bool), make(map[string]bool)
	for _, mount := range mounts {
		if len(mount.Name) > 60 || len(validation.IsDNS1123Label(mount.Name)) != 0 {
			return fmt.Errorf("invalid PostgresDatabase mount name %q", mount.Name)
		}
		if names[mount.Name] {
			return fmt.Errorf("duplicate PostgresDatabase mount name %q", mount.Name)
		}
		names[mount.Name] = true
		if len(validation.IsDNS1123Subdomain(mount.ReferenceName)) != 0 {
			return fmt.Errorf("mount %q requires a valid postgresDatabaseRef.name", mount.Name)
		}
		if !postgresDatabaseNamePattern.MatchString(mount.Database) {
			return fmt.Errorf("mount %q requires a valid database name", mount.Name)
		}
		if databases[mount.Database] {
			return fmt.Errorf("database %q has conflicting PostgresDatabase bundle filenames", mount.Database)
		}
		databases[mount.Database] = true
	}
	return nil
}
