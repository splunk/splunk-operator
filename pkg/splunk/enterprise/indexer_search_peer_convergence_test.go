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
	"net/http"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	mgr := &indexerClusterPodManager{
		c:       fakeClient,
		log:     logging.FromContext(context.Background()),
		cr:      cr,
		secrets: &corev1.Secret{Data: map[string][]byte{"password": []byte("secret")}},
		newSplunkClient: func(managementURI, _, _ string) *splclient.SplunkClient {
			body := `{"entry":[{"name":"10.0.1.4:8089","content":{"guid":"peer-guid","status":"Up","disabled":false}}]}`
			if duplicateStalePeer {
				body = `{"entry":[{"name":"10.0.1.4:8089","content":{"guid":"peer-guid","status":"Up","disabled":false}},{"name":"10.0.0.4:8089","content":{"guid":"peer-guid","status":"Down","disabled":false}}]}`
			}
			mockHTTPClient := &spltest.MockHTTPClient{}
			request, err := http.NewRequest("GET", fmt.Sprintf("%s/services/search/distributed/peers?count=0&output_mode=json", managementURI), nil)
			require.NoError(t, err)
			mockHTTPClient.AddHandler(request, http.StatusOK, body, nil)
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
