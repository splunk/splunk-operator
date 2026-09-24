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

import monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"

type EventKind int

const (
	EventConfigMapNotFound EventKind = iota
	EventInvalidQuery
	EventQueryApplied
	EventQueryRepaired
	EventCollision
	EventConfigTooLarge
	EventOwnershipConflict
)

type Event struct {
	Kind    EventKind
	Message string
}

type InvalidKind int

const (
	InvalidNone InvalidKind = iota
	InvalidConfigMapNotFound
	InvalidQuery
	InvalidCollision
	InvalidConfigTooLarge
	InvalidOwnershipConflict
)

// Invalid outcomes retain the complete last-known-good provisioner state.
type Outcome struct {
	Disabled              bool
	Pending               bool
	Configuring           bool
	Requeue               bool
	Invalid               InvalidKind
	InvalidDetail         string
	Events                []Event
	DatabaseContributions []monitoring.DatabaseAcknowledgement
}
