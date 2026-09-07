// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	noahclient "github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/noah"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type noahIndexerScaleOutTestOptions struct {
	peerStatus       noahclient.PeerStatus
	peerStart        int64
	cacheWarmEnabled *bool
	timeoutSeconds   *int32
}

type noahIndexerScaleOutTestFixture struct {
	client         *spltest.MockClient
	cr             *enterpriseApi.IndexerCluster
	statefulSet    *appsv1.StatefulSet
	requestHeaders http.Header
}

func newNoahIndexerScaleOutTestFixture(t *testing.T, options noahIndexerScaleOutTestOptions) *noahIndexerScaleOutTestFixture {
	t.Helper()
	if options.peerStart == 0 {
		options.peerStart = time.Now().Unix()
	}

	fixture := &noahIndexerScaleOutTestFixture{client: spltest.NewMockClient()}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fixture.requestHeaders = request.Header.Clone()
		if err := json.NewEncoder(response).Encode([]noahclient.Peer{{
			ID:            "splunk-main-indexer-0.splunk-main-indexer-headless.test.svc.corp.example",
			Status:        options.peerStatus,
			Data:          noahclient.PeerData{StartTime: options.peerStart},
			LastHeartbeat: options.peerStart + 1,
		}}); err != nil {
			t.Errorf("encode Noah peers: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	fixture.cr = &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "test", Generation: 7},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}
	require.NoError(t, fixture.client.Create(t.Context(), &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: fixture.cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:                        server.URL,
			Tenant:                          "tenant",
			AuthSecretRef:                   corev1.LocalObjectReference{Name: "noah-auth"},
			CacheWarmScaleOutEnabled:        options.cacheWarmEnabled,
			CacheWarmScaleOutTimeoutSeconds: options.timeoutSeconds,
		},
	}))
	require.NoError(t, fixture.client.Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: fixture.cr.Namespace},
		Data:       map[string][]byte{noah.AuthSecretKey: []byte("unit-test-noah-key")},
	}))

	appliedReplicas := int32(1)
	fixture.statefulSet = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-main-indexer", Namespace: fixture.cr.Namespace, Generation: 1},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &appliedReplicas,
			ServiceName: "splunk-main-indexer-headless",
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "splunk",
				Env:  []corev1.EnvVar{{Name: resources.ClusterDomainEnvName, Value: "corp.example"}},
			}}}},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 1,
			Replicas:           appliedReplicas,
			ReadyReplicas:      appliedReplicas,
			CurrentReplicas:    appliedReplicas,
			UpdatedReplicas:    appliedReplicas,
			CurrentRevision:    "revision-1",
			UpdateRevision:     "revision-1",
		},
	}
	require.NoError(t, fixture.client.Create(t.Context(), fixture.statefulSet))
	require.NoError(t, fixture.client.Create(t.Context(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-main-indexer-0",
			Namespace: fixture.cr.Namespace,
			Labels:    map[string]string{"controller-revision-hash": "revision-1"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "splunk",
				Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.NewTime(time.Unix(options.peerStart-1, 0)),
				}},
			}},
		},
	}))
	return fixture
}

func (fixture *noahIndexerScaleOutTestFixture) podManager() *noahIndexerPodManager {
	mgr := newNoahIndexerPodManager(fixture.client, fixture.cr)
	mgr.statefulSet = fixture.statefulSet
	return mgr
}

type noahIndexerRolloutTestFixture struct {
	client         *spltest.MockClient
	cr             *enterpriseApi.IndexerCluster
	statefulSetKey types.NamespacedName
	mutex          sync.RWMutex
	peers          []noahclient.Peer
	noahRequests   int
}

type noahIndexerScaleDownTestFixture struct {
	client         *spltest.MockClient
	cr             *enterpriseApi.IndexerCluster
	statefulSetKey types.NamespacedName
	mutex          sync.RWMutex
	peers          []noahclient.Peer
	bucketPeerIDs  []string
	bucketMapState noahclient.BucketMapStatus
	unregistered   []string
	unregisterCode int
}

func newNoahIndexerScaleDownTestFixture(t *testing.T) *noahIndexerScaleDownTestFixture {
	t.Helper()
	fixture := &noahIndexerScaleDownTestFixture{client: spltest.NewMockClient()}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fixture.mutex.Lock()
		defer fixture.mutex.Unlock()
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/peers"):
			require.NoError(t, json.NewEncoder(response).Encode(fixture.peers))
		case request.Method == http.MethodDelete && strings.Contains(request.URL.Path, "/peers/"):
			fixture.unregistered = append(fixture.unregistered, request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:])
			status := fixture.unregisterCode
			if status == 0 {
				status = http.StatusAccepted
			}
			response.WriteHeader(status)
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/bucketMaps/latest"):
			require.NoError(t, json.NewEncoder(response).Encode(noahclient.BucketMap{
				ID: 7, Status: fixture.bucketMapState, PeerIDs: fixture.bucketPeerIDs,
			}))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	fixture.cr = &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "test", Generation: 9},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       1,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}
	fixture.bucketMapState = noahclient.BucketMapStatusActive
	require.NoError(t, fixture.client.Create(t.Context(), &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: fixture.cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      server.URL,
			Tenant:        "tenant",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	}))
	require.NoError(t, fixture.client.Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: fixture.cr.Namespace},
		Data:       map[string][]byte{noah.AuthSecretKey: []byte("unit-test-noah-key")},
	}))

	replicas := int32(3)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-main-indexer", Namespace: fixture.cr.Namespace, Generation: 1},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: "splunk-main-indexer-headless",
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": "noah-indexer"}},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc", Namespace: fixture.cr.Namespace},
			}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "noah-indexer"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "splunk",
					Env:  []corev1.EnvVar{{Name: resources.ClusterDomainEnvName, Value: "corp.example"}},
				}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 1,
			Replicas:           replicas,
			ReadyReplicas:      replicas,
			CurrentReplicas:    replicas,
			UpdatedReplicas:    replicas,
			CurrentRevision:    "revision-1",
			UpdateRevision:     "revision-1",
		},
	}
	require.NoError(t, fixture.client.Create(t.Context(), statefulSet))
	fixture.statefulSetKey = types.NamespacedName{Name: statefulSet.Name, Namespace: statefulSet.Namespace}

	const startTime int64 = 1_700_000_000
	for ordinal := range replicas {
		fixture.createPod(t, ordinal, startTime)
		peerID := fixture.peerID(ordinal)
		fixture.peers = append(fixture.peers, noahclient.Peer{
			ID: peerID, Status: noahclient.PeerStatusUp,
			Data: noahclient.PeerData{StartTime: startTime}, LastHeartbeat: startTime + 1,
		})
		fixture.bucketPeerIDs = append(fixture.bucketPeerIDs, peerID)
		require.NoError(t, fixture.client.Create(t.Context(), &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("pvc-etc-%s-%d", statefulSet.Name, ordinal), Namespace: fixture.cr.Namespace},
		}))
	}
	return fixture
}

