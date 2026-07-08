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

package common

import (
	"context"
	"time"
)

// RemoteObject contains contents returned as part of a remote storage listing response.
type RemoteObject struct {
	Etag         *string
	Key          *string
	LastModified *time.Time
	Size         *int64
	StorageClass *string
}

// RemoteDataListRequest contains inputs specifying a storage listing request.
type RemoteDataListRequest struct{}

// RemoteDataListResponse contains the list of RemoteObject entries from a listing.
type RemoteDataListResponse struct {
	Objects []*RemoteObject
}

// RemoteDataDownloadRequest specifies the remote file path, local destination path,
// and optional etag for a download operation.
type RemoteDataDownloadRequest struct {
	LocalFile  string
	RemoteFile string
	Etag       string
}

// RemoteDataClient provides listing and downloading of app packages from remote storage.
type RemoteDataClient interface {
	GetAppsList(context.Context) (RemoteDataListResponse, error)
	DownloadApp(context.Context, RemoteDataDownloadRequest) (bool, error)
}

// GetInitFunc returns a provider-specific session client object given endpoint, access key, and secret key.
type GetInitFunc func(context.Context, string, string, string) interface{}
