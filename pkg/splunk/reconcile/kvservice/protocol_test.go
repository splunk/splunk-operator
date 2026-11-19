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

package kvservice

import (
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func TestKVServiceClientProtocolDiagnosticsAndRecovery(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr, _, c := postgresReadinessFixture(t)
	ns := &corev1.Namespace{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "ns-a"}, ns))
	ns.Annotations[splcommon.AnnotationKVServiceAddr] = "https://splunk-kvs2-kvservice-service.shared:8066"
	require.NoError(t, c.Update(t.Context(), ns))
	key := client.ObjectKeyFromObject(cr)
	recorder := events.NewFakeRecorder(100)
	for range 2 {
		_, err := Apply(t.Context(), c, key, recorder)
		require.NoError(t, err)
		require.NoError(t, c.Get(t.Context(), key, cr))
		condition := meta.FindStatusCondition(cr.Status.Conditions, clientConfigurationReady)
		require.NotNil(t, condition)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, "ListenerProtocolMismatch", condition.Reason)
		require.Contains(t, condition.Message, "ns-a")
		require.Contains(t, condition.Message, "http://splunk-kvs2-kvservice-service.shared:8066")
		require.Len(t, cr.Status.Tenants, 2)
		for _, tenant := range cr.Status.Tenants {
			require.True(t, tenant.Ready, "protocol diagnosis must not block tenant DB provisioning")
		}
	}
	require.Equal(t, 1, countKVServiceEvents(drainKVServiceEvents(recorder), "Warning ListenerProtocolMismatch "))
	membership := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(resources.NewKVServiceTenantConfig(cr, nil)), membership))
	require.Len(t, membership.Data, 2, "both tenants must remain eligible")
	ns.Annotations[splcommon.AnnotationKVServiceAddr] = "splunk-kvs2-kvservice-service.shared:8066"
	require.NoError(t, c.Update(t.Context(), ns))
	_, err := Apply(t.Context(), c, key, recorder)
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), key, cr))
	require.Equal(t, metav1.ConditionTrue, meta.FindStatusCondition(cr.Status.Conditions, clientConfigurationReady).Status)
	require.Equal(t, 1, countKVServiceEvents(drainKVServiceEvents(recorder), "Normal ListenerProtocolMatched "))
	cr.Spec.Scheme = "https" // Missing certificates fail evaluation before discovery.
	require.NoError(t, c.Update(t.Context(), cr))
	_, err = Apply(t.Context(), c, key, recorder)
	require.Error(t, err)
	require.NoError(t, c.Get(t.Context(), key, cr))
	require.Equal(t, metav1.ConditionUnknown, meta.FindStatusCondition(cr.Status.Conditions, clientConfigurationReady).Status, "a failed evaluation must not retain stale success")
}