func (fixture *noahIndexerScaleDownTestFixture) peerID(ordinal int32) string {
	return fmt.Sprintf("splunk-main-indexer-%d.splunk-main-indexer-headless.test.svc.corp.example", ordinal)
}

func (fixture *noahIndexerScaleDownTestFixture) createPod(t *testing.T, ordinal int32, startTime int64) {
	t.Helper()
	require.NoError(t, fixture.client.Create(t.Context(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("splunk-main-indexer-%d", ordinal),
			Namespace: fixture.cr.Namespace,
			Labels:    map[string]string{"app": "noah-indexer", "controller-revision-hash": "revision-1"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "splunk", Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Unix(startTime, 0))}},
			}},
		},
	}))
}

func (fixture *noahIndexerScaleDownTestFixture) update(t *testing.T) (enterpriseApi.Phase, error) {
	t.Helper()
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	phase, err := newNoahIndexerPodManager(fixture.client, fixture.cr).Update(
		t.Context(), fixture.client, statefulSet, fixture.cr.Spec.Replicas,
	)
	fixture.cr.Status.Phase = phase
	return phase, err
}

func (fixture *noahIndexerScaleDownTestFixture) finishPodRemoval(t *testing.T, ordinal, replicas int32) {
	t.Helper()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("splunk-main-indexer-%d", ordinal), Namespace: fixture.cr.Namespace}}
	require.NoError(t, fixture.client.Delete(t.Context(), pod))
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.Replicas = replicas
	statefulSet.Status.ReadyReplicas = replicas
	statefulSet.Status.CurrentReplicas = replicas
	statefulSet.Status.UpdatedReplicas = replicas
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))
}

func (fixture *noahIndexerScaleDownTestFixture) setPeerStatus(ordinal int32, status noahclient.PeerStatus) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	for index := range fixture.peers {
		if fixture.peers[index].ID == fixture.peerID(ordinal) {
			fixture.peers[index].Status = status
		}
	}
}

func (fixture *noahIndexerScaleDownTestFixture) excludeFromBucketMap(ordinal int32) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	fixture.bucketPeerIDs = slices.DeleteFunc(fixture.bucketPeerIDs, func(peerID string) bool {
		return peerID == fixture.peerID(ordinal)
	})
}

func (fixture *noahIndexerScaleDownTestFixture) setBucketMap(status noahclient.BucketMapStatus, peerIDs []string) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	fixture.bucketMapState = status
	fixture.bucketPeerIDs = peerIDs
}

func (fixture *noahIndexerScaleDownTestFixture) unregisterCount() int {
	fixture.mutex.RLock()
	defer fixture.mutex.RUnlock()
	return len(fixture.unregistered)
}

func (fixture *noahIndexerScaleDownTestFixture) failUnregister(status int) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	fixture.unregisterCode = status
}

func newNoahIndexerRolloutTestFixture(t *testing.T) *noahIndexerRolloutTestFixture {
	t.Helper()
	fixture := &noahIndexerRolloutTestFixture{client: spltest.NewMockClient()}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("unexpected Noah rollout request method: %s", request.Method)
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fixture.mutex.Lock()
		fixture.noahRequests++
		peers := append([]noahclient.Peer(nil), fixture.peers...)
		fixture.mutex.Unlock()
		if err := json.NewEncoder(response).Encode(peers); err != nil {
			t.Errorf("encode Noah peers: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	fixture.cr = &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "test", Generation: 8},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       2,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}
	require.NoError(t, fixture.client.Create(t.Context(), &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: fixture.cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      server.URL,
			Tenant:        "tenant",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	}))
	require.NoError(t, fixture.client.Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: fixture.cr.Namespace},
		Data:       map[string][]byte{noah.AuthSecretKey: []byte("unit-test-noah-key")},
	}))

	replicas := int32(2)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-main-indexer", Namespace: fixture.cr.Namespace, Generation: 2},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: "splunk-main-indexer-headless",
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": "noah-indexer"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "noah-indexer"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "splunk",
					Env:  []corev1.EnvVar{{Name: resources.ClusterDomainEnvName, Value: "corp.example"}},
				}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 2,
			Replicas:           replicas,
			ReadyReplicas:      replicas,
			CurrentReplicas:    replicas,
			CurrentRevision:    "revision-1",
			UpdateRevision:     "revision-2",
		},
	}
	require.NoError(t, fixture.client.Create(t.Context(), statefulSet))
	fixture.statefulSetKey = types.NamespacedName{Name: statefulSet.Name, Namespace: statefulSet.Namespace}

	const oldStart int64 = 1_700_000_000
	for ordinal := range replicas {
		fixture.createPod(t, ordinal, "revision-1", oldStart, true)
	}
	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusUp, oldStart),
		fixture.peer(1, noahclient.PeerStatusUp, oldStart),
	)
	return fixture
}

func (fixture *noahIndexerRolloutTestFixture) peerID(ordinal int32) string {
	return fmt.Sprintf("splunk-main-indexer-%d.splunk-main-indexer-headless.test.svc.corp.example", ordinal)
}

func (fixture *noahIndexerRolloutTestFixture) peer(ordinal int32, status noahclient.PeerStatus, startTime int64) noahclient.Peer {
	return noahclient.Peer{
		ID:            fixture.peerID(ordinal),
		Status:        status,
		Data:          noahclient.PeerData{StartTime: startTime},
		LastHeartbeat: startTime + 1,
	}
}

func (fixture *noahIndexerRolloutTestFixture) setPeers(peers ...noahclient.Peer) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	fixture.peers = append([]noahclient.Peer(nil), peers...)
}

func (fixture *noahIndexerRolloutTestFixture) noahRequestCount() int {
	fixture.mutex.RLock()
	defer fixture.mutex.RUnlock()
	return fixture.noahRequests
}

func (fixture *noahIndexerRolloutTestFixture) createPod(t *testing.T, ordinal int32, revision string, startTime int64, ready bool) {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("splunk-main-indexer-%d", ordinal),
			Namespace:       fixture.cr.Namespace,
			UID:             types.UID(fmt.Sprintf("pod-%d-%s", ordinal, revision)),
			ResourceVersion: fmt.Sprintf("%d", ordinal+1),
			Labels: map[string]string{
				"app":                      "noah-indexer",
				"controller-revision-hash": revision,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "splunk",
				Ready: ready,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.NewTime(time.Unix(startTime, 0)),
				}},
			}},
		},
	}
	if ready {
		pod.Status.Conditions[0].Status = corev1.ConditionTrue
	}
	require.NoError(t, fixture.client.Create(t.Context(), pod))
}

func (fixture *noahIndexerRolloutTestFixture) update(t *testing.T) (enterpriseApi.Phase, error) {
	t.Helper()
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	return newNoahIndexerPodManager(fixture.client, fixture.cr).Update(
		t.Context(),
		fixture.client,
		statefulSet,
		fixture.cr.Spec.Replicas,
	)
}

