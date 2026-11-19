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

// Package types defines provider-neutral data exchanged by the
// PostgresDatabase bounded context and its adapters.
package types

import corev1 "k8s.io/api/core/v1"

// EnvironmentRetirement scopes declaration removal to one child and environment.
type EnvironmentRetirement struct {
	Child       corev1.ObjectReference
	Environment corev1.ObjectReference
	Databases   []DatabaseRetirement
}

type DatabaseRetirement struct {
	DatabaseName string
	Resources    []corev1.ObjectReference
}
