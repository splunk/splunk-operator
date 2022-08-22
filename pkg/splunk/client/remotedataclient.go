// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package client

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// RemoteDataClientsMap is a map of remote storage provider name to
// their respective initialization procedures
// Currently supported remote data clients are:
// aws
// minio
// azure
// in future we may have
// googlestorage
var RemoteDataClientsMap = make(map[string]GetRemoteDataClientWrapper)

// RemoteObject struct contains contents returned as part of remote data client response
// for example, it can be a blob in azure or s3 object in aws/minio
type RemoteObject struct {
	Etag         *string
	Key          *string
	LastModified *time.Time
	Size         *int64
	StorageClass *string
}

// RemoteDataListRequest struct contains inputs specifying storage account
//
type RemoteDataListRequest struct {
}

// RemoteDataListResponse struct contains list of RemoteObject objects
type RemoteDataListResponse struct {
	Objects []*RemoteObject
}

// RemoteDataDownloadRequest struct specifies the remote data file path,
// local file where the downloaded data should be written as well as
// the authontication data if available
type RemoteDataDownloadRequest struct {
	LocalFile  string // file name where the remote data will be written
	RemoteFile string // file name with path relative to the bucket
	Etag       string // unique tag of the object
}

// RemoteDataClient is an interface to provide
// listing and downloading of apps from remote data storage
//
type RemoteDataClient interface {

	// Get the list of Apps
	GetAppsList(context.Context) (RemoteDataListResponse, error)

	// Download a given app as per the inputs provided in the `RemoteDataClientRequest`
	DownloadApp(context.Context, RemoteDataDownloadRequest) (bool /* return pass/fail */, error)
}

// GetRemoteDataClientWrapper is a wrapper around init function pointers
type GetRemoteDataClientWrapper struct {
	GetRemoteDataClient
	GetInitFunc
}

// SetRemoteDataClientFuncPtr sets the GetRemoteDataClient function pointer member of GetRemoteDataClientWrapper struct
func (c *GetRemoteDataClientWrapper) SetRemoteDataClientFuncPtr(ctx context.Context, provider string, fn GetRemoteDataClient) {
	c.GetRemoteDataClient = fn
	RemoteDataClientsMap[provider] = *c
}

// GetRemoteDataClientFuncPtr gets the GetRemoteDataClient function pointer member of GetRemoteDataClientWrapper struct
func (c *GetRemoteDataClientWrapper) GetRemoteDataClientFuncPtr(ctx context.Context) GetRemoteDataClient {
	return c.GetRemoteDataClient
}

// SetRemoteDataClientInitFuncPtr sets the GetRemoteDataClient function pointer member of GetRemoteDataClientWrapper struct
func (c *GetRemoteDataClientWrapper) SetRemoteDataClientInitFuncPtr(ctx context.Context, provider string, fn GetInitFunc) {
	c.GetInitFunc = fn
	RemoteDataClientsMap[provider] = *c
}

// GetRemoteDataClientInitFuncPtr gets the GetRemoteDataClient function pointer member of GetRemoteDataClientWrapper struct
func (c *GetRemoteDataClientWrapper) GetRemoteDataClientInitFuncPtr(ctx context.Context) GetInitFunc {
	return c.GetInitFunc
}

// GetInitFunc gets the init function pointer which returns the new RemoteDataClient session client object
type GetInitFunc func(context.Context, string, string, string) interface{}

//GetRemoteDataClient gets the required RemoteDataClient based on the storageType and provider
type GetRemoteDataClient func(context.Context, string /* bucket */, string, /* Access key ID */
	string /* Secret access key */, string /* Prefix */, string /* StartAfter */, string /* Region */, string /* Endpoint */, GetInitFunc) (RemoteDataClient, error)

// SplunkRemoteDataClient is a simple object used to connect to RemoteDataClient
type SplunkRemoteDataClient struct {
	Client RemoteDataClient
}

//RegisterRemoteDataClient registers the respective Client
func RegisterRemoteDataClient(ctx context.Context, provider string) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("RegisterRemoteDataClient")
	switch provider {
	case "aws":
		RegisterAWSS3Client()
	case "minio":
		RegisterMinioClient()
	case "azure":
		RegisterAzureBlobClient()
	default:
		scopedLog.Error(nil, "invalid provider specified", "provider", provider)
	}
}
