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

package monitoring

// Target identifies the feature owner and provider resource without exposing
// provider or Kubernetes object types to the domain.
type Target struct {
	Namespace    string
	FeatureName  string
	FeatureUID   string
	ProviderName string
}

type ExpectedState struct {
	Revision   string
	Enabled    bool
	QueryCount int
}

type ObservationState int

const (
	ObservationPending ObservationState = iota
	ObservationReady
)

type Observation struct {
	State     ObservationState
	Confirmed *ConfirmedState
	Message   string
}

type ConfirmedState struct {
	Revision   string
	Enabled    bool
	QueryCount int
}

type SaveResult struct {
	Changed bool
}

type RollbackResult struct {
	Available bool
	Expected  ExpectedState
	Changed   bool
	Message   string
}
