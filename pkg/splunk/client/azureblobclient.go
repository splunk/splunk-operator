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
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AzureBlobClient is a client to implement Azure Blob specific APIs using Azure SDK
type AzureBlobClient struct {
	BucketName         string
	StorageAccountName string
	SecretAccessKey    string
	Prefix             string
	StartAfter         string
	Endpoint           string
	ContainerClient    ContainerClientInterface // Use the interface here
}

// Define an interface that matches the methods you need from the container client
type ContainerClientInterface interface {
	NewListBlobsFlatPager(options *azblob.ListBlobsFlatOptions) *runtime.Pager[azblob.ListBlobsFlatResponse]
	NewBlobClient(blobName string) *blob.Client
}

// NewAzureBlobClient initializes and returns an AzureBlob client using Azure SDK
func NewAzureBlobClient(ctx context.Context, bucketName string, storageAccountName string, secretAccessKey string, prefix string, startAfter string, region string, endpoint string, fn GetInitFunc) (RemoteDataClient, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("NewAzureBlobClient")

	scopedLog.Info("Creating AzureBlobClient using Azure SDK")

	// Create the blob service client
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", storageAccountName)
	credential, err := azblob.NewSharedKeyCredential(storageAccountName, secretAccessKey)
	if err != nil {
		scopedLog.Error(err, "Failed to create SharedKeyCredential")
		return nil, err
	}

	// Create the container client
	containerClient, err := container.NewClientWithSharedKeyCredential(fmt.Sprintf("%s%s", serviceURL, bucketName), credential, nil)
	if err != nil {
		scopedLog.Error(err, "Failed to create ContainerClient")
		return nil, err
	}

	return &AzureBlobClient{
		BucketName:         bucketName,
		StorageAccountName: storageAccountName,
		SecretAccessKey:    secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Endpoint:           endpoint,
		ContainerClient:    containerClient, // Assign the real client here
	}, nil
}

// GetAppsList gets the list of apps (blobs) from the Azure Blob container
func (client *AzureBlobClient) GetAppsList(ctx context.Context) (RemoteDataListResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AzureBlob:GetAppsList").WithValues("Bucket", client.BucketName)

	scopedLog.Info("Fetching list of apps")

	// Set prefix and other options if needed
	options := container.ListBlobsFlatOptions{
		Prefix: &client.Prefix,
	}

	// Use ListBlobsFlatPager to paginate through the blobs in the container
	pager := client.ContainerClient.NewListBlobsFlatPager(&options)

	var blobs []*RemoteObject
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			scopedLog.Error(err, "Error listing blobs")
			return RemoteDataListResponse{}, err
		}

		for _, blob := range resp.Segment.BlobItems {
			etag := string(*blob.Properties.ETag)
			name := *blob.Name
			lastModified := *blob.Properties.LastModified
			size := *blob.Properties.ContentLength

			remoteObject := &RemoteObject{
				Etag:         &etag,
				Key:          &name,
				LastModified: &lastModified,
				Size:         &size,
			}
			blobs = append(blobs, remoteObject)
		}
	}

	return RemoteDataListResponse{Objects: blobs}, nil
}

// DownloadApp downloads a specific blob (app package) from Azure Blob storage
func (client *AzureBlobClient) DownloadApp(ctx context.Context, downloadRequest RemoteDataDownloadRequest) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AzureBlob:DownloadApp").WithValues("Bucket", client.BucketName, "RemoteFile", downloadRequest.RemoteFile)

	scopedLog.Info("Downloading app package")

	blobClient := client.ContainerClient.NewBlobClient(downloadRequest.RemoteFile)

	// Download the blob content
	get, err := blobClient.DownloadStream(ctx, nil)
	if err != nil {
		scopedLog.Error(err, "Failed to download blob")
		return false, err
	}

	defer get.Body.Close()

	// Create local file
	localFile, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		scopedLog.Error(err, "Failed to create local file")
		return false, err
	}
	defer localFile.Close()

	// Write the content to the local file
	_, err = io.Copy(localFile, get.Body)
	if err != nil {
		scopedLog.Error(err, "Failed to write blob content to local file")
		return false, err
	}

	scopedLog.Info("Download successful")

	return true, nil
}

// RegisterAzureBlobClient will add the corresponding function pointer to the map
func RegisterAzureBlobClient() {
	wrapperObject := GetRemoteDataClientWrapper{
		GetRemoteDataClient: NewAzureBlobClient,
		GetInitFunc:         InitAzureBlobClientWrapper,
	}
	RemoteDataClientsMap["azure"] = wrapperObject
}

// InitAzureBlobClientWrapper is a wrapper around InitAzureBlobClientSession
func InitAzureBlobClientWrapper(ctx context.Context, appAzureBlobEndPoint string, storageAccountName string, secretAccessKey string) interface{} {
	return InitAzureBlobClientSession(ctx)
}

// InitAzureBlobClientSession initializes and returns a client session object
func InitAzureBlobClientSession(ctx context.Context) *container.Client {
	// This can be used to initialize the Azure client session if needed
	return nil // Placeholder
}
