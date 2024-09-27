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
	"io"
	"os"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// blank assignment to verify that GCSClient implements RemoteDataClient
var _ RemoteDataClient = &GCSClient{}

// GCSClientInterface defines the interface for GCS client operations
type GCSClientInterface interface {
	Bucket(bucketName string) *storage.BucketHandle
}

// GCSClientWrapper wraps the actual GCS client to implement the interface
type GCSClientWrapper struct {
	Client *storage.Client
}

// Bucket is a wrapper around the actual GCS Bucket method
func (g *GCSClientWrapper) Bucket(bucketName string) *storage.BucketHandle {
	return g.Client.Bucket(bucketName)
}

// BucketHandleInterface is an interface for wrapping both real and mock bucket handles
type BucketHandleInterface interface {
	Objects(ctx context.Context, query *storage.Query) *storage.ObjectIterator
}

// RealBucketHandleWrapper wraps the real *storage.BucketHandle and implements BucketHandleInterface
type RealBucketHandleWrapper struct {
	BucketHandle *storage.BucketHandle
}

// Objects delegates to the real *storage.BucketHandle's Objects method
func (r *RealBucketHandleWrapper) Objects(ctx context.Context, query *storage.Query) *storage.ObjectIterator {
	return r.BucketHandle.Objects(ctx, query)
}

// GCSClient is a client to implement GCS specific APIs
type GCSClient struct {
	BucketName     string
	GCPCredentials string
	Prefix         string
	StartAfter     string
	Client         GCSClientInterface
	BucketHandle   BucketHandleInterface // Use the new interface here
}

// InitGCSClient initializes and returns a GCS client implementing GCSClientInterface
func InitGCSClient(ctx context.Context, gcpCredentials string) (GCSClientInterface, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("InitGCSClient")

	var client *storage.Client
	var err error

	if len(gcpCredentials) == 0 {
		client, err = storage.NewClient(ctx)
	} else if len(gcpCredentials) > 0 {
		var creds google.Credentials
		err = json.Unmarshal([]byte(gcpCredentials), &creds)
		if err != nil {
			scopedLog.Error(err, "Secret key.json value is not parsable")
			return nil, err
		}
		client, err = storage.NewClient(ctx, option.WithCredentials(&creds))
	}

	if err != nil {
		scopedLog.Error(err, "Failed to initialize a GCS client.")
		return nil, err
	}

	scopedLog.Info("GCS Client initialization successful.")
	return &GCSClientWrapper{Client: client}, nil
}

// InitGcloudClientWrapper is a wrapper around InitGCSClient
func InitGcloudClientWrapper(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
	client, _ := InitGCSClient(ctx, secretAccessKey)
	return client
}

// NewGCSClient returns a GCS client
func NewGCSClient(ctx context.Context, bucketName string, gcpCredentials string, secretAccessKey string, prefix string, startAfter string, region string, endpoint string, fn GetInitFunc) (RemoteDataClient, error) {
	client, err := InitGCSClient(ctx, secretAccessKey)
	if err != nil {
		return nil, err
	}

	realBucketHandle := client.Bucket(bucketName)
	bucketHandleWrapper := &RealBucketHandleWrapper{
		BucketHandle: realBucketHandle,
	}

	return &GCSClient{
		BucketName:     bucketName,
		GCPCredentials: secretAccessKey,
		Prefix:         prefix,
		StartAfter:     startAfter,
		Client:         client,
		BucketHandle:   bucketHandleWrapper,
	}, nil
}

// RegisterGCSClient will add the corresponding function pointer to the map
func RegisterGCSClient() {
	wrapperObject := GetRemoteDataClientWrapper{GetRemoteDataClient: NewGCSClient, GetInitFunc: InitGcloudClientWrapper}
	RemoteDataClientsMap["gcloud"] = wrapperObject
}

// GetAppsList gets the list of apps from remote storage
// GetAppsList gets the list of apps from remote storage
func (gcsClient *GCSClient) GetAppsList(ctx context.Context) (RemoteDataListResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetAppsList")

	scopedLog.Info("Getting Apps list", "GCS Bucket", gcsClient.BucketName)
	remoteDataClientResponse := RemoteDataListResponse{}

	query := &storage.Query{
		Prefix:    gcsClient.Prefix,
		Delimiter: "/",
	}

	startAfterFound := gcsClient.StartAfter == ""    // If StartAfter is empty, skip this check
	it := gcsClient.BucketHandle.Objects(ctx, query) // Use BucketHandleInterface here

	var objects []RemoteObject
	maxKeys := 4000 // Limit the number of objects manually

	for count := 0; count < maxKeys; {
		obj, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			scopedLog.Error(err, "Error fetching object from GCS", "GCS Bucket", gcsClient.BucketName)
			return remoteDataClientResponse, err
		}

		// Map GCS object attributes to RemoteObject
		remoteObj := RemoteObject{
			Etag:         &obj.Etag,
			Key:          &obj.Name,
			LastModified: &obj.Updated,
			Size:         &obj.Size,
			StorageClass: &obj.StorageClass,
		}

		// Implement "StartAfter" logic to skip objects until the desired one is found
		if !startAfterFound {
			if obj.Name == gcsClient.StartAfter {
				startAfterFound = true // Start adding objects after this point
			}
			continue
		}

		objects = append(objects, remoteObj)
		count++
	}

	tmp, err := json.Marshal(objects)
	if err != nil {
		scopedLog.Error(err, "Failed to marshal GCS response", "GCS Bucket", gcsClient.BucketName)
		return remoteDataClientResponse, err
	}

	err = json.Unmarshal(tmp, &(remoteDataClientResponse.Objects))
	if err != nil {
		scopedLog.Error(err, "Failed to unmarshal GCS response", "GCS Bucket", gcsClient.BucketName)
		return remoteDataClientResponse, err
	}

	return remoteDataClientResponse, nil
}

// DownloadApp downloads the app from remote storage to the local file system
func (gcsClient *GCSClient) DownloadApp(ctx context.Context, downloadRequest RemoteDataDownloadRequest) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("DownloadApp").WithValues("remoteFile", downloadRequest.RemoteFile, "localFile",
		downloadRequest.LocalFile, "etag", downloadRequest.Etag)

	file, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		scopedLog.Error(err, "Unable to open local file")
		return false, err
	}
	defer file.Close()

	obj := gcsClient.Client.Bucket(gcsClient.BucketName).Object(downloadRequest.RemoteFile)
	reader, err := obj.NewReader(ctx)
	if err != nil {
		scopedLog.Error(err, "Unable to download item", "RemoteFile", downloadRequest.RemoteFile)
		os.Remove(downloadRequest.RemoteFile)
		return false, err
	}
	defer reader.Close()

	if _, err := io.Copy(file, reader); err != nil {
		scopedLog.Error(err, "Unable to copy data to local file")
		return false, err
	}

	scopedLog.Info("File downloaded")

	return true, nil
}
