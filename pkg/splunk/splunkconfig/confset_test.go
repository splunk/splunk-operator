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

package splunkconfig_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
)

// fakeFeature exercises composition without depending on a real feature.
type fakeFeature struct {
	name     string
	confFile string
	// secretConfFile defaults to confFile+"-secret"; set it equal to confFile to
	// mimic a feature like noah that writes one conf file in both halves.
	secretConfFile string
	dir            string
	err            error
	onlyRole       common.InstanceType
	askedRoles     []common.InstanceType
}

func (feature *fakeFeature) Name() string { return feature.name }

func (feature *fakeFeature) Contribute(role common.InstanceType) (splunkconfig.ConfSet, error) {
	feature.askedRoles = append(feature.askedRoles, role)
	if feature.err != nil {
		return splunkconfig.ConfSet{}, feature.err
	}
	if feature.onlyRole != "" && feature.onlyRole != role {
		return splunkconfig.ConfSet{}, nil
	}
	entry := func(confFileName string) common.ConfFileEntry {
		return common.ConfFileEntry{
			ConfFileName: confFileName,
			Value:        common.ConfFileValue{Directory: feature.dir},
		}
	}
	secretConfFile := feature.secretConfFile
	if secretConfFile == "" {
		secretConfFile = feature.confFile + "-secret"
	}
	return splunkconfig.ConfSet{
		NonSensitive: []common.ConfFileEntry{entry(feature.confFile)},
		Secrets:      []common.ConfFileEntry{entry(secretConfFile)},
	}, nil
}

func TestBuild_NoFeatures(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).Build()
	require.NoError(t, err)
	assert.Empty(t, set.NonSensitive)
	assert.Empty(t, set.Secrets)
}

// Contributions concatenate in injection order, halves kept separate.
func TestBuild_ConcatenatesInInjectionOrder(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).
		With(&fakeFeature{name: "first", confFile: "server"}).
		With(&fakeFeature{name: "second", confFile: "outputs"}).
		Build()
	require.NoError(t, err)

	require.Len(t, set.NonSensitive, 2)
	assert.Equal(t, "server", set.NonSensitive[0].ConfFileName)
	assert.Equal(t, "outputs", set.NonSensitive[1].ConfFileName)

	require.Len(t, set.Secrets, 2)
	assert.Equal(t, "server-secret", set.Secrets[0].ConfFileName)
	assert.Equal(t, "outputs-secret", set.Secrets[1].ConfFileName)
}

// Nil is ignored, so a reconciler can inject a constructor result unconditionally.
func TestWith_IgnoresNilFeature(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).
		With(nil).
		With(&fakeFeature{name: "real", confFile: "server"}).
		With(nil).
		Build()
	require.NoError(t, err)
	require.Len(t, set.NonSensitive, 1)
	assert.Equal(t, "server", set.NonSensitive[0].ConfFileName)
	require.Len(t, set.Secrets, 1)
	assert.Equal(t, "server-secret", set.Secrets[0].ConfFileName)
}

// Every feature is asked about the builder's role, and decides from it.
func TestBuild_PassesBuilderRoleToEveryFeature(t *testing.T) {
	ingestorOnly := &fakeFeature{name: "ingestor-only", confFile: "outputs", onlyRole: common.SplunkIngestor}
	always := &fakeFeature{name: "always", confFile: "server"}

	set, err := splunkconfig.ForRole(common.SplunkSearchHead).With(ingestorOnly).With(always).Build()
	require.NoError(t, err)

	assert.Equal(t, []common.InstanceType{common.SplunkSearchHead}, ingestorOnly.askedRoles)
	assert.Equal(t, []common.InstanceType{common.SplunkSearchHead}, always.askedRoles)
	require.Len(t, set.NonSensitive, 1, "the role-mismatched feature must contribute nothing")
	assert.Equal(t, "server", set.NonSensitive[0].ConfFileName)
	require.Len(t, set.Secrets, 1, "only the always-on feature should contribute a secret")
	assert.Equal(t, "server-secret", set.Secrets[0].ConfFileName)
}

// Each half defaults to its own SOK app dir, whatever directory a feature chose.
func TestBuild_DefaultsBothHalvesToSOKAppDirs(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).
		With(&fakeFeature{name: "smartbus", confFile: "outputs", dir: "/opt/splunk/etc/apps/900-feature-choice/local"}).
		With(&fakeFeature{name: "noah", confFile: "server"}).
		Build()
	require.NoError(t, err)

	require.Len(t, set.NonSensitive, 2)
	for _, entry := range set.NonSensitive {
		assert.Equal(t, splunkconfig.SOKAppDir, entry.Value.Directory)
	}
	require.Len(t, set.Secrets, 2)
	for _, entry := range set.Secrets {
		assert.Equal(t, splunkconfig.SOKSecretsAppDir, entry.Value.Directory)
	}
}

// The override wins over every feature, including one that left the directory empty.
func TestBuild_ConfDirOverridesEveryFeature(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).
		NonSensitiveConfDir("/opt/splunk/etc/apps/200-pinned/local").
		SecretsConfDir("/opt/splunk/etc/apps/201-pinned-secrets/local").
		With(&fakeFeature{name: "smartbus", confFile: "outputs", dir: splunkconfig.SOKAppDir}).
		With(&fakeFeature{name: "noah", confFile: "server"}).
		Build()
	require.NoError(t, err)

	require.Len(t, set.NonSensitive, 2)
	for _, entry := range set.NonSensitive {
		assert.Equal(t, "/opt/splunk/etc/apps/200-pinned/local", entry.Value.Directory)
	}
	require.Len(t, set.Secrets, 2)
	for _, entry := range set.Secrets {
		assert.Equal(t, "/opt/splunk/etc/apps/201-pinned-secrets/local", entry.Value.Directory)
	}
}

