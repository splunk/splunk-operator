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

type eventPublisher interface {
	Normal(context.Context, string, string)
	Warning(context.Context, string, string)
}

type noopEventPublisher struct{}

func (noopEventPublisher) Normal(context.Context, string, string)  {}
func (noopEventPublisher) Warning(context.Context, string, string) {}

func getEventPublisher(ctx context.Context) eventPublisher {
	if publisher, ok := ctx.Value(splcommon.EventPublisherKey).(eventPublisher); ok && publisher != nil {
		return publisher
	}
	return noopEventPublisher{}
}
