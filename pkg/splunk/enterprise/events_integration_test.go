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
	"k8s.io/client-go/tools/record"
)

// TestEventRecorderIntegration verifies that events are properly recorded
func TestEventRecorderIntegration(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		reason        string
		message       string
		expectInEvent []string
	}{
		{
			name:          "Warning event for validation failure",
			eventType:     "Warning",
			reason:        "ValidationFailed",
			message:       "Spec validation failed: invalid replicas",
			expectInEvent: []string{"Warning", "ValidationFailed", "invalid replicas"},
		},
		{
			name:          "Warning event for config error",
			eventType:     "Warning",
			reason:        "ConfigError",
			message:       "Failed to apply configuration",
			expectInEvent: []string{"Warning", "ConfigError", "Failed to apply"},
		},
		{
			name:          "Normal event for successful operation",
			eventType:     "Normal",
			reason:        "ConfigApplied",
			message:       "Configuration successfully applied",
			expectInEvent: []string{"Normal", "ConfigApplied", "successfully applied"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake recorder with buffer
			recorder := record.NewFakeRecorder(10)

			// Create a test instance
			cm := &enterpriseApi.ClusterManager{}
			cm.Name = "test-cm"
			cm.Namespace = "default"

			// Create event publisher
			eventPublisher, err := newK8EventPublisher(recorder, cm)
			if err != nil {
				t.Fatalf("Failed to create event publisher: %v", err)
			}

			ctx := context.TODO()

			// Publish event based on type
			if tt.eventType == "Warning" {
				eventPublisher.Warning(ctx, tt.reason, tt.message)
			} else {
				eventPublisher.Normal(ctx, tt.reason, tt.message)
			}

			// Verify event was recorded
			select {
			case event := <-recorder.Events:
				t.Logf("Received event: %s", event)
				for _, expected := range tt.expectInEvent {
					if !strings.Contains(event, expected) {
						t.Errorf("Expected event to contain '%s', got: %s", expected, event)
					}
				}
			case <-time.After(time.Second):
				t.Error("Timeout waiting for event")
			}
		})
	}
}

// TestEventRecorderNilHandling verifies that nil recorder is handled gracefully
func TestEventRecorderNilHandling(t *testing.T) {
	cm := &enterpriseApi.ClusterManager{}
	cm.Name = "test-cm"

	// Create event publisher with nil recorder (simulating test environment)
	eventPublisher, err := newK8EventPublisher(nil, cm)
	if err != nil {
		t.Fatalf("Failed to create event publisher with nil recorder: %v", err)
	}

	ctx := context.TODO()

	// These should not panic
	eventPublisher.Warning(ctx, "TestReason", "Test message")
	eventPublisher.Normal(ctx, "TestReason", "Test message")

	t.Log("Nil recorder handled gracefully without panic")
}

// TestEventRecorderMultipleEvents verifies multiple events are recorded
func TestEventRecorderMultipleEvents(t *testing.T) {
	recorder := record.NewFakeRecorder(100)

	cm := &enterpriseApi.ClusterManager{}
	cm.Name = "test-cm"

	eventPublisher, err := newK8EventPublisher(recorder, cm)
	if err != nil {
		t.Fatalf("Failed to create event publisher: %v", err)
	}

	ctx := context.TODO()

	// Emit multiple events
	events := []struct {
		eventType string
		reason    string
		message   string
	}{
		{"Warning", "ValidationFailed", "First error"},
		{"Warning", "ConfigError", "Second error"},
		{"Normal", "ConfigApplied", "Success message"},
	}

	for _, e := range events {
		if e.eventType == "Warning" {
			eventPublisher.Warning(ctx, e.reason, e.message)
		} else {
			eventPublisher.Normal(ctx, e.reason, e.message)
		}
	}

	// Verify all events were recorded
	receivedCount := 0
	timeout := time.After(2 * time.Second)

	for i := 0; i < len(events); i++ {
		select {
		case event := <-recorder.Events:
			receivedCount++
			t.Logf("Received event %d: %s", receivedCount, event)
		case <-timeout:
			t.Errorf("Only received %d events, expected %d", receivedCount, len(events))
			return
		}
	}

	if receivedCount != len(events) {
		t.Errorf("Expected %d events, received %d", len(events), receivedCount)
	}
}

// TestEventRecorderContextPassing verifies event recorder passes through context
func TestEventRecorderContextPassing(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	// Simulate controller setup
	ctx := context.Background()
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)

	// Verify recorder can be retrieved from context
	retrievedRecorder := ctx.Value(splcommon.EventRecorderKey)
	if retrievedRecorder == nil {
		t.Fatal("Failed to retrieve event recorder from context")
	}

	if rec, ok := retrievedRecorder.(record.EventRecorder); !ok {
		t.Error("Retrieved value is not an EventRecorder")
	} else {
		cm := &enterpriseApi.ClusterManager{}
		eventPublisher, err := newK8EventPublisher(rec, cm)
		if err != nil {
			t.Fatalf("Failed to create event publisher from context recorder: %v", err)
		}

		// Emit event
		eventPublisher.Warning(ctx, "TestReason", "Test from context")

		// Verify
		select {
		case event := <-recorder.Events:
			if !strings.Contains(event, "TestReason") {
				t.Errorf("Expected event with TestReason, got: %s", event)
			}
		case <-time.After(time.Second):
			t.Error("No event received")
		}
	}
}

