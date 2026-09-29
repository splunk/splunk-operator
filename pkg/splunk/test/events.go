// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package test

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
)

// MockEvent stores Kubernetes event details captured by MockEventRecorder.
type MockEvent struct {
	EventType string
	Reason    string
	Message   string
}

// MockEventRecorder implements record.EventRecorder for tests that need to assert
// published events without draining a channel.
type MockEventRecorder struct {
	Events []MockEvent
}

func (m *MockEventRecorder) Event(_ runtime.Object, eventType, reason, message string) {
	m.Events = append(m.Events, MockEvent{EventType: eventType, Reason: reason, Message: message})
}

func (m *MockEventRecorder) Eventf(_ runtime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	m.Events = append(m.Events, MockEvent{EventType: eventType, Reason: reason, Message: fmt.Sprintf(messageFmt, args...)})
}

func (m *MockEventRecorder) AnnotatedEventf(_ runtime.Object, _ map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
	m.Events = append(m.Events, MockEvent{EventType: eventType, Reason: reason, Message: fmt.Sprintf(messageFmt, args...)})
}
