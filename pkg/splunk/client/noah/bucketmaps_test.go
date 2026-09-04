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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetLatestBucketMap(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "authenticated", request.Header.Get(testAuthHeader))
		requestPath = request.URL.EscapedPath()
		_ = json.NewEncoder(response).Encode(BucketMap{
			ID:      7,
			Status:  BucketMapStatusActive,
			PeerIDs: []string{"indexer-0.example"},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "tenant one", testAuthenticator, WithHTTPClient(server.Client()))
	assert.NoError(t, err)
	bucketMap, err := client.GetLatestBucketMap(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, &BucketMap{
		ID:      7,
		Status:  BucketMapStatusActive,
		PeerIDs: []string{"indexer-0.example"},
	}, bucketMap)
	assert.Equal(t, "/tenant%20one/noah/v1/bucketMaps/latest", requestPath)
}

func TestLatestBucketMapAllowsEmptyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(BucketMap{ID: 7})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "tenant", testAuthenticator, WithHTTPClient(server.Client()))
	assert.NoError(t, err)
	bucketMap, err := client.GetLatestBucketMap(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, &BucketMap{ID: 7, Status: BucketMapStatusUnknown}, bucketMap)
}