// TestEventRecorderDifferentCRTypes verifies events work for all CR types
func TestEventRecorderDifferentCRTypes(t *testing.T) {
	testCases := []struct {
		name string
		cr   func() (*K8EventPublisher, *record.FakeRecorder)
	}{
		{
			name: "Standalone",
			cr: func() (*K8EventPublisher, *record.FakeRecorder) {
				recorder := record.NewFakeRecorder(10)
				pub, _ := newK8EventPublisher(recorder, &enterpriseApi.Standalone{})
				return pub, recorder
			},
		},
		{
			name: "ClusterManager",
			cr: func() (*K8EventPublisher, *record.FakeRecorder) {
				recorder := record.NewFakeRecorder(10)
				pub, _ := newK8EventPublisher(recorder, &enterpriseApi.ClusterManager{})
				return pub, recorder
			},
		},
		{
			name: "IndexerCluster",
			cr: func() (*K8EventPublisher, *record.FakeRecorder) {
				recorder := record.NewFakeRecorder(10)
				pub, _ := newK8EventPublisher(recorder, &enterpriseApi.IndexerCluster{})
				return pub, recorder
			},
		},
		{
			name: "SearchHeadCluster",
			cr: func() (*K8EventPublisher, *record.FakeRecorder) {
				recorder := record.NewFakeRecorder(10)
				pub, _ := newK8EventPublisher(recorder, &enterpriseApi.SearchHeadCluster{})
				return pub, recorder
			},
		},
		{
			name: "LicenseManager",
			cr: func() (*K8EventPublisher, *record.FakeRecorder) {
				recorder := record.NewFakeRecorder(10)
				pub, _ := newK8EventPublisher(recorder, &enterpriseApi.LicenseManager{})
				return pub, recorder
			},
		},
		{
			name: "MonitoringConsole",
			cr: func() (*K8EventPublisher, *record.FakeRecorder) {
				recorder := record.NewFakeRecorder(10)
				pub, _ := newK8EventPublisher(recorder, &enterpriseApi.MonitoringConsole{})
				return pub, recorder
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			eventPublisher, recorder := tt.cr()
			if eventPublisher == nil {
				t.Fatalf("Failed to create event publisher for %s", tt.name)
			}

			ctx := context.TODO()
			eventPublisher.Warning(ctx, "TestEvent", "Testing "+tt.name)

			select {
			case event := <-recorder.Events:
				if !strings.Contains(event, "TestEvent") {
					t.Logf("Event for %s: %s", tt.name, event)
				}
			case <-time.After(time.Second):
				t.Errorf("No event received for %s", tt.name)
			}
		})
	}
}

// TestEventRecorderRealWorldScenario simulates real reconciliation scenario
func TestEventRecorderRealWorldScenario(t *testing.T) {
	recorder := record.NewFakeRecorder(50)

	cm := &enterpriseApi.ClusterManager{}
	cm.Name = "production-cm"
	cm.Namespace = "splunk"

	// Create event publisher
	eventPublisher, err := newK8EventPublisher(recorder, cm)
	if err != nil {
		t.Fatalf("Failed to create event publisher: %v", err)
	}

	ctx := context.TODO()
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Simulate reconciliation flow with various events
	scenarios := []struct {
		stage   string
		success bool
		reason  string
		message string
	}{
		{"Validation", false, "validateClusterManagerSpec", "validate clustermanager spec failed"},
		{"Config", false, "ApplySplunkConfig", "create or update general config failed"},
		{"Service", false, "ApplyService", "create or update service failed"},
		{"StatefulSet", false, "getStatefulSet", "failed to create statefulset"},
	}

	for _, scenario := range scenarios {
		t.Logf("Testing scenario: %s", scenario.stage)
		eventPublisher.Warning(ctx, scenario.reason, scenario.message)

		select {
		case event := <-recorder.Events:
			if !strings.Contains(event, scenario.reason) {
				t.Errorf("Expected event with reason '%s', got: %s", scenario.reason, event)
			}
			t.Logf("✓ Event recorded for %s: %s", scenario.stage, event)
		case <-time.After(time.Second):
			t.Errorf("No event received for scenario: %s", scenario.stage)
		}
	}
}

// BenchmarkEventEmission benchmarks event emission performance
func BenchmarkEventEmission(b *testing.B) {
	recorder := record.NewFakeRecorder(10000)
	cm := &enterpriseApi.ClusterManager{}
	eventPublisher, _ := newK8EventPublisher(recorder, cm)
	ctx := context.TODO()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventPublisher.Warning(ctx, "BenchmarkReason", "Benchmark message")
	}
}
