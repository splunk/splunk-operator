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

package core

import (
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFinalizationPlan(t *testing.T) {
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "postgres"}}
	conventional := testEnvironment(cluster, "orders", "", identitytypes.EnvironmentRoleAuthoritative)
	green := testEnvironment(cluster, "orders-green", "green-uid", identitytypes.EnvironmentRoleCandidate)
	blue := testEnvironment(cluster, "orders-blue", "blue-uid", identitytypes.EnvironmentRoleRetained)

	tests := []struct {
		name       string
		policy     string
		managed    []identitytypes.Environment
		wantAction finalizationAction
		wantNames  []string
		wantErr    string
	}{
		{
			name:       "deletes every unique managed environment",
			policy:     clusterDeletionPolicyDelete,
			managed:    []identitytypes.Environment{conventional, green, blue, green},
			wantAction: finalizationActionDelete,
			wantNames:  []string{"orders", "orders-green", "orders-blue"},
		},
		{
			name:       "retains every managed lifecycle role",
			policy:     clusterDeletionPolicyRetain,
			managed:    []identitytypes.Environment{conventional, green, blue},
			wantAction: finalizationActionRetain,
			wantNames:  []string{"orders", "orders-green", "orders-blue"},
		},
		{
			name:    "rejects unknown policy",
			policy:  "delete",
			managed: []identitytypes.Environment{conventional},
			wantErr: "unknown ClusterDeletionPolicy \"delete\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planned, err := finalizationPlan(tt.policy, identitytypes.ClusterCard{Managed: tt.managed})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, planned, len(tt.wantNames))
			for index, name := range tt.wantNames {
				assert.Equal(t, name, planned[index].Environment.Identity.Name)
				assert.Equal(t, tt.wantAction, planned[index].Action)
			}
		})
	}
}
