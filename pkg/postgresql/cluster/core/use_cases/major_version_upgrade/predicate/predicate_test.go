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

package majorversionupgradepredicate

import (
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestPredicateObservesBlueGreenFalseAllowGate(t *testing.T) {
	tests := []struct {
		name string
		spec *platformv1alpha1.PostgresClusterSpec
		want bool
	}{
		{
			name: "pgUpgrade false allow remains inactive",
			spec: &platformv1alpha1.PostgresClusterSpec{PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{
				Allow: ptr.To(false),
			}},
			want: false,
		},
		{
			name: "blueGreen false allow is observed for rearm",
			spec: &platformv1alpha1.PostgresClusterSpec{PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{
				Allow:    ptr.To(false),
				Strategy: ptr.To(mvutypes.MajorUpgradeFlowBlueGreen),
			}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Predicate(tt.spec))
		})
	}
}