func (fixture *noahIndexerRolloutTestFixture) setStatefulSetStatus(t *testing.T, readyReplicas, updatedReplicas int32) {
	t.Helper()
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.ReadyReplicas = readyReplicas
	statefulSet.Status.UpdatedReplicas = updatedReplicas
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))
}

func TestNoahIndexerPodManagerRollsHighestOrdinalAndWaitsForReplacementPeer(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodNotFound(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)

	const replacementStart int64 = 1_700_000_100
	fixture.createPod(t, 1, "revision-2", replacementStart, true)
	fixture.setStatefulSetStatus(t, 2, 1)
	staleReplacementPeer := fixture.peer(1, noahclient.PeerStatusUp, replacementStart)
	staleReplacementPeer.LastHeartbeat = replacementStart
	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusUp, 1_700_000_000),
		staleReplacementPeer,
	)

	// Constructing a new manager simulates an operator restart. The stale Noah
	// record must not authorize recycling the next ordinal.
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)

	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusUp, 1_700_000_000),
		fixture.peer(1, noahclient.PeerStatusUp, replacementStart),
	)
	requestsBeforeUpdate := fixture.noahRequestCount()
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assert.Equal(t, requestsBeforeUpdate+2, fixture.noahRequestCount())
	assertPodNotFound(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerContinuesRollbackWhenControllerRevisionIsReused(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	pod := &corev1.Pod{}
	podKey := types.NamespacedName{Name: "splunk-main-indexer-1", Namespace: fixture.cr.Namespace}
	require.NoError(t, fixture.client.Get(t.Context(), podKey, pod))
	pod.Labels["controller-revision-hash"] = "revision-2"
	require.NoError(t, fixture.client.Update(t.Context(), pod))

	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.CurrentRevision = "revision-1"
	statefulSet.Status.UpdateRevision = "revision-1"
	statefulSet.Status.UpdatedReplicas = 1
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodNotFound(t, fixture.client, podKey.Name, podKey.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerUsesPodRevisionsWhenUpdatedReplicaStatusLags(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.CurrentRevision = "revision-1"
	statefulSet.Status.UpdateRevision = "revision-1"
	statefulSet.Status.UpdatedReplicas = 1
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerScalesOutBeforeRollingExistingPods(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	fixture.cr.Spec.Replicas = 3

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)

	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(3), *statefulSet.Spec.Replicas)
}

func TestValidateNoahIndexerUpgradePath(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	fixture.cr.Spec.Image = "splunk/splunk:new"
	fixture.cr.Spec.LicenseManagerRef = corev1.ObjectReference{Name: "license-manager", Namespace: "licenses"}

	licenseManager := &enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{Name: "license-manager", Namespace: fixture.cr.Spec.LicenseManagerRef.Namespace},
		Status:     enterpriseApi.LicenseManagerStatus{Phase: enterpriseApi.PhaseUpdating},
	}
	require.NoError(t, fixture.client.Create(t.Context(), licenseManager))
	licenseManagerStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkLicenseManager, licenseManager.Name),
			Namespace: licenseManager.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "splunk",
			Image: fixture.cr.Spec.Image,
		}}}}},
	}
	require.NoError(t, fixture.client.Create(t.Context(), licenseManagerStatefulSet))

	continueReconcile, err := validateNoahIndexerUpgradePath(t.Context(), fixture.client, fixture.cr)
	require.NoError(t, err)
	assert.False(t, continueReconcile)

	licenseManager.Status.Phase = enterpriseApi.PhaseReady
	require.NoError(t, fixture.client.Update(t.Context(), licenseManager))
	licenseManagerStatefulSet.Spec.Template.Spec.Containers[0].Image = "splunk/splunk:other"
	require.NoError(t, fixture.client.Update(t.Context(), licenseManagerStatefulSet))
	continueReconcile, err = validateNoahIndexerUpgradePath(t.Context(), fixture.client, fixture.cr)
	assert.False(t, continueReconcile)
	require.ErrorContains(t, err, "different than CR image")

	licenseManagerStatefulSet.Spec.Template.Spec.Containers[0].Image = fixture.cr.Spec.Image
	require.NoError(t, fixture.client.Update(t.Context(), licenseManagerStatefulSet))
	continueReconcile, err = validateNoahIndexerUpgradePath(t.Context(), fixture.client, fixture.cr)
	require.NoError(t, err)
	assert.True(t, continueReconcile)
}

func TestNoahIndexerPodManagerReportsTerminalPodFailure(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.CurrentRevision = "revision-1"
	statefulSet.Status.UpdateRevision = "revision-1"
	statefulSet.Status.ReadyReplicas = 1
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

	pod := &corev1.Pod{}
	podKey := types.NamespacedName{Name: "splunk-main-indexer-1", Namespace: fixture.cr.Namespace}
	require.NoError(t, fixture.client.Get(t.Context(), podKey, pod))
	pod.Status.Phase = corev1.PodPending
	pod.Status.ContainerStatuses[0].Ready = false
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
		Reason:  "ImagePullBackOff",
		Message: "unable to pull image",
	}}
	require.NoError(t, fixture.client.Update(t.Context(), pod))

	phase, err := fixture.update(t)
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	require.Error(t, err)
	message, terminal := splcommon.TerminalMessage(err)
	assert.True(t, terminal)
	assert.Equal(t, "Pod stuck in terminal state — manual fix required", message)
}

func assertPodExists(t *testing.T, client splcommon.ControllerClient, name, namespace string) {
	t.Helper()
	err := client.Get(t.Context(), types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Pod{})
	require.NoError(t, err)
}

func assertPodNotFound(t *testing.T, client splcommon.ControllerClient, name, namespace string) {
	t.Helper()
	err := client.Get(t.Context(), types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Pod{})
	require.True(t, k8serrors.IsNotFound(err))
}

