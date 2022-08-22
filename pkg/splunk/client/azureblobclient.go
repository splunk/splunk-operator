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
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// blank assignment to verify that AzureBlobClient implements BlobClient
var _ RemoteDataClient = &AzureBlobClient{}

// DownloadOptions ...Options for downloading the apps
type DownloadOptions struct {
	MaxChunkSize int // chunk size in gb
	Timeout      int //timeout in seconds
}

// Config contains configuration for httpclient
type Config struct {
	MaxRetries int         // max number of retries for the rest calls
	HTTPClient http.Client // the http client to use for calling the rest end points
}

// ListBlobsOptions contains options for listing the apps
type ListBlobsOptions struct {
	Prefix      string // prefix of the files to list, useful to filter apps based on the path they belong to
	MaxEnteries int    // total number of files to return in one call
}

// ListBlobsOutput contains XML output from the blob rest call
type ListBlobsOutput struct {
	BlobsFlatXML string
}

// SplunkAzureBlobClient is an interface to AzureBlob  client
type SplunkAzureBlobClient interface {
	ListApps(ctx context.Context, bucketName string, listAppsOpts map[string]string) ([]byte, error)
	DownloadApp(ctx context.Context, bucketName string, remoteFileName string, localFileName string, downloadOpts map[string]string) error
}

// SplunkAzureBlobClientTemp is temp struct to let the build successfull and
// have a place holder for azureblobclient
type SplunkAzureBlobClientTemp struct {
	client http.Client
}

// ListApps to be revisited
func (client *SplunkAzureBlobClientTemp) ListApps(ctx context.Context, bucketName string, listAppsOpts map[string]string) ([]byte, error) {
	return nil, nil
}

// DownloadApp to be revsited
func (client *SplunkAzureBlobClientTemp) DownloadApp(ctx context.Context, bucketName string, remoteFileName string, localFileName string, downloadOpts map[string]string) error {
	return nil
}

// AzureBlobClient is a client to implement Azure Blob specific APIs
type AzureBlobClient struct {
	BucketName         string
	StorageAccountName string
	SecretAccessKey    string
	Prefix             string
	StartAfter         string
	Endpoint           string
	Client             SplunkAzureBlobClient
}

// NewAzureBlobClient returns an AzureBlob client
func NewAzureBlobClient(ctx context.Context, bucketName string, storageAccountName string, secretAccessKey string, prefix string, startAfter string, region string, endpoint string, fn GetInitFunc) (RemoteDataClient, error) {

	var splunkClient SplunkAzureBlobClient
	var err error

	cl := fn(ctx, endpoint, storageAccountName, secretAccessKey)
	if cl == nil {
		err = fmt.Errorf("failed to create an Azure blob client")
		return nil, err
	}

	// TODO next sprint CSPL XXXX List and download azure object
	// This part need to be developed when the Azure blob rest handlers
	// are built
	//splunkClient = cl.(*azure.Client)

	return &AzureBlobClient{
		BucketName:         bucketName,
		StorageAccountName: storageAccountName,
		SecretAccessKey:    secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Endpoint:           endpoint,
		Client:             splunkClient,
	}, nil
}

// InitAzureBlobClientWrapper is a wrapper around InitAzureBlobClientSession
func InitAzureBlobClientWrapper(ctx context.Context, appAzureBlobEndPoint string, storageAccountName string, secretAccessKey string) interface{} {
	return InitAzureBlobClientSession(ctx, appAzureBlobEndPoint, storageAccountName, secretAccessKey)
}

