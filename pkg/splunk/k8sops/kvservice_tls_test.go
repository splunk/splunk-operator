// Copyright (c) 2026 Splunk Inc. All rights reserved.
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

package k8sops

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestApplyKVServiceDeploymentTLSChanges(t *testing.T) {
	tests := []struct {
		name                                     string
		currentTLS, desiredTLS, legacyEntrypoint bool
	}{
		{name: "enable TLS", desiredTLS: true},
		{name: "disable TLS", currentTLS: true},
		{name: "replace legacy HTTP entrypoint", legacyEntrypoint: true},
		{name: "replace legacy HTTPS entrypoint", currentTLS: true, desiredTLS: true, legacyEntrypoint: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, appsv1.AddToScheme(scheme))
			cr := &enterpriseApi.KVService{Name: "kv", Namespace: "test", Spec: enterpriseApi.KVServiceSpec{Replicas: 1}}
			certs := []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "serving-cert"}, Role: enterpriseApi.CertRoleServer}}
			if tt.currentTLS {
				cr.Spec.Certs = certs
			}
			current := resources.NewKVServiceDeployment(cr)
			if tt.legacyEntrypoint {
				current.Spec.Template.Spec.Containers[0].Command = nil
				current.Spec.Template.Spec.Containers[0].Args = nil
			}
			cr.Spec.Certs = nil
			if tt.desiredTLS {
				cr.Spec.Certs = certs
			}
			desired := resources.NewKVServiceDeployment(cr)
			expected := desired.Spec.Template.Spec.Containers[0].DeepCopy()
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
			phase, err := ApplyDeployment(t.Context(), c, desired)
			require.NoError(t, err)
			assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
			stored := &appsv1.Deployment{}
			key := types.NamespacedName{Namespace: current.Namespace, Name: current.Name}
			require.NoError(t, c.Get(t.Context(), key, stored))
			actual := stored.Spec.Template.Spec.Containers[0]
			assert.Equal(t, expected.Command, actual.Command)
			assert.Equal(t, expected.Args, actual.Args)
			assert.Equal(t, expected.LivenessProbe.HTTPGet, actual.LivenessProbe.HTTPGet)
			assert.Equal(t, expected.ReadinessProbe.HTTPGet, actual.ReadinessProbe.HTTPGet)
			version := stored.ResourceVersion
			_, err = ApplyDeployment(t.Context(), c, resources.NewKVServiceDeployment(cr))
			require.NoError(t, err)
			require.NoError(t, c.Get(t.Context(), key, stored))
			assert.Equal(t, version, stored.ResourceVersion, "unchanged configuration must not update the Deployment again")
		})
	}
}
