/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hubApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// populatedIndexerCluster returns a v3 IndexerCluster with every field set to a
// distinctive non-zero value, so a dropped field shows up as a round-trip diff
// rather than passing by coincidence against a zero value.
func populatedIndexerCluster() *IndexerCluster {
	return &IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "idx",
			Namespace:   "splunk",
			Labels:      map[string]string{"tier": "indexing"},
			Annotations: map[string]string{"indexercluster.enterprise.splunk.com/paused": "true"},
			Finalizers:  []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: IndexerClusterSpec{
			Replicas: 7,
			CommonSplunkSpec: hubApi.CommonSplunkSpec{
				ClusterManagerRef:    corev1.ObjectReference{Name: "cm", Namespace: "splunk"},
				ClusterMasterRef:     corev1.ObjectReference{Name: "legacy-cm"},
				LicenseManagerRef:    corev1.ObjectReference{Name: "lm"},
				MonitoringConsoleRef: corev1.ObjectReference{Name: "mc"},
				Mock:                 true,
				ServiceAccount:       "splunk-sa",
				EtcVolumeStorageConfig: hubApi.StorageClassSpec{
					StorageCapacity:  "10Gi",
					StorageClassName: "gp3",
				},
				VarVolumeStorageConfig: hubApi.StorageClassSpec{
					StorageCapacity: "100Gi",
				},
				Spec: hubApi.Spec{
					Image:           "splunk/splunk:10.0.0",
					ImagePullPolicy: "IfNotPresent",
					SchedulerName:   "default-scheduler",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("2"),
						},
					},
				},
			},
		},
		Status: IndexerClusterStatus{
			Phase:                          hubApi.PhaseReady,
			ClusterMasterPhase:             hubApi.PhasePending,
			ClusterManagerPhase:            hubApi.PhaseReady,
			Replicas:                       7,
			ReadyReplicas:                  6,
			Selector:                       "app=splunk",
			Initialized:                    true,
			IndexingReady:                  true,
			ServiceReady:                   true,
			IndexerSecretChanged:           []bool{true, false},
			NamespaceSecretResourceVersion: "12345",
			IdxcPasswordChangedSecrets:     map[string]bool{"secret-a": true},
			MaintenanceMode:                true,
			Peers: []IndexerClusterMemberStatus{{
				ID:             "guid-1",
				Name:           "splunk-idx-0",
				Status:         "Up",
				ActiveBundleID: "bundle-1",
				BucketCount:    42,
				Searchable:     true,
			}},
		},
	}
}

// populatedSearchHeadCluster returns a v3 SearchHeadCluster with every field set to a
// distinctive non-zero value, so a dropped field shows up as a round-trip diff
// rather than passing by coincidence against a zero value.
func populatedSearchHeadCluster() *SearchHeadCluster {
	return &SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shc",
			Namespace: "splunk",
			Labels:    map[string]string{"tier": "search"},
		},
		Spec: SearchHeadClusterSpec{
			Replicas:                3,
			DetentionTimeoutSeconds: 1800,
			CommonSplunkSpec: hubApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
				ServiceAccount:    "splunk-sa",
			},
			AppFrameworkConfig: hubApi.AppFrameworkSpec{
				Defaults:             hubApi.AppSourceDefaultSpec{Scope: "cluster"},
				AppsRepoPollInterval: 600,
			},
		},
		Status: SearchHeadClusterStatus{
			Phase:                          hubApi.PhaseReady,
			DeployerPhase:                  hubApi.PhaseReady,
			Replicas:                       3,
			ReadyReplicas:                  3,
			Selector:                       "app=splunk-shc",
			Captain:                        "splunk-shc-0",
			CaptainReady:                   true,
			Initialized:                    true,
			MinPeersJoined:                 true,
			MaintenanceMode:                true,
			ShcSecretChanged:               []bool{true},
			AdminSecretChanged:             []bool{false, true},
			AdminPasswordChangedSecrets:    map[string]bool{"admin-secret": true},
			NamespaceSecretResourceVersion: "54321",
			TelAppInstalled:                true,
			DetentionStartTimestamp:        1700000000,
			DetainedMemberName:             "splunk-shc-1",
			DetainedPodRevision:            "rev-7",
			Members: []SearchHeadClusterMemberStatus{{
				Name:                        "splunk-shc-0",
				Status:                      "Up",
				Adhoc:                       true,
				Registered:                  true,
				ActiveHistoricalSearchCount: 4,
				ActiveRealtimeSearchCount:   2,
				PodRevision:                 "rev-7",
			}},
		},
	}
}