func TestApplyNoahIndexerResourcesCreatesIdentityAwareStatefulSet(t *testing.T) {
	t.Setenv(resources.ClusterDomainEnvName, "corp.example")

	ctx := t.Context()
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v4",
			Kind:       "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "main",
			Namespace: "test",
			UID:       types.UID("indexer-cluster-uid"),
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	require.NoError(t, client.Create(ctx, &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint: "https://noah.test.svc:8080",
			Tenant:   "axolotl",
			AuthSecretRef: corev1.LocalObjectReference{
				Name: "noah-auth",
			},
		},
	}))
	require.NoError(t, client.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: cr.Namespace},
		Data:       map[string][]byte{noah.AuthSecretKey: []byte("unit-test-noah-key")},
	}))

	statefulSet, phase, err := applyNoahIndexerResources(ctx, client, cr, newNoahIndexerPodManager(client, cr))
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, phase)

	created := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      statefulSet.Name,
		Namespace: statefulSet.Namespace,
	}, created))
	require.NotNil(t, created.Spec.Replicas)
	assert.Equal(t, int32(3), *created.Spec.Replicas, "initial creation must start every requested replica")
	for key, value := range created.Spec.Selector.MatchLabels {
		assert.Equal(t, value, created.Spec.Template.Labels[key])
	}
	for _, headless := range []bool{true, false} {
		service := &corev1.Service{}
		require.NoError(t, client.Get(ctx, types.NamespacedName{
			Name:      splcommon.GetSplunkServiceName(SplunkIndexer, cr.Name, headless),
			Namespace: cr.Namespace,
		}, service))
		assert.Equal(t, created.Spec.Selector.MatchLabels, service.Spec.Selector)
		assert.Equal(t, headless, service.Spec.PublishNotReadyAddresses)
		if headless {
			assert.Equal(t, created.Spec.ServiceName, service.Name)
		}
	}

	var defaultsConfigMapName string
	for _, volume := range created.Spec.Template.Spec.Volumes {
		if volume.ConfigMap != nil && strings.HasPrefix(volume.ConfigMap.Name, "sok-indexercluster-defaults-") {
			defaultsConfigMapName = volume.ConfigMap.Name
			break
		}
	}
	require.NotEmpty(t, defaultsConfigMapName)
	defaultsConfigMap := &corev1.ConfigMap{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      defaultsConfigMapName,
		Namespace: created.Namespace,
	}, defaultsConfigMap))
	assert.Contains(t, defaultsConfigMap.Data["conf-defaults.yml"], "https://noah.test.svc:8080")
	assert.Contains(t, defaultsConfigMap.Data["conf-defaults.yml"], "tenant: axolotl")

	env := make(map[string]corev1.EnvVar)
	for _, item := range created.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item
	}
	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, created.Spec.ServiceName, env[resources.NoahHeadlessServiceEnvName].Value)
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)
	require.NotNil(t, env[resources.PodNameEnvName].ValueFrom)
	require.NotNil(t, env[resources.PodNamespaceEnvName].ValueFrom)

	var initEtc *corev1.Container
	for i := range created.Spec.Template.Spec.InitContainers {
		if created.Spec.Template.Spec.InitContainers[i].Name == "init-etc" {
			initEtc = &created.Spec.Template.Spec.InitContainers[i]
			break
		}
	}
	require.NotNil(t, initEtc)
	require.Len(t, initEtc.Command, 3)
	assert.Contains(t, initEtc.Command[2], `print "[noahService]"`)
	assert.Contains(t, initEtc.Command[2], `print "disabled = true"`)
	assert.Contains(t, initEtc.Command[2], noahAuthMountPath+"/"+noah.AuthSecretKey)
	assert.NotContains(t, initEtc.Command[2], "unit-test-noah-key")

	var authVolume *corev1.Volume
	for i := range created.Spec.Template.Spec.Volumes {
		if created.Spec.Template.Spec.Volumes[i].Name == noahAuthVolumeName {
			authVolume = &created.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	require.NotNil(t, authVolume)
	require.NotNil(t, authVolume.Secret)
	assert.Equal(t, "noah-auth", authVolume.Secret.SecretName)
	assert.Contains(t, initEtc.VolumeMounts, corev1.VolumeMount{
		Name:      noahAuthVolumeName,
		MountPath: noahAuthMountPath,
		ReadOnly:  true,
	})
	for _, mount := range created.Spec.Template.Spec.Containers[0].VolumeMounts {
		assert.NotEqual(t, noahAuthVolumeName, mount.Name, "the main container must not mount the plaintext Noah credential")
	}
}

func TestApplyNoahIndexerResourcesDefersBeforeDefaultsGarbageCollection(t *testing.T) {
	ctx := t.Context()
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "main",
			Namespace: "test",
			UID:       types.UID("indexer-cluster-uid"),
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       1,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec:              enterpriseApi.Spec{Image: "splunk/splunk:new"},
				LicenseManagerRef: corev1.ObjectReference{Name: "license-manager", Namespace: "licenses"},
			},
		},
	}
	require.NoError(t, client.Create(ctx, &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "https://noah.test.svc:8080",
			Tenant:        "axolotl",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	}))
	require.NoError(t, client.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: cr.Namespace},
		Data:       map[string][]byte{noah.AuthSecretKey: []byte("unit-test-noah-key")},
	}))

	replicas := int32(1)
	currentStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkIndexer, cr.Name),
			Namespace: cr.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "noah-indexer"}},
		},
	}
	require.NoError(t, client.Create(ctx, currentStatefulSet))
	licenseManager := &enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{Name: cr.Spec.LicenseManagerRef.Name, Namespace: cr.Spec.LicenseManagerRef.Namespace},
		Status:     enterpriseApi.LicenseManagerStatus{Phase: enterpriseApi.PhaseUpdating},
	}
	require.NoError(t, client.Create(ctx, licenseManager))
	require.NoError(t, client.Create(ctx, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkLicenseManager, licenseManager.Name),
			Namespace: licenseManager.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "splunk",
			Image: cr.Spec.Image,
		}}}}},
	}))

	labels := map[string]string{resources.LabelCRName: cr.Name, resources.LabelCRKind: cr.Kind}
	staleConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "old-defaults", Namespace: cr.Namespace, Labels: labels}}
	staleSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "old-defaults", Namespace: cr.Namespace, Labels: labels}}
	require.NoError(t, client.Create(ctx, staleConfigMap))
	require.NoError(t, client.Create(ctx, staleSecret))

	statefulSet, phase, err := applyNoahIndexerResources(ctx, client, cr, newNoahIndexerPodManager(client, cr))
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, phase)
	assert.Equal(t, currentStatefulSet.Name, statefulSet.Name)
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: staleConfigMap.Name, Namespace: staleConfigMap.Namespace}, &corev1.ConfigMap{}))
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: staleSecret.Name, Namespace: staleSecret.Namespace}, &corev1.Secret{}))
}

func TestApplyNoahIndexerResourcesRequiresReferencedNoahCluster(t *testing.T) {
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: "missing"},
		},
	}

	statefulSet, phase, err := applyNoahIndexerResources(t.Context(), client, cr, newNoahIndexerPodManager(client, cr))
	require.Error(t, err)
	assert.Nil(t, statefulSet)
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	assert.Contains(t, err.Error(), "get referenced NoahCluster test/missing")
	outcome, outcomeErr, handled := noahIndexerOutcomeFromError(err)
	assert.True(t, handled)
	assert.NoError(t, outcomeErr)
	assert.Equal(t, enterpriseApi.PhasePending, outcome.phase)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyMissing), outcome.condition.Reason)
	assert.Contains(t, outcome.condition.Message, "test/missing")
}

func TestApplyNoahIndexerResourcesValidatesRuntimeBeforeCreatingResources(t *testing.T) {
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}
	require.NoError(t, client.Create(t.Context(), &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "https://noah.test.svc",
			Tenant:        "tenant",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	}))
	require.NoError(t, client.Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: cr.Namespace},
		Data:       map[string][]byte{"wrong-key": []byte("unit-test-noah-key")},
	}))
	client.ResetCalls()

	statefulSet, phase, err := applyNoahIndexerResources(t.Context(), client, cr, newNoahIndexerPodManager(client, cr))

	assert.Nil(t, statefulSet)
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	require.Error(t, err)
	outcome, outcomeErr, handled := noahIndexerOutcomeFromError(err)
	require.True(t, handled)
	_, terminal := splcommon.TerminalMessage(outcomeErr)
	assert.True(t, terminal)
	assert.Equal(t, string(enterpriseApi.ReasonNoahConfigurationInvalid), outcome.condition.Reason)
	assert.Contains(t, outcome.condition.Message, noah.AuthSecretKey)
	assert.Empty(t, client.Calls["Create"], "invalid Noah configuration must fail before creating workload resources")
}

