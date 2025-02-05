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
	"fmt"
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

// MockContainerClient is a mock implementation of ContainerClientInterface.
type MockContainerClient struct {
	mock.Mock
}

// NewListBlobsFlatPager mocks the NewListBlobsFlatPager method.
func (m *MockContainerClient) NewListBlobsFlatPager(options *azblob.ListBlobsFlatOptions) *runtime.Pager[azblob.ListBlobsFlatResponse] {
	args := m.Called(options)
	return args.Get(0).(*runtime.Pager[azblob.ListBlobsFlatResponse])
}

// NewBlobClient mocks the NewBlobClient method.
func (m *MockContainerClient) NewBlobClient(blobName string) BlobClientInterface {
	args := m.Called(blobName)
	return args.Get(0).(BlobClientInterface)
}

// MockBlobClient is a mock implementation of BlobClientInterface.
type MockBlobClient struct {
	mock.Mock
}

// DownloadStream mocks the DownloadStream method.
func (m *MockBlobClient) DownloadStream(ctx context.Context, options *blob.DownloadStreamOptions) (blob.DownloadStreamResponse, error) {
	args := m.Called(ctx, options)
	return args.Get(0).(blob.DownloadStreamResponse), args.Error(1)
}

// TestAzureBlobClient_GetAppsList_SharedKey tests the GetAppsList method using Shared Key authentication.
func TestAzureBlobClient_GetAppsList_SharedKey(t *testing.T) {
	// Initialize mocks.
	mockContainerClient := new(MockContainerClient)

	// Create a runtime pager that returns the mockListResponse.
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
								{
									Name: to.Ptr("blob2"),
									Properties: &container.BlobProperties{
										ETag:          to.Ptr(azcore.ETag("etag2")),
										LastModified:  to.Ptr(time.Now()),
										ContentLength: to.Ptr(int64(200)),
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

	// Setup mock behavior to return the pager.
	mockContainerClient.On("NewListBlobsFlatPager", mock.Anything).Return(runtimePager)

	// Initialize AzureBlobClient with the mock container client.
	azureClient := &AzureBlobClient{
		BucketName:         "test-container",
		StorageAccountName: "test-account",
		Prefix:             "",
		StartAfter:         "",
		Endpoint:           "",
		ContainerClient:    mockContainerClient,
		CredentialType:     CredentialTypeSharedKey,
	}

	// Execute GetAppsList.
	ctx := context.Background()
	resp, err := azureClient.GetAppsList(ctx)

	// Assertions.
	require.NoError(t, err)
	require.Len(t, resp.Objects, 2)
	require.Equal(t, "blob1", *resp.Objects[0].Key)
	require.Equal(t, "blob2", *resp.Objects[1].Key)

	// Verify that all expectations were met.
	mockContainerClient.AssertExpectations(t)
}

// TestAzureBlobClient_GetAppsList_AzureAD tests the GetAppsList method using Azure AD authentication.
func TestAzureBlobClient_GetAppsList_AzureAD(t *testing.T) {
	// Initialize mocks.
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
									Name: to.Ptr("blob3"),
									Properties: &container.BlobProperties{
										ETag:          to.Ptr(azcore.ETag("etag3")),
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

	// Setup mock behavior to return the pager.
	mockContainerClient.On("NewListBlobsFlatPager", mock.Anything).Return(runtimePager)

	// Initialize AzureBlobClient with the mock container client.
	azureClient := &AzureBlobClient{
		BucketName:         "test-container",
		StorageAccountName: "test-account",
		Prefix:             "",
		StartAfter:         "",
		Endpoint:           "",
		ContainerClient:    mockContainerClient,
		CredentialType:     CredentialTypeAzureAD,
	}

	// Execute GetAppsList.
	ctx := context.Background()
	resp, err := azureClient.GetAppsList(ctx)

	// Assertions.
	require.NoError(t, err)
	require.Len(t, resp.Objects, 1)
	require.Equal(t, "blob3", *resp.Objects[0].Key)

	// Verify that all expectations were met.
	mockContainerClient.AssertExpectations(t)
}

// TestAzureBlobClient_GetAppsList_Error tests the GetAppsList method handling an error scenario.
func TestAzureBlobClient_GetAppsList_Error(t *testing.T) {
	// Initialize mocks.
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
			return container.ListBlobsFlatResponse{}, fmt.Errorf("failed to list blobs")
		},
	})

	// Setup mock behavior to return the pager.
	mockContainerClient.On("NewListBlobsFlatPager", mock.Anything).Return(runtimePager)

	// Initialize AzureBlobClient with the mock container client.
	azureClient := &AzureBlobClient{
		BucketName:         "test-container",
		StorageAccountName: "test-account",
		Prefix:             "",
		StartAfter:         "",
		Endpoint:           "",
		ContainerClient:    mockContainerClient,
		CredentialType:     CredentialTypeAzureAD,
	}

	// Execute GetAppsList.
	ctx := context.Background()
	resp, err := azureClient.GetAppsList(ctx)

	// Assertions.
	require.Error(t, err)
	require.Equal(t, RemoteDataListResponse{}, resp)

	// Verify that all expectations were met.
	mockContainerClient.AssertExpectations(t)
}

