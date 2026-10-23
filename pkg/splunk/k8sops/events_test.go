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
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
)

func TestK8EventPublisherWithEventsRecorder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := kubernetesfake.NewClientset()
	broadcaster := events.NewBroadcaster(&events.EventSinkImpl{Interface: c.EventsV1()})
	t.Cleanup(func() {
		cancel()
		broadcaster.Shutdown()
	})
	require.NoError(t, broadcaster.StartRecordingToSinkWithContext(ctx))
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	cr := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{Name: "kvservice", Namespace: "test", UID: types.UID("kvservice-uid")},
	}
	recorder := broadcaster.NewRecorder(scheme, "kvservice-controller")
	publisher := NewK8EventPublisherWithEventsRecorder(recorder, cr)
	publisher.Normal(ctx, "Ready", "100% ready")
	publisher.Warning(ctx, "ValidateSpecFailed", "invalid configuration")

	require.Eventually(t, func() bool {
		list, err := c.EventsV1().Events(cr.Namespace).List(ctx, metav1.ListOptions{})
		return err == nil && len(list.Items) == 2
	}, 5*time.Second, time.Millisecond)
	list, err := c.EventsV1().Events(cr.Namespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	for _, event := range list.Items {
		assert.Equal(t, "kvservice-controller", event.ReportingController)
		assert.NotEmpty(t, event.ReportingInstance)
		assert.Equal(t, "Reconcile", event.Action)
		assert.Equal(t, cr.Name, event.Regarding.Name)
		assert.Equal(t, cr.Namespace, event.Regarding.Namespace)
		assert.Equal(t, cr.UID, event.Regarding.UID)
		switch event.Reason {
		case "Ready":
			assert.Equal(t, "Normal", event.Type)
			assert.Equal(t, "100% ready", event.Note)
		case "ValidateSpecFailed":
			assert.Equal(t, "Warning", event.Type)
			assert.Equal(t, "invalid configuration", event.Note)
		default:
			t.Errorf("unexpected event reason: %s", event.Reason)
		}
	}
}

func TestK8EventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(2)
	publisher, err := NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.Standalone{})
	require.NoError(t, err)

	ctx := context.Background()
	publisher.Normal(ctx, "NormalReason", "normal message")
	publisher.Warning(ctx, "WarningReason", "warning message")

	assert.Eventually(t, func() bool {
		return len(recorder.Events) == 2
	}, time.Second, time.Millisecond)
	assert.Contains(t, <-recorder.Events, "NormalReason")
	assert.Contains(t, <-recorder.Events, "WarningReason")
}

func TestMonitoringConsoleEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(2)
	publisher, err := NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.MonitoringConsole{})
	require.NoError(t, err)

	ctx := context.Background()
	publisher.Normal(ctx, "NormalReason", "normal message")
	publisher.Warning(ctx, "WarningReason", "warning message")

	assert.Eventually(t, func() bool {
		return len(recorder.Events) == 2
	}, time.Second, time.Millisecond)
	assert.Contains(t, <-recorder.Events, "NormalReason")
	assert.Contains(t, <-recorder.Events, "WarningReason")
}

func TestEmitStalledTransitionEvents(t *testing.T) {
	recorder := record.NewFakeRecorder(2)
	publisher, err := NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.Standalone{})
	require.NoError(t, err)

	stalled := []metav1.Condition{{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionTrue}}
	resolved := []metav1.Condition{{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionFalse}}

	EmitStalledTransitionEvents(context.Background(), publisher, "standalone", nil, stalled)
	EmitStalledTransitionEvents(context.Background(), publisher, "standalone", stalled, resolved)

	assert.Eventually(t, func() bool {
		return len(recorder.Events) == 2
	}, time.Second, time.Millisecond)
	assert.Contains(t, <-recorder.Events, "Stalled")
	assert.Contains(t, <-recorder.Events, "StalledResolved")
}
