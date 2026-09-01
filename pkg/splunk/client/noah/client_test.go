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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAuthHeader = "X-Test-Noah-Auth"

var testAuthenticator = AuthenticatorFunc(func(request *http.Request, _ []byte) error {
	request.Header.Set(testAuthHeader, "authenticated")
	return nil
})

func TestNewClientValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		endpoint      string
		tenant        string
		authenticator Authenticator
		options       []Option
	}{
		{name: "missing scheme", endpoint: "noah.test:8443", tenant: "tenant", authenticator: testAuthenticator},
		{name: "unsupported scheme", endpoint: "file:///noah", tenant: "tenant", authenticator: testAuthenticator},
		{name: "missing host", endpoint: "https://", tenant: "tenant", authenticator: testAuthenticator},
		{name: "credentials", endpoint: "https://user:nope@noah.test", tenant: "tenant", authenticator: testAuthenticator},
		{name: "path", endpoint: "https://noah.test/api", tenant: "tenant", authenticator: testAuthenticator},
		{name: "empty query marker", endpoint: "https://noah.test?", tenant: "tenant", authenticator: testAuthenticator},
		{name: "empty tenant", endpoint: "https://noah.test", authenticator: testAuthenticator},
		{name: "tenant whitespace", endpoint: "https://noah.test", tenant: " tenant ", authenticator: testAuthenticator},
		{name: "nil authenticator", endpoint: "https://noah.test", tenant: "tenant"},
		{name: "nil option", endpoint: "https://noah.test", tenant: "tenant", authenticator: testAuthenticator, options: []Option{nil}},
		{name: "nil HTTP client", endpoint: "https://noah.test", tenant: "tenant", authenticator: testAuthenticator, options: []Option{WithHTTPClient(nil)}},
		{name: "invalid timeout", endpoint: "https://noah.test", tenant: "tenant", authenticator: testAuthenticator, options: []Option{WithRequestTimeout(0)}},
		{name: "invalid response limit", endpoint: "https://noah.test", tenant: "tenant", authenticator: testAuthenticator, options: []Option{WithMaxResponseBytes(0)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewClient(test.endpoint, test.tenant, test.authenticator, test.options...); err == nil {
				t.Fatal("NewClient() error = nil, want configuration error")
			}
		})
	}
}

func TestClientMembershipEndpoints(t *testing.T) {
	requests := make([]struct {
		method string
		path   string
	}, 0, 4)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get(testAuthHeader) != "authenticated" {
			t.Errorf("authentication header = %q, want authenticated", request.Header.Get(testAuthHeader))
		}
		requests = append(requests, struct {
			method string
			path   string
		}{method: request.Method, path: request.URL.EscapedPath()})

		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/peers"):
			_ = json.NewEncoder(response).Encode([]Peer{{
				ID:            "indexer-0.example",
				Status:        PeerStatusUp,
				LastHeartbeat: 1_700_000_010,
				Data: PeerData{
					ID:        "indexer-0",
					StartTime: 1_700_000_000,
				},
			}})
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/peers/"):
			_ = json.NewEncoder(response).Encode(Peer{
				ID:     "indexer/0",
				Status: PeerStatusWarmed,
			})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/bucketMaps/latest"):
			_ = json.NewEncoder(response).Encode(BucketMap{
				ID:      7,
				Status:  BucketMapStatusActive,
				PeerIDs: []string{"indexer-0.example"},
			})
		case request.Method == http.MethodDelete:
			response.WriteHeader(http.StatusAccepted)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "tenant one", testAuthenticator, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	peers, err := client.ListPeers(context.Background())
	if err != nil {
		t.Fatalf("ListPeers() error = %v", err)
	}
	if len(peers) != 1 || peers[0].ID != "indexer-0.example" || peers[0].Status != PeerStatusUp {
		t.Fatalf("ListPeers() = %+v, want one up peer", peers)
	}

	peer, err := client.GetPeer(context.Background(), "indexer/0")
	if err != nil {
		t.Fatalf("GetPeer() error = %v", err)
	}
	if peer.ID != "indexer/0" || peer.Status != PeerStatusWarmed {
		t.Fatalf("GetPeer() = %+v, want warmed indexer/0", peer)
	}

	bucketMap, err := client.GetLatestBucketMap(context.Background())
	if err != nil {
		t.Fatalf("GetLatestBucketMap() error = %v", err)
	}
	if bucketMap.ID != 7 || bucketMap.Status != "active" || len(bucketMap.PeerIDs) != 1 {
		t.Fatalf("GetLatestBucketMap() = %+v, want active map 7", bucketMap)
	}

	acknowledgement, err := client.DecommissionPeer(context.Background(), "indexer/0")
	if err != nil {
		t.Fatalf("DecommissionPeer() error = %v", err)
	}
	if acknowledgement.PeerID != "indexer/0" {
		t.Fatalf("DecommissionPeer() = %+v, want indexer/0 acknowledgement", acknowledgement)
	}

	wantRequests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/tenant%20one/noah/v1/peers"},
		{method: http.MethodGet, path: "/tenant%20one/noah/v1/peers/indexer%2F0"},
		{method: http.MethodGet, path: "/tenant%20one/noah/v1/bucketMaps/latest"},
		{method: http.MethodDelete, path: "/tenant%20one/noah/v1/peers/indexer%2F0"},
	}
	if len(requests) != len(wantRequests) {
		t.Fatalf("request count = %d, want %d: %+v", len(requests), len(wantRequests), requests)
	}
	for index := range wantRequests {
		if requests[index] != wantRequests[index] {
			t.Errorf("request %d = %+v, want %+v", index, requests[index], wantRequests[index])
		}
	}
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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	peers, err := client.ListPeers(context.Background())
	if err != nil {
		t.Fatalf("ListPeers() error = %v", err)
	}
	if len(peers) != 2 || peers[0].Status != PeerStatusUnknown || peers[1].Status != "future-state" {
		t.Fatalf("ListPeers() = %+v, want empty and unrecognized statuses preserved", peers)
	}
}

func TestLatestBucketMapAllowsEmptyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(BucketMap{ID: 7})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "tenant", testAuthenticator, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	bucketMap, err := client.GetLatestBucketMap(context.Background())
	if err != nil {
		t.Fatalf("GetLatestBucketMap() error = %v", err)
	}
	if bucketMap.ID != 7 || bucketMap.Status != BucketMapStatusUnknown {
		t.Fatalf("GetLatestBucketMap() = %+v, want map 7 with empty status", bucketMap)
	}
}

func TestDecommissionAcceptanceIsNotCompletion(t *testing.T) {
	for _, statusCode := range []int{http.StatusOK, http.StatusNoContent} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(statusCode)
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "tenant", testAuthenticator, WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			acknowledgement, err := client.DecommissionPeer(context.Background(), "indexer-0")
			if acknowledgement != nil {
				t.Fatalf("DecommissionPeer() acknowledgement = %+v, want nil", acknowledgement)
			}
			assertErrorKind(t, err, ErrorKindUnexpectedStatus)
		})
	}
}

func TestClientClassifiesAndRedactsHTTPFailures(t *testing.T) {
	tests := []struct {
		statusCode int
		kind       ErrorKind
		retryable  bool
	}{
		{statusCode: http.StatusBadRequest, kind: ErrorKindInvalidRequest},
		{statusCode: http.StatusUnauthorized, kind: ErrorKindUnauthorized},
		{statusCode: http.StatusForbidden, kind: ErrorKindForbidden},
		{statusCode: http.StatusNotFound, kind: ErrorKindNotFound},
		{statusCode: http.StatusConflict, kind: ErrorKindConflict},
		{statusCode: http.StatusTooManyRequests, kind: ErrorKindRateLimited, retryable: true},
		{statusCode: http.StatusServiceUnavailable, kind: ErrorKindUnavailable, retryable: true},
	}

	for _, test := range tests {
		t.Run(http.StatusText(test.statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.statusCode)
				_, _ = response.Write([]byte("pass4SymmKey=must-not-leak"))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "tenant", testAuthenticator, WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, err = client.ListPeers(context.Background())
			apiError := assertErrorKind(t, err, test.kind)
			if apiError.Retryable() != test.retryable {
				t.Errorf("Retryable() = %v, want %v", apiError.Retryable(), test.retryable)
			}
			if strings.Contains(err.Error(), "pass4SymmKey") {
				t.Fatalf("error leaks response body: %v", err)
			}
		})
	}
}

type httpClientFunc func(*http.Request) (*http.Response, error)

func (fn httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestClientAppliesRequestTimeout(t *testing.T) {
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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ListPeers(context.Background())
	apiError := assertErrorKind(t, err, ErrorKindTimeout)
	if !apiError.Retryable() {
		t.Fatal("timeout must be retryable")
	}
}

func TestDecommissionTimeoutIsNotRetryableWithoutIdempotency(t *testing.T) {
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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.DecommissionPeer(context.Background(), "indexer-0")
	apiError := assertErrorKind(t, err, ErrorKindTimeout)
	if apiError.Retryable() {
		t.Fatal("decommission timeout must not be retryable without an idempotency contract")
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", 17)))
	}))
	defer server.Close()

	client, err := NewClient(
		server.URL,
		"tenant",
		testAuthenticator,
		WithHTTPClient(server.Client()),
		WithMaxResponseBytes(16),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.ListPeers(context.Background())
	assertErrorKind(t, err, ErrorKindInvalidResponse)
}

func TestClientClassifiesCallerCancellation(t *testing.T) {
	httpClient := httpClientFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})
	client, err := NewClient(
		"https://noah.test",
		"tenant",
		testAuthenticator,
		WithHTTPClient(httpClient),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.ListPeers(ctx)
	assertErrorKind(t, err, ErrorKindCanceled)
}

func assertErrorKind(t *testing.T, err error, want ErrorKind) *Error {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want kind %q", want)
	}
	var apiError *Error
	if !errors.As(err, &apiError) {
		t.Fatalf("error type = %T, want *noah.Error: %v", err, err)
	}
	if apiError.Kind != want {
		t.Fatalf("error kind = %q, want %q: %v", apiError.Kind, want, err)
	}
	return apiError
}
