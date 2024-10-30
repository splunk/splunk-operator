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

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ RemoteDataClient = &AzureBlobClient{}

// ContainerClientInterface abstracts the methods used from the Azure SDK's ContainerClient.
type ContainerClientInterface interface {
	NewListBlobsFlatPager(options *container.ListBlobsFlatOptions) *runtime.Pager[azblob.ListBlobsFlatResponse]
	NewBlobClient(blobName string) BlobClientInterface
}

// BlobClientInterface abstracts the methods used from the Azure SDK's BlobClient.
type BlobClientInterface interface {
	DownloadStream(ctx context.Context, options *blob.DownloadStreamOptions) (blob.DownloadStreamResponse, error)
}

func (c *ContainerClientWrapper) NewListBlobsFlatPager(options *azblob.ListBlobsFlatOptions) *runtime.Pager[azblob.ListBlobsFlatResponse] {
	return c.Client.NewListBlobsFlatPager(options)
}

// ContainerClientWrapper wraps the Azure SDK's ContainerClient and implements ContainerClientInterface.
type ContainerClientWrapper struct {
	*container.Client
}

// NewBlobClient wraps the Azure SDK's NewBlobClient method to return BlobClientInterface.
func (w *ContainerClientWrapper) NewBlobClient(blobName string) BlobClientInterface {
	return &BlobClientWrapper{w.Client.NewBlobClient(blobName)}
}

// BlobClientWrapper wraps the Azure SDK's BlobClient and implements BlobClientInterface.
type BlobClientWrapper struct {
	*blob.Client
}

// DownloadStream wraps the Azure SDK's DownloadStream method.
func (w *BlobClientWrapper) DownloadStream(ctx context.Context, options *blob.DownloadStreamOptions) (blob.DownloadStreamResponse, error) {
	return w.Client.DownloadStream(ctx, options)
}

// CredentialType defines the type of credential used for authentication.
type CredentialType int

const (
	// CredentialTypeSharedKey indicates Shared Key authentication.
	CredentialTypeSharedKey CredentialType = iota
	// CredentialTypeAzureAD indicates Azure AD authentication.
	CredentialTypeAzureAD
)

// AzureBlobClient implements the RemoteDataClient interface for Azure Blob Storage.
type AzureBlobClient struct {
	BucketName         string
	StorageAccountName string
	Prefix             string
	StartAfter         string
	Endpoint           string
	ContainerClient    ContainerClientInterface
	CredentialType     CredentialType
}

// NewAzureBlobClient initializes and returns an AzureBlobClient.
// It supports both Shared Key and Azure AD authentication based on provided credentials.
// NewAzureBlobClient initializes a new AzureBlobClient with the provided parameters.
// It supports both Shared Key and Azure AD authentication methods.
//
// Parameters:
//   - ctx: The context for the operation.
//   - bucketName: The name of the Azure Blob container.
//   - storageAccountName: The name of the Azure Storage account.
//   - secretAccessKey: The shared key for authentication (optional; leave empty to use Azure AD).
//   - prefix: The prefix for blob listing (optional).
//   - startAfter: The marker for blob listing (optional).
//   - region: The Azure region (e.g., "eastus").
//   - endpoint: A custom endpoint (optional).
//   - initFunc: An initialization function to be executed (optional).
//
// Returns:
//   - RemoteDataClient: An interface representing the remote data client.
//   - error: An error object if the initialization fails.
//
// The function logs the initialization process and selects the appropriate
// authentication method based on the presence of the secretAccessKey. If the
// secretAccessKey is provided, Shared Key authentication is used; otherwise,
// Azure AD authentication is used.
func NewAzureBlobClient(
	ctx context.Context,
	bucketName string, // Azure Blob container name
	storageAccountName string, // Azure Storage account name
	secretAccessKey string, // Shared Key (optional; leave empty to use Azure AD)
	prefix string, // Prefix for blob listing (optional)
	startAfter string, // Marker for blob listing (optional)
	region string, // Azure region (e.g., "eastus")
	endpoint string, // Custom endpoint (optional)
	initFunc GetInitFunc, // Initialization function
) (RemoteDataClient, error) { // Matches GetRemoteDataClient signature
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("NewAzureBlobClient")

	scopedLog.Info("Initializing AzureBlobClient")

	// Execute the initialization function if provided.
	if initFunc != nil {
		initResult := initFunc(ctx, endpoint, storageAccountName, secretAccessKey)
		// Currently, no action is taken with initResult. Modify if needed.
		_ = initResult
	}

	// Construct the service URL.
	var serviceURL string
	if endpoint != "" {
		serviceURL = endpoint
	} else if region != "" {
		serviceURL = fmt.Sprintf("https://%s.blob.%s.core.windows.net", storageAccountName, region)
	} else {
		serviceURL = fmt.Sprintf("https://%s.blob.core.windows.net", storageAccountName)
	}

	var containerClient ContainerClientInterface
	var credentialType CredentialType

	if secretAccessKey != "" {
		// Use Shared Key authentication.
		scopedLog.Info("Using Shared Key authentication")

		// Create a Shared Key Credential.
		sharedKeyCredential, err := azblob.NewSharedKeyCredential(storageAccountName, secretAccessKey)
		if err != nil {
			scopedLog.Error(err, "Failed to create SharedKeyCredential")
			return nil, fmt.Errorf("failed to create SharedKeyCredential: %w", err)
		}

		// Initialize the container client with Shared Key Credential.
		rawContainerClient, err := container.NewClientWithSharedKeyCredential(
			fmt.Sprintf("%s/%s", serviceURL, bucketName),
			sharedKeyCredential,
			nil,
		)
		if err != nil {
			scopedLog.Error(err, "Failed to create ContainerClient with SharedKeyCredential")
			return nil, fmt.Errorf("failed to create ContainerClient with SharedKeyCredential: %w", err)
		}

		// Wrap the container client.
		containerClient = &ContainerClientWrapper{rawContainerClient}

		credentialType = CredentialTypeSharedKey
	} else {
		// Use Azure AD authentication.
		scopedLog.Info("Using Azure AD authentication")

		// Create a Token Credential using DefaultAzureCredential.
		tokenCredential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			scopedLog.Error(err, "Failed to create DefaultAzureCredential")
			return nil, fmt.Errorf("failed to create DefaultAzureCredential: %w", err)
		}

		// Initialize the container client with Token Credential.
		rawContainerClient, err := container.NewClient(
			fmt.Sprintf("%s%s", serviceURL, bucketName),
			tokenCredential,
			nil,
		)
		if err != nil {
			scopedLog.Error(err, "Failed to create ContainerClient with TokenCredential")
			return nil, fmt.Errorf("failed to create ContainerClient with TokenCredential: %w", err)
		}

		// Wrap the container client.
		containerClient = &ContainerClientWrapper{rawContainerClient}

		credentialType = CredentialTypeAzureAD
	}

	scopedLog.Info("AzureBlobClient initialized successfully",
		"CredentialType", credentialType,
		"BucketName", bucketName,
		"StorageAccountName", storageAccountName,
	)

	return &AzureBlobClient{
		BucketName:         bucketName,
		StorageAccountName: storageAccountName,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Endpoint:           endpoint,
		ContainerClient:    containerClient,
		CredentialType:     credentialType,
	}, nil
}