// TestAzureBlobClient_DownloadApp_SharedKey tests the DownloadApp method using Shared Key authentication.
func TestAzureBlobClient_DownloadApp_SharedKey(t *testing.T) {
	// Initialize mocks.
	mockContainerClient := new(MockContainerClient)
	mockBlobClient := new(MockBlobClient)

	// Define the blob download response.
	mockDownloadResponse := blob.DownloadStreamResponse{
		DownloadResponse: blob.DownloadResponse{
			Body: io.NopCloser(strings.NewReader("mock blob content")),
		},
	}

	// Setup mock behavior.
	mockContainerClient.On("NewBlobClient", "test-file-sharedkey.txt").Return(mockBlobClient)
	mockBlobClient.On("DownloadStream", mock.Anything, mock.Anything).Return(mockDownloadResponse, nil)

	// Initialize AzureBlobClient with the mock container client.
	azureClient := &AzureBlobClient{
		BucketName:         "test-container",
		StorageAccountName: "test-account",
		Prefix:             "",
		StartAfter:         "",
		Endpoint:           "",
		ContainerClient:    mockContainerClient,
		CredentialType:     CredentialTypeSharedKey,
	}

	// Create a temporary file to simulate download.
	tempFile, err := os.CreateTemp("", "test-download-sharedkey")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())

	// Execute DownloadApp.
	ctx := context.Background()
	req := RemoteDataDownloadRequest{
		LocalFile:  tempFile.Name(),
		RemoteFile: "test-file-sharedkey.txt",
	}
	success, err := azureClient.DownloadApp(ctx, req)

	// Assertions.
	require.NoError(t, err)
	require.True(t, success)

	// Verify file content.
	fileContent, err := os.ReadFile(tempFile.Name())
	require.NoError(t, err)
	require.Equal(t, "mock blob content", string(fileContent))

	// Verify that all expectations were met.
	mockContainerClient.AssertExpectations(t)
	mockBlobClient.AssertExpectations(t)
}

// TestAzureBlobClient_DownloadApp_AzureAD tests the DownloadApp method using Azure AD authentication.
func TestAzureBlobClient_DownloadApp_AzureAD(t *testing.T) {
	// Initialize mocks.
	mockContainerClient := new(MockContainerClient)
	mockBlobClient := new(MockBlobClient)

	// Define the blob download response.
	mockDownloadResponse := blob.DownloadStreamResponse{
		DownloadResponse: blob.DownloadResponse{
			Body: io.NopCloser(strings.NewReader("mock blob content AD")),
		},
	}

	// Setup mock behavior.
	mockContainerClient.On("NewBlobClient", "test-file-azuread.txt").Return(mockBlobClient)
	mockBlobClient.On("DownloadStream", mock.Anything, mock.Anything).Return(mockDownloadResponse, nil)

	// Initialize AzureBlobClient with the mock container client.
	azureClient := &AzureBlobClient{
		BucketName:         "test-container",
		StorageAccountName: "test-account",
		Prefix:             "",
		StartAfter:         "",
		Endpoint:           "",
		ContainerClient:    mockContainerClient,
		CredentialType:     CredentialTypeAzureAD,
	}

	// Create a temporary file to simulate download.
	tempFile, err := os.CreateTemp("", "test-download-azuread")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())

	// Execute DownloadApp.
	ctx := context.Background()
	req := RemoteDataDownloadRequest{
		LocalFile:  tempFile.Name(),
		RemoteFile: "test-file-azuread.txt",
	}
	success, err := azureClient.DownloadApp(ctx, req)

	// Assertions.
	require.NoError(t, err)
	require.True(t, success)

	// Verify file content.
	fileContent, err := os.ReadFile(tempFile.Name())
	require.NoError(t, err)
	require.Equal(t, "mock blob content AD", string(fileContent))

	// Verify that all expectations were met.
	mockContainerClient.AssertExpectations(t)
	mockBlobClient.AssertExpectations(t)
}

// TestAzureBlobClient_DownloadApp_Error tests the DownloadApp method handling an error scenario.
func TestAzureBlobClient_DownloadApp_Error(t *testing.T) {
	// Initialize mocks.
	mockContainerClient := new(MockContainerClient)
	mockBlobClient := new(MockBlobClient)

	// Setup mock behavior to return an error.
	mockContainerClient.On("NewBlobClient", "nonexistent-file.txt").Return(mockBlobClient)
	mockBlobClient.On("DownloadStream", mock.Anything, mock.Anything).Return(blob.DownloadStreamResponse{}, fmt.Errorf("blob not found"))

	// Initialize AzureBlobClient with the mock container client.
	azureClient := &AzureBlobClient{
		BucketName:         "test-container",
		StorageAccountName: "test-account",
		Prefix:             "",
		StartAfter:         "",
		Endpoint:           "",
		ContainerClient:    mockContainerClient,
		CredentialType:     CredentialTypeAzureAD,
	}

	// Create a temporary file to simulate download.
	tempFile, err := os.CreateTemp("", "test-download-error")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())

	// Execute DownloadApp.
	ctx := context.Background()
	req := RemoteDataDownloadRequest{
		LocalFile:  tempFile.Name(),
		RemoteFile: "nonexistent-file.txt",
	}
	success, err := azureClient.DownloadApp(ctx, req)

	// Assertions.
	require.Error(t, err)
	require.False(t, success)

	// Verify that all expectations were met.
	mockContainerClient.AssertExpectations(t)
	mockBlobClient.AssertExpectations(t)
}
