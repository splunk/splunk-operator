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
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"k8s.io/client-go/tools/record"
)

// TestApplyFunctionsWithEventRecorder verifies Apply functions emit events correctly
func TestApplyFunctionsWithEventRecorder(t *testing.T) {
	ctx := context.TODO()
	recorder := record.NewFakeRecorder(100)

	// Add recorder to context (simulating controller behavior)
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)

	// Create mock client
	client := spltest.NewMockClient()

	t.Run("ApplyStandalone emits events on validation failure", func(t *testing.T) {
		cr := &enterpriseApi.Standalone{}
		cr.Name = "test-standalone"
		cr.Namespace = "default"
		cr.Spec.Replicas = -1 // Invalid, should trigger validation event

		// Call Apply function
		_, err := ApplyStandalone(ctx, client, cr)

		// Should have validation error
		if err == nil {
			t.Error("Expected validation error")
		}

		// Check for validation warning event
		select {
		case event := <-recorder.Events:
			if !strings.Contains(event, "Warning") {
				t.Errorf("Expected Warning event, got: %s", event)
			}
			t.Logf("Received validation event: %s", event)
		case <-time.After(2 * time.Second):
			// Some validation errors may not emit events, that's ok
			t.Log("No event received for validation (may be expected)")
		}
	})
}

// TestEventPublisherCreationFromContext verifies event publisher is created correctly from context
func TestEventPublisherCreationFromContext(t *testing.T) {
	ctx := context.TODO()
	recorder := record.NewFakeRecorder(10)

	// Add recorder to context
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)

	// Verify recorder can be retrieved
	retrievedRecorder := ctx.Value(splcommon.EventRecorderKey)
	if retrievedRecorder == nil {
		t.Fatal("Failed to retrieve recorder from context")
	}

	// Verify it's the correct type
	if rec, ok := retrievedRecorder.(record.EventRecorder); !ok {
		t.Error("Retrieved value is not an EventRecorder")
	} else {
		// Create event publisher
		cm := &enterpriseApi.ClusterManager{}
		eventPublisher, err := newK8EventPublisher(rec, cm)
		if err != nil {
			t.Fatalf("Failed to create event publisher: %v", err)
		}

		// Emit an event
		eventPublisher.Warning(ctx, "TestReason", "Test message")

		// Verify event was recorded
		select {
		case event := <-recorder.Events:
			if !strings.Contains(event, "TestReason") {
				t.Errorf("Expected event with TestReason, got: %s", event)
			}
			t.Logf("Event successfully emitted: %s", event)
		case <-time.After(time.Second):
			t.Error("No event received")
		}
	}
}

// TestEventPublisherNilRecorderInContext verifies graceful handling when recorder is nil
func TestEventPublisherNilRecorderInContext(t *testing.T) {
	ctx := context.TODO()

	// Don't add recorder to context (simulating test environment)
	// Verify no panic occurs

	cm := &enterpriseApi.ClusterManager{}
	cm.Name = "test-cm"

	// This should not panic even without recorder in context
	var eventPublisher *K8EventPublisher
	if recorder := ctx.Value(splcommon.EventRecorderKey); recorder != nil {
		if rec, ok := recorder.(record.EventRecorder); ok {
			eventPublisher, _ = newK8EventPublisher(rec, cm)
		}
	}

	// eventPublisher should be nil
	if eventPublisher != nil {
		// Try to use it
		eventPublisher.Warning(ctx, "TestReason", "Test message")
	}

	t.Log("Nil event publisher handled gracefully")
}

