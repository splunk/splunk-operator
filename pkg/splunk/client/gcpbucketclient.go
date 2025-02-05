// Copyright (c) 2018-2022 Splunk Inc.
// All rights reserved.
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

	"cloud.google.com/go/storage"
	//"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// blank assignment to verify that GCSClient implements RemoteDataClient
var _ RemoteDataClient = &GCSClient{}

// GCSClientInterface defines the interface for GCS client operations
type GCSClientInterface interface {
	Bucket(bucketName string) BucketHandleInterface
}

// GCSClientWrapper wraps the actual GCS client to implement the interface
type GCSClientWrapper struct {
	Client *storage.Client
}

// Bucket is a wrapper around the actual GCS Bucket method
func (g *GCSClientWrapper) Bucket(bucketName string) BucketHandleInterface {
	return &RealBucketHandleWrapper{BucketHandle: g.Client.Bucket(bucketName)}
}

// BucketHandleInterface is an interface for wrapping both real and mock bucket handles
type BucketHandleInterface interface {
	Objects(ctx context.Context, query *storage.Query) ObjectIteratorInterface
	Object(name string) ObjectHandleInterface
}

// RealBucketHandleWrapper wraps the real *storage.BucketHandle and implements BucketHandleInterface
type RealBucketHandleWrapper struct {
	BucketHandle *storage.BucketHandle
}

// Objects delegates to the real *storage.BucketHandle's Objects method
func (r *RealBucketHandleWrapper) Objects(ctx context.Context, query *storage.Query) ObjectIteratorInterface {
	return &RealObjectIteratorWrapper{Iterator: r.BucketHandle.Objects(ctx, query)}
}

// Object delegates to the real *storage.BucketHandle's Object method
func (r *RealBucketHandleWrapper) Object(name string) ObjectHandleInterface {
	return &RealObjectHandleWrapper{ObjectHandle: r.BucketHandle.Object(name)}
}

// ObjectIteratorInterface defines the interface for object iterators
type ObjectIteratorInterface interface {
	Next() (*storage.ObjectAttrs, error)
}

// RealObjectIteratorWrapper wraps the real *storage.ObjectIterator and implements ObjectIteratorInterface
type RealObjectIteratorWrapper struct {
	Iterator *storage.ObjectIterator
}

// Next delegates to the real *storage.ObjectIterator's Next method
func (r *RealObjectIteratorWrapper) Next() (*storage.ObjectAttrs, error) {
	return r.Iterator.Next()
}

// ObjectHandleInterface defines the interface for object handles
type ObjectHandleInterface interface {
	NewReader(ctx context.Context) (io.ReadCloser, error)
}

// RealObjectHandleWrapper wraps the real *storage.ObjectHandle and implements ObjectHandleInterface
type RealObjectHandleWrapper struct {
	ObjectHandle *storage.ObjectHandle
}

// NewReader delegates to the real *storage.ObjectHandle's NewReader method
func (r *RealObjectHandleWrapper) NewReader(ctx context.Context) (io.ReadCloser, error) {
	return r.ObjectHandle.NewReader(ctx)
}

// GCSClient is a client to implement GCS specific APIs
type GCSClient struct {
	BucketName     string
	GCPCredentials string
	Prefix         string
	StartAfter     string
	Client         GCSClientInterface
	BucketHandle   BucketHandleInterface
}

// InitGCSClient initializes and returns a GCS client implementing GCSClientInterface
func InitGCSClient(ctx context.Context, gcpCredentials string) (GCSClientInterface, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("InitGCSClient")

	var client *storage.Client
	var err error

	if len(gcpCredentials) == 0 {
		// The storage.NewClient(ctx) internally uses Application Default Credentials (ADC) to authenticate,
		// and ADC works with Workload Identity when the required environment variables and setup are correctly configured.
		// If the environment variables are not set, the client will use the default service account credentials.
		// To use Google Workload Identity with storage.NewClient(ctx), ensure the following environment variables are properly set in your pod:
		// 	GOOGLE_APPLICATION_CREDENTIALS (Optional):
		// 		If you're not using the default workload identity path (/var/run/secrets/google.cloud/com.google.cloudsecrets/metadata/token),
		//		you can set GOOGLE_APPLICATION_CREDENTIALS to point to the federated token file manually.
		// 		Otherwise, this can be left unset when Workload Identity is configured correctly.
		// 	GOOGLE_CLOUD_PROJECT (Optional):
		// 		Set this to your Google Cloud project ID if the SDK is not detecting it automatically.
		// Additional Kubernetes Setup for Workload Identity:
		// The Workload Identity configuration on your cluster ensures that the necessary tokens are automatically mounted for the pod and available without needing GOOGLE_APPLICATION_CREDENTIALS.
		client, err = storage.NewClient(ctx)
	} else {
		client, err = storage.NewClient(ctx, option.WithCredentialsJSON([]byte(gcpCredentials)))
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

	bucketHandle := client.Bucket(bucketName)

	return &GCSClient{
		BucketName:     bucketName,
		GCPCredentials: secretAccessKey,
		Prefix:         prefix,
		StartAfter:     startAfter,
		Client:         client,
		BucketHandle:   bucketHandle,
	}, nil
}

// RegisterGCSClient will add the corresponding function pointer to the map
func RegisterGCSClient() {
	wrapperObject := GetRemoteDataClientWrapper{GetRemoteDataClient: NewGCSClient, GetInitFunc: InitGcloudClientWrapper}
	RemoteDataClientsMap["gcp"] = wrapperObject
}

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

	startAfterFound := gcsClient.StartAfter == "" // If StartAfter is empty, skip this check
	it := gcsClient.BucketHandle.Objects(ctx, query)

	var objects []*RemoteObject
	maxKeys := 4000 // Limit the number of objects manually

	if strings.HasSuffix(gcsClient.StartAfter, "/") {
		startAfterFound = true
	}

	for count := 0; count < maxKeys; {
		objAttrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			scopedLog.Error(err, "Error fetching object from GCS", "GCS Bucket", gcsClient.BucketName)
			return remoteDataClientResponse, err
		}

		// Implement "StartAfter" logic to skip objects until the desired one is found
		if !startAfterFound {
			if objAttrs.Name == gcsClient.StartAfter {
				startAfterFound = true // Start adding objects after this point
			}
			continue
		}

		// Map GCS object attributes to RemoteObject
		remoteObj := &RemoteObject{
			Etag:         &objAttrs.Etag,
			Key:          &objAttrs.Name,
			LastModified: &objAttrs.Updated,
			Size:         &objAttrs.Size,
			StorageClass: &objAttrs.StorageClass,
		}

		objects = append(objects, remoteObj)
		count++
	}

	remoteDataClientResponse.Objects = objects

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

	objHandle := gcsClient.BucketHandle.Object(downloadRequest.RemoteFile)
	reader, err := objHandle.NewReader(ctx)
	if err != nil {
		scopedLog.Error(err, "Unable to download item", "RemoteFile", downloadRequest.RemoteFile)
		os.Remove(downloadRequest.LocalFile)
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