// TestIndexerClusterConvertTo covers the upgrade direction, v3 -> v4, where the v3 object is the source.
func TestIndexerClusterConvertTo(t *testing.T) {
	t.Run("preserves every shared field", func(t *testing.T) {
		src := populatedIndexerCluster()
		hub := &hubApi.IndexerCluster{}

		require.NoError(t, src.ConvertTo(hub))

		assert.Equal(t, src.ObjectMeta, hub.ObjectMeta)
		assert.Equal(t, src.Spec.Replicas, hub.Spec.Replicas)
		assert.Equal(t, src.Spec.CommonSplunkSpec, hub.Spec.CommonSplunkSpec)
		assert.Equal(t, src.Status.Phase, hub.Status.Phase)
		assert.Equal(t, src.Status.IdxcPasswordChangedSecrets, hub.Status.IdxcPasswordChangedSecrets)
		assert.Equal(t, src.Status.IndexerSecretChanged, hub.Status.IndexerSecretChanged)
		require.Len(t, hub.Status.Peers, 1)
		assert.Equal(t, "guid-1", hub.Status.Peers[0].ID)
		assert.Equal(t, int64(42), hub.Status.Peers[0].BucketCount)
	})

	t.Run("does not invent Noah configuration", func(t *testing.T) {
		src := populatedIndexerCluster()
		hub := &hubApi.IndexerCluster{}

		require.NoError(t, src.ConvertTo(hub))

		assert.Nil(t, hub.Spec.NoahClusterRef, "a classic v3 resource must stay classic")
		assert.False(t, hub.Spec.NoahEnabled())
		assert.Nil(t, hub.Spec.QueueRef)
		assert.Nil(t, hub.Spec.ObjectStorageRef)
	})

	t.Run("converts a zero-valued object without error", func(t *testing.T) {
		hub := &hubApi.IndexerCluster{}
		require.NoError(t, (&IndexerCluster{}).ConvertTo(hub))
		assert.Nil(t, hub.Status.Peers, "nil peers must not become an empty slice")
	})

	t.Run("refuses a hub of the wrong kind", func(t *testing.T) {
		err := populatedIndexerCluster().ConvertTo(&hubApi.SearchHeadCluster{})
		assert.EqualError(t, err,
			"unsupported conversion hub for IndexerCluster: *v4.SearchHeadCluster")
	})
}

// TestIndexerClusterConvertFrom covers the downgrade direction, v4 -> v3, where the v3 object is the destination.
func TestIndexerClusterConvertFrom(t *testing.T) {
	t.Run("round-trips a classic resource without loss", func(t *testing.T) {
		original := populatedIndexerCluster()
		hub := &hubApi.IndexerCluster{}
		require.NoError(t, original.ConvertTo(hub))

		roundTripped := &IndexerCluster{}
		require.NoError(t, roundTripped.ConvertFrom(hub))

		assert.Equal(t, original, roundTripped)
	})

	t.Run("refuses noahClusterRef", func(t *testing.T) {
		hub := &hubApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "splunk"},
			Spec: hubApi.IndexerClusterSpec{
				NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			},
		}

		err := (&IndexerCluster{}).ConvertFrom(hub)

		assert.EqualError(t, err, "IndexerCluster splunk/idx cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource")
	})

	t.Run("refuses queueRef and objectStorageRef, naming both in spec order", func(t *testing.T) {
		hub := &hubApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "splunk"},
			Spec: hubApi.IndexerClusterSpec{
				Replicas:         3,
				QueueRef:         &corev1.ObjectReference{Name: "queue"},
				ObjectStorageRef: &corev1.ObjectReference{Name: "store"},
			},
		}

		err := (&IndexerCluster{}).ConvertFrom(hub)

		assert.EqualError(t, err, "IndexerCluster splunk/idx cannot be represented in apiVersion enterprise.splunk.com/v3: spec.queueRef, spec.objectStorageRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource")
	})

	t.Run("does not partially populate the destination when it refuses", func(t *testing.T) {
		hub := &hubApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "splunk"},
			Spec: hubApi.IndexerClusterSpec{
				Replicas:       9,
				NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			},
		}
		dst := &IndexerCluster{}

		require.Error(t, dst.ConvertFrom(hub))

		assert.Equal(t, &IndexerCluster{}, dst,
			"a refused conversion must leave the destination untouched")
	})

	t.Run("converts a classic resource", func(t *testing.T) {
		hub := &hubApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "splunk"},
			Spec:       hubApi.IndexerClusterSpec{Replicas: 3},
		}

		dst := &IndexerCluster{}
		require.NoError(t, dst.ConvertFrom(hub))
		assert.Equal(t, int32(3), dst.Spec.Replicas)
	})

	t.Run("refuses a hub of the wrong kind", func(t *testing.T) {
		err := (&IndexerCluster{}).ConvertFrom(&hubApi.SearchHeadCluster{})
		assert.EqualError(t, err,
			"unsupported conversion hub for IndexerCluster: *v4.SearchHeadCluster")
	})
}

