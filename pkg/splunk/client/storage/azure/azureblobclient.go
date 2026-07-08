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

package azure

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
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

var _ splcommon.RemoteDataClient = &BlobClient{}

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

// BlobClient implements the RemoteDataClient interface for Azure Blob Storage.
type BlobClient struct {
	BucketName         string
	StorageAccountName string
	Prefix             string
	StartAfter         string
	Endpoint           string
	ContainerClient    ContainerClientInterface
	CredentialType     CredentialType
}

// NewBlobClient initializes and returns a BlobClient.
// It supports both Shared Key and Azure AD authentication based on provided credentials.
// NewBlobClient initializes a new BlobClient with the provided parameters.
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
func NewBlobClient(
	ctx context.Context,
	bucketName string, // Azure Blob container name
	storageAccountName string, // Azure Storage account name
	secretAccessKey string, // Shared Key (optional; leave empty to use Azure AD)
	prefix string, // Prefix for blob listing (optional)
	startAfter string, // Marker for blob listing (optional)
	region string, // Azure region (e.g., "eastus")
	endpoint string, // Custom endpoint (optional)
	initFunc splcommon.GetInitFunc, // Initialization function
) (splcommon.RemoteDataClient, error) { // Matches GetRemoteDataClient signature
	scopedLog := logging.FromContext(ctx).With("func", "NewBlobClient")

	scopedLog.InfoContext(ctx, "initializing BlobClient")

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
		if serviceURL[len(serviceURL)-1] == '/' {
			serviceURL = serviceURL[:len(serviceURL)-1]
		}
	} else if region != "" {
		serviceURL = fmt.Sprintf("https://%s.blob.%s.core.windows.net", storageAccountName, region)
	} else {
		serviceURL = fmt.Sprintf("https://%s.blob.core.windows.net", storageAccountName)
	}

	var containerClient ContainerClientInterface
	var credentialType CredentialType

	if secretAccessKey != "" {
		// Use Shared Key authentication.
		scopedLog.InfoContext(ctx, "using Shared Key authentication")

		// Create a Shared Key Credential.
		sharedKeyCredential, err := azblob.NewSharedKeyCredential(storageAccountName, secretAccessKey)
		if err != nil {
			scopedLog.ErrorContext(ctx, "failed to create SharedKeyCredential", "error", err)
			return nil, fmt.Errorf("failed to create SharedKeyCredential: %w", err)
		}

		// Initialize the container client with Shared Key Credential.
		rawContainerClient, err := container.NewClientWithSharedKeyCredential(
			fmt.Sprintf("%s/%s", serviceURL, bucketName),
			sharedKeyCredential,
			nil,
		)
		if err != nil {
			scopedLog.ErrorContext(ctx, "failed to create ContainerClient with SharedKeyCredential", "error", err)
			return nil, fmt.Errorf("failed to create ContainerClient with SharedKeyCredential: %w", err)
		}

		// Wrap the container client.
		containerClient = &ContainerClientWrapper{rawContainerClient}

		credentialType = CredentialTypeSharedKey
	} else {
		// Use Azure AD authentication.
		scopedLog.InfoContext(ctx, "using Azure AD authentication")

		// Create a Token Credential using DefaultAzureCredential.
		// The Azure SDK uses environment variables to configure authentication when using DefaultAzureCredential.
		// For Workload Identity, by adding annotations to the pod's service account:
		// azure.workload.identity/client-id: <CLIENT_ID>
		// the following environment variables are typically used:
		// AZURE_AUTHORITY_HOST: The Azure Active Directory endpoint (default is https://login.microsoftonline.com/).
		// AZURE_CLIENT_ID: The client ID of the Azure AD application linked to the pod's service account.
		// AZURE_TENANT_ID: The tenant ID of the Azure Active Directory where the Azure AD application resides.
		// AZURE_FEDERATED_TOKEN_FILE: The path to the file containing the token issued by Kubernetes, usually mounted as a volume.
		// when using Azure AD Pod Identity (deprecated), the following environment variables are typically used:
		// AZURE_POD_IDENTITY_AUTHORITY_HOST: The Azure Active Directory endpoint (default is https://login.microsoftonline.com/).
		// AZURE_POD_IDENTITY_CLIENT_ID: The client ID of the Azure AD application linked to the pod's service account.
		// AZURE_POD_IDENTITY_TENANT_ID: The tenant ID of the Azure Active Directory where the Azure AD application resides.
		// AZURE_POD_IDENTITY_TOKEN_FILE: The path to the file containing the token issued by Kubernetes, usually mounted as a volume.
		// AZURE_POD_IDENTITY_RESOURCE_ID: The resource ID of the Azure resource to access.
		// AZURE_POD_IDENTITY_USE_MSI: Set to "true" to use Managed Service Identity (MSI) for authentication.
		// AZURE_POD_IDENTITY_USER_ASSIGNED_ID

		tokenCredential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			scopedLog.ErrorContext(ctx, "failed to create DefaultAzureCredential", "error", err)
			return nil, fmt.Errorf("failed to create DefaultAzureCredential: %w", err)
		}

		// Initialize the container client with Token Credential.
		rawContainerClient, err := container.NewClient(
			fmt.Sprintf("%s/%s", serviceURL, bucketName),
			tokenCredential,
			nil,
		)
		if err != nil {
			scopedLog.ErrorContext(ctx, "failed to create ContainerClient with TokenCredential", "error", err)
			return nil, fmt.Errorf("failed to create ContainerClient with TokenCredential: %w", err)
		}

		// Wrap the container client.
		containerClient = &ContainerClientWrapper{rawContainerClient}

		credentialType = CredentialTypeAzureAD
	}

	scopedLog.InfoContext(ctx, "azureBlobClient initialized successfully",
		"CredentialType", credentialType,
		"BucketName", bucketName,
		"StorageAccountName", storageAccountName,
	)

	return &BlobClient{
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
func (client *BlobClient) GetAppsList(ctx context.Context) (splcommon.RemoteDataListResponse, error) {
	scopedLog := logging.FromContext(ctx).With("func", "AzureBlob:GetAppsList", "Bucket", client.BucketName)

	scopedLog.InfoContext(ctx, "fetching list of apps")

	// Define options for listing blobs.
	options := &container.ListBlobsFlatOptions{
		Prefix: &client.Prefix,
	}

	// Set the Marker if StartAfter is provided.
	//if client.StartAfter != "" {
	//	options.Marker = &client.StartAfter
	//}

	// Create a pager to iterate through blobs.
	pager := client.ContainerClient.NewListBlobsFlatPager(options)

	var blobs []*splcommon.RemoteObject
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			scopedLog.ErrorContext(ctx, "error listing blobs", "error", err)
			return splcommon.RemoteDataListResponse{}, fmt.Errorf("error listing blobs: %w", err)
		}

		for _, blob := range resp.Segment.BlobItems {
			etag := string(*blob.Properties.ETag)
			name := *blob.Name
			lastModified := blob.Properties.LastModified
			size := blob.Properties.ContentLength

			remoteObject := &splcommon.RemoteObject{
				Etag:         &etag,
				Key:          &name,
				LastModified: lastModified,
				Size:         size,
			}
			blobs = append(blobs, remoteObject)
		}
	}

	scopedLog.InfoContext(ctx, "successfully fetched list of apps", "TotalBlobs", len(blobs))

	return splcommon.RemoteDataListResponse{Objects: blobs}, nil
}

