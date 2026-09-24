// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package enterprise

import (
	"context"
	"strings"
	"testing"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestClusterManagerEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.ClusterManager{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	ctx := context.TODO()
	k8sevent.Normal(ctx, "testing", "normal message")
	k8sevent.Warning(ctx, "testing", "warning message")

	cmaster := enterpriseApiV3.ClusterMaster{}
	k8sevent.instance = &cmaster
	k8sevent.Normal(ctx, "", "")
}

func TestIndexerClusterEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.IndexerCluster{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestMonitoringConsoleEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.MonitoringConsole{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestSearchHeadClusterEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.SearchHeadCluster{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestStandaloneEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.Standalone{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")

	// Negative testing
	ctx := context.TODO()
	k8sevent.recorder = nil
	k8sevent.publishEvent(ctx, "", "", "")

	// Test with different instance type (this should work with EventRecorder)
	k8sevent.recorder = recorder
	k8sevent.instance = &cm
	k8sevent.publishEvent(ctx, "Normal", "TestReason", "Test message")
}

func TestLicenseManagerEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	lmanager := enterpriseApi.LicenseManager{}
	k8sevent, err := newK8EventPublisher(recorder, &lmanager)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	ctx := context.TODO()
	k8sevent.Normal(ctx, "testing", "normal message")
	k8sevent.Warning(ctx, "testing", "warning message")

	lmaster := enterpriseApiV3.LicenseMaster{}
	k8sevent.instance = &lmaster
	k8sevent.Normal(ctx, "", "")

}

func TestEmitStalledTransitionEvents(t *testing.T) {
	stalledTrue := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionStalled),
			Status: metav1.ConditionTrue,
			Reason: string(enterpriseApi.ReasonStalled),
		},
	}
	stalledFalse := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionStalled),
			Status: metav1.ConditionFalse,
			Reason: string(enterpriseApi.ReasonNotStalled),
		},
	}
	empty := []metav1.Condition{}

	tests := []struct {
		name          string
		oldConditions []metav1.Condition
		newConditions []metav1.Condition
		wantCount     int
		wantType      string
		wantReason    string
	}{
		{
			name:          "false to true emits Warning/Stalled",
			oldConditions: stalledFalse,
			newConditions: stalledTrue,
			wantCount:     1,
			wantType:      "Warning",
			wantReason:    EventReasonStalled,
		},
		{
			name:          "true to false emits Normal/StalledResolved",
			oldConditions: stalledTrue,
			newConditions: stalledFalse,
			wantCount:     1,
			wantType:      "Normal",
			wantReason:    EventReasonStalledResolved,
		},
		{
			name:          "false stays false emits no event",
			oldConditions: stalledFalse,
			newConditions: stalledFalse,
			wantCount:     0,
		},
		{
			name:          "true stays true emits Warning on every stalled reconcile",
			oldConditions: stalledTrue,
			newConditions: stalledTrue,
			wantCount:     1,
			wantType:      "Warning",
			wantReason:    EventReasonStalled,
		},
		{
			name:          "empty old conditions treated as not-stalled, new stalled emits Warning",
			oldConditions: empty,
			newConditions: stalledTrue,
			wantCount:     1,
			wantType:      "Warning",
			wantReason:    EventReasonStalled,
		},
		{
			name:          "empty old conditions treated as not-stalled, new not-stalled emits no event",
			oldConditions: empty,
			newConditions: stalledFalse,
			wantCount:     0,
		},
	}

	cr := &enterpriseApi.Standalone{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &mockEventRecorder{events: []mockEvent{}}
			ep := &K8EventPublisher{recorder: rec, instance: cr}

			EmitStalledTransitionEvents(context.TODO(), ep, "test-cr", tc.oldConditions, tc.newConditions)

			if len(rec.events) != tc.wantCount {
				t.Fatalf("got %d events, want %d", len(rec.events), tc.wantCount)
			}
			if tc.wantCount == 0 {
				return
			}
			got := rec.events[0]
			if got.eventType != tc.wantType {
				t.Errorf("eventType: got %q, want %q", got.eventType, tc.wantType)
			}
			if got.reason != tc.wantReason {
				t.Errorf("reason: got %q, want %q", got.reason, tc.wantReason)
			}
			if !strings.Contains(got.message, "test-cr") {
				t.Errorf("message %q does not contain CR name", got.message)
			}
		})
	}
}

