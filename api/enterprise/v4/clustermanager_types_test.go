// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v4

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestClusterManagerRecoveryConfigDefaults(t *testing.T) {
	cfg := ClusterManagerRecoveryConfig{}
	assert.False(t, cfg.Enabled)
	assert.Zero(t, cfg.GracePeriodSeconds)
}

func TestClusterManagerRecoveryStatusDefaults(t *testing.T) {
	status := ClusterManagerRecoveryStatus{}
	assert.Empty(t, status.Phase)
	assert.Empty(t, status.NodeName)
	assert.Empty(t, status.PodUID)
	assert.Empty(t, status.Message)
	assert.Nil(t, status.TransitionTime)
}

func TestClusterManagerRecoveryConfigValidation(t *testing.T) {
	apiClient := requireAPIServer(t)

	t.Run("should reject gracePeriodSeconds below minimum", func(t *testing.T) {
		cm := &ClusterManager{
			ObjectMeta: metav1.ObjectMeta{Name: "recovery-below-min", Namespace: "default"},
			Spec: ClusterManagerSpec{
				Recovery: &ClusterManagerRecoveryConfig{
					Enabled:            true,
					GracePeriodSeconds: 10,
				},
			},
		}
		err := apiClient.Create(t.Context(), cm)
		assert.ErrorContains(t, err, "30")
	})

	t.Run("should accept gracePeriodSeconds at minimum", func(t *testing.T) {
		cm := &ClusterManager{
			ObjectMeta: metav1.ObjectMeta{Name: "recovery-at-min", Namespace: "default"},
			Spec: ClusterManagerSpec{
				Recovery: &ClusterManagerRecoveryConfig{
					Enabled:            true,
					GracePeriodSeconds: 30,
				},
			},
		}
		assert.NoError(t, apiClient.Create(t.Context(), cm))
	})

	t.Run("should accept gracePeriodSeconds above minimum", func(t *testing.T) {
		cm := &ClusterManager{
			ObjectMeta: metav1.ObjectMeta{Name: "recovery-above-min", Namespace: "default"},
			Spec: ClusterManagerSpec{
				Recovery: &ClusterManagerRecoveryConfig{
					Enabled:            true,
					GracePeriodSeconds: 120,
				},
			},
		}
		assert.NoError(t, apiClient.Create(t.Context(), cm))
	})

	t.Run("should default gracePeriodSeconds to 300 when not set", func(t *testing.T) {
		cm := &ClusterManager{
			ObjectMeta: metav1.ObjectMeta{Name: "recovery-default-grace", Namespace: "default"},
			Spec: ClusterManagerSpec{
				Recovery: &ClusterManagerRecoveryConfig{
					Enabled: true,
				},
			},
		}
		require.NoError(t, apiClient.Create(t.Context(), cm))

		fetched := &ClusterManager{}
		require.NoError(t, apiClient.Get(t.Context(),
			types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, fetched))
		require.NotNil(t, fetched.Spec.Recovery)
		assert.Equal(t, int32(300), fetched.Spec.Recovery.GracePeriodSeconds)
	})

	t.Run("should leave recovery nil when no recovery block is set", func(t *testing.T) {
		cm := &ClusterManager{
			ObjectMeta: metav1.ObjectMeta{Name: "recovery-absent", Namespace: "default"},
		}
		require.NoError(t, apiClient.Create(t.Context(), cm))

		fetched := &ClusterManager{}
		require.NoError(t, apiClient.Get(t.Context(),
			types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, fetched))
		assert.Nil(t, fetched.Spec.Recovery)
	})
}
