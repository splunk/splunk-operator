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

package indexercluster

import (
	"context"
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	noahclient "github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
)

const acceptedGeneralTerms = "--accept-sgt-current-at-splunk-com"

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
	mutex          sync.RWMutex
	peers          []noahclient.Peer
	requestHeaders http.Header
}

type noahHTTPClientFunc func(*http.Request) (*http.Response, error)

func (fn noahHTTPClientFunc) Do(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func newNoahResponseClient(t *testing.T, statusCode int, responseBody string) *noahclient.Client {
	t.Helper()
	client, err := noahclient.NewClient(
		"https://noah.test",
		"tenant",
		noahclient.AuthenticatorFunc(func(*http.Request, []byte) error { return nil }),
		noahclient.WithHTTPClient(noahHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			response := httptest.NewRecorder()
			response.WriteHeader(statusCode)
			_, err := response.WriteString(responseBody)
			require.NoError(t, err)
			return response.Result(), nil
		})),
	)
	require.NoError(t, err)
	return client
}

func newNoahIndexerDependencyTestCR(generation int64, noahClusterName string) *enterpriseApi.IndexerCluster {
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "test", Generation: generation},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       1,
			NoahClusterRef: &corev1.LocalObjectReference{Name: noahClusterName},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	return cr
}

func newNoahIndexerPodManagerForTest(t *testing.T, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) *noahIndexerPodManager {
	t.Helper()
	runtime, err := configworkflow.ResolveNoahRuntime(t.Context(), client, cr.Namespace, *cr.Spec.NoahClusterRef)
	require.NoError(t, err)
	return newNoahIndexerPodManager(client, cr, runtime)
}

func newNoahIndexerScaleOutTestFixture(t *testing.T, options noahIndexerScaleOutTestOptions) *noahIndexerScaleOutTestFixture {
	t.Helper()
	if options.peerStart == 0 {
		options.peerStart = time.Now().Unix()
	}

	fixture := &noahIndexerScaleOutTestFixture{client: spltest.NewMockClient()}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fixture.mutex.Lock()
		defer fixture.mutex.Unlock()
		fixture.requestHeaders = request.Header.Clone()
		if err := json.NewEncoder(response).Encode(fixture.peers); err != nil {
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
	require.NoError(t, fixture.client.Create(t.Context(), fixture.cr.DeepCopy()))
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
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte("unit-test-noah-key")},
	}))
	fixture.peers = []noahclient.Peer{{
		ID:            "splunk-main-indexer-0.splunk-main-indexer-headless.test.svc.corp.example",
		Status:        options.peerStatus,
		Data:          noahclient.PeerData{StartTime: options.peerStart},
		LastHeartbeat: options.peerStart + 1,
	}}

	appliedReplicas := int32(1)
	fixture.statefulSet = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-main-indexer", Namespace: fixture.cr.Namespace, UID: "statefulset-uid", Generation: 1},
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
			UID:       "source-pod-uid",
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

func (fixture *noahIndexerScaleOutTestFixture) podManager(t *testing.T) *noahIndexerPodManager {
	t.Helper()
	mgr := newNoahIndexerPodManagerForTest(t, fixture.client, fixture.cr)
	mgr.statefulSet = fixture.statefulSet
	return mgr
}

func (fixture *noahIndexerScaleOutTestFixture) update(t *testing.T) (enterpriseApi.Phase, error) {
	t.Helper()
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace,
	}, statefulSet))
	return newNoahIndexerPodManagerForTest(t, fixture.client, fixture.cr).Update(
		t.Context(), fixture.client, statefulSet, fixture.cr.Spec.Replicas,
	)
}

func (fixture *noahIndexerScaleOutTestFixture) persistAndReloadCR(t *testing.T) {
	t.Helper()
	fixture.cr = persistAndReloadIndexerCluster(t, fixture.client, fixture.cr)
}

func (fixture *noahIndexerScaleOutTestFixture) advanceToPendingAction(t *testing.T) {
	t.Helper()
	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)
	fixture.persistAndReloadCR(t)
}

func (fixture *noahIndexerScaleOutTestFixture) advanceToMembershipWait(t *testing.T) {
	t.Helper()
	fixture.advanceToPendingAction(t)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)

	fixture.persistAndReloadCR(t)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)
	assert.Nil(t, fixture.cr.Status.Lifecycle.PendingAction)
	fixture.persistAndReloadCR(t)
}

func (fixture *noahIndexerScaleOutTestFixture) makeTargetReady(t *testing.T, currentRevision, updateRevision string) *appsv1.StatefulSet {
	t.Helper()
	const targetStart int64 = 1_800_000_000
	require.NoError(t, fixture.client.Create(t.Context(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-main-indexer-1",
			Namespace: fixture.cr.Namespace,
			UID:       "target-pod-uid",
			Labels:    map[string]string{"controller-revision-hash": currentRevision},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue,
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "splunk", Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.NewTime(time.Unix(targetStart-1, 0)),
				}},
			}},
		},
	}))

	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace,
	}, statefulSet))
	statefulSet.Status.ObservedGeneration = statefulSet.Generation
	statefulSet.Status.Replicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.CurrentReplicas = 2
	statefulSet.Status.CurrentRevision = currentRevision
	statefulSet.Status.UpdateRevision = updateRevision
	if currentRevision == updateRevision {
		statefulSet.Status.UpdatedReplicas = 2
	}
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

	fixture.mutex.Lock()
	fixture.peers = append(fixture.peers, noahclient.Peer{
		ID:            "splunk-main-indexer-1.splunk-main-indexer-headless.test.svc.corp.example",
		Status:        noahclient.PeerStatusUp,
		Data:          noahclient.PeerData{StartTime: targetStart},
		LastHeartbeat: targetStart + 1,
	})
	fixture.mutex.Unlock()
	return statefulSet
}

func (fixture *noahIndexerScaleOutTestFixture) observeScaleOutReady(t *testing.T, statefulSet *appsv1.StatefulSet) noahIndexerOutcome {
	t.Helper()
	mgr := newNoahIndexerPodManagerForTest(t, fixture.client, fixture.cr)
	mgr.statefulSet = statefulSet
	outcome, err := mgr.observeReady(t.Context(), 2, enterpriseApi.PhaseScalingUp, 1)
	require.NoError(t, err)
	return outcome
}

type noahIndexerRolloutTestFixture struct {
	client         *spltest.MockClient
	cr             *enterpriseApi.IndexerCluster
	statefulSetKey types.NamespacedName
	mutex          sync.RWMutex
	peers          []noahclient.Peer
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
	require.NoError(t, fixture.client.Create(t.Context(), fixture.cr.DeepCopy()))
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
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte("unit-test-noah-key")},
	}))

	replicas := int32(3)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-main-indexer", Namespace: fixture.cr.Namespace, UID: "statefulset-uid", Generation: 1},
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
			UID:       types.UID(fmt.Sprintf("pod-%d", ordinal)),
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
	phase, err := newNoahIndexerPodManagerForTest(t, fixture.client, fixture.cr).Update(
		t.Context(), fixture.client, statefulSet, fixture.cr.Spec.Replicas,
	)
	fixture.cr.Status.Phase = phase
	return phase, err
}

