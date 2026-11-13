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

package webhook_test

import (
	"testing"

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPostgresClusterClassPlacementAdmission(t *testing.T) {
	server := validation.NewWebhookServer(validation.WebhookServerOptions{Validators: validation.DefaultValidators})
	tests := []struct {
		name     string
		affinity platformApi.CNPGAffinityConfig
		want     string
	}{
		{name: "defaults"},
		{name: "qualified labels", affinity: platformApi.CNPGAffinityConfig{TopologyKey: "example.com/Zone_1", NodeSelector: map[string]string{"example.com/Pool_1": "Database_1", "empty": ""}}},
		{name: "wildcard toleration", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}}}},
		{name: "default Equal operator", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", Value: "database"}}}},
		{name: "timed NoExecute", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: new(int64(60))}}}},
		{name: "negative seconds supported by provider", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", Effect: corev1.TaintEffectNoExecute, TolerationSeconds: new(int64(-1))}}}},
		{name: "invalid topology key", affinity: platformApi.CNPGAffinityConfig{TopologyKey: "bad key"}, want: "topologyKey"},
		{name: "uppercase DNS prefix", affinity: platformApi.CNPGAffinityConfig{TopologyKey: "EXAMPLE.COM/zone"}, want: "topologyKey"},
		{name: "invalid selector key", affinity: platformApi.CNPGAffinityConfig{NodeSelector: map[string]string{"bad key": "db"}}, want: "nodeSelector"},
		{name: "invalid selector value", affinity: platformApi.CNPGAffinityConfig{NodeSelector: map[string]string{"pool": "bad value"}}, want: "nodeSelector"},
		{name: "invalid toleration key", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "bad/key/name"}}}, want: "tolerations[0].key"},
		{name: "invalid Equal value", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", Value: "bad value"}}}, want: "tolerations[0].value"},
		{name: "Exists value", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", Operator: corev1.TolerationOpExists, Value: "db"}}}, want: "tolerations[0].value"},
		{name: "empty key requires Exists", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpEqual}}}, want: "tolerations[0].operator"},
		{name: "unknown operator", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", Operator: "Unknown"}}}, want: "tolerations[0].operator"},
		{name: "unknown effect", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", Effect: "Unknown"}}}, want: "tolerations[0].effect"},
		{name: "seconds require effect", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", TolerationSeconds: new(int64(60))}}}, want: "tolerations[0].effect"},
		{name: "seconds require NoExecute", affinity: platformApi.CNPGAffinityConfig{Tolerations: []corev1.Toleration{{Key: "pool", Effect: corev1.TaintEffectNoSchedule, TolerationSeconds: new(int64(60))}}}, want: "tolerations[0].effect"},
	}
	for _, tt := range tests {
		for _, op := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			t.Run(tt.name+"/"+string(op), func(t *testing.T) {
				obj := &platformApi.PostgresClusterClass{
					APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresClusterClass", Name: "placement",
					Spec: platformApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io", CNPG: &platformApi.CNPGConfig{Affinity: &tt.affinity}},
				}
				old := obj.DeepCopy()
				old.Spec.CNPG = nil
				response := sendAdmissionReview(t, server, newPostgresClusterClassAdmissionReview(t, "placement", op, obj, old))
				assert.Equal(t, tt.want == "", response.Allowed)
				if tt.want != "" {
					require.NotNil(t, response.Result)
					assert.Equal(t, metav1.StatusReasonInvalid, response.Result.Reason)
					assert.Contains(t, response.Result.Message, "spec.cnpg.affinity."+tt.want)
				}
			})
		}
	}
}
