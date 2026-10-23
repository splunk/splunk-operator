// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v4

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommonWorkloadSpecSerialization(t *testing.T) {
	// Existing manifest field names must remain flat after extracting the shared spec.
	const manifest = `{
		"image":"kvservice:test", "imagePullPolicy":"IfNotPresent",
		"resources":{"requests":{"cpu":"1","memory":"4Gi"}},
		"disableResourceDefaults":true, "schedulerName":"custom-scheduler",
		"affinity":{"nodeAffinity":{}}, "tolerations":[{"key":"dedicated","operator":"Exists"}],
		"topologySpreadConstraints":[{"maxSkew":1,"topologyKey":"zone","whenUnsatisfiable":"ScheduleAnyway"}],
		"serviceTemplate":{"spec":{"type":"ClusterIP"}}, "podAnnotations":{"custom":"value"},
		"volumes":[{"name":"postgres-tls","secret":{"secretName":"postgres-tls"}}],
		"defaults":"configuration", "serviceAccount":"workload-sa",
		"livenessInitialDelaySeconds":90, "readinessInitialDelaySeconds":75,
		"extraEnv":[{"name":"DB_PASSWORD","valueFrom":{"secretKeyRef":{"name":"postgres","key":"password"}}}],
		"imagePullSecrets":[{"name":"registry"}],
		"certs":[{"secretRef":{"name":"postgres-tls"},"dnsNames":["kv.example.com"]}]
	}`
	var expected map[string]any
	require.NoError(t, json.Unmarshal([]byte(manifest), &expected))
	for name, spec := range map[string]any{
		"enterprise": &CommonSplunkSpec{},
		"kvservice":  &KVServiceSpec{},
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, json.Unmarshal([]byte(manifest), spec))
			encoded, err := json.Marshal(spec)
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, json.Unmarshal(encoded, &actual))
			for key, value := range expected {
				if key == "serviceTemplate" {
					service := actual[key].(map[string]any)
					assert.Equal(t, "ClusterIP", service["spec"].(map[string]any)["type"])
					continue
				}
				assert.Equal(t, value, actual[key], "field %s", key)
			}
			assert.NotContains(t, actual, "CommonWorkloadSpec")
			assert.NotContains(t, actual, "Spec")
		})
	}

	var enterprise CommonSplunkSpec
	require.NoError(t, json.Unmarshal([]byte(manifest), &enterprise))
	copy := enterprise.DeepCopy()
	copy.PodAnnotations["custom"] = "changed"
	copy.Volumes[0].Secret.SecretName = "changed"
	copy.ExtraEnv[0].ValueFrom.SecretKeyRef.Name = "changed"
	copy.ImagePullSecrets[0].Name = "changed"
	copy.Certs[0].DNSNames[0] = "changed"
	assert.Equal(t, "value", enterprise.PodAnnotations["custom"])
	assert.Equal(t, "postgres-tls", enterprise.Volumes[0].Secret.SecretName)
	assert.Equal(t, "postgres", enterprise.ExtraEnv[0].ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "registry", enterprise.ImagePullSecrets[0].Name)
	assert.Equal(t, "kv.example.com", enterprise.Certs[0].DNSNames[0])
}

func TestKVServiceSpecExcludesEnterpriseSettings(t *testing.T) {
	kvservice := reflect.TypeOf(KVServiceSpec{})
	enterprise := reflect.TypeOf(CommonSplunkSpec{})
	// Check every Enterprise-specific field, including legacy API fields.
	for i := 0; i < enterprise.NumField(); i++ {
		field := enterprise.Field(i).Name
		switch field {
		case "CommonWorkloadSpec", "LivenessProbe", "ReadinessProbe":
			// These settings are supported by both workload types.
			continue
		}
		t.Run(field, func(t *testing.T) {
			_, exists := kvservice.FieldByName(field)
			assert.False(t, exists, "KVService must not expose %s", field)
		})
	}
}
