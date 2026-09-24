/*
Copyright 2026.

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

package custom_metrics

import (
	"context"

	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

// Provisioner owns provider-specific application, exact observation, and
// last-known-good persistence.
type Provisioner interface {
	Apply(ctx context.Context, cfg monitoring.AggregatedConfig) (monitoring.ExpectedState, error)
	Observe(ctx context.Context, expected monitoring.ExpectedState) (monitoring.Observation, error)
	Save(ctx context.Context, confirmed monitoring.ConfirmedState) (monitoring.SaveResult, error)
	Rollback(ctx context.Context) (monitoring.RollbackResult, error)
}
