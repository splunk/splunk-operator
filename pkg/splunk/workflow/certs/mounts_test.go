// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package certs

import (
	"testing"

	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

func TestCertMountConfigMountPathForSecret(t *testing.T) {
	var absent *CertMountConfig
	path, found := absent.MountPathForSecret("serving")
	assert.Empty(t, path)
	assert.False(t, found)
	config := &CertMountConfig{
		Volumes: []corev1.Volume{
			{Name: "non-secret"},
			{Name: "serving-volume", Secret: &corev1.SecretVolumeSource{SecretName: "serving"}},
			{Name: "unmounted-volume", Secret: &corev1.SecretVolumeSource{SecretName: "unmounted"}},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "non-secret", MountPath: "/unrelated"},
			{Name: "serving-volume", MountPath: "/resolved/custom/path"},
		},
	}
	path, found = config.MountPathForSecret("serving")
	assert.True(t, found)
	assert.Equal(t, "/resolved/custom/path", path)
	for _, name := range []string{"missing", "unmounted"} {
		path, found = config.MountPathForSecret(name)
		assert.Empty(t, path)
		assert.False(t, found)
	}
	config.VolumeMounts[1].MountPath = ""
	_, found = config.MountPathForSecret("serving")
	assert.False(t, found)
}

func TestReconcileCertsFinalMountPaths(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.CertManagement, true)
	const serverPath = "/mnt/tls/splunk-server-tls-cert"
	const inputPath = "/mnt/tls/splunk-input-tls-cert"
	tests := []struct {
		name     string
		entries  []CertEntry
		operator bool
		conflict string
		want     map[string]string
	}{
		{name: "different Secrets with server role", entries: []CertEntry{{SecretName: "first", Role: "server"}, {SecretName: "second", Role: "server"}}, conflict: serverPath},
		{name: "different Secrets with input role", entries: []CertEntry{{SecretName: "first", Role: "input"}, {SecretName: "second", Role: "input"}}, conflict: inputPath},
		{name: "different roles", entries: []CertEntry{{SecretName: "first", Role: "server"}, {SecretName: "second", Role: "input"}}, want: map[string]string{"first": serverPath, "second": inputPath}},
		{name: "different as-is paths", entries: []CertEntry{{SecretName: "first"}, {SecretName: "second"}}, want: map[string]string{"first": "/mnt/tls/first", "second": "/mnt/tls/second"}},
		{name: "duplicate Secret and role", entries: []CertEntry{{SecretName: "first", Role: "server"}, {SecretName: "first", Role: "server"}}, want: map[string]string{"first": serverPath}},
		{name: "duplicate Secret changes role", entries: []CertEntry{{SecretName: "first", Role: "server"}, {SecretName: "first", Role: "input"}}, want: map[string]string{"first": inputPath}},
		{name: "deduplication removes intermediate collision", entries: []CertEntry{{SecretName: "first", Role: "server"}, {SecretName: "second", Role: "server"}, {SecretName: "first", Role: "input"}}, want: map[string]string{"first": inputPath, "second": serverPath}},
		{name: "final duplicate entry creates collision", entries: []CertEntry{{SecretName: "first", Role: "input"}, {SecretName: "second", Role: "server"}, {SecretName: "first", Role: "server"}}, conflict: serverPath},
		{name: "as-is path conflicts with role", entries: []CertEntry{{SecretName: "first", Role: "server"}, {SecretName: "splunk-server-tls-cert"}}, conflict: serverPath},
		{name: "operator-driven path conflicts with role", operator: true, entries: []CertEntry{{SecretName: "first", Role: "server"}}, conflict: serverPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := standaloneWithCerts("ns", "standalone", nil)
			c := buildClient(cr, fullSecret("ns", "first"), fullSecret("ns", "second"), fullSecret("ns", "splunk-server-tls-cert"))
			requester := &certRequesterStandalone{Standalone: cr}
			if tt.operator {
				requester.secrets = []string{"splunk-server-tls-cert"}
			}
			cfg, err := ReconcileCerts(t.Context(), c, requester, tt.entries)
			if tt.conflict != "" {
				require.ErrorContains(t, err, "same mount path")
				assert.Contains(t, err.Error(), tt.conflict)
				assert.Nil(t, cfg, "conflicting mounts must never be passed to workload reconciliation")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cfg)
			assert.Len(t, cfg.Volumes, len(tt.want))
			assert.Len(t, cfg.VolumeMounts, len(tt.want))
			for secretName, want := range tt.want {
				path, found := cfg.MountPathForSecret(secretName)
				assert.True(t, found)
				assert.Equal(t, want, path)
			}
		})
	}
}