func (fixture *noahIndexerScaleDownTestFixture) persistAndReloadCR(t *testing.T) {
	t.Helper()
	fixture.cr = persistAndReloadIndexerCluster(t, fixture.client, fixture.cr)
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

func (fixture *noahIndexerScaleDownTestFixture) setPeerIncarnation(ordinal int32, startTime int64) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	for index := range fixture.peers {
		if fixture.peers[index].ID == fixture.peerID(ordinal) {
			fixture.peers[index].Data.StartTime = startTime
			fixture.peers[index].LastHeartbeat = startTime + 1
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

func (fixture *noahIndexerScaleDownTestFixture) addPeer(peer noahclient.Peer) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	fixture.peers = append(fixture.peers, peer)
}

func (fixture *noahIndexerScaleDownTestFixture) unregisterCount() int {
	fixture.mutex.RLock()
	defer fixture.mutex.RUnlock()
	return len(fixture.unregistered)
}

func (fixture *noahIndexerScaleDownTestFixture) unregisteredPeerIDs() []string {
	fixture.mutex.RLock()
	defer fixture.mutex.RUnlock()
	return slices.Clone(fixture.unregistered)
}

func (fixture *noahIndexerScaleDownTestFixture) failUnregister(status int) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	fixture.unregisterCode = status
}

func (fixture *noahIndexerScaleDownTestFixture) applyNextScaleIn(t *testing.T) {
	t.Helper()
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	sourceReplicas := *statefulSet.Spec.Replicas

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleScaleIn, fixture.cr.Status.Lifecycle.Kind)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)
	assert.Equal(t, sourceReplicas, fixture.cr.Status.Lifecycle.Target.SourceReplicas)
	assert.Equal(t, sourceReplicas-1, fixture.cr.Status.Lifecycle.Target.TargetReplicas)
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, sourceReplicas, *statefulSet.Spec.Replicas, "replicas must not change before lifecycle status is persisted")

	fixture.persistAndReloadCR(t)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, sourceReplicas-1, *statefulSet.Spec.Replicas)
}

func (fixture *noahIndexerScaleDownTestFixture) advanceScaleInToCleanup(t *testing.T, ordinal, replicas int32) {
	t.Helper()
	fixture.applyNextScaleIn(t)
	fixture.finishPodRemoval(t, ordinal, replicas)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)
	fixture.persistAndReloadCR(t)
}

func (fixture *noahIndexerScaleDownTestFixture) completeScaleIn(t *testing.T) {
	t.Helper()
	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, fixture.cr.Status.Lifecycle.Checkpoint)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assert.Nil(t, fixture.cr.Status.Lifecycle)
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
		fixture.mutex.RLock()
		peers := append([]noahclient.Peer(nil), fixture.peers...)
		fixture.mutex.RUnlock()
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
	require.NoError(t, fixture.client.Create(t.Context(), fixture.cr.DeepCopy()))
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
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte("unit-test-noah-key")},
	}))

	replicas := int32(2)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-main-indexer", Namespace: fixture.cr.Namespace, UID: "statefulset-uid", Generation: 2},
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
	return newNoahIndexerPodManagerForTest(t, fixture.client, fixture.cr).Update(
		t.Context(),
		fixture.client,
		statefulSet,
		fixture.cr.Spec.Replicas,
	)
}

func (fixture *noahIndexerRolloutTestFixture) persistAndReloadCR(t *testing.T) {
	t.Helper()
	fixture.cr = persistAndReloadIndexerCluster(t, fixture.client, fixture.cr)
}

func persistAndReloadIndexerCluster(t *testing.T, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) *enterpriseApi.IndexerCluster {
	t.Helper()
	require.NoError(t, client.Update(t.Context(), cr.DeepCopy()))
	reloaded := &enterpriseApi.IndexerCluster{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, reloaded))
	return reloaded
}

func (fixture *noahIndexerRolloutTestFixture) setStatefulSetStatus(t *testing.T, readyReplicas, updatedReplicas int32) {
	t.Helper()
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.ReadyReplicas = readyReplicas
	statefulSet.Status.UpdatedReplicas = updatedReplicas
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))
}

func (fixture *noahIndexerRolloutTestFixture) persistRolloutAction(t *testing.T) {
	t.Helper()
	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)
	assert.Equal(t, int32(1), fixture.cr.Status.Lifecycle.Target.Peers[0].Ordinal)
	fixture.persistAndReloadCR(t)
}

func (fixture *noahIndexerRolloutTestFixture) executeRolloutAction(t *testing.T) {
	t.Helper()
	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
}

func TestNoahIndexerFailedLifecycleReplansAfterStatefulSetReplacement(t *testing.T) {
	newFailedLifecycle := func() *enterpriseApi.IndexerClusterLifecycleStatus {
		now := metav1.Now()
		return &enterpriseApi.IndexerClusterLifecycleStatus{
			Kind:               enterpriseApi.IndexerClusterLifecycleScaleOut,
			Checkpoint:         enterpriseApi.IndexerClusterLifecycleFailed,
			Generation:         1,
			StartedAt:          now,
			LastTransitionTime: now,
			Target: enterpriseApi.IndexerClusterLifecycleTarget{
				StatefulSetUID: "original-statefulset",
				SourceReplicas: 3,
				TargetReplicas: 4,
				Peers: []enterpriseApi.IndexerClusterLifecyclePeerTarget{{
					Ordinal: 3,
					PeerID:  "peer-3",
					PodName: "indexer-3",
				}},
			},
		}
	}

	tests := []struct {
		name           string
		statefulSetUID types.UID
		wantReplan     bool
	}{
		{name: "replacement incarnation replans", statefulSetUID: "replacement-statefulset", wantReplan: true},
		{name: "same incarnation remains failed", statefulSetUID: "original-statefulset"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cr := &enterpriseApi.IndexerCluster{Status: enterpriseApi.IndexerClusterStatus{Lifecycle: newFailedLifecycle()}}
			mgr := &noahIndexerPodManager{
				cr:          cr,
				statefulSet: &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{UID: test.statefulSetUID}},
			}

			decision, err := mgr.reconcileLifecycle(3)

			if test.wantReplan {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "lifecycle failed")
			}
			assert.True(t, decision.block)
			assert.Equal(t, enterpriseApi.PhaseScalingUp, decision.phaseOverride)
			if test.wantReplan {
				assert.Nil(t, cr.Status.Lifecycle)
			} else {
				assert.NotNil(t, cr.Status.Lifecycle)
			}
		})
	}
}

