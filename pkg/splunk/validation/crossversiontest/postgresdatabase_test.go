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

package crossversiontest

import (
	"testing"

	v3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	v4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPostgresDatabaseMountsV4Only(t *testing.T) {
	if apiServerClient == nil {
		t.Skip("KUBEBUILDER_ASSETS is not set; run via make test")
	}
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	mounts := []v4.PostgresDatabaseMountSpec{{Name: "app", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "database"}, Database: "app"}}
	for _, kind := range []string{"Standalone", "SearchHeadCluster"} {
		t.Run(kind, func(t *testing.T) {
			var stored, old client.Object
			if kind == "Standalone" {
				stored = &v4.Standalone{Name: "postgres-standalone", Namespace: "default", Spec: v4.StandaloneSpec{Replicas: 1, PostgresDatabaseMounts: mounts}}
				old = &v3.Standalone{}
			} else {
				stored = &v4.SearchHeadCluster{Name: "postgres-shc", Namespace: "default", Spec: v4.SearchHeadClusterSpec{Replicas: 3, PostgresDatabaseMounts: mounts}}
				old = &v3.SearchHeadCluster{}
			}
			require.NoError(t, apiServerClient.Create(t.Context(), stored))
			t.Cleanup(func() { _ = apiServerClient.Delete(t.Context(), stored) })
			key := client.ObjectKeyFromObject(stored)
			before := stored.GetResourceVersion()
			// GET remains allowed, but an older client cannot safely write back.
			require.NoError(t, apiServerClient.Get(t.Context(), key, old))
			old.SetLabels(map[string]string{"edited-through-v3": "true"})
			err := apiServerClient.Update(t.Context(), old)
			require.ErrorContains(t, err, "spec.postgresDatabaseMounts set but unsupported in v3")
			err = apiServerClient.SubResource("scale").Update(t.Context(), old, client.WithSubResourceBody(&autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: 5}}))
			require.ErrorContains(t, err, "spec.postgresDatabaseMounts set but unsupported in v3")
			require.NoError(t, apiServerClient.Get(t.Context(), key, stored))
			assert.Equal(t, before, stored.GetResourceVersion())
		})
	}
}
