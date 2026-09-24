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
package pgcConstants

type State uint64

const (
	Empty        State = 0
	Ready        State = 1
	Pending      State = 2
	Provisioning State = 4
	Configuring  State = 8
	Failed       State = 16
)

// Contains reports whether all bits of state are set in s.
func (s State) Contains(state State) bool {
	return s&state == state
}

// Add sets the bits of state in s.
func (s State) Add(state State) State {
	return s | state
}

// Remove clears the bits of state from s.
func (s State) Remove(state State) State {
	return s &^ state
}
