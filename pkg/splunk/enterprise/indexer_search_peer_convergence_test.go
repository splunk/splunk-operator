// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type boundedObservationHTTPClient struct {
	started chan<- struct{}
	release <-chan struct{}
	active  *atomic.Int32
	maximum *atomic.Int32
	calls   *atomic.Int32
}

func (client *boundedObservationHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client.calls.Add(1)
	active := client.active.Add(1)
	defer client.active.Add(-1)
	for {
		maximum := client.maximum.Load()
		if active <= maximum || client.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	client.started <- struct{}{}
	select {
	case <-client.release:
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"entry":[{"name":"10.0.1.4:8089","content":{"guid":"peer-guid","status":"Up","disabled":false}}]}`,
			)),
		}, nil
	case <-request.Context().Done():
		return nil, request.Context().Err()
	}
}

func TestSearchDistributedPeerConverged(t *testing.T) {
	current := splclient.SearchDistributedPeerInfo{
		Name:   "10.0.1.4:8089",
		ID:     "peer-guid",
		Status: "Up",
	}
	for _, test := range []struct {
		name  string
		peers []splclient.SearchDistributedPeerInfo
		want  bool
	}{
		{name: "one current entry", peers: []splclient.SearchDistributedPeerInfo{current}, want: true},
		{name: "missing", peers: nil},
		{name: "wrong address", peers: []splclient.SearchDistributedPeerInfo{{Name: "10.0.0.4:8089", ID: current.ID, Status: "Up"}}},
		{name: "down", peers: []splclient.SearchDistributedPeerInfo{{Name: current.Name, ID: current.ID, Status: "Down"}}},
		{name: "disabled", peers: []splclient.SearchDistributedPeerInfo{{Name: current.Name, ID: current.ID, Status: "Up", Disabled: true}}},
		{name: "duplicate stale address", peers: []splclient.SearchDistributedPeerInfo{current, {Name: "10.0.0.4:8089", ID: current.ID, Status: "Down"}}},
		{name: "unrelated peer ignored", peers: []splclient.SearchDistributedPeerInfo{current, {Name: "10.0.2.4:8089", ID: "other-guid", Status: "Up"}}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, searchDistributedPeerConverged(test.peers, current.ID, current.Name))
		})
	}
}

func TestSearchHeadClusterManagerName(t *testing.T) {
	require.Empty(t, searchHeadClusterManagerName(nil))
	require.Equal(t, "current", searchHeadClusterManagerName(&enterpriseApi.SearchHeadCluster{
		Spec: enterpriseApi.SearchHeadClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			ClusterManagerRef: corev1.ObjectReference{Name: "current"},
			ClusterMasterRef:  corev1.ObjectReference{Name: "deprecated"},
		}},
	}))
	require.Equal(t, "deprecated", searchHeadClusterManagerName(&enterpriseApi.SearchHeadCluster{
		Spec: enterpriseApi.SearchHeadClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			ClusterMasterRef: corev1.ObjectReference{Name: "deprecated"},
		}},
	}))
}

func TestObserveSearchHeadPeersBoundsConcurrency(t *testing.T) {
	const targetCount = maxConcurrentSearchPeerObservations + 2
	started := make(chan struct{}, targetCount)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	var calls atomic.Int32
	targets := make([]searchPeerObservationTarget, 0, targetCount)
	for targetIndex := range targetCount {
		httpClient := &boundedObservationHTTPClient{
			started: started,
			release: release,
			active:  &active,
			maximum: &maximum,
			calls:   &calls,
		}
		client := splclient.NewSplunkClient(
			fmt.Sprintf("https://search-%d:8089", targetIndex),
			"admin",
			"secret",
		)
		client.Client = httpClient
		targets = append(targets, searchPeerObservationTarget{
			podName: fmt.Sprintf("search-%d", targetIndex),
			client:  client,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultChannel := make(chan []searchPeerObservationResult, 1)
	go func() {
		resultChannel <- observeSearchHeadPeers(ctx, targets)
	}()
	for range maxConcurrentSearchPeerObservations {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for bounded observation worker")
		}
	}
	select {
	case <-started:
		t.Fatal("more than the bounded number of observations started")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	select {
	case results := <-resultChannel:
		require.Len(t, results, targetCount)
		for _, result := range results {
			require.NoError(t, result.err)
			require.Len(t, result.peers, 1)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bounded observations to finish")
	}
	require.Equal(t, int32(targetCount), calls.Load())
	require.Equal(t, int32(maxConcurrentSearchPeerObservations), maximum.Load())
}

func TestObserveSearchHeadPeersHonorsParentCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	var calls atomic.Int32
	httpClient := &boundedObservationHTTPClient{
		started: started,
		release: release,
		active:  &active,
		maximum: &maximum,
		calls:   &calls,
	}
	client := splclient.NewSplunkClient("https://search-0:8089", "admin", "secret")
	client.Client = httpClient
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultChannel := make(chan []searchPeerObservationResult, 1)
	go func() {
		resultChannel <- observeSearchHeadPeers(ctx, []searchPeerObservationTarget{{
			podName: "search-0",
			client:  client,
		}})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for observation request")
	}
	cancel()

	select {
	case results := <-resultChannel:
		require.Len(t, results, 1)
		require.ErrorIs(t, results[0].err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("observation did not stop after parent cancellation")
	}
}

func TestIndexerSearchPeerConvergenceObserved(t *testing.T) {
	fakeClient := spltest.NewMockClient()
	fakeClient.ListObj = &enterpriseApi.SearchHeadClusterList{Items: []enterpriseApi.SearchHeadCluster{{
		ObjectMeta: metav1.ObjectMeta{Name: "search", Namespace: "test"},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "manager"},
			},
			Replicas: 2,
		},
	}}}
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "indexers", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			ClusterManagerRef: corev1.ObjectReference{Name: "manager"},
		}},
	}
	duplicateStalePeer := false
	distributedPeersStatus := http.StatusOK
	distributedPeersCalls := 0
	mgr := &indexerClusterPodManager{
		c:       fakeClient,
		log:     logging.FromContext(context.Background()),
		cr:      cr,
		secrets: &corev1.Secret{Data: map[string][]byte{"password": []byte("secret")}},
		newSplunkClient: func(managementURI, _, _ string) *splclient.SplunkClient {
			distributedPeersCalls++
			body := `{"entry":[{"name":"10.0.1.4:8089","content":{"guid":"peer-guid","status":"Up","disabled":false}}]}`
			if duplicateStalePeer {
				body = `{"entry":[{"name":"10.0.1.4:8089","content":{"guid":"peer-guid","status":"Up","disabled":false}},{"name":"10.0.0.4:8089","content":{"guid":"peer-guid","status":"Down","disabled":false}}]}`
			}
			mockHTTPClient := &spltest.MockHTTPClient{}
			request, err := http.NewRequest("GET", fmt.Sprintf("%s/services/search/distributed/peers?count=0&output_mode=json", managementURI), nil)
			require.NoError(t, err)
			mockHTTPClient.AddHandler(request, distributedPeersStatus, body, nil)
			client := splclient.NewSplunkClient(managementURI, "admin", "secret")
			client.Client = mockHTTPClient
			return client
		},
	}

	oldGetClusterManagerPeersCall := GetClusterManagerPeersCall
	t.Cleanup(func() { GetClusterManagerPeersCall = oldGetClusterManagerPeersCall })
	var clusterManagerError error
	GetClusterManagerPeersCall = func(context.Context, *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
		if clusterManagerError != nil {
			return nil, clusterManagerError
		}
		return map[string]splclient.ClusterManagerPeerInfo{
			"splunk-indexers-indexer-2": {
				ID:                    "peer-guid",
				RegisterSearchAddress: "10.0.1.4:8089",
			},
		}, nil
	}

	replacement := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "splunk-indexers-indexer-2"}}
	required, converged, message, err := mgr.indexerSearchPeerConvergenceObserved(context.Background(), replacement)
	require.NoError(t, err)
	require.True(t, required)
	require.True(t, converged)
	require.Contains(t, message, "Every Search Head")

	duplicateStalePeer = true
	required, converged, message, err = mgr.indexerSearchPeerConvergenceObserved(context.Background(), replacement)
	require.NoError(t, err)
	require.True(t, required)
	require.False(t, converged)
	require.Contains(t, message, "has not converged")

	duplicateStalePeer = false
	clusterManagerError = errors.New("temporary observation failure")
	required, converged, message, err = mgr.indexerSearchPeerConvergenceObserved(context.Background(), replacement)
	require.NoError(t, err)
	require.True(t, required)
	require.False(t, converged)
	require.Contains(t, message, "waiting for Cluster Manager peers")

	clusterManagerError = nil
	distributedPeersStatus = http.StatusServiceUnavailable
	required, converged, message, err = mgr.indexerSearchPeerConvergenceObserved(context.Background(), replacement)
	require.NoError(t, err)
	require.True(t, required)
	require.False(t, converged)
	require.Contains(t, message, "waiting for distributed peers")

	distributedPeersStatus = http.StatusOK
	fakeClient.ListObj = &enterpriseApi.SearchHeadClusterList{Items: []enterpriseApi.SearchHeadCluster{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "search", Namespace: "test"},
			Spec: enterpriseApi.SearchHeadClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					ClusterManagerRef: corev1.ObjectReference{Name: "manager"},
				},
				Replicas: 2,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "second-search", Namespace: "test"},
			Spec: enterpriseApi.SearchHeadClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					ClusterManagerRef: corev1.ObjectReference{Name: "manager"},
				},
				Replicas: 1,
			},
		},
	}}
	callsBeforeMultipleClusters := distributedPeersCalls
	required, converged, message, err = mgr.indexerSearchPeerConvergenceObserved(context.Background(), replacement)
	require.NoError(t, err)
	require.True(t, required)
	require.True(t, converged)
	require.Contains(t, message, "Every Search Head in 2 cluster(s)")
	require.Equal(t, 3, distributedPeersCalls-callsBeforeMultipleClusters)

	fakeClient.InduceErrorKind[splcommon.MockClientInduceErrorList] = errors.New("temporary Kubernetes list failure")
	required, converged, message, err = mgr.indexerSearchPeerConvergenceObserved(context.Background(), replacement)
	require.ErrorContains(t, err, "list SearchHeadClusters")
	require.True(t, required)
	require.False(t, converged)
	require.Empty(t, message)
	fakeClient.InduceErrorKind[splcommon.MockClientInduceErrorList] = nil

	fakeClient.ListObj = &enterpriseApi.SearchHeadClusterList{Items: []enterpriseApi.SearchHeadCluster{{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "test"},
		Spec: enterpriseApi.SearchHeadClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			ClusterManagerRef: corev1.ObjectReference{Name: "other-manager"},
		}},
	}}}
	required, converged, message, err = mgr.indexerSearchPeerConvergenceObserved(context.Background(), replacement)
	require.NoError(t, err)
	require.False(t, required)
	require.True(t, converged)
	require.Contains(t, message, "No SearchHeadCluster")
}