func TestNoahIndexerPodManagerPersistsHighestOrdinalBeforeDeletion(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	fixture.persistRolloutAction(t)

	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)

	fixture.executeRolloutAction(t)
	assertPodNotFound(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerRevalidatesPeersBeforeRolloutDeletion(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	fixture.persistRolloutAction(t)
	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusDown, 1_700_000_000),
		fixture.peer(1, noahclient.PeerStatusUp, 1_700_000_000),
	)

	fixture.executeRolloutAction(t)
	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)

	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusUp, 1_700_000_000),
		fixture.peer(1, noahclient.PeerStatusUp, 1_700_000_000),
	)
	fixture.executeRolloutAction(t)
	assertPodNotFound(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerCompletesRolloutBeforeStartingNewRevision(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	fixture.persistRolloutAction(t)
	fixture.executeRolloutAction(t)

	const replacementStart int64 = 1_700_000_100
	fixture.createPod(t, 1, "revision-2", replacementStart, true)
	fixture.setStatefulSetStatus(t, 2, 1)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.UpdateRevision = "revision-3"
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))
	staleReplacementPeer := fixture.peer(1, noahclient.PeerStatusUp, replacementStart)
	staleReplacementPeer.LastHeartbeat = replacementStart
	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusUp, 1_700_000_000),
		staleReplacementPeer,
	)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)
	assert.Equal(t, types.UID("pod-1-revision-2"), fixture.cr.Status.Lifecycle.Target.Peers[0].TargetPodUID)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
	fixture.persistAndReloadCR(t)

	// A fresh manager must retain the exact target and reject a Noah record
	// without a post-start heartbeat.
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)

	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusUp, 1_700_000_000),
		fixture.peer(1, noahclient.PeerStatusUp, replacementStart),
	)

	// A newer template must not replace this Pod again before the persisted
	// operation observes its exact replacement incarnation in Noah.
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, fixture.cr.Status.Lifecycle.Checkpoint)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assert.Nil(t, fixture.cr.Status.Lifecycle)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, int32(1), fixture.cr.Status.Lifecycle.Target.Peers[0].Ordinal)
	assert.Equal(t, "revision-3", fixture.cr.Status.Lifecycle.Target.TargetRevision)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodNotFound(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
	assertPodExists(t, fixture.client, "splunk-main-indexer-0", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerKeepsActiveRolloutTargetReachable(t *testing.T) {
	fixture := newNoahIndexerRolloutTestFixture(t)
	fixture.cr.Spec.Replicas = 3

	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	*statefulSet.Spec.Replicas = 3
	statefulSet.Status.Replicas = 3
	statefulSet.Status.ReadyReplicas = 3
	statefulSet.Status.CurrentReplicas = 3
	statefulSet.Status.UpdatedReplicas = 1
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))
	fixture.createPod(t, 2, "revision-2", 1_700_000_000, true)
	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusUp, 1_700_000_000),
		fixture.peer(1, noahclient.PeerStatusUp, 1_700_000_000),
		fixture.peer(2, noahclient.PeerStatusUp, 1_700_000_000),
	)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, int32(1), fixture.cr.Status.Lifecycle.Target.Peers[0].Ordinal)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodNotFound(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)

	const replacementStart int64 = 1_700_000_100
	fixture.createPod(t, 1, "revision-2", replacementStart, true)
	statefulSet = &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.UpdateRevision = "revision-3"
	statefulSet.Status.ReadyReplicas = 3
	statefulSet.Status.UpdatedReplicas = 0
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))
	fixture.setPeers(
		fixture.peer(0, noahclient.PeerStatusUp, 1_700_000_000),
		fixture.peer(1, noahclient.PeerStatusUp, replacementStart),
		fixture.peer(2, noahclient.PeerStatusUp, 1_700_000_000),
	)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)
	assertPodExists(t, fixture.client, "splunk-main-indexer-2", fixture.cr.Namespace)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, fixture.cr.Status.Lifecycle.Checkpoint)
	assertPodExists(t, fixture.client, "splunk-main-indexer-2", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerReplansRolloutAfterReplicaDrift(t *testing.T) {
	for _, replicas := range []int32{1, 3} {
		t.Run(fmt.Sprintf("replicas_%d", replicas), func(t *testing.T) {
			fixture := newNoahIndexerRolloutTestFixture(t)
			phase, err := fixture.update(t)
			require.NoError(t, err)
			assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
			require.NotNil(t, fixture.cr.Status.Lifecycle)

			statefulSet := &appsv1.StatefulSet{}
			require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
			*statefulSet.Spec.Replicas = replicas
			statefulSet.Status.Replicas = replicas
			statefulSet.Status.ReadyReplicas = replicas
			statefulSet.Status.CurrentReplicas = replicas
			require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

			phase, err = fixture.update(t)
			require.NoError(t, err)
			assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
			assert.Nil(t, fixture.cr.Status.Lifecycle)
			assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)

			stored := &appsv1.StatefulSet{}
			require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, stored))
			assert.Equal(t, replicas, *stored.Spec.Replicas, "reset must be status-only")
		})
	}
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
	assertPodExists(t, fixture.client, podKey.Name, podKey.Namespace)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
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

	phase, err = fixture.update(t)
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
			Name:      splutil.GetSplunkStatefulsetName(splcommon.SplunkLicenseManager, licenseManager.Name),
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

func TestEnsureNoahIndexerDefaultsCombinesNoahAndSmartBusCredentials(t *testing.T) {
	ctx := t.Context()
	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	client := newFakeClientBuilder(scheme).Build()
	queue, objectStorage := newQueueOSFixture(t, ctx, client, "queue", "queue-secrets")
	cr := &enterpriseApi.IndexerCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			NoahClusterRef:   &corev1.LocalObjectReference{Name: "noah"},
			QueueRef:         &corev1.ObjectReference{Name: queue.Name},
			ObjectStorageRef: &corev1.ObjectReference{Name: objectStorage.Name},
		},
	}

	noahCredential := t.Name()
	configMap, secret, err := ensureNoahIndexerDefaults(
		ctx,
		client,
		cr,
		enterpriseApi.NoahClusterSpec{Endpoint: "https://noah.example.invalid:8080", Tenant: "tenant"},
		noahCredential,
	)
	require.NoError(t, err)
	require.NotEmpty(t, configMap.Name)
	require.NotEmpty(t, secret.Name)

	storedConfigMap := &corev1.ConfigMap{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: configMap.Name}, storedConfigMap))
	config := storedConfigMap.Data["conf-defaults.yml"]
	assert.Contains(t, config, "https://noah.example.invalid:8080")
	assert.Contains(t, config, "test-queue")
	assert.NotContains(t, config, noahCredential)
	assert.NotContains(t, config, "AKIAEXAMPLE")

	storedSecret := &corev1.Secret{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: secret.Name}, storedSecret))
	credentials := string(storedSecret.Data["conf-defaults.yml"])
	assert.Contains(t, credentials, noahCredential)
	assert.Contains(t, credentials, "AKIAEXAMPLE")
	assert.Contains(t, credentials, "shhh-secret")

	_, rotatedSecret, err := ensureNoahIndexerDefaults(
		ctx,
		client,
		cr,
		enterpriseApi.NoahClusterSpec{Endpoint: "https://noah.example.invalid:8080", Tenant: "tenant"},
		noahCredential+"-rotated",
	)
	require.NoError(t, err)
	assert.NotEqual(t, secret.Name, rotatedSecret.Name)
}

