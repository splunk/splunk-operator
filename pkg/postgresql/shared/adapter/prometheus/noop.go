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
package prometheus

import "github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"

// NoopRecorder is a no-op implementation of Recorder for use in tests.
type NoopRecorder struct{}

func (n *NoopRecorder) IncStatusTransition(string, string, string, string) {}
func (n *NoopRecorder) ObserveProvisioningDuration(string, float64)        {}
func (n *NoopRecorder) SetClusterPhases(map[string]float64)                {}
func (n *NoopRecorder) SetPoolerEnabledClusters(float64)                   {}
func (n *NoopRecorder) SetDatabasePhases(map[string]float64)               {}
func (n *NoopRecorder) SetManagedUsers(string, map[string]float64)         {}

// Compile-time interface check.
var _ ports.Recorder = (*NoopRecorder)(nil)
