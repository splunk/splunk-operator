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

// GetS3ClientWrapper is a wrapper around init function pointers
type GetS3ClientWrapper struct {
	GetS3Client
	GetInitFunc
}

// SetS3ClientFuncPtr sets the GetS3Client function pointer member of GetS3ClientWrapper struct
func (c *GetS3ClientWrapper) SetS3ClientFuncPtr(ctx context.Context, volName string, fn GetS3Client) {
	c.GetS3Client = fn
	S3Clients[volName] = *c
}

// GetS3ClientFuncPtr gets the GetS3Client function pointer member of GetS3ClientWrapper struct
func (c *GetS3ClientWrapper) GetS3ClientFuncPtr(ctx context.Context) GetS3Client {
	return c.GetS3Client
}

// SetS3ClientInitFuncPtr sets the GetS3Client function pointer member of GetS3ClientWrapper struct
func (c *GetS3ClientWrapper) SetS3ClientInitFuncPtr(ctx context.Context, volName string, fn GetInitFunc) {
	c.GetInitFunc = fn
	S3Clients[volName] = *c
}

// GetS3ClientInitFuncPtr gets the GetS3Client function pointer member of GetS3ClientWrapper struct
func (c *GetS3ClientWrapper) GetS3ClientInitFuncPtr(ctx context.Context) GetInitFunc {
	return c.GetInitFunc
}

// GetInitFunc gets the init function pointer which returns the new S3 session client object
type GetInitFunc func(context.Context, string, string, string) interface{}

//GetS3Client gets the required S3Client based on the provider
type GetS3Client func(context.Context, string /* bucket */, string, /* AWS access key ID */
	string /* AWS secret access key */, string /* Prefix */, string /* StartAfter */, string /* Region */, string /* Endpoint */, GetInitFunc) (S3Client, error)

// S3Clients is a map of provider name to init functions
var S3Clients = make(map[string]GetS3ClientWrapper)

// S3Client is an interface to implement different S3 client APIs
type S3Client interface {
	GetAppsList(context.Context) (S3Response, error)
	GetInitContainerImage(context.Context) string
	GetInitContainerCmd(context.Context, string /* endpoint */, string /* bucket */, string /* path */, string /* app src name */, string /* app mnt */) []string
	DownloadApp(context.Context, string, string, string) (bool, error)
}

// SplunkS3Client is a simple object used to connect to S3
type SplunkS3Client struct {
	Client S3Client
}

// S3Response struct contains list of RemoteObject objects as part of S3 response
type S3Response struct {
	Objects []*RemoteObject
}

// RemoteObject struct contains contents returned as part of S3 response
type RemoteObject struct {
	Etag         *string
	Key          *string
	LastModified *time.Time
	Size         *int64
	StorageClass *string
}

//RegisterS3Client registers the respective Client
func RegisterS3Client(ctx context.Context, provider string) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("RegisterS3Client")
	switch provider {
	case "aws":
		RegisterAWSS3Client()
	case "minio":
		RegisterMinioClient()
	default:
		scopedLog.Error(nil, "invalid provider specified", "provider", provider)
	}
}