func TestEnsureNoahIndexerDefaultsIgnoresEmptySmartBusReferences(t *testing.T) {
	ctx := t.Context()
	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	client := newFakeClientBuilder(scheme).Build()
	cr := &enterpriseApi.IndexerCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			NoahClusterRef:   &corev1.LocalObjectReference{Name: "noah"},
			QueueRef:         &corev1.ObjectReference{},
			ObjectStorageRef: &corev1.ObjectReference{},
		},
	}

	configMap, secret, err := ensureNoahIndexerDefaults(
		ctx,
		client,
		cr,
		enterpriseApi.NoahClusterSpec{Endpoint: "https://noah.example.invalid:8080", Tenant: "tenant"},
		t.Name(),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, configMap.Name)
	assert.NotEmpty(t, secret.Name)
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
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	splunkCredential := t.Name()
	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, cr.Namespace)
	require.NoError(t, err)
	namespaceSecret.Data["pass4SymmKey"] = []byte(splunkCredential)
	require.NoError(t, client.Update(ctx, namespaceSecret))
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
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte("unit-test-noah-key")},
	}))

	statefulSet, phase, err := applyNoahIndexerResources(ctx, client, cr, newNoahIndexerPodManagerForTest(t, client, cr))
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, phase)

	created := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      statefulSet.Name,
		Namespace: statefulSet.Namespace,
	}, created))
	require.NotNil(t, created.Spec.Replicas)
	assert.Equal(t, int32(3), *created.Spec.Replicas, "initial creation must start every requested replica")
	assert.Equal(t, resources.GetSplunkLabels(cr.Name, splcommon.SplunkIndexer, cr.Spec.NoahClusterRef.Name), created.Spec.Selector.MatchLabels)
	for key, value := range created.Spec.Selector.MatchLabels {
		assert.Equal(t, value, created.Spec.Template.Labels[key])
	}
	for _, headless := range []bool{true, false} {
		service := &corev1.Service{}
		require.NoError(t, client.Get(ctx, types.NamespacedName{
			Name:      splcommon.GetSplunkServiceName(splcommon.SplunkIndexer, cr.Name, headless),
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
	assert.NotContains(t, defaultsConfigMap.Data["conf-defaults.yml"], "unit-test-noah-key")

	var defaultsSecretName string
	for _, volume := range created.Spec.Template.Spec.Volumes {
		if volume.Secret != nil && strings.HasPrefix(volume.Secret.SecretName, "sok-indexercluster-creds-") {
			defaultsSecretName = volume.Secret.SecretName
			break
		}
	}
	require.NotEmpty(t, defaultsSecretName)
	defaultsSecret := &corev1.Secret{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      defaultsSecretName,
		Namespace: created.Namespace,
	}, defaultsSecret))
	assert.Contains(t, string(defaultsSecret.Data["conf-defaults.yml"]), "unit-test-noah-key")
	assert.Contains(t, string(defaultsSecret.Data["conf-defaults.yml"]), "pass4SymmKey")
	assert.NotContains(t, string(defaultsSecret.Data["conf-defaults.yml"]), splunkCredential)

	env := make(map[string]corev1.EnvVar)
	for _, item := range created.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item
	}
	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, created.Spec.ServiceName, env[resources.NoahHeadlessServiceEnvName].Value)
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)
	require.NotNil(t, env[resources.PodNameEnvName].ValueFrom)
	require.NotNil(t, env[resources.PodNamespaceEnvName].ValueFrom)

	for _, initContainer := range created.Spec.Template.Spec.InitContainers {
		assert.NotEqual(t, "init-etc", initContainer.Name)
	}
	assert.Contains(t, env["SPLUNK_DEFAULTS_URL"].Value, resources.SecretMountPath())
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
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte("unit-test-noah-key")},
	}))

	replicas := int32(1)
	currentStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      splutil.GetSplunkStatefulsetName(splcommon.SplunkIndexer, cr.Name),
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
			Name:      splutil.GetSplunkStatefulsetName(splcommon.SplunkLicenseManager, licenseManager.Name),
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

	statefulSet, phase, err := applyNoahIndexerResources(ctx, client, cr, newNoahIndexerPodManagerForTest(t, client, cr))
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, phase)
	assert.Equal(t, currentStatefulSet.Name, statefulSet.Name)
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: staleConfigMap.Name, Namespace: staleConfigMap.Namespace}, &corev1.ConfigMap{}))
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: staleSecret.Name, Namespace: staleSecret.Namespace}, &corev1.Secret{}))
}

func TestApplyNoahIndexerClusterReportsMissingDependency(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
	client := spltest.NewMockClient()
	cr := newNoahIndexerDependencyTestCR(7, "missing-noah")
	require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

	_, err := applyNoahIndexerCluster(t.Context(), client, cr)
	require.NoError(t, err, "a missing dependency is retryable, not terminal")

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionUnknown, condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyMissing), condition.Reason)
	assert.Equal(t, int64(7), condition.ObservedGeneration)
	assert.Contains(t, condition.Message, "missing-noah")
}

func TestApplyNoahIndexerClusterPreservesPeerStatusOnUnknownDependencyReadFailure(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
	client := spltest.NewMockClient()
	cr := newNoahIndexerDependencyTestCR(4, "noah")
	cr.Status.Conditions = splcommon.UpsertCondition(cr.Status.Conditions, metav1.Condition{
		Type:    string(enterpriseApi.ConditionNoahDependencyResolved),
		Status:  metav1.ConditionTrue,
		Reason:  string(enterpriseApi.ReasonNoahDependencyResolved),
		Message: "Referenced NoahCluster and authentication Secret resolved",
	})
	cr.Status.Conditions = splcommon.UpsertCondition(cr.Status.Conditions, newNoahPeersReadyCondition(
		metav1.ConditionTrue, enterpriseApi.ReasonNoahPeersReady, "All expected Noah peers are up"))
	require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))
	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = assert.AnError

	_, err := applyNoahIndexerCluster(t.Context(), client, cr)
	require.Error(t, err)

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionUnknown, condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyUnknown), condition.Reason)
	assert.NotContains(t, condition.Message, "resolved")
	peers := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady)
	require.NotNil(t, peers)
	assert.Equal(t, metav1.ConditionTrue, peers.Status)
	ready := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, "Failed to resolve Noah dependencies", ready.Message)
	for _, condition := range cr.Status.Conditions {
		assert.NotEmpty(t, condition.Type)
	}
}

