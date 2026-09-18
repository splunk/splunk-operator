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

package identity

import "k8s.io/apimachinery/pkg/types"

// ObjectIdentity identifies one Kubernetes object without exposing the object
// itself. UID is empty only when no durable provider UID has been observed.
type ObjectIdentity struct {
	// APIVersion is the Kubernetes API version for the identified object.
	APIVersion string
	// Kind is the Kubernetes kind for the identified object.
	Kind string
	// Name is the object name.
	Name string
	// Namespace is the object namespace.
	Namespace string
	// UID is the observed Kubernetes object UID when one is available.
	UID types.UID
}
