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

package reconcile

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// IsPureConflict reports whether err is non-nil and every non-nil error
// within it is a 409 Conflict. When a business error and a status-write
// conflict are joined together the business error takes priority and this
// returns false, preserving exponential backoff for real failures.
//
// TODO(human): implement this function.
// Guidance: errors.Join wraps multiple errors; use the Unwrap() []error
// interface to walk all joined errors. Consider all four cases:
//   - err == nil                      → false
//   - single conflict error           → true
//   - single non-conflict error       → false
//   - joined errors, mixed conflict   → false (business error wins)
func IsPureConflict(err error) bool {
	if err == nil {
		return false
	}
	_ = apierrors.IsConflict // ensure the import is used once implemented
	// TODO(human): replace this placeholder with the real implementation
	return false
}