func TestNoahIndexerPodManagerScalesDownOneOrdinalAndWaitsForNoahCleanup(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(2), *statefulSet.Spec.Replicas)
	assert.Equal(t, "2", statefulSet.Annotations[pendingScaleDownOrdinalAnnotation])
	assert.Zero(t, fixture.unregisterCount())
	assertPodExists(t, fixture.client, "splunk-main-indexer-2", fixture.cr.Namespace)
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: "pvc-etc-splunk-main-indexer-2", Namespace: fixture.cr.Namespace,
	}, &corev1.PersistentVolumeClaim{}))

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assert.Zero(t, fixture.unregisterCount(), "must wait for Pod removal before unregistering")

	fixture.finishPodRemoval(t, 2, 2)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assert.Equal(t, 1, fixture.unregisterCount())
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: "pvc-etc-splunk-main-indexer-2", Namespace: fixture.cr.Namespace,
	}, &corev1.PersistentVolumeClaim{}))

	fixture.setPeerStatus(2, noahclient.PeerStatusDown)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, int32(2), *statefulSet.Spec.Replicas, "bucket-map inclusion must block the next ordinal")
	assert.Equal(t, 2, fixture.unregisterCount())

	fixture.excludeFromBucketMap(2)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, int32(1), *statefulSet.Spec.Replicas)
	assert.Equal(t, "1", statefulSet.Annotations[pendingScaleDownOrdinalAnnotation])
	assert.Equal(t, 3, fixture.unregisterCount())

	fixture.finishPodRemoval(t, 1, 1)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assert.Equal(t, 4, fixture.unregisterCount())

	fixture.setPeerStatus(1, noahclient.PeerStatusDown)
	fixture.excludeFromBucketMap(1)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase)
	assert.Equal(t, 5, fixture.unregisterCount())
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.NotContains(t, statefulSet.Annotations, pendingScaleDownOrdinalAnnotation)
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: "pvc-etc-splunk-main-indexer-1", Namespace: fixture.cr.Namespace,
	}, &corev1.PersistentVolumeClaim{}))
}

func TestNoahIndexerPodManagerBlocksUnsafeScaleDownUntilPeersAreReady(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.setPeerStatus(1, noahclient.PeerStatusWarming)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(3), *statefulSet.Spec.Replicas)
}

func TestNoahIndexerPodManagerResumesScaleDownCleanupFromStatefulSetAnnotation(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)

	// Simulate a transient error after the replica update has succeeded and CR
	// status has lost all evidence of the pending cleanup.
	fixture.cr.Status.Phase = enterpriseApi.PhaseError
	fixture.cr.Status.Replicas = 2
	fixture.finishPodRemoval(t, 2, 2)
	fixture.setPeerStatus(2, noahclient.PeerStatusDown)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assert.Equal(t, 1, fixture.unregisterCount())

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, int32(2), *statefulSet.Spec.Replicas, "bucket-map inclusion must block the next ordinal")
	assert.Equal(t, 2, fixture.unregisterCount())
}

func TestNoahIndexerPodManagerChecksCleanBucketMapAfterUnregister(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	fixture.finishPodRemoval(t, 2, 2)
	fixture.excludeFromBucketMap(2)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase)
	assert.Equal(t, 1, fixture.unregisterCount())
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, int32(2), *statefulSet.Spec.Replicas)
	assert.NotContains(t, statefulSet.Annotations, pendingScaleDownOrdinalAnnotation)
}

func TestNoahIndexerPodManagerRejectsIncompleteBucketMapDuringScaleDown(t *testing.T) {
	tests := []struct {
		name    string
		status  noahclient.BucketMapStatus
		peerIDs func(*noahIndexerScaleDownTestFixture) []string
	}{
		{
			name: "unknown status", status: noahclient.BucketMapStatusUnknown,
			peerIDs: func(fixture *noahIndexerScaleDownTestFixture) []string {
				return []string{fixture.peerID(0), fixture.peerID(1)}
			},
		},
		{name: "omitted peers", status: noahclient.BucketMapStatusActive},
		{
			name: "missing remaining peer", status: noahclient.BucketMapStatusActive,
			peerIDs: func(fixture *noahIndexerScaleDownTestFixture) []string {
				return []string{fixture.peerID(0)}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNoahIndexerScaleDownTestFixture(t)
			fixture.cr.Spec.Replicas = 2
			phase, err := fixture.update(t)
			require.NoError(t, err)
			assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
			fixture.finishPodRemoval(t, 2, 2)

			var peerIDs []string
			if test.peerIDs != nil {
				peerIDs = test.peerIDs(fixture)
			}
			fixture.setBucketMap(test.status, peerIDs)
			phase, err = fixture.update(t)
			require.NoError(t, err)
			assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
			statefulSet := &appsv1.StatefulSet{}
			require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
			assert.Equal(t, int32(2), *statefulSet.Spec.Replicas)
			assert.Equal(t, "2", statefulSet.Annotations[pendingScaleDownOrdinalAnnotation])
		})
	}
}

func TestNoahIndexerPodManagerPropagatesScaleDownCleanupFailure(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)

	fixture.finishPodRemoval(t, 2, 2)
	fixture.failUnregister(http.StatusInternalServerError)
	phase, err = fixture.update(t)
	require.ErrorContains(t, err, "unregister Noah peer")
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, "2", statefulSet.Annotations[pendingScaleDownOrdinalAnnotation])
}

func TestNoahInitEtcUsesRenderedEtcVolume(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "splunk",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "rendered-etc",
							MountPath: "/opt/splunk/etc",
						}},
					}},
				},
			},
		},
	}

	noahInitEtcOption(&enterpriseApi.CommonSplunkSpec{})(statefulSet)
	require.Len(t, statefulSet.Spec.Template.Spec.InitContainers, 1)
	initEtc := statefulSet.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, "rendered-etc", initEtc.VolumeMounts[0].Name)
	require.NotNil(t, initEtc.SecurityContext)
	require.NotNil(t, initEtc.SecurityContext.AllowPrivilegeEscalation)
	assert.False(t, *initEtc.SecurityContext.AllowPrivilegeEscalation)
	require.NotNil(t, initEtc.SecurityContext.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, initEtc.SecurityContext.Capabilities.Drop)
	require.NotNil(t, initEtc.SecurityContext.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, initEtc.SecurityContext.SeccompProfile.Type)
}

