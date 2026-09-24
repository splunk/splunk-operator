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
	"net/http"
)

const operationBucketMapsGet = "noah.bucketmaps.get"

// BucketMap is the typed representation of a Noah bucket map.
type BucketMap struct {
	ID        int64           `json:"id"`
	Status    BucketMapStatus `json:"status"`
	PeerIDs   []string        `json:"peerIDs"`
	CreatedAt int64           `json:"createdAt,omitempty"`
	UpdatedAt int64           `json:"updatedAt,omitempty"`
}

// BucketMapStatus is the lifecycle state reported by Noah for a bucket map.
type BucketMapStatus string

const (
	BucketMapStatusUnknown BucketMapStatus = ""
	BucketMapStatusActive  BucketMapStatus = "active"
)

// GetLatestBucketMap returns the latest Noah bucket map.
func (client *Client) GetLatestBucketMap(ctx context.Context) (*BucketMap, error) {
	bucketMap := &BucketMap{}
	requestURL := client.url("bucketMaps/latest")
	if err := client.do(ctx, operationBucketMapsGet, http.MethodGet, requestURL, http.StatusOK, true, bucketMap); err != nil {
		return nil, err
	}
	return bucketMap, nil
}