// DownloadApp downloads a specific blob from Azure Blob Storage to a local file.
func (client *BlobClient) DownloadApp(ctx context.Context, downloadRequest splcommon.RemoteDataDownloadRequest) (bool, error) {
	scopedLog := logging.FromContext(ctx).With("func", "AzureBlob:DownloadApp",
		"Bucket", client.BucketName,
		"RemoteFile", downloadRequest.RemoteFile,
		"LocalFile", downloadRequest.LocalFile,
	)

	scopedLog.InfoContext(ctx, "initiating blob download")

	// Create a blob client for the specific blob.
	blobClient := client.ContainerClient.NewBlobClient(downloadRequest.RemoteFile)

	// Download the blob content.
	get, err := blobClient.DownloadStream(ctx, nil)
	if err != nil {
		scopedLog.ErrorContext(ctx, "failed to download blob", "error", err)
		return false, fmt.Errorf("failed to download blob: %w", err)
	}
	defer get.Body.Close()

	// Create or truncate the local file.
	localFile, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		scopedLog.ErrorContext(ctx, "failed to create local file", "error", err)
		return false, fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	// Write the content to the local file.
	_, err = io.Copy(localFile, get.Body)
	if err != nil {
		scopedLog.ErrorContext(ctx, "failed to write blob content to local file", "error", err)
		return false, fmt.Errorf("failed to write blob content to local file: %w", err)
	}

	scopedLog.InfoContext(ctx, "blob downloaded successfully")

	return true, nil
}

// NoOpInitFunc performs no additional initialization.
// It satisfies the splcommon.GetInitFunc type and can be used when no extra setup is needed.
func NoOpInitFunc(
	ctx context.Context,
	appAzureBlobEndPoint string,
	storageAccountName string,
	secretAccessKey string, // Optional: can be empty
) interface{} {
	// No additional initialization required.
	return nil
}
