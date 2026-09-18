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

package upgrade

import (
	"context"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// EventPublisher is the event surface used by upgrade validation.
type EventPublisher interface {
	Normal(context.Context, string, string)
	Warning(context.Context, string, string)
}

type noOpEventPublisher struct{}

func (noOpEventPublisher) Normal(context.Context, string, string)  {}
func (noOpEventPublisher) Warning(context.Context, string, string) {}

// GetEventPublisher reads the publisher supplied by the reconcile-facing
// adapter. The no-op fallback preserves the previous behavior when validation
// is called without an event recorder.
var GetEventPublisher = func(ctx context.Context, _ splcommon.MetaObject) EventPublisher {
	if publisher, ok := ctx.Value(splcommon.EventPublisherKey).(EventPublisher); ok && publisher != nil {
		return publisher
	}
	return noOpEventPublisher{}
}