func TestApplyNoahIndexerClusterReportsResolvedDependency(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
	client := spltest.NewMockClient()
	client.AddObject(&enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "test"},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "http://noah.example:8080",
			Tenant:        "linus-dev",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	})
	client.AddObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte(t.Name())},
	})
	cr := newNoahIndexerDependencyTestCR(5, "noah")
	require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

	result, err := applyNoahIndexerCluster(t.Context(), client, cr)
	require.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter)

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyResolved), condition.Reason)
	assert.Equal(t, int64(5), condition.ObservedGeneration)
}

func TestApplyNoahIndexerClusterDependencyLossClearsStalePeersReady(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
	client := spltest.NewMockClient()
	cr := newNoahIndexerDependencyTestCR(3, "missing-noah")
	cr.Status.Conditions = splcommon.UpsertCondition(cr.Status.Conditions, newNoahPeersReadyCondition(
		metav1.ConditionTrue, enterpriseApi.ReasonNoahPeersReady, "All expected Noah peers are up"))
	require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

	_, err := applyNoahIndexerCluster(t.Context(), client, cr)
	require.NoError(t, err, "a missing dependency is retryable, not terminal")

	peers := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady)
	require.NotNil(t, peers)
	assert.Equal(t, metav1.ConditionUnknown, peers.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahPeerObservationFailed), peers.Reason)

	dependency := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, dependency)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyMissing), dependency.Reason)
}

func TestNoahIndexerPodManagerScalesDownOneOrdinalAndWaitsForNoahCleanup(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	statefulSet := &appsv1.StatefulSet{}
	fixture.applyNextScaleIn(t)

	assert.Zero(t, fixture.unregisterCount())
	assertPodExists(t, fixture.client, "splunk-main-indexer-2", fixture.cr.Namespace)
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: "pvc-etc-splunk-main-indexer-2", Namespace: fixture.cr.Namespace,
	}, &corev1.PersistentVolumeClaim{}))

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assert.Zero(t, fixture.unregisterCount(), "must wait for Pod removal before unregistering")
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)
	fixture.persistAndReloadCR(t)

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
	fixture.completeScaleIn(t)
	assert.Equal(t, 3, fixture.unregisterCount())

	fixture.advanceScaleInToCleanup(t, 1, 1)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assert.Equal(t, 4, fixture.unregisterCount())

	fixture.setPeerStatus(1, noahclient.PeerStatusDown)
	fixture.excludeFromBucketMap(1)
	fixture.completeScaleIn(t)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase)
	assert.Equal(t, 5, fixture.unregisterCount())
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: "pvc-etc-splunk-main-indexer-1", Namespace: fixture.cr.Namespace,
	}, &corev1.PersistentVolumeClaim{}))
}

func TestNoahIndexerPodManagerBlocksScaleDownUntilPeersAreReady(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.setPeerStatus(1, noahclient.PeerStatusWarming)

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(3), *statefulSet.Spec.Replicas)
	assert.Nil(t, fixture.cr.Status.Lifecycle)
}

func TestNoahIndexerPodManagerRevalidatesPeersBeforeScaleDown(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)
	fixture.persistAndReloadCR(t)

	fixture.setPeerStatus(1, noahclient.PeerStatusWarming)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(3), *statefulSet.Spec.Replicas)

	fixture.setPeerStatus(1, noahclient.PeerStatusUp)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(2), *statefulSet.Spec.Replicas)
}

func TestNoahIndexerPodManagerReplansScaleInForReplacementSourcePod(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, types.UID("pod-2"), fixture.cr.Status.Lifecycle.Target.Peers[0].SourcePodUID)
	fixture.persistAndReloadCR(t)

	pod := &corev1.Pod{}
	podKey := types.NamespacedName{Name: "splunk-main-indexer-2", Namespace: fixture.cr.Namespace}
	require.NoError(t, fixture.client.Get(t.Context(), podKey, pod))
	require.NoError(t, fixture.client.Delete(t.Context(), pod))
	fixture.createPod(t, 2, 1_700_000_100)
	require.NoError(t, fixture.client.Get(t.Context(), podKey, pod))
	pod.UID = "replacement-pod-2"
	require.NoError(t, fixture.client.Update(t.Context(), pod))

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assert.Nil(t, fixture.cr.Status.Lifecycle)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, int32(3), *statefulSet.Spec.Replicas)

	fixture.setPeerIncarnation(2, 1_700_000_100)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, types.UID("replacement-pod-2"), fixture.cr.Status.Lifecycle.Target.Peers[0].SourcePodUID)
}

func TestNoahIndexerPodManagerNeverUnregistersForeignPeer(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2
	const foreignPeerID = "splunk-other-indexer-0.splunk-other-indexer-headless.test.svc.corp.example"
	fixture.addPeer(noahclient.Peer{
		ID:            foreignPeerID,
		Status:        noahclient.PeerStatusUp,
		Data:          noahclient.PeerData{StartTime: 1_700_000_000},
		LastHeartbeat: 1_700_000_001,
	})

	fixture.advanceScaleInToCleanup(t, 2, 2)
	fixture.excludeFromBucketMap(2)

	fixture.completeScaleIn(t)
	assert.Equal(t, []string{fixture.peerID(2)}, fixture.unregisteredPeerIDs())
}

func TestNoahIndexerPodManagerResumesScaleDownCleanupFromLifecycle(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2
	fixture.advanceScaleInToCleanup(t, 2, 2)

	// A newly constructed manager resumes cleanup from persisted lifecycle state,
	// regardless of the projected phase and replica status.
	fixture.cr.Status.Phase = enterpriseApi.PhaseError
	fixture.cr.Status.Replicas = 2
	fixture.setPeerStatus(2, noahclient.PeerStatusDown)
	fixture.persistAndReloadCR(t)
	phase, err := fixture.update(t)
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

func TestNoahIndexerPodManagerFinishesPendingScaleDownBeforeChangedScaleOut(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2
	fixture.advanceScaleInToCleanup(t, 2, 2)

	fixture.cr.Spec.Replicas = 3
	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(2), *statefulSet.Spec.Replicas)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleScaleIn, fixture.cr.Status.Lifecycle.Kind)

	fixture.excludeFromBucketMap(2)
	fixture.completeScaleIn(t)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	fixture.persistAndReloadCR(t)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(3), *statefulSet.Spec.Replicas)
}

