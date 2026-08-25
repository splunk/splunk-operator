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
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var conflictErr = apierrors.NewConflict(schema.GroupResource{Group: "platform.splunk.com", Resource: "postgresclusters"}, "my-cluster", fmt.Errorf("rv mismatch"))
var errBusiness = fmt.Errorf("failed to fetch ClusterClass")

func TestIsPureConflict(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "single conflict error",
			err:      conflictErr,
			expected: true,
		},
		{
			name:     "single non-conflict error",
			err:      errBusiness,
			expected: false,
		},
		{
			name:     "wrapped non-conflict error (fmt.Errorf %w)",
			err:      fmt.Errorf("reconcile failed: %w", errBusiness),
			expected: false,
		},
		{
			name:     "joined: business + conflict — business wins",
			err:      errors.Join(errBusiness, conflictErr),
			expected: false,
		},
		{
			name:     "joined: conflict + business — business wins regardless of order",
			err:      errors.Join(conflictErr, errBusiness),
			expected: false,
		},
		{
			name:     "joined: two conflict errors",
			err:      errors.Join(conflictErr, conflictErr),
			expected: true,
		},
		{
			name:     "joined: single conflict (nil partner discarded by errors.Join)",
			err:      errors.Join(conflictErr, nil),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPureConflict(tt.err)
			if got != tt.expected {
				t.Errorf("IsPureConflict(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