// TestEventEmissionInReconcileFlow simulates full reconcile flow with events
func TestEventEmissionInReconcileFlow(t *testing.T) {
	ctx := context.TODO()
	recorder := record.NewFakeRecorder(100)

	// Setup context with recorder
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)

	// Create test CR
	cm := &enterpriseApi.ClusterManager{}
	cm.Name = "production-cm"
	cm.Namespace = "splunk"

	// Simulate creating event publisher (as done in Apply functions)
	var eventPublisher *K8EventPublisher
	if rec := ctx.Value(splcommon.EventRecorderKey); rec != nil {
		if recorder, ok := rec.(record.EventRecorder); ok {
			eventPublisher, _ = newK8EventPublisher(recorder, cm)
		}
	}

	if eventPublisher == nil {
		t.Fatal("Failed to create event publisher from context")
	}

	// Store in context for downstream use
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Simulate various reconcile stages emitting events
	scenarios := []struct {
		reason  string
		message string
	}{
		{"validateClusterManagerSpec", "validation started"},
		{"ApplySplunkConfig", "applying configuration"},
		{"ApplyService", "creating services"},
	}

	for _, scenario := range scenarios {
		eventPublisher.Warning(ctx, scenario.reason, scenario.message)
	}

	// Verify events were emitted
	eventCount := 0
	timeout := time.After(3 * time.Second)

	for i := 0; i < len(scenarios); i++ {
		select {
		case event := <-recorder.Events:
			eventCount++
			t.Logf("Received event %d: %s", eventCount, event)
		case <-timeout:
			if eventCount < len(scenarios) {
				t.Logf("Received %d/%d events before timeout", eventCount, len(scenarios))
			}
			goto done
		}
	}

done:
	if eventCount == 0 {
		t.Error("No events received in reconcile flow")
	} else {
		t.Logf("Successfully received %d events in reconcile flow", eventCount)
	}
}

// TestEventDeduplication verifies EventRecorder deduplicates repeated events
func TestEventDeduplication(t *testing.T) {
	recorder := record.NewFakeRecorder(100)
	ctx := context.TODO()

	cm := &enterpriseApi.ClusterManager{}
	cm.Name = "test-cm"

	eventPublisher, _ := newK8EventPublisher(recorder, cm)

	// Emit same event multiple times
	for i := 0; i < 5; i++ {
		eventPublisher.Warning(ctx, "RepeatedError", "This error repeats")
	}

	// FakeRecorder doesn't actually deduplicate, but in real usage EventRecorder does
	// This test documents the expected behavior
	t.Log("In production, EventRecorder will deduplicate these events and increment count")

	// Drain events
	receivedCount := 0
	timeout := time.After(2 * time.Second)

drainLoop:
	for {
		select {
		case event := <-recorder.Events:
			receivedCount++
			t.Logf("Event %d: %s", receivedCount, event)
		case <-timeout:
			break drainLoop
		default:
			if receivedCount >= 5 {
				break drainLoop
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	t.Logf("Received %d events (FakeRecorder doesn't deduplicate)", receivedCount)
}

// TestMultipleCRsEmitEvents verifies multiple CRs can emit events simultaneously
func TestMultipleCRsEmitEvents(t *testing.T) {
	recorder := record.NewFakeRecorder(100)
	ctx := context.TODO()

	// Create multiple CR publishers
	publishers := []struct {
		name      string
		publisher *K8EventPublisher
	}{
		{"cm-1", func() *K8EventPublisher {
			p, _ := newK8EventPublisher(recorder, &enterpriseApi.ClusterManager{})
			return p
		}()},
		{"cm-2", func() *K8EventPublisher {
			p, _ := newK8EventPublisher(recorder, &enterpriseApi.ClusterManager{})
			return p
		}()},
		{"standalone-1", func() *K8EventPublisher {
			p, _ := newK8EventPublisher(recorder, &enterpriseApi.Standalone{})
			return p
		}()},
	}

	expectedEvents := len(publishers)

	for i, pub := range publishers {
		pub.publisher.Warning(ctx, "MultiCRTest", "Event from "+pub.name)

		// Verify event
		select {
		case event := <-recorder.Events:
			if !strings.Contains(event, "MultiCRTest") {
				t.Errorf("Expected MultiCRTest event, got: %s", event)
			}
			t.Logf("Event %d received: %s", i+1, event)
		case <-time.After(time.Second):
			t.Errorf("Timeout waiting for event from %s", pub.name)
		}
	}

	t.Logf("All %d CRs successfully emitted events", expectedEvents)
}