func TestNoahIndexerPodManagerFinishesPendingScaleDownBeforeRollout(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2
	fixture.advanceScaleInToCleanup(t, 2, 2)

	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	statefulSet.Status.UpdateRevision = "revision-2"
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)

	fixture.excludeFromBucketMap(2)
	fixture.completeScaleIn(t)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)

	fixture.persistAndReloadCR(t)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodNotFound(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerChecksCleanBucketMapAfterUnregister(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.cr.Spec.Replicas = 2
	fixture.advanceScaleInToCleanup(t, 2, 2)
	fixture.excludeFromBucketMap(2)

	fixture.completeScaleIn(t)
	assert.Equal(t, 1, fixture.unregisterCount())
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
	assert.Equal(t, int32(2), *statefulSet.Spec.Replicas)
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
			fixture.advanceScaleInToCleanup(t, 2, 2)

			var peerIDs []string
			if test.peerIDs != nil {
				peerIDs = test.peerIDs(fixture)
			}
			fixture.setBucketMap(test.status, peerIDs)
			phase, err := fixture.update(t)
			require.NoError(t, err)
			assert.Equal(t, enterpriseApi.PhaseScalingDown, phase)
			statefulSet := &appsv1.StatefulSet{}
			require.NoError(t, fixture.client.Get(t.Context(), fixture.statefulSetKey, statefulSet))
			assert.Equal(t, int32(2), *statefulSet.Spec.Replicas)
			require.NotNil(t, fixture.cr.Status.Lifecycle)
			assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)
		})
	}
}

func TestNoahIndexerPodManagerPropagatesScaleDownCleanupFailure(t *testing.T) {
	fixture := newNoahIndexerScaleDownTestFixture(t)
	fixture.advanceScaleInToCleanup(t, 2, 2)
	fixture.failUnregister(http.StatusInternalServerError)
	phase, err := fixture.update(t)
	require.ErrorContains(t, err, "unregister Noah peer")
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)
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

func TestNoahIndexerScaleOutUsesReferencedAuthentication(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	fixture.advanceToPendingAction(t)
	assert.NotEmpty(t, fixture.requestHeaders.Get("x-splunk-lm-nonce"))
	assert.NotEmpty(t, fixture.requestHeaders.Get("x-splunk-lm-timestamp"))
	assert.True(t, strings.HasPrefix(fixture.requestHeaders.Get("x-splunk-digest"), "v2,"))
}

func TestNoahIndexerPodManagerPersistsScaleOutActionBeforeApplyingTarget(t *testing.T) {
	disabled := false
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{
		peerStatus:       noahclient.PeerStatusWarming,
		cacheWarmEnabled: &disabled,
	})
	fixture.advanceToPendingAction(t)

	stored := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace}, stored))
	require.NotNil(t, stored.Spec.Replicas)
	assert.Equal(t, int32(1), *stored.Spec.Replicas, "the authorized action must be persisted before the StatefulSet changes")

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)

	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace}, stored))
	require.NotNil(t, stored.Spec.Replicas)
	assert.Equal(t, int32(2), *stored.Spec.Replicas)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint,
		"a lost update response remains retryable from the durable action")
}

func TestNoahIndexerPodManagerCompletesDurableScaleOut(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	fixture.advanceToMembershipWait(t)
	statefulSet := fixture.makeTargetReady(t, "revision-1", "revision-1")

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase)

	outcome := fixture.observeScaleOutReady(t, statefulSet)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, outcome.phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, types.UID("target-pod-uid"), fixture.cr.Status.Lifecycle.Target.Peers[0].TargetPodUID)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleWaitingForMembership, fixture.cr.Status.Lifecycle.Checkpoint)

	fixture.persistAndReloadCR(t)
	outcome = fixture.observeScaleOutReady(t, statefulSet)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, outcome.phase)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, fixture.cr.Status.Lifecycle.Checkpoint)
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace,
	}, statefulSet))
	assert.Equal(t, int32(2), *statefulSet.Spec.Replicas, "one lifecycle record must complete only its exact batch")
}

func TestNoahIndexerPodManagerReplansScaleOutAfterReplicaDrift(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	fixture.advanceToMembershipWait(t)

	statefulSet := &appsv1.StatefulSet{}
	key := types.NamespacedName{Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace}
	require.NoError(t, fixture.client.Get(t.Context(), key, statefulSet))
	replicas := int32(1)
	statefulSet.Spec.Replicas = &replicas
	statefulSet.Status.Replicas = replicas
	statefulSet.Status.ReadyReplicas = replicas
	statefulSet.Status.CurrentReplicas = replicas
	statefulSet.Status.UpdatedReplicas = replicas
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	assert.Nil(t, fixture.cr.Status.Lifecycle)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)
	assert.Equal(t, int32(1), fixture.cr.Status.Lifecycle.Target.SourceReplicas)
	assert.Equal(t, int32(2), fixture.cr.Status.Lifecycle.Target.TargetReplicas)
}

func TestNoahIndexerPodManagerCompletesScaleOutBeforeNewerRollout(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	fixture.advanceToMembershipWait(t)
	statefulSet := fixture.makeTargetReady(t, "revision-1", "revision-2")

	phase, err := fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase,
		"a newer rollout must not prevent membership observation for the authorized scale-out")

	fixture.observeScaleOutReady(t, statefulSet)
	fixture.persistAndReloadCR(t)

	fixture.observeScaleOutReady(t, statefulSet)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleCompleted, fixture.cr.Status.Lifecycle.Checkpoint)
	// Stop at this batch so the next operation is the pending template rollout.
	fixture.cr.Spec.Replicas = 2
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	assert.Nil(t, fixture.cr.Status.Lifecycle)
	fixture.persistAndReloadCR(t)
	require.Nil(t, fixture.cr.Status.Lifecycle)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodExists(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
	require.NotNil(t, fixture.cr.Status.Lifecycle)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)
	fixture.persistAndReloadCR(t)

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	assertPodNotFound(t, fixture.client, "splunk-main-indexer-1", fixture.cr.Namespace)
}

func TestNoahIndexerPodManagerWaitsForStatefulSetToObserveTemplate(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	statefulSet := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{
		Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace,
	}, statefulSet))
	statefulSet.Generation++
	require.NoError(t, fixture.client.Update(t.Context(), statefulSet))

	phase, err := fixture.podManager(t).Update(t.Context(), fixture.client, statefulSet, fixture.cr.Spec.Replicas)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, phase)
	assert.Equal(t, int32(1), *statefulSet.Spec.Replicas)
}

