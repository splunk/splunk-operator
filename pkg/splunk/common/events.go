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

package common

import "context"

// EventPublisher is the event surface used by workflows.
type EventPublisher interface {
	Normal(context.Context, string, string)
	Warning(context.Context, string, string)
}

// GetEventPublisher returns the event publisher supplied through context.
func GetEventPublisher(ctx context.Context) EventPublisher {
	publisher, _ := ctx.Value(EventPublisherKey).(EventPublisher)
	return publisher
}
