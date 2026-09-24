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

package storage

import (
	"context"

	"github.com/splunk/splunk-operator/pkg/logging"
	storageaws "github.com/splunk/splunk-operator/pkg/splunk/client/storage/aws"
	storageazure "github.com/splunk/splunk-operator/pkg/splunk/client/storage/azure"
	storagegcp "github.com/splunk/splunk-operator/pkg/splunk/client/storage/gcp"
	storageminio "github.com/splunk/splunk-operator/pkg/splunk/client/storage/minio"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// RemoteDataClientsMap is a map of remote storage provider name to
// their respective initialization procedures.
// Currently supported: aws, minio, azure, gcp.
var RemoteDataClientsMap = make(map[string]GetRemoteDataClientWrapper)

// GetRemoteDataClientWrapper is a wrapper around init function pointers
type GetRemoteDataClientWrapper struct {
	GetRemoteDataClient
	splcommon.GetInitFunc
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

// SetRemoteDataClientInitFuncPtr sets the GetInitFunc function pointer member of GetRemoteDataClientWrapper struct
func (c *GetRemoteDataClientWrapper) SetRemoteDataClientInitFuncPtr(ctx context.Context, provider string, fn splcommon.GetInitFunc) {
	c.GetInitFunc = fn
	RemoteDataClientsMap[provider] = *c
}

// GetRemoteDataClientInitFuncPtr gets the GetInitFunc function pointer member of GetRemoteDataClientWrapper struct
func (c *GetRemoteDataClientWrapper) GetRemoteDataClientInitFuncPtr(ctx context.Context) splcommon.GetInitFunc {
	return c.GetInitFunc
}

// GetRemoteDataClient gets the required RemoteDataClient based on the storageType and provider
type GetRemoteDataClient func(context.Context, string /* bucket */, string, /* Access key ID */
	string /* Secret access key */, string /* Prefix */, string /* StartAfter */, string /* Region */, string /* Endpoint */, splcommon.GetInitFunc) (splcommon.RemoteDataClient, error)

// SplunkRemoteDataClient is a simple object used to connect to RemoteDataClient
type SplunkRemoteDataClient struct {
	Client splcommon.RemoteDataClient
}

// RegisterRemoteDataClient registers the respective Client
func RegisterRemoteDataClient(ctx context.Context, provider string) {
	scopedLog := logging.FromContext(ctx).With("func", "RegisterRemoteDataClient")
	switch provider {
	case "aws":
		RemoteDataClientsMap["aws"] = GetRemoteDataClientWrapper{
			GetRemoteDataClient: storageaws.NewS3Client,
			GetInitFunc:         storageaws.InitClientWrapper,
		}
	case "minio":
		RemoteDataClientsMap["minio"] = GetRemoteDataClientWrapper{
			GetRemoteDataClient: storageminio.NewMinioClient,
			GetInitFunc:         storageminio.InitClientWrapper,
		}
	case "azure":
		RemoteDataClientsMap["azure"] = GetRemoteDataClientWrapper{
			GetRemoteDataClient: storageazure.NewBlobClient,
			GetInitFunc:         storageazure.NoOpInitFunc,
		}
	case "gcp":
		RemoteDataClientsMap["gcp"] = GetRemoteDataClientWrapper{
			GetRemoteDataClient: storagegcp.NewGCSClient,
			GetInitFunc:         storagegcp.InitGcloudClientWrapper,
		}
	default:
		scopedLog.ErrorContext(ctx, "invalid provider specified", "provider", provider)
	}
}
