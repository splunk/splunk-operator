// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package splunkconfig

import (
	"fmt"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

const SOKAppDir = "/opt/splunk/etc/apps/100-sok/local"

// Must differ from SOKAppDir: splunk-ansible deletes a target .conf file before
// writing each entry, so two entries for one conf file need distinct app dirs for
// btool to union them at runtime.
const SOKSecretsAppDir = "/opt/splunk/etc/apps/101-sok-secrets/local"

// ConfSet is the composed .conf output for one role. NonSensitive is delivered
// through a ConfigMap, Secrets through a Secret.
type ConfSet struct {
	NonSensitive []common.ConfFileEntry
	Secrets      []common.ConfFileEntry
}

// ConfFeature is one configuration concern, constructed with its own resolved
// inputs. The builder owns placement, so a directory the feature sets on an entry
// is ignored.
type ConfFeature interface {
	Name() string

	// Contribute returns a zero ConfSet for a role the feature does not configure.
	Contribute(role common.InstanceType) (ConfSet, error)
}

// RoleConfBuilder composes the features injected into it for one role. It
// deliberately knows no feature.
type RoleConfBuilder struct {
	role     common.InstanceType
	features []ConfFeature

	nonSensitiveDir string
	secretsDir      string
}

type confTarget struct {
	directory    string
	confFileName string
}

func ForRole(role common.InstanceType) *RoleConfBuilder {
	return &RoleConfBuilder{
		role:            role,
		nonSensitiveDir: SOKAppDir,
		secretsDir:      SOKSecretsAppDir,
	}
}

// NonSensitiveConfDir replaces SOKAppDir as the app directory of every
// non-sensitive entry. "" selects Splunk's default $SPLUNK_HOME/etc/system/local.
func (builder *RoleConfBuilder) NonSensitiveConfDir(dir string) *RoleConfBuilder {
	builder.nonSensitiveDir = dir
	return builder
}

// SecretsConfDir replaces SOKSecretsAppDir as the app directory of every secret
// entry.
func (builder *RoleConfBuilder) SecretsConfDir(dir string) *RoleConfBuilder {
	builder.secretsDir = dir
	return builder
}

// With injects a feature. Nil is ignored so a reconciler can inject a constructor
// result unconditionally.
func (builder *RoleConfBuilder) With(feature ConfFeature) *RoleConfBuilder {
	if feature != nil {
		builder.features = append(builder.features, feature)
	}
	return builder
}

// Build runs every injected feature and concatenates the results. Both halves claim
// out of one map because a claim is on a physical file: one conf file may appear in
// both halves only while the halves resolve to different directories, which is what
// the defaults do.
func (builder *RoleConfBuilder) Build() (ConfSet, error) {
	var set ConfSet
	claims := map[confTarget]string{}

	for _, feature := range builder.features {
		contribution, err := feature.Contribute(builder.role)
		if err != nil {
			return ConfSet{}, fmt.Errorf(
				"build conf for role %q from feature %q: %w",
				builder.role,
				feature.Name(),
				err,
			)
		}

		nonSensitive, err := place(contribution.NonSensitive, builder.nonSensitiveDir, claims, feature.Name()+" (non-sensitive)")
		if err != nil {
			return ConfSet{}, fmt.Errorf(
				"place non-sensitive conf for role %q from feature %q: %w",
				builder.role,
				feature.Name(),
				err,
			)
		}
		secrets, err := place(contribution.Secrets, builder.secretsDir, claims, feature.Name()+" (secrets)")
		if err != nil {
			return ConfSet{}, fmt.Errorf(
				"place secret conf for role %q from feature %q: %w",
				builder.role,
				feature.Name(),
				err,
			)
		}

		set.NonSensitive = append(set.NonSensitive, nonSensitive...)
		set.Secrets = append(set.Secrets, secrets...)
	}
	return set, nil
}

// place moves every entry into dir and rejects two entries resolving to the same
// file, where the later write would silently destroy the earlier one.
func place(entries []common.ConfFileEntry, dir string, claims map[confTarget]string, featureName string) ([]common.ConfFileEntry, error) {
	placed := make([]common.ConfFileEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Value.Directory = dir

		target := confTarget{directory: entry.Value.Directory, confFileName: entry.ConfFileName}
		if claimedBy, taken := claims[target]; taken {
			return nil, fmt.Errorf("%s and %s both write %s.conf to %q",
				claimedBy, featureName, entry.ConfFileName, entry.Value.Directory)
		}
		claims[target] = featureName

		placed = append(placed, entry)
	}
	return placed, nil
}