func TestNoahAuthSecretOptionUsesSecretResourceVersion(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "init-etc"}}},
			},
		},
	}

	noahAuthSecretOption("noah-auth", "42")(statefulSet)

	assert.Equal(t, "42", statefulSet.Spec.Template.Annotations[noahAuthRevisionAnnotation])
	require.Len(t, statefulSet.Spec.Template.Spec.Volumes, 1)
	assert.Equal(t, "noah-auth", statefulSet.Spec.Template.Spec.Volumes[0].Secret.SecretName)
}

func TestNoahIndexerStatefulSetOptionsAppliesStableIdentityLast(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-main-indexer-headless",
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "splunk",
					}},
				},
			},
		},
	}

	callerOption := func(statefulSet *appsv1.StatefulSet) {
		statefulSet.Spec.ServiceName = "caller-selected-headless"
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(
			statefulSet.Spec.Template.Spec.Containers[0].Env,
			corev1.EnvVar{Name: resources.NoahEnabledEnvName, Value: "false"},
		)
	}

	options := noahIndexerStatefulSetOptions("corp.example", callerOption)
	resources.ApplyStatefulSetOptions(statefulSet, options...)

	env := make(map[string]corev1.EnvVar)
	for _, item := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item
	}

	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, "caller-selected-headless", env[resources.NoahHeadlessServiceEnvName].Value)
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)

	for envName, fieldPath := range map[string]string{
		resources.PodNameEnvName:      "metadata.name",
		resources.PodNamespaceEnvName: "metadata.namespace",
	} {
		fieldRef := env[envName].ValueFrom
		require.NotNil(t, fieldRef)
		require.NotNil(t, fieldRef.FieldRef)
		assert.Equal(t, fieldPath, fieldRef.FieldRef.FieldPath)
	}
}

func TestNoahIndexerStatefulSetConverged(t *testing.T) {
	const desiredReplicas int32 = 3

	converged := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 2,
			CurrentRevision:    "revision-2",
			UpdateRevision:     "revision-2",
			UpdatedReplicas:    desiredReplicas,
			ReadyReplicas:      desiredReplicas,
		},
	}

	tests := []struct {
		name        string
		statefulSet *appsv1.StatefulSet
		want        bool
	}{
		{
			name:        "latest revision is fully ready",
			statefulSet: converged,
			want:        true,
		},
		{
			name:        "missing StatefulSet",
			statefulSet: nil,
		},
		{
			name: "latest generation is not observed",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.ObservedGeneration--
				return statefulSet
			}(),
		},
		{
			name: "update revision is not available",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.UpdateRevision = ""
				return statefulSet
			}(),
		},
		{
			name: "pods are on the previous revision",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.CurrentRevision = "revision-1"
				return statefulSet
			}(),
		},
		{
			name: "not all replicas are updated",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.UpdatedReplicas--
				return statefulSet
			}(),
		},
		{
			name: "updated replicas are not all ready",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.ReadyReplicas--
				return statefulSet
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, noahIndexerStatefulSetConverged(tt.statefulSet, desiredReplicas))
		})
	}
}

func TestNoahIndexerWorkloadPhase(t *testing.T) {
	const replicas int32 = 2
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 2,
			CurrentRevision:    "revision-2",
			UpdateRevision:     "revision-2",
			UpdatedReplicas:    replicas,
			ReadyReplicas:      replicas - 1,
		},
	}

	assert.Equal(t, enterpriseApi.PhasePending, noahIndexerWorkloadPhase(enterpriseApi.PhaseReady, statefulSet, replicas),
		"same-revision replica readiness is not a template update")

	statefulSet.Status.CurrentRevision = "revision-1"
	assert.Equal(t, enterpriseApi.PhaseUpdating, noahIndexerWorkloadPhase(enterpriseApi.PhaseReady, statefulSet, replicas),
		"a pending template revision must remain Updating")

	statefulSet.Status.CurrentRevision = statefulSet.Status.UpdateRevision
	statefulSet.Status.ReadyReplicas = replicas
	assert.Equal(t, enterpriseApi.PhaseReady, noahIndexerWorkloadPhase(enterpriseApi.PhaseReady, statefulSet, replicas))
}

func TestExpectedNoahIndexerPeersReady(t *testing.T) {
	const currentStart int64 = 1_700_000_000
	peer0 := "splunk-main-indexer-0.splunk-main-indexer-headless.test.svc.corp.example"
	peer1 := "splunk-main-indexer-1.splunk-main-indexer-headless.test.svc.corp.example"
	expectedPeers := map[string]int64{
		peer0: currentStart,
		peer1: currentStart,
	}
	currentPeer := func(id string, status noahclient.PeerStatus) noahclient.Peer {
		return noahclient.Peer{ID: id, Status: status, Data: noahclient.PeerData{StartTime: currentStart}, LastHeartbeat: currentStart + 1}
	}

	tests := []struct {
		name  string
		peers []noahclient.Peer
		want  bool
	}{
		{
			name: "all expected peers are up",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				currentPeer(peer1, noahclient.PeerStatusUp),
			},
			want: true,
		},
		{
			name: "missing expected peer",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
			},
		},
		{
			name: "expected peer is down",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				currentPeer(peer1, noahclient.PeerStatusDown),
			},
		},
		{
			name: "bare pod name does not satisfy exact advertised identity",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				currentPeer("splunk-main-indexer-1", noahclient.PeerStatusUp),
			},
		},
		{
			name: "foreign cluster domain does not satisfy exact advertised identity",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				currentPeer("splunk-main-indexer-1.splunk-main-indexer-headless.test.svc.foreign.example", noahclient.PeerStatusUp),
			},
		},
		{
			name: "unrelated peer does not prevent expected peers becoming ready",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				currentPeer(peer1, noahclient.PeerStatusUp),
				currentPeer("splunk-other-indexer-4", noahclient.PeerStatusUp),
			},
			want: true,
		},
		{
			name: "duplicate active identity is not ready",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				currentPeer(peer1, noahclient.PeerStatusUp),
				currentPeer(peer1, noahclient.PeerStatusWarming),
			},
		},
		{
			name: "stale up incarnation does not satisfy expected peer",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				{ID: peer1, Status: noahclient.PeerStatusUp, Data: noahclient.PeerData{StartTime: currentStart - 1}, LastHeartbeat: currentStart},
			},
		},
		{
			name: "same-second stale heartbeat does not satisfy expected peer",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				{ID: peer1, Status: noahclient.PeerStatusUp, Data: noahclient.PeerData{StartTime: currentStart}, LastHeartbeat: currentStart},
			},
		},
		{
			name: "historical down incarnation is ignored",
			peers: []noahclient.Peer{
				currentPeer(peer0, noahclient.PeerStatusUp),
				currentPeer(peer1, noahclient.PeerStatusUp),
				{ID: peer1, Status: noahclient.PeerStatusDown, Data: noahclient.PeerData{StartTime: currentStart - 1}, LastHeartbeat: currentStart},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, expectedNoahIndexerPeersReady(test.peers, expectedPeers))
		})
	}
}