// TestEmitStalledTransitionEvents_AlwaysOnStalled verifies that a Warning event
// is emitted on every reconcile where Stalled=True, regardless of the previous state.
// StalledResolved is only emitted when the condition transitions from True→False.
func TestEmitStalledTransitionEvents_AlwaysOnStalled(t *testing.T) {
	stalled := []metav1.Condition{
		{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionTrue, Reason: string(enterpriseApi.ReasonStalled)},
	}
	notStalled := []metav1.Condition{
		{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionFalse, Reason: string(enterpriseApi.ReasonNotStalled)},
	}

	cr := &enterpriseApi.Standalone{}

	// Stall persists: Warning fires on every reconcile regardless of old conditions.
	for _, oldConds := range [][]metav1.Condition{stalled, notStalled} {
		rec := &mockEventRecorder{events: []mockEvent{}}
		ep := &K8EventPublisher{recorder: rec, instance: cr}
		EmitStalledTransitionEvents(context.TODO(), ep, "cr", oldConds, stalled)
		if len(rec.events) != 1 || rec.events[0].reason != EventReasonStalled {
			t.Errorf("oldConds=%v: got %v, want 1x Warning/Stalled", oldConds, rec.events)
		}
	}

	// Stall resolved: StalledResolved only fires when previously stalled.
	rec := &mockEventRecorder{events: []mockEvent{}}
	ep := &K8EventPublisher{recorder: rec, instance: cr}
	EmitStalledTransitionEvents(context.TODO(), ep, "cr", stalled, notStalled)
	if len(rec.events) != 1 || rec.events[0].reason != EventReasonStalledResolved {
		t.Errorf("resolved: got %v, want 1x Normal/StalledResolved", rec.events)
	}

	// Not stalled, was not stalled: no event.
	rec2 := &mockEventRecorder{events: []mockEvent{}}
	ep2 := &K8EventPublisher{recorder: rec2, instance: cr}
	EmitStalledTransitionEvents(context.TODO(), ep2, "cr", notStalled, notStalled)
	if len(rec2.events) != 0 {
		t.Errorf("not stalled persisting: got %d events, want 0", len(rec2.events))
	}
}

// TestEmitStalledTransitionEvents_BaselineAdvance verifies that after a
// StalledResolved event is emitted, the caller must advance the baseline so
// subsequent non-stalled calls in the same reconcile do not emit a duplicate.
func TestEmitStalledTransitionEvents_BaselineAdvance(t *testing.T) {
	preReconcile := []metav1.Condition{
		{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionTrue, Reason: string(enterpriseApi.ReasonStalled)},
	}
	notStalled := []metav1.Condition{
		{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionFalse, Reason: string(enterpriseApi.ReasonNotStalled)},
	}

	cr := &enterpriseApi.Standalone{}
	rec := &mockEventRecorder{events: []mockEvent{}}
	ep := &K8EventPublisher{recorder: rec, instance: cr}

	// First real non-stalled call: Stalled=True→False emits StalledResolved.
	EmitStalledTransitionEvents(context.TODO(), ep, "cr", preReconcile, notStalled)
	if len(rec.events) != 1 || rec.events[0].reason != EventReasonStalledResolved {
		t.Fatalf("first non-stalled call: got %v, want 1x Normal/StalledResolved", rec.events)
	}

	// Caller advances the baseline (the fix). Second non-stalled call must be silent.
	advancedBaseline := notStalled
	EmitStalledTransitionEvents(context.TODO(), ep, "cr", advancedBaseline, notStalled)
	if len(rec.events) != 1 {
		t.Errorf("second non-stalled call with advanced baseline: got %d events, want 0 new (duplicate StalledResolved)", len(rec.events)-1)
	}

	// Without baseline advance (stale baseline), a duplicate would be emitted.
	EmitStalledTransitionEvents(context.TODO(), ep, "cr", preReconcile, notStalled)
	if len(rec.events) != 2 || rec.events[1].reason != EventReasonStalledResolved {
		t.Errorf("second non-stalled call with stale baseline: got %v, want duplicate StalledResolved", rec.events)
	}
}

func TestEmitStalledTransitionEvents_NilPublisher(t *testing.T) {
	stalledTrue := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionStalled),
			Status: metav1.ConditionTrue,
			Reason: string(enterpriseApi.ReasonStalled),
		},
	}
	stalledFalse := []metav1.Condition{}

	// Must not panic with nil publisher
	EmitStalledTransitionEvents(context.TODO(), nil, "test-cr", stalledFalse, stalledTrue)
}

func TestGetEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	cm := &enterpriseApi.ClusterManager{}

	// Test 1: GetEventPublisher with recorder in context
	ctx := context.WithValue(context.TODO(), splcommon.EventRecorderKey, recorder)
	eventPublisher := GetEventPublisher(ctx, cm)
	if eventPublisher == nil {
		t.Error("Expected non-nil event publisher")
	}

	// Test 2: GetEventPublisher with existing publisher in context
	ctx = context.WithValue(context.TODO(), splcommon.EventPublisherKey, eventPublisher)
	eventPublisher2 := GetEventPublisher(ctx, cm)
	if eventPublisher2 != eventPublisher {
		t.Error("Expected to get same event publisher from context")
	}

	// Test 3: GetEventPublisher with no recorder in context
	ctx = context.TODO()
	eventPublisher3 := GetEventPublisher(ctx, cm)
	if eventPublisher3 == nil {
		t.Error("Expected non-nil event publisher even without recorder")
	}

	// Test 4: Verify publisher works (no panic)
	eventPublisher.Normal(context.TODO(), "TestReason", "Test message")
	eventPublisher.Warning(context.TODO(), "TestReason", "Test warning")
}