// Setting both directories to "" selects Splunk's default etc/system/local.
func TestBuild_ConfDirCanSelectSplunkDefault(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).
		NonSensitiveConfDir("").
		SecretsConfDir("").
		With(&fakeFeature{name: "smartbus", confFile: "outputs", dir: splunkconfig.SOKAppDir}).
		Build()
	require.NoError(t, err)

	require.Len(t, set.NonSensitive, 1)
	assert.Empty(t, set.NonSensitive[0].Value.Directory)
	require.Len(t, set.Secrets, 1)
	assert.Empty(t, set.Secrets[0].Value.Directory)
}

// A collision must fail the build and name both features. The directories the
// features chose differ, so it is the builder's placement that collapses them onto
// one target — which must be rejected rather than silently merged.
func TestBuild_RejectsCollidingTargets(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).
		With(&fakeFeature{name: "smartbus", confFile: "outputs", dir: "/opt/splunk/etc/apps/900-smartbus/local"}).
		With(&fakeFeature{name: "latecomer", confFile: "outputs", dir: splunkconfig.SOKAppDir}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("place non-sensitive conf for role %q from feature %q:", common.SplunkIndexer.ToString(), "latecomer"))
	assert.Contains(t, err.Error(), "smartbus")
	assert.Contains(t, err.Error(), "latecomer")
	assert.Contains(t, err.Error(), "outputs.conf")
	assert.Contains(t, err.Error(), splunkconfig.SOKAppDir)
	assert.Empty(t, set.NonSensitive, "a failed build must not return partial non-sensitive config")
	assert.Empty(t, set.Secrets, "a failed build must not return partial secrets")
}

// Distinct non-sensitive files do not prevent collisions between secret entries.
func TestBuild_RejectsCollidingSecretTargets(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).
		With(&fakeFeature{name: "smartbus", confFile: "outputs", secretConfFile: "shared"}).
		With(&fakeFeature{name: "latecomer", confFile: "server", secretConfFile: "shared"}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("place secret conf for role %q from feature %q:", common.SplunkIndexer.ToString(), "latecomer"))
	assert.Contains(t, err.Error(), "smartbus")
	assert.Contains(t, err.Error(), "latecomer")
	assert.Contains(t, err.Error(), "shared.conf")
	assert.Contains(t, err.Error(), splunkconfig.SOKSecretsAppDir)
	assert.Empty(t, set.NonSensitive, "a failed build must not return partial non-sensitive config")
	assert.Empty(t, set.Secrets, "a failed build must not return partial secrets")
}

// Pointing both halves at one directory collapses them onto a single physical file,
// which splunk-ansible would truncate on the second write, so it must be rejected.
func TestBuild_RejectsSameConfFileWhenHalvesShareADirectory(t *testing.T) {
	shared := map[string]string{
		"SOK app dir":    splunkconfig.SOKAppDir,
		"Splunk default": "",
	}
	for name, dir := range shared {
		t.Run(name, func(t *testing.T) {
			set, err := splunkconfig.ForRole(common.SplunkIndexer).
				NonSensitiveConfDir(dir).
				SecretsConfDir(dir).
				With(&fakeFeature{name: "noah", confFile: "server", secretConfFile: "server"}).
				Build()
			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("place secret conf for role %q from feature %q:", common.SplunkIndexer.ToString(), "noah"))
			assert.Contains(t, err.Error(), "server.conf")
			assert.Contains(t, err.Error(), "noah")
			assert.Empty(t, set.NonSensitive, "a failed build must not return partial non-sensitive config")
			assert.Empty(t, set.Secrets, "a failed build must not return partial secrets")
		})
	}
}

// The same .conf filename is allowed in both outputs when they resolve to different directories.
func TestBuild_SameConfFileAcrossHalvesIsAllowed(t *testing.T) {
	set, err := splunkconfig.ForRole(common.SplunkIndexer).
		With(&fakeFeature{name: "noah", confFile: "server", secretConfFile: "server"}).
		Build()
	require.NoError(t, err)

	require.Len(t, set.NonSensitive, 1)
	require.Len(t, set.Secrets, 1)
	assert.Equal(t, "server", set.NonSensitive[0].ConfFileName)
	assert.Equal(t, "server", set.Secrets[0].ConfFileName)
	assert.Equal(t, splunkconfig.SOKAppDir, set.NonSensitive[0].Value.Directory)
	assert.Equal(t, splunkconfig.SOKSecretsAppDir, set.Secrets[0].Value.Directory)
}

// A failing feature aborts the build and is named in the error.
func TestBuild_AttributesFeatureError(t *testing.T) {
	featureErr := errors.New("unsupported queue provider: \"kafka\"")
	failing := &fakeFeature{name: "smartbus", err: featureErr}
	never := &fakeFeature{name: "never-reached", confFile: "server"}

	set, err := splunkconfig.ForRole(common.SplunkIndexer).With(failing).With(never).Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("build conf for role %q from feature %q:", common.SplunkIndexer.ToString(), "smartbus"))
	assert.Contains(t, err.Error(), "smartbus")
	assert.Contains(t, err.Error(), common.SplunkIndexer.ToString())
	assert.Contains(t, err.Error(), "unsupported queue provider")
	assert.ErrorIs(t, err, featureErr, "Build should preserve the feature's original error")
	assert.Empty(t, set.NonSensitive, "a failed build must not return partial config")
	assert.Empty(t, never.askedRoles, "build must stop at the first failing feature")
	assert.Equal(t, []common.InstanceType{common.SplunkIndexer}, failing.askedRoles)
}
