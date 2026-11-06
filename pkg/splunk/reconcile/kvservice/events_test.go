/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package kvservice

import (
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKVServiceStalledAndRecoveredEvents(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr, c := pendingPostgresFixture(t)
	previous := config.DefaultMutableFeatureGate.Enabled(config.PostgresController)
	t.Cleanup(func() {
		require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): previous}))
	})
	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false}))
	require.NoError(t, c.Update(t.Context(), cr))
	recorder := events.NewFakeRecorder(100)
	key := client.ObjectKeyFromObject(cr)

	// Like the other Enterprise controllers, a repeated terminal reconcile
	// emits another Warning. Recovery emits Normal only on True -> False.
	for attempt := 0; attempt < 2; attempt++ {
		result, err := Apply(t.Context(), c, key, recorder)
		_, terminal := splcommon.TerminalMessage(err)
		require.True(t, terminal)
		require.Zero(t, result.RequeueAfter)
		require.NoError(t, c.Get(t.Context(), key, cr))
		require.True(t, splcommon.IsStalled(cr.Status.Conditions))
		messages := drainKVServiceEvents(recorder)
		require.Equal(t, 1, countKVServiceEvents(messages, "Warning "+splcommon.EventReasonStalled+" "))
		require.Zero(t, countKVServiceEvents(messages, "Normal "+splcommon.EventReasonStalledResolved+" "))
	}

	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true}))
	require.NoError(t, c.Update(t.Context(), cr))
	result, err := Apply(t.Context(), c, key, recorder)
	require.NoError(t, err)
	require.Positive(t, result.RequeueAfter, "recovery can resume provisioning before workloads are Ready")
	require.NoError(t, c.Get(t.Context(), key, cr))
	require.Equal(t, enterpriseApi.PhasePending, cr.Status.Phase)
	require.False(t, splcommon.IsStalled(cr.Status.Conditions))
	messages := drainKVServiceEvents(recorder)
	require.Equal(t, 1, countKVServiceEvents(messages, "Normal "+splcommon.EventReasonStalledResolved+" "))
	require.Zero(t, countKVServiceEvents(messages, "Warning "+splcommon.EventReasonStalled+" "))
	for _, message := range messages {
		require.NotContains(t, message, "alpha-key")
		require.NotContains(t, message, "bravo-key")
	}

	_, err = Apply(t.Context(), c, key, recorder)
	require.NoError(t, err)
	require.Zero(t, countKVServiceEvents(drainKVServiceEvents(recorder), "Normal "+splcommon.EventReasonStalledResolved+" "), "ordinary provisioning must not repeat the recovery event")
}

func TestKVServiceRetryableFailureDoesNotEmitStalled(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr, _, c := postgresReadinessFixture(t)
	cr.Spec.ServiceAccount = "missing-service-account"
	require.NoError(t, c.Update(t.Context(), cr))
	recorder := events.NewFakeRecorder(100)
	_, err := Apply(t.Context(), c, client.ObjectKeyFromObject(cr), recorder)
	require.Error(t, err)
	_, terminal := splcommon.TerminalMessage(err)
	require.False(t, terminal)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cr), cr))
	require.False(t, splcommon.IsStalled(cr.Status.Conditions))
	messages := drainKVServiceEvents(recorder)
	require.Zero(t, countKVServiceEvents(messages, "Warning "+splcommon.EventReasonStalled+" "))
	require.Zero(t, countKVServiceEvents(messages, "Normal "+splcommon.EventReasonStalledResolved+" "))
}

func drainKVServiceEvents(recorder *events.FakeRecorder) []string {
	var messages []string
	for {
		select {
		case message := <-recorder.Events:
			messages = append(messages, message)
		default:
			return messages
		}
	}
}

func countKVServiceEvents(messages []string, prefix string) int {
	count := 0
	for _, message := range messages {
		if strings.HasPrefix(message, prefix) {
			count++
		}
	}
	return count
}
