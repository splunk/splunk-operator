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
package clusterreadiness

// Input contains database-owned context needed to classify the current facts.
// PreviousClusterReadyReason is intentionally just a condition reason: events
// and full status objects remain facade concerns.
type Input struct {
	Namespace string
	Name      string

	WasReady                   bool
	PreviousClusterReadyReason string
}
