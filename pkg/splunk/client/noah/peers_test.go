// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package noah

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPeerEndpoints(t *testing.T) {
	type recordedRequest struct {
		method string
		path   string
	}
	var requests []recordedRequest

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "authenticated", request.Header.Get(testAuthHeader))
		requests = append(requests, recordedRequest{method: request.Method, path: request.URL.EscapedPath()})

		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/peers"):
			_ = json.NewEncoder(response).Encode([]Peer{{
				ID:            "indexer-0.example",
				Status:        PeerStatusUp,
				LastHeartbeat: 1_700_000_010,
				Data:          PeerData{ID: "indexer-0", StartTime: 1_700_000_000},
			}})
		case request.Method == http.MethodGet:
			_ = json.NewEncoder(response).Encode(Peer{ID: "indexer/0", Status: PeerStatusWarmed})
		case request.Method == http.MethodDelete:
			response.WriteHeader(http.StatusAccepted)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "tenant one", testAuthenticator, WithHTTPClient(server.Client()))
	assert.NoError(t, err)

	peers, err := client.ListPeers(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, []Peer{{
		ID:            "indexer-0.example",
		Status:        PeerStatusUp,
		LastHeartbeat: 1_700_000_010,
		Data:          PeerData{ID: "indexer-0", StartTime: 1_700_000_000},
	}}, peers)

	peer, err := client.GetPeer(context.Background(), "indexer/0")
	assert.NoError(t, err)
	assert.Equal(t, &Peer{ID: "indexer/0", Status: PeerStatusWarmed}, peer)

	err = client.UnregisterPeer(context.Background(), "indexer/0")
	assert.NoError(t, err)
	assert.Equal(t, []recordedRequest{
		{method: http.MethodGet, path: "/tenant%20one/noah/v1/peers"},
		{method: http.MethodGet, path: "/tenant%20one/noah/v1/peers/indexer%2F0"},
		{method: http.MethodDelete, path: "/tenant%20one/noah/v1/peers/indexer%2F0"},
	}, requests)
}

func TestListPeersPreservesUnknownStates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode([]Peer{
			{ID: "indexer-0", Status: PeerStatusUnknown},
			{ID: "indexer-1", Status: "future-state"},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "tenant", testAuthenticator, WithHTTPClient(server.Client()))
	assert.NoError(t, err)
	peers, err := client.ListPeers(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, []Peer{
		{ID: "indexer-0", Status: PeerStatusUnknown},
		{ID: "indexer-1", Status: "future-state"},
	}, peers)
}

func TestUnregisterRequiresAcceptedResponse(t *testing.T) {
	for _, statusCode := range []int{http.StatusOK, http.StatusNoContent} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(statusCode)
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "tenant", testAuthenticator, WithHTTPClient(server.Client()))
			assert.NoError(t, err)
			assertErrorKind(t, client.UnregisterPeer(context.Background(), "indexer-0"), ErrorKindUnexpectedStatus)
		})
	}
}

func TestUnregisterTimeoutIsRetryable(t *testing.T) {
	httpClient := httpClientFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client, err := NewClient(
		"https://noah.test",
		"tenant",
		testAuthenticator,
		WithHTTPClient(httpClient),
		WithRequestTimeout(time.Millisecond),
	)
	assert.NoError(t, err)

	apiError := assertErrorKind(t, client.UnregisterPeer(context.Background(), "indexer-0"), ErrorKindTimeout)
	assert.True(t, apiError.Retryable())
}