// TestSearchHeadClusterConvertTo covers the upgrade direction, v3 -> v4, where the v3 object is the source.
func TestSearchHeadClusterConvertTo(t *testing.T) {
	t.Run("preserves every shared field", func(t *testing.T) {
		src := populatedSearchHeadCluster()
		hub := &hubApi.SearchHeadCluster{}

		require.NoError(t, src.ConvertTo(hub))

		assert.Equal(t, src.ObjectMeta, hub.ObjectMeta)
		assert.Equal(t, src.Spec.Replicas, hub.Spec.Replicas)
		assert.Equal(t, src.Spec.CommonSplunkSpec, hub.Spec.CommonSplunkSpec)
		assert.Equal(t, src.Spec.AppFrameworkConfig, hub.Spec.AppFrameworkConfig)
		assert.Equal(t, src.Spec.DetentionTimeoutSeconds, hub.Spec.DetentionTimeoutSeconds)
		assert.Equal(t, src.Status.Captain, hub.Status.Captain)
		assert.Equal(t, src.Status.DetainedPodRevision, hub.Status.DetainedPodRevision)
		require.Len(t, hub.Status.Members, 1)
		assert.Equal(t, 4, hub.Status.Members[0].ActiveHistoricalSearchCount)
	})

	t.Run("does not invent Noah configuration", func(t *testing.T) {
		src := populatedSearchHeadCluster()
		hub := &hubApi.SearchHeadCluster{}

		require.NoError(t, src.ConvertTo(hub))

		assert.Nil(t, hub.Spec.NoahClusterRef)
		assert.False(t, hub.Spec.NoahEnabled())
	})

	t.Run("refuses a hub of the wrong kind", func(t *testing.T) {
		err := populatedSearchHeadCluster().ConvertTo(&hubApi.IndexerCluster{})
		assert.EqualError(t, err,
			"unsupported conversion hub for SearchHeadCluster: *v4.IndexerCluster")
	})
}

// TestSearchHeadClusterConvertFrom covers the downgrade direction, hub -> v3,
// where the v3 object is the destination. noahClusterRef has no v3 equivalent, so
// it must be rejected rather than dropped; classic resources still convert.
func TestSearchHeadClusterConvertFrom(t *testing.T) {
	t.Run("round-trips a classic resource without loss", func(t *testing.T) {
		original := populatedSearchHeadCluster()
		hub := &hubApi.SearchHeadCluster{}
		require.NoError(t, original.ConvertTo(hub))

		roundTripped := &SearchHeadCluster{}
		require.NoError(t, roundTripped.ConvertFrom(hub))

		assert.Equal(t, original, roundTripped)
	})

	t.Run("refuses noahClusterRef", func(t *testing.T) {
		hub := &hubApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "splunk"},
			Spec: hubApi.SearchHeadClusterSpec{
				NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			},
		}

		err := (&SearchHeadCluster{}).ConvertFrom(hub)

		assert.EqualError(t, err, "SearchHeadCluster splunk/shc cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource")
	})

	t.Run("refuses the deployer fields, which are unrelated to Noah", func(t *testing.T) {
		hub := &hubApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "splunk"},
			Spec: hubApi.SearchHeadClusterSpec{
				Replicas: 3,
				DeployerResourceSpec: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
				},
				DeployerNodeAffinity: &corev1.NodeAffinity{},
			},
		}

		err := (&SearchHeadCluster{}).ConvertFrom(hub)

		assert.EqualError(t, err, "SearchHeadCluster splunk/shc cannot be represented in apiVersion enterprise.splunk.com/v3: spec.deployerResourceSpec, spec.deployerNodeAffinity set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource")
	})

	t.Run("does not partially populate the destination when it refuses", func(t *testing.T) {
		hub := &hubApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "splunk"},
			Spec: hubApi.SearchHeadClusterSpec{
				Replicas:       9,
				NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			},
		}
		dst := &SearchHeadCluster{}

		require.Error(t, dst.ConvertFrom(hub))

		assert.Equal(t, &SearchHeadCluster{}, dst,
			"a refused conversion must leave the destination untouched")
	})

	t.Run("refuses a hub of the wrong kind", func(t *testing.T) {
		err := (&SearchHeadCluster{}).ConvertFrom(&hubApi.IndexerCluster{})
		assert.EqualError(t, err,
			"unsupported conversion hub for SearchHeadCluster: *v4.IndexerCluster")
	})
}
