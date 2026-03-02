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

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// K8EventPublisher structure used to publish k8s event
type K8EventPublisher struct {
	recorder record.EventRecorder
	instance runtime.Object
}

// newK8EventPublisher creates a new k8s event publisher (variable to allow mocking in tests)
var newK8EventPublisher = func(recorder record.EventRecorder, instance runtime.Object) (*K8EventPublisher, error) {
	eventPublisher := &K8EventPublisher{
		recorder: recorder,
		instance: instance,
	}

	return eventPublisher, nil
}

// NewK8EventPublisherWithRecorder creates a new k8s event publisher with recorder (exported for controller use)
func NewK8EventPublisherWithRecorder(recorder record.EventRecorder, instance runtime.Object) (*K8EventPublisher, error) {
	return newK8EventPublisher(recorder, instance)
}

// publishEvent adds events to k8s using event recorder
func (k *K8EventPublisher) publishEvent(ctx context.Context, eventType, reason, message string) {
	if k == nil {
		return
	}

	// in the case of testing, recorder is not passed
	if k.recorder == nil {
		return
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("PublishEvent")
	scopedLog.Info("publishing event", "eventType", eventType, "reason", reason, "message", message)

	// Use the EventRecorder to emit the event
	k.recorder.Event(k.instance, eventType, reason, message)
}

// Normal publish normal events to k8s
func (k *K8EventPublisher) Normal(ctx context.Context, reason, message string) {
	k.publishEvent(ctx, "Normal", reason, message)
}

// Warning publish warning events to k8s
func (k *K8EventPublisher) Warning(ctx context.Context, reason, message string) {
	k.publishEvent(ctx, "Warning", reason, message)
}

// GetEventPublisher returns an event publisher from context if available,
// otherwise creates a new one. This is a shared helper to avoid code duplication.
func GetEventPublisher(ctx context.Context, cr runtime.Object) *K8EventPublisher {
	// First check if there's already an event publisher in context
	if pub := ctx.Value(splcommon.EventPublisherKey); pub != nil {
		if eventPublisher, ok := pub.(*K8EventPublisher); ok {
			return eventPublisher
		}
	}

	// Otherwise, create a new one from the recorder in context
	var recorder record.EventRecorder
	if rec := ctx.Value(splcommon.EventRecorderKey); rec != nil {
		recorder, _ = rec.(record.EventRecorder)
	}
	eventPublisher, _ := newK8EventPublisher(recorder, cr)
	return eventPublisher
}