func TestNoahIndexerPodManagerPropagatesScaleOutUpdateError(t *testing.T) {
	fixture := newNoahIndexerScaleOutTestFixture(t, noahIndexerScaleOutTestOptions{peerStatus: noahclient.PeerStatusUp})
	fixture.advanceToPendingAction(t)
	wantErr := errors.New("StatefulSet update failed")
	fixture.client.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = wantErr

	phase, err := fixture.update(t)

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

	phase, err := fixture.update(t)
	require.Error(t, err)
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	outcome, outcomeErr, handled := noahIndexerOutcomeFromError(err, "")
	require.True(t, handled)
	err = outcomeErr
	message, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	assert.Contains(t, message, "Cache warming timed out")
	reason, _ := splcommon.TerminalReason(err)
	assert.Equal(t, splcommon.EventReasonNoahCacheWarmTimeout, reason)
	assert.Equal(t, enterpriseApi.PhaseError, outcome.phase)
	assert.Zero(t, outcome.requeueAfter)
	assert.Equal(t, string(enterpriseApi.ReasonNoahCacheWarmTimeout), outcome.condition.Reason)

	noahCluster := &enterpriseApi.NoahCluster{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: "noah", Namespace: fixture.cr.Namespace}, noahCluster))
	cacheWarmDisabled := false
	noahCluster.Spec.CacheWarmScaleOutEnabled = &cacheWarmDisabled
	require.NoError(t, fixture.client.Update(t.Context(), noahCluster))

	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	assert.Equal(t, enterpriseApi.IndexerClusterLifecycleActionPending, fixture.cr.Status.Lifecycle.Checkpoint)
	phase, err = fixture.update(t)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, phase)
	stored := &appsv1.StatefulSet{}
	require.NoError(t, fixture.client.Get(t.Context(), types.NamespacedName{Name: fixture.statefulSet.Name, Namespace: fixture.statefulSet.Namespace}, stored))
	require.NotNil(t, stored.Spec.Replicas)
	assert.Equal(t, int32(2), *stored.Spec.Replicas)
}

func TestWaitForNoahIndexerWorkloadPreservesUpdatingForPendingTemplateRevision(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhaseUpdating, enterpriseApi.PhaseScalingUp, 2, false)

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
			outcome := waitForNoahIndexerWorkload(test.phase, test.previousPhase, 2, false)

			assert.Equal(t, test.phase, outcome.phase)
			assert.NotEmpty(t, outcome.phaseMessage)
		})
	}
}

func TestWaitForNoahIndexerWorkloadPreservesScaleOutForReplicaReadiness(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhasePending, enterpriseApi.PhaseScalingUp, 2, false)

	assert.Equal(t, enterpriseApi.PhaseScalingUp, outcome.phase)
	assert.Equal(t, "Waiting for 2 applied replicas to become ready before continuing scale-out", outcome.phaseMessage)
	assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestWaitForNoahIndexerWorkloadPreservesGracefulScaleDown(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhasePending, enterpriseApi.PhaseScalingDown, 2, false)

	assert.Equal(t, enterpriseApi.PhaseScalingDown, outcome.phase)
	assert.Equal(t, "Gracefully scaling down the indexer workload", outcome.phaseMessage)
	assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestWaitForNoahIndexerWorkloadReportsPendingScaleDownCleanup(t *testing.T) {
	outcome := waitForNoahIndexerWorkload(enterpriseApi.PhaseScalingDown, enterpriseApi.PhaseScalingDown, 2, true)

	assert.Equal(t, enterpriseApi.PhaseScalingDown, outcome.phase)
	assert.Equal(t, "Waiting for graceful Noah scale-in cleanup", outcome.phaseMessage)
	assert.Equal(t, "Waiting for removed-peer cleanup and active bucket-map confirmation", outcome.condition.Message)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestNoahIndexerLifecycleErrorPreservesPhase(t *testing.T) {
	outcome, err, handled := noahIndexerOutcomeFromError(
		newNoahIndexerObservationError(errors.New("Noah unavailable"), enterpriseApi.PhaseScalingDown),
		enterpriseApi.PhaseUpdating,
	)

	require.True(t, handled)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, outcome.phase)
	assert.Equal(t, string(enterpriseApi.ReasonNoahPeerObservationFailed), outcome.condition.Reason)
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestNoahIndexerOutcomeUsesFallbackPhaseWithoutMutatingError(t *testing.T) {
	lifecycleErr := newNoahIndexerObservationError(errors.New("Noah unavailable"), "")
	outcome, mappedErr, handled := noahIndexerOutcomeFromError(lifecycleErr, enterpriseApi.PhaseScalingUp)

	require.True(t, handled)
	require.NoError(t, mappedErr)
	assert.Equal(t, enterpriseApi.PhaseScalingUp, outcome.phase)
	assert.Empty(t, lifecycleErr.phase)
}

func TestNoahIndexerOutcomeClassifiesNoahPeerObservationErrors(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		kind         noahclient.ErrorKind
		retryable    bool
	}{
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, kind: noahclient.ErrorKindUnavailable, retryable: true},
		{name: "unauthorized", statusCode: http.StatusUnauthorized, kind: noahclient.ErrorKindUnauthorized},
		{name: "malformed response", statusCode: http.StatusOK, responseBody: "{", kind: noahclient.ErrorKindInvalidResponse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newNoahResponseClient(t, test.statusCode, test.responseBody)
			_, apiErr := client.ListPeers(t.Context())
			require.Error(t, apiErr)
			outcome, mappedErr, handled := noahIndexerOutcomeFromError(
				newNoahIndexerObservationError(fmt.Errorf("list Noah peers: %w", apiErr), enterpriseApi.PhaseScalingDown),
				enterpriseApi.PhasePending,
			)

			require.True(t, handled)
			assert.Contains(t, outcome.condition.Message, "noah.peers.list")
			assert.Contains(t, outcome.condition.Message, string(test.kind))
			if test.retryable {
				assert.NoError(t, mappedErr)
				assert.Equal(t, enterpriseApi.PhaseScalingDown, outcome.phase)
				assert.Equal(t, metav1.ConditionUnknown, outcome.condition.Status)
				assert.Equal(t, string(enterpriseApi.ReasonNoahPeerObservationFailed), outcome.condition.Reason)
				assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
				return
			}

			_, terminal := splcommon.TerminalMessage(mappedErr)
			assert.True(t, terminal)
			reason, _ := splcommon.TerminalReason(mappedErr)
			assert.Equal(t, splcommon.EventReasonNoahOperationFailed, reason)
			assert.Equal(t, enterpriseApi.PhaseError, outcome.phase)
			assert.Equal(t, metav1.ConditionFalse, outcome.condition.Status)
			assert.Equal(t, string(enterpriseApi.ReasonNoahOperationFailed), outcome.condition.Reason)
			assert.Zero(t, outcome.requeueAfter)
		})
	}
}

func TestNoahIndexerRetryableLifecycleOperationUsesOperationReason(t *testing.T) {
	client := newNoahResponseClient(t, http.StatusServiceUnavailable, "")
	apiErr := client.UnregisterPeer(t.Context(), "peer-0")
	require.Error(t, apiErr)

	outcome, mappedErr, handled := noahIndexerOutcomeFromError(
		newNoahIndexerOperationError(fmt.Errorf("unregister Noah peer peer-0: %w", apiErr), enterpriseApi.PhaseScalingDown),
		enterpriseApi.PhasePending,
	)

	require.True(t, handled)
	require.NoError(t, mappedErr)
	assert.Equal(t, enterpriseApi.PhaseScalingDown, outcome.phase)
	assert.Equal(t, "Unable to complete Noah operation", outcome.phaseMessage)
	assert.Equal(t, metav1.ConditionUnknown, outcome.condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahOperationFailed), outcome.condition.Reason)
	assert.Contains(t, outcome.condition.Message, "noah.peers.unregister")
	assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
}

