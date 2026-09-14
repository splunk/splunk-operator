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

package k8sops

import (
	"context"
	"testing"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplyMonitoringConsoleEnvConfigMap(t *testing.T) {
	ctx := context.Background()
	client := spltest.NewMockClient()

	configMap, err := ApplyMonitoringConsoleEnvConfigMap(ctx, client, "test", "standalone", "monitoring-console", []corev1.EnvVar{{Name: "SPLUNK_INDEXER_URL", Value: "indexer-0"}}, true)
	require.NoError(t, err)
	assert.Equal(t, "indexer-0", configMap.Data["SPLUNK_INDEXER_URL"])

	configMap, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, "test", "standalone", "monitoring-console", []corev1.EnvVar{{Name: "SPLUNK_INDEXER_URL", Value: "indexer-0,indexer-1"}}, true)
	require.NoError(t, err)
	assert.Equal(t, "indexer-0,indexer-1", configMap.Data["SPLUNK_INDEXER_URL"])
}

func TestAddAndDeleteURLsConfigMap(t *testing.T) {
	configMap := &corev1.ConfigMap{Data: map[string]string{
		"SPLUNK_STANDALONE_URL": "splunk-test-cr-standalone-0,splunk-other-cr-standalone-0",
	}}

	AddMonitoringConsoleURLs(configMap, "test-cr", []corev1.EnvVar{{Name: "SPLUNK_STANDALONE_URL", Value: "splunk-test-cr-standalone-0,splunk-test-cr-standalone-1"}})
	assert.Contains(t, configMap.Data["SPLUNK_STANDALONE_URL"], "splunk-test-cr-standalone-1")
	assert.Contains(t, configMap.Data["SPLUNK_STANDALONE_URL"], "splunk-other-cr-standalone-0")

	DeleteMonitoringConsoleURLs(configMap, "test-cr", []corev1.EnvVar{{Name: "SPLUNK_STANDALONE_URL", Value: "splunk-test-cr-standalone-0"}}, false)
	assert.NotContains(t, configMap.Data["SPLUNK_STANDALONE_URL"], "splunk-test-cr-standalone-1")
	assert.Contains(t, configMap.Data["SPLUNK_STANDALONE_URL"], "splunk-other-cr-standalone-0")
}

func TestValidateMonitoringConsoleRef(t *testing.T) {
	ctx := context.Background()
	client := spltest.NewMockClient()

	current := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-monitoring-console", Namespace: "test"}, Data: map[string]string{"A": "a"}}
	_, err := ApplyConfigMap(ctx, client, current)
	require.NoError(t, err)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-s1-standalone", Namespace: "test"},
		Spec:       appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: []corev1.EnvVar{{Name: "SPLUNK_MONITORING_CONSOLE_REF", Value: "test"}}}}}}},
	}
	require.NoError(t, splutil.CreateResource(ctx, client, statefulSet))

	revised := statefulSet.DeepCopy()
	revised.Spec.Template.Spec.Containers[0].Env[0].Value = "new-test"
	require.NoError(t, ValidateMonitoringConsoleRef(ctx, client, revised, []corev1.EnvVar{{Name: "A", Value: "a"}}))
}