// GetAppsList retrieves a list of blobs (apps) from the Azure Blob container.
func (client *AzureBlobClient) GetAppsList(ctx context.Context) (RemoteDataListResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AzureBlob:GetAppsList").WithValues("Bucket", client.BucketName)

	scopedLog.Info("Fetching list of apps")

	// Define options for listing blobs.
	options := &container.ListBlobsFlatOptions{
		Prefix: &client.Prefix,
	}

	// Set the Marker if StartAfter is provided.
	if client.StartAfter != "" {
		options.Marker = &client.StartAfter
	}

	// Create a pager to iterate through blobs.
	pager := client.ContainerClient.NewListBlobsFlatPager(options)

	var blobs []*RemoteObject
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			scopedLog.Error(err, "Error listing blobs")
			return RemoteDataListResponse{}, fmt.Errorf("error listing blobs: %w", err)
		}

		for _, blob := range resp.Segment.BlobItems {
			etag := string(*blob.Properties.ETag)
			name := *blob.Name
			lastModified := blob.Properties.LastModified
			size := blob.Properties.ContentLength

			remoteObject := &RemoteObject{
				Etag:         &etag,
				Key:          &name,
				LastModified: lastModified,
				Size:         size,
			}
			blobs = append(blobs, remoteObject)
		}
	}

	scopedLog.Info("Successfully fetched list of apps", "TotalBlobs", len(blobs))

	return RemoteDataListResponse{Objects: blobs}, nil
}

// DownloadApp downloads a specific blob from Azure Blob Storage to a local file.
func (client *AzureBlobClient) DownloadApp(ctx context.Context, downloadRequest RemoteDataDownloadRequest) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AzureBlob:DownloadApp").WithValues(
		"Bucket", client.BucketName,
		"RemoteFile", downloadRequest.RemoteFile,
		"LocalFile", downloadRequest.LocalFile,
	)

	scopedLog.Info("Initiating blob download")

	// Create a blob client for the specific blob.
	blobClient := client.ContainerClient.NewBlobClient(downloadRequest.RemoteFile)

	// Download the blob content.
	get, err := blobClient.DownloadStream(ctx, nil)
	if err != nil {
		scopedLog.Error(err, "Failed to download blob")
		return false, fmt.Errorf("failed to download blob: %w", err)
	}
	defer get.Body.Close()

	// Create or truncate the local file.
	localFile, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		scopedLog.Error(err, "Failed to create local file")
		return false, fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	// Write the content to the local file.
	_, err = io.Copy(localFile, get.Body)
	if err != nil {
		scopedLog.Error(err, "Failed to write blob content to local file")
		return false, fmt.Errorf("failed to write blob content to local file: %w", err)
	}

	scopedLog.Info("Blob downloaded successfully")

	return true, nil
}

// NoOpInitFunc performs no additional initialization.
// It satisfies the GetInitFunc type and can be used when no extra setup is needed.
func NoOpInitFunc(
	ctx context.Context,
	appAzureBlobEndPoint string,
	storageAccountName string,
	secretAccessKey string, // Optional: can be empty
) interface{} {
	// No additional initialization required.
	return nil
}

// RegisterAzureBlobClient registers the AzureBlobClient in the RemoteDataClientsMap.
func RegisterAzureBlobClient() {
	wrapperObject := GetRemoteDataClientWrapper{
		GetRemoteDataClient: NewAzureBlobClient,
		GetInitFunc:         NoOpInitFunc, // Use CustomInitFunc if additional initialization is needed
	}
	RemoteDataClientsMap["azure"] = wrapperObject
}