func TestNoahIndexerCanceledOperationIsNotTerminal(t *testing.T) {
	operationErr := &noahclient.Error{Operation: "noah.peers.list", Kind: noahclient.ErrorKindCanceled, Err: context.Canceled}
	outcome, mappedErr, handled := noahIndexerOutcomeFromError(
		newNoahIndexerObservationError(operationErr, enterpriseApi.PhaseUpdating),
		enterpriseApi.PhasePending,
	)

	require.True(t, handled)
	assert.ErrorIs(t, mappedErr, context.Canceled)
	_, terminal := splcommon.TerminalMessage(mappedErr)
	assert.False(t, terminal)
	assert.Equal(t, enterpriseApi.PhaseUpdating, outcome.phase)
	assert.Equal(t, metav1.ConditionUnknown, outcome.condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahPeerObservationFailed), outcome.condition.Reason)
	assert.Zero(t, outcome.requeueAfter)
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

func TestSetNoahIndexerPhaseAndConditionsPreservesTransitionTimeForRepeatedObservation(t *testing.T) {
	transitionTime := metav1.NewTime(time.Unix(1_700_000_000, 0))
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Generation: 7},
		Status: enterpriseApi.IndexerClusterStatus{Conditions: []metav1.Condition{{
			Type:               string(enterpriseApi.ConditionNoahPeersReady),
			Status:             metav1.ConditionUnknown,
			Reason:             string(enterpriseApi.ReasonNoahPeerObservationFailed),
			Message:            "Unable to observe Noah peers",
			ObservedGeneration: 6,
			LastTransitionTime: transitionTime,
		}}},
	}

	setNoahIndexerPhaseAndConditions(cr, false, enterpriseApi.PhaseScalingDown, "Unable to observe Noah peers", newNoahPeersReadyCondition(
		metav1.ConditionUnknown,
		enterpriseApi.ReasonNoahPeerObservationFailed,
		"Unable to observe Noah peers",
	))

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady)
	require.NotNil(t, condition)
	assert.Equal(t, transitionTime, condition.LastTransitionTime)
	assert.Equal(t, int64(7), condition.ObservedGeneration)
}

func TestNoahIndexerResourcesRecoverAfterDependencyRecreation(t *testing.T) {
	t.Setenv(resources.ClusterDomainEnvName, "corp.example")

	ctx := t.Context()
	client := spltest.NewMockClient()

	// Two IndexerClusters share one NoahCluster, so deletion must block both.
	first := &enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "first",
			Namespace: "test",
			UID:       types.UID("first-uid"),
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       1,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	resources.SetVolumeDefaults(&first.Spec.CommonSplunkSpec)

	second := &enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "second",
			Namespace: "test",
			UID:       types.UID("second-uid"),
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       1,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	resources.SetVolumeDefaults(&second.Spec.CommonSplunkSpec)

	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, first.Namespace)
	require.NoError(t, err)
	namespaceSecret.Data["pass4SymmKey"] = []byte(t.Name())
	require.NoError(t, client.Update(ctx, namespaceSecret))

	noahCluster := &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: first.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "https://noah.test.svc:8080",
			Tenant:        "axolotl",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	}
	authSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: first.Namespace},
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte("unit-test-noah-key")},
	}
	require.NoError(t, client.Create(ctx, noahCluster))
	require.NoError(t, client.Create(ctx, authSecret))

	// Both workloads resolve their dependency and build a StatefulSet.
	firstSet, _, err := applyNoahIndexerResources(ctx, client, first, newNoahIndexerPodManagerForTest(t, client, first))
	require.NoError(t, err)
	require.NotNil(t, firstSet)
	secondSet, _, err := applyNoahIndexerResources(ctx, client, second, newNoahIndexerPodManagerForTest(t, client, second))
	require.NoError(t, err)
	require.NotNil(t, secondSet)

	// Deleting the Secret blocks every referencing workload. The reference is
	// untouched, so NoahEnabled stays true and no Cluster Manager path runs.
	require.NoError(t, client.Delete(ctx, authSecret))
	for _, cr := range []*enterpriseApi.IndexerCluster{first, second} {
		assert.True(t, cr.Spec.NoahEnabled(), "a missing dependency must not clear Noah mode")

		dependency := reconcileutil.ResolveNoahDependency(ctx, client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)
		assert.Nil(t, dependency.Runtime)
		assert.NoError(t, dependency.ReconcileErr, "a missing dependency is retryable, not terminal")
		outcome := noahIndexerDependencyOutcome(dependency)
		assert.Equal(t, enterpriseApi.PhasePending, outcome.phase)
		assert.Equal(t, noahIndexerPollInterval, outcome.requeueAfter)
		assert.Equal(t, string(enterpriseApi.ReasonNoahPeerObservationFailed), outcome.condition.Reason,
			"a dependency failure must not be restated as a peers reason")
	}

	// Deleting the NoahCluster blocks them the same way.
	require.NoError(t, client.Delete(ctx, noahCluster))
	for _, cr := range []*enterpriseApi.IndexerCluster{first, second} {
		dependency := reconcileutil.ResolveNoahDependency(ctx, client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)
		assert.Nil(t, dependency.Runtime)
		outcome := noahIndexerDependencyOutcome(dependency)
		assert.Equal(t, enterpriseApi.PhasePending, outcome.phase)
		assert.Equal(t, string(enterpriseApi.ReasonNoahPeerObservationFailed), outcome.condition.Reason,
			"a dependency failure must not be restated as a peers reason")
	}

	// Recreating both dependencies under the same names recovers every
	// workload. The rotated credential proves resolution reads the new object
	// rather than anything cached from the deleted one.
	recreatedNoahCluster := noahCluster.DeepCopy()
	recreatedNoahCluster.ResourceVersion = ""
	require.NoError(t, client.Create(ctx, recreatedNoahCluster))
	recreatedSecret := authSecret.DeepCopy()
	recreatedSecret.ResourceVersion = ""
	recreatedSecret.Data = map[string][]byte{configworkflow.NoahAuthSecretKey: []byte("rotated-noah-key")}
	require.NoError(t, client.Create(ctx, recreatedSecret))

	for _, cr := range []*enterpriseApi.IndexerCluster{first, second} {
		statefulSet, _, err := applyNoahIndexerResources(ctx, client, cr, newNoahIndexerPodManagerForTest(t, client, cr))
		require.NoError(t, err)
		require.NotNil(t, statefulSet)
	}

	runtime, err := configworkflow.ResolveNoahRuntime(ctx, client, first.Namespace, *first.Spec.NoahClusterRef)
	require.NoError(t, err)
	assert.Equal(t, []byte("rotated-noah-key"), runtime.Credential(),
		"recreation under the same name must revalidate and pick up new data")
}