func TestExpectedNoahIndexerPeersRegistered(t *testing.T) {
	const currentStart int64 = 1_700_000_000
	const peerID = "splunk-main-indexer-0.splunk-main-indexer-headless.test.svc.corp.example"
	expectedPeers := map[string]int64{peerID: currentStart}
	currentPeer := func(status noahclient.PeerStatus) noahclient.Peer {
		return noahclient.Peer{ID: peerID, Status: status, Data: noahclient.PeerData{StartTime: currentStart}, LastHeartbeat: currentStart + 1}
	}

	tests := []struct {
		name  string
		peers []noahclient.Peer
		want  bool
	}{
		{name: "started peer is registered", peers: []noahclient.Peer{currentPeer(noahclient.PeerStatusStarted)}, want: true},
		{name: "warming peer is registered", peers: []noahclient.Peer{currentPeer(noahclient.PeerStatusWarming)}, want: true},
		{name: "warmed peer is registered", peers: []noahclient.Peer{currentPeer(noahclient.PeerStatusWarmed)}, want: true},
		{name: "up peer is registered", peers: []noahclient.Peer{currentPeer(noahclient.PeerStatusUp)}, want: true},
		{name: "missing peer is not registered"},
		{name: "down peer is not registered", peers: []noahclient.Peer{currentPeer(noahclient.PeerStatusDown)}},
		{name: "decommissioning peer is not registered", peers: []noahclient.Peer{currentPeer(noahclient.PeerStatusDecommissioning)}},
		{
			name:  "stale incarnation is not registered",
			peers: []noahclient.Peer{{ID: peerID, Status: noahclient.PeerStatusUp, Data: noahclient.PeerData{StartTime: currentStart - 1}, LastHeartbeat: currentStart}},
		},
		{
			name:  "duplicate current incarnation is not registered",
			peers: []noahclient.Peer{currentPeer(noahclient.PeerStatusUp), currentPeer(noahclient.PeerStatusWarming)},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, expectedNoahIndexerPeersRegistered(test.peers, expectedPeers))
		})
	}
}

func TestNoahCacheWarmScaleOutPolicyDefaults(t *testing.T) {
	disabled := false
	noTimeout := int32(0)
	customTimeout := int32(90)

	tests := []struct {
		name        string
		spec        enterpriseApi.NoahClusterSpec
		wantEnabled bool
		wantTimeout time.Duration
	}{
		{name: "omitted values use defaults", wantEnabled: true, wantTimeout: time.Hour},
		{
			name: "explicit false and zero are preserved",
			spec: enterpriseApi.NoahClusterSpec{
				CacheWarmScaleOutEnabled:        &disabled,
				CacheWarmScaleOutTimeoutSeconds: &noTimeout,
			},
			wantTimeout: 0,
		},
		{
			name:        "custom timeout is preserved",
			spec:        enterpriseApi.NoahClusterSpec{CacheWarmScaleOutTimeoutSeconds: &customTimeout},
			wantEnabled: true,
			wantTimeout: 90 * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.wantEnabled, noahCacheWarmScaleOutEnabled(test.spec))
			assert.Equal(t, test.wantTimeout, noahCacheWarmScaleOutTimeout(test.spec))
		})
	}
}

func TestTimedOutNoahIndexerCacheWarmPeer(t *testing.T) {
	const currentStart int64 = 1_700_000_000
	const peerID = "splunk-main-indexer-1.splunk-main-indexer-headless.test.svc.corp.example"
	expectedPeers := map[string]int64{peerID: currentStart}
	now := time.Unix(currentStart+60, 0)
	peer := func(status noahclient.PeerStatus, startTime int64) noahclient.Peer {
		return noahclient.Peer{
			ID:            peerID,
			Status:        status,
			Data:          noahclient.PeerData{StartTime: startTime},
			LastHeartbeat: startTime + 1,
		}
	}

	tests := []struct {
		name    string
		peers   []noahclient.Peer
		timeout time.Duration
		want    string
	}{
		{
			name:    "warming peer reaches timeout",
			peers:   []noahclient.Peer{peer(noahclient.PeerStatusWarming, currentStart)},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "started peer reaches timeout",
			peers:   []noahclient.Peer{peer(noahclient.PeerStatusStarted, currentStart)},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "warmed peer must still become up",
			peers:   []noahclient.Peer{peer(noahclient.PeerStatusWarmed, currentStart)},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "down peer reaches timeout",
			peers:   []noahclient.Peer{peer(noahclient.PeerStatusDown, currentStart)},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "warming remains within timeout",
			peers:   []noahclient.Peer{peer(noahclient.PeerStatusWarming, currentStart)},
			timeout: time.Minute + time.Second,
		},
		{
			name:    "up peer does not time out",
			peers:   []noahclient.Peer{peer(noahclient.PeerStatusUp, currentStart)},
			timeout: time.Minute,
		},
		{
			name:    "stale warming incarnation does not mask missing peer timeout",
			peers:   []noahclient.Peer{peer(noahclient.PeerStatusWarming, currentStart-1)},
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "missing peer reaches timeout",
			timeout: time.Minute,
			want:    peerID,
		},
		{
			name:    "missing peer remains within timeout",
			timeout: time.Minute + time.Second,
		},
		{
			name:    "zero timeout is disabled",
			peers:   []noahclient.Peer{peer(noahclient.PeerStatusWarming, currentStart)},
			timeout: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, timedOutNoahIndexerCacheWarmPeer(test.peers, expectedPeers, test.timeout, now))
		})
	}
}

func TestNoahIndexerScaleOutUsesReferencedAuthentication(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	plan, err := fixture.podManager().NextReplicas(t.Context(), 1, fixture.cr.Spec.Replicas)
	require.NoError(t, err)
	assert.Equal(t, int32(2), plan.TargetReplicas)
	assert.NotEmpty(t, fixture.requestHeaders.Get("x-splunk-lm-nonce"))
	assert.NotEmpty(t, fixture.requestHeaders.Get("x-splunk-lm-timestamp"))
	assert.True(t, strings.HasPrefix(fixture.requestHeaders.Get("x-splunk-digest"), "v2,"))
}

func TestNoahIndexerScaleOutPolicy(t *testing.T) {
	disabled := false
	tests := []struct {
		name             string
		peerStatus       noahclient.PeerStatus
		cacheWarmEnabled *bool
		requested        int32
		wantTarget       int32
		wantComplete     bool
	}{
		{name: "enabled waits for warming peer", peerStatus: noahclient.PeerStatusWarming, requested: 3, wantTarget: 1},
		{name: "disabled advances after registration", peerStatus: noahclient.PeerStatusWarming, cacheWarmEnabled: &disabled, requested: 3, wantTarget: 2},
		{name: "final warming peer is incomplete", peerStatus: noahclient.PeerStatusWarming, cacheWarmEnabled: &disabled, requested: 1, wantTarget: 1},
		{name: "final up peer is complete", peerStatus: noahclient.PeerStatusUp, requested: 1, wantTarget: 1, wantComplete: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{
				peerStatus:       test.peerStatus,
				cacheWarmEnabled: test.cacheWarmEnabled,
			})
			plan, err := fixture.podManager().NextReplicas(t.Context(), 1, test.requested)
			require.NoError(t, err)
			assert.Equal(t, test.wantTarget, plan.TargetReplicas)
			assert.Equal(t, test.wantComplete, plan.Complete)
		})
	}
}

