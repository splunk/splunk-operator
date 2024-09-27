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
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockContainerClient is a mock for the Azure ContainerClient
type MockContainerClient struct {
	mock.Mock
}

func (m *MockContainerClient) NewListBlobsFlatPager(options *azblob.ListBlobsFlatOptions) *runtime.Pager[azblob.ListBlobsFlatResponse] {
	args := m.Called(options)
	return args.Get(0).(*runtime.Pager[azblob.ListBlobsFlatResponse])
}

func (m *MockContainerClient) NewBlobClient(blobName string) BlobClientInterface {
	args := m.Called(blobName)
	return args.Get(0).(BlobClientInterface)
}

// MockBlobClient is a mock for the Azure BlobClient
type MockBlobClient struct {
	mock.Mock
}

func (m *MockBlobClient) DownloadStream(ctx context.Context, options *azblob.DownloadStreamOptions) (azblob.DownloadStreamResponse, error) {
	args := m.Called(ctx, options)
	return args.Get(0).(azblob.DownloadStreamResponse), args.Error(1)
}

// TestAzureBlobClient_GetAppsList tests the GetAppsList method using runtime.Pager
func TestAzureBlobClient_GetAppsList(t *testing.T) {
	mockContainerClient := new(MockContainerClient)

	// Create a runtime pager for simulating paginated blob listing
	runtimePager := runtime.NewPager(runtime.PagingHandler[azblob.ListBlobsFlatResponse]{
		More: func(resp azblob.ListBlobsFlatResponse) bool {
			// If resp is zero value (before first fetch), we have more pages
			if resp.Segment == nil && resp.NextMarker == nil {
				return true
			}
			// If NextMarker is not empty, we have more pages
			if resp.NextMarker != nil && *resp.NextMarker != "" {
				return true
			}
			// No more pages
			return false
		},
		Fetcher: func(ctx context.Context, cur *azblob.ListBlobsFlatResponse) (azblob.ListBlobsFlatResponse, error) {
			if cur == nil {
				// Simulate the first page of blobs
				return azblob.ListBlobsFlatResponse{
					ListBlobsFlatSegmentResponse: container.ListBlobsFlatSegmentResponse{
						ContainerName:   to.Ptr("test-container"),
						ServiceEndpoint: to.Ptr("https://test.blob.core.windows.net/"),
						MaxResults:      to.Ptr(int32(1)),
						Segment: &container.BlobFlatListSegment{
							BlobItems: []*container.BlobItem{
								{
									Name: to.Ptr("blob1"),
									Properties: &container.BlobProperties{
										ETag:          to.Ptr(azcore.ETag("etag1")),
										LastModified:  to.Ptr(time.Now()),
										ContentLength: to.Ptr(int64(100)),
									},
								},
							},
						},
						NextMarker: nil,
					},
				}, nil
			}
			// Simulate no more pages
			return azblob.ListBlobsFlatResponse{}, nil
		},
	})

	// Setup mock behavior to return the pager
	mockContainerClient.On("NewListBlobsFlatPager", mock.Anything).Return(runtimePager)

	client := &AzureBlobClient{
		BucketName:      "test-bucket",
		ContainerClient: mockContainerClient, // Use the mock client
	}

	ctx := context.Background()
	resp, err := client.GetAppsList(ctx)

	// Assertions
	require.NoError(t, err)
	require.Len(t, resp.Objects, 1)
	require.Equal(t, "blob1", *resp.Objects[0].Key)

	// Verify expectations
	mockContainerClient.AssertExpectations(t)
}

// TestAzureBlobClient_DownloadApp tests the DownloadApp method using Azure Blob SDK
func TestAzureBlobClient_DownloadApp(t *testing.T) {
	mockContainerClient := new(MockContainerClient)
	mockBlobClient := new(MockBlobClient)

	// Create a mock reader to simulate blob content
	mockBlobContent := "mock blob content"
	mockBlobDownloadResponse := azblob.DownloadStreamResponse{
		DownloadResponse: blob.DownloadResponse{
			Body: io.NopCloser(strings.NewReader(mockBlobContent)),
		},
	}

	// Setup mock behavior
	mockContainerClient.On("NewBlobClient", "test-file.txt").Return(mockBlobClient)
	mockBlobClient.On("DownloadStream", mock.Anything, mock.Anything).Return(mockBlobDownloadResponse, nil)

	client := &AzureBlobClient{
		BucketName:      "test-bucket",
		ContainerClient: mockContainerClient, // Use the mock client
	}

	// Create a temporary file to simulate download
	tempFile, err := os.CreateTemp("", "test-download")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())

	ctx := context.Background()
	req := RemoteDataDownloadRequest{
		LocalFile:  tempFile.Name(),
		RemoteFile: "test-file.txt",
	}

	success, err := client.DownloadApp(ctx, req)

	// Assertions
	require.NoError(t, err)
	require.True(t, success)

	// Verify file content
	fileContent, err := os.ReadFile(tempFile.Name())
	require.NoError(t, err)
	require.Equal(t, mockBlobContent, string(fileContent))

	// Verify expectations
	mockContainerClient.AssertExpectations(t)
	mockBlobClient.AssertExpectations(t)
}