// InitAzureBlobClientSession initializes and returns a client session object
func InitAzureBlobClientSession(ctx context.Context, appAzureBlobEndPoint string, storageAccountName string, secretAccessKey string) SplunkAzureBlobClient {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("InitAzureBlobClientSession")

	scopedLog.Info("Initializing  AzureBlob Service for apps", "appAzureBlobEndPoint", appAzureBlobEndPoint)
	var azureBlobClient SplunkAzureBlobClientTemp

	// TODO target for next sprint CSPL XXXX List and download azure object
	// This part need to be developed when the Azure blob rest handlers
	// are built.

	// var err error

	// Enforcing minimum version TLS1.2
	// tr := &http.Transport{
	// 	TLSClientConfig: &tls.Config{
	// 		MinVersion: tls.VersionTLS12,
	// 	},
	// }
	// tr.ForceAttemptHTTP2 = true
	// httpClient := http.Client{Transport: tr}

	// config := &Config{
	// 	MaxRetries: 3,
	// 	HTTPClient: httpClient,
	// }

	// azureBlobClient, err = NewAzureSession(ctx, appAzureBlobEndPoint, storageAccountName, secretAccessKey, config)
	// if err != nil {
	// 	scopedLog.Info("Error creating new AzureBlob Client Session", "err", err)
	// 	return nil
	// }

	azureBlobClient = SplunkAzureBlobClientTemp{client: http.Client{}}
	return &azureBlobClient

}

// GetAppsList get the list of apps from remote storage
func (client *AzureBlobClient) GetAppsList(ctx context.Context) (RemoteDataListResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetAppsList")

	scopedLog.Info("Getting Apps list", " S3 Bucket", client.BucketName, "Prefix", client.Prefix)

	azureBlobClient := client.Client

	// Create a bucket list command for all files in bucket

	//TODO: revise it in next sprint. had to use map temporary to let build work
	opts := make(map[string]string)

	opts["Prefix"] = client.Prefix
	opts["MaxEnteries"] = "4000"

	// List all objects from a bucket-name with a matching prefix.
	listResponse, err := azureBlobClient.ListApps(context.Background(), client.BucketName, opts)

	//TODO actual working logic to be covered in CSPL XXXX list azure apps
	remoteDataListResponse, err := extractResponse(listResponse)

	if err != nil {
		scopedLog.Error(err, "Failed to get list apps response", "Azure Blob Bucket", client.BucketName)
		return remoteDataListResponse, err
	}

	return remoteDataListResponse, err
}

func extractResponse(listBlobsXML []byte) (RemoteDataListResponse, error) {
	remoteDataListResponse := RemoteDataListResponse{}

	err := json.Unmarshal(listBlobsXML, &(remoteDataListResponse.Objects))
	if err != nil {
		return remoteDataListResponse, err
	}

	return remoteDataListResponse, nil
}

// DownloadApp downloads an app package from remote storage
func (client *AzureBlobClient) DownloadApp(ctx context.Context, downloadRequest RemoteDataDownloadRequest) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("DownloadApp").WithValues("remoteFile", downloadRequest.RemoteFile,
		downloadRequest.LocalFile, downloadRequest.Etag)

	file, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		scopedLog.Error(err, "Unable to create local file")
		return false, err
	}
	defer file.Close()

	azureBlobClient := client.Client

	//TODO : revise in next sprint . these are just place holder to more
	// specific data structure so we can pass download options to rest calls
	downloadOptions := make(map[string]string)

	downloadOptions["maxChunkSize"] = "5gb"
	downloadOptions["timeout"] = "5sec"

	err = azureBlobClient.DownloadApp(ctx, client.BucketName, downloadRequest.RemoteFile, downloadRequest.LocalFile, downloadOptions)
	if err != nil {
		scopedLog.Error(err, "Unable to download remote file")
		return false, err
	}

	scopedLog.Info("File downloaded")

	return true, nil
}

// RegisterAzureBlobClient will add the corresponding function pointer to the map
func RegisterAzureBlobClient() {
	wrapperObject := GetRemoteDataClientWrapper{GetRemoteDataClient: NewAzureBlobClient, GetInitFunc: InitAzureBlobClientWrapper}
	RemoteDataClientsMap["azure"] = wrapperObject
}