func TestNoahIndexerPodManagerAppliesScaleOutTarget(t *testing.T) {
	disabled := false
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{
		peerStatus:       noahclient.PeerStatusWarming,
		cacheWarmEnabled: &disabled,
	})

	phase, err := fixture.podManager().Update(t.Context(), fixture.client, fixture.statefulSet, fixture.cr.Spec.Replicas)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)

	stored := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace}, stored))
	require.NotNil(t, stored.Spec.Replicas)
	assert.Equal(t, int32(2), *stored.Spec.Replicas)
}

func TestNoahIndexerPodManagerWaitsForStatefulSetToObserveTemplate(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace,
	}, statefulSet))
	statefulSet.Generation++
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

	phase, err := fixture.podManager().Update(t.Context(), fixture.client, statefulSet, fixture.cr.Spec.Replicas)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, phase)
	assert.Equal(t, int32(1), *statefulSet.Spec.Replicas)
}

func TestNoahIndexerPodManagerPropagatesScaleOutUpdateError(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	wantErr := errors.New("StatefulSet update failed")
	fixture.client.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = wantErr

	phase, err := fixture.podManager().Update(t.Context(), fixture.client, fixture.statefulSet, fixture.cr.Spec.Replicas)

	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, enterpriseApi.PhaseError, phase)
}

func TestNoahIndexerCacheWarmTimeoutStopsRequeueAndAllowsReevaluation(t *testing.T) {
	timeoutSeconds := int32(60)
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{
		peerStatus:     noahclient.PeerStatusWarming,
		peerStart:      time.Now().Add(-2 * time.Minute).Unix(),
		timeoutSeconds: &timeoutSeconds,
	})

	phase, err := fixture.podManager().Update(t.Context(), fixture.client, fixture.statefulSet, fixture.cr.Spec.Replicas)
	require.Error(t, err)
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	outcome, outcomeErr, handled := noahIndexerOutcomeFromError(err)
	require.True(t, handled)
	err = outcomeErr
	message, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	assert.Contains(t, message, "Cache warming timed out")
	reason, _ := splcommon.TerminalReason(err)
	assert.Equal(t, EventReasonNoahCacheWarmTimeout, reason)
	assert.Equal(t, enterpriseApi.PhaseError, outcome.phase)
	assert.Zero(t, outcome.requeueAfter)
	assert.Equal(t, string(enterpriseApi.ReasonNoahCacheWarmTimeout), outcome.condition.Reason)

	noahCluster := &enterpriseApi.NoahCluster{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: "noah", Namespace: fixture.cr.Namespace}, noahCluster))
	cacheWarmDisabled := false
	noahCluster.Spec.CacheWarmScaleOutEnabled = &cacheWarmDisabled
	require.NoError(t, fixture.client.Update(t.Context(), noahCluster))

	phase, err = fixture.podManager().Update(t.Context(), fixture.client, fixture.statefulSet, fixture.cr.Spec.Replicas)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	stored := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace}, stored))
	require.NotNil(t, stored.Spec.Replicas)
	assert.Equal(t, int32(2), *stored.Spec.Replicas)
}

func TestWaitForNoahIndexerWorkloadPreservesUpdatingForPendingTemplateRevision(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhaseUpdating, enterpriseApi.PhaseScalingUp, 2)

	assert.Equal(t, enterpriseApi.PhaseUpdating, outcome.phase)
	assert.Equal(t, "Waiting for the StatefulSet pod-template revision to be applied", outcome.phaseMessage)
	assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestWaitForNoahIndexerWorkloadPrefersCurrentLifecyclePhase(t *testing.T) {
	tests := []struct {
		name          string
		phase         enterpriseApi.Phase
		previousPhase enterpriseApi.Phase
	}{
		{name: "scale out after scale down", phase: enterpriseApi.PhaseScalingUp, previousPhase: enterpriseApi.PhaseScalingDown},
		{name: "update after scale down", phase: enterpriseApi.PhaseUpdating, previousPhase: enterpriseApi.PhaseScalingDown},
		{name: "scale down after update", phase: enterpriseApi.PhaseScalingDown, previousPhase: enterpriseApi.PhaseUpdating},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome := waitForNoahIndexerWorkload(test.phase, test.previousPhase, 2)

			assert.Equal(t, test.phase, outcome.phase)
			assert.NotEmpty(t, outcome.phaseMessage)
		})
	}
}

func TestWaitForNoahIndexerWorkloadPreservesScaleOutForReplicaReadiness(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhasePending, enterpriseApi.PhaseScalingUp, 2)

	assert.Equal(t, enterpriseApi.PhaseScalingUp, outcome.phase)
	assert.Equal(t, "Waiting for 2 applied replicas to become ready before continuing scale-out", outcome.phaseMessage)
	assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestWaitForNoahIndexerWorkloadPreservesUnsafeScaleDown(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhasePending, enterpriseApi.PhaseScalingDown, 2)

	assert.Equal(t, enterpriseApi.PhaseScalingDown, outcome.phase)
	assert.Equal(t, "Scaling down without Noah decommission; development use only", outcome.phaseMessage)
	assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestNoahIndexerScaleDownObservationErrorPreservesPhase(t *testing.T) {
	outcome, err, handled := noahIndexerOutcomeFromError(&noahIndexerPeerObservationError{
		err: errors.New("Noah unavailable"), phase: enterpriseApi.PhaseScalingDown,
	})

	require.True(t, handled)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, outcome.phase)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestSetNoahIndexerPhaseAndConditions(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	setNoahIndexerPhaseAndConditions(cr, false, enterpriseApi.PhaseReady, "", newNoahPeersReadyCondition(
		metav1.ConditionTrue,
		enterpriseApi.ReasonNoahPeersReady,
		"All expected Noah peers are up",
	))

	assert.Equal(t, enterpriseApi.PhaseReady, cr.Status.Phase)
	assert.Equal(t, int64(7), cr.Status.ObservedGeneration)
	assert.True(t, splcommon.IsReady(cr.Status.Conditions))
	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahPeersReady), condition.Reason)
	assert.Equal(t, int64(7), condition.ObservedGeneration)
}

func TestSetNoahIndexerPhaseAndConditionsPreservesUnspecifiedConditions(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Generation: 7},
		Status: enterpriseApi.IndexerClusterStatus{Conditions: []metav1.Condition{
			{
				Type:               string(enterpriseApi.ConditionNoahPeersReady),
				Status:             metav1.ConditionTrue,
				Reason:             string(enterpriseApi.ReasonNoahPeersReady),
				ObservedGeneration: 6,
			},
		}},
	}

	setNoahIndexerPhaseAndConditions(cr, false, enterpriseApi.PhaseError, "resource application failed")

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, int64(6), condition.ObservedGeneration)
}
