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
	"encoding/json"
	"fmt"

	"github.com/splunk/splunk-operator/pkg/logging"
	storageaws "github.com/splunk/splunk-operator/pkg/splunk/client/storage/aws"
	storageazure "github.com/splunk/splunk-operator/pkg/splunk/client/storage/azure"
	storageminio "github.com/splunk/splunk-operator/pkg/splunk/client/storage/minio"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

// NewMockAWSS3Client returns an AWS S3 mock client for testing.
func NewMockAWSS3Client(ctx context.Context, bucketName, accessKeyID, secretAccessKey, prefix, startAfter, region, endpoint string, fn splcommon.GetInitFunc) (splcommon.RemoteDataClient, error) {
	region = fmt.Sprintf("%s%s%s", region, storageaws.RegionEndpointDelimiter, endpoint)
	cl := fn(ctx, region, accessKeyID, secretAccessKey)
	if cl == nil {
		return nil, fmt.Errorf("failed to create an AWS S3 client")
	}

	return &storageaws.S3Client{
		Region:             region,
		BucketName:         bucketName,
		AWSAccessKeyID:     accessKeyID,
		AWSSecretAccessKey: secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Client:             cl.(storageaws.SplunkS3Client),
		Downloader:         spltest.MockAWSDownloadClient{},
	}, nil
}

// NewMockMinioS3Client returns a Minio mock client for testing.
func NewMockMinioS3Client(ctx context.Context, bucketName, accessKeyID, secretAccessKey, prefix, startAfter, region, endpoint string, fn splcommon.GetInitFunc) (splcommon.RemoteDataClient, error) {
	cl := fn(ctx, endpoint, accessKeyID, secretAccessKey)
	if cl == nil {
		return nil, fmt.Errorf("failed to create a Minio S3 client")
	}

	return &storageminio.MinioClient{
		BucketName:        bucketName,
		S3AccessKeyID:     accessKeyID,
		S3SecretAccessKey: secretAccessKey,
		Prefix:            prefix,
		StartAfter:        startAfter,
		Endpoint:          endpoint,
		Client:            cl.(storageminio.SplunkMinioClient),
	}, nil
}

// NewMockAzureBlobClient returns an Azure Blob mock client for testing.
func NewMockAzureBlobClient(ctx context.Context, bucketName, storageAccountName, secretAccessKey, prefix, startAfter, region, endpoint string, fn splcommon.GetInitFunc) (splcommon.RemoteDataClient, error) {
	cl := fn(ctx, endpoint, storageAccountName, secretAccessKey)
	if cl == nil {
		return nil, fmt.Errorf("failed to create an Azure blob client")
	}

	return &storageazure.BlobClient{
		BucketName:         bucketName,
		StorageAccountName: storageAccountName,
		Prefix:             prefix,
		Endpoint:           endpoint,
	}, nil
}

// ConvertRemoteDataListResponse converts a RemoteDataListResponse to a MockRemoteDataClient.
func ConvertRemoteDataListResponse(ctx context.Context, resp splcommon.RemoteDataListResponse) (spltest.MockRemoteDataClient, error) {
	scopedLog := logging.FromContext(ctx).With("func", "ConvertRemoteDataListResponse")

	var mockResponse spltest.MockRemoteDataClient
	tmp, err := json.Marshal(resp)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to marshal response", "error", err)
		return mockResponse, err
	}
	if err = json.Unmarshal(tmp, &mockResponse); err != nil {
		scopedLog.ErrorContext(ctx, "unable to unmarshal response", "error", err)
		return mockResponse, err
	}
	return mockResponse, nil
}
