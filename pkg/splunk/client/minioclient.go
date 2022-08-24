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
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// blank assignment to verify that MinioClient implements S3Client
var _ RemoteDataClient = &MinioClient{}

// SplunkMinioClient is an interface to Minio S3 client
type SplunkMinioClient interface {
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	FGetObject(ctx context.Context, bucketName string, remoteFileName string, localFileName string, opts minio.GetObjectOptions) error
}

// MinioClient is a client to implement S3 specific APIs
type MinioClient struct {
	BucketName        string
	S3AccessKeyID     string
	S3SecretAccessKey string
	Prefix            string
	StartAfter        string
	Endpoint          string
	Client            SplunkMinioClient
}

// NewMinioClient returns an Minio client
func NewMinioClient(ctx context.Context, bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string, region string, endpoint string, fn GetInitFunc) (RemoteDataClient, error) {

	var s3SplunkClient SplunkMinioClient
	var err error

	cl := fn(ctx, endpoint, accessKeyID, secretAccessKey)
	if cl == nil {
		err = fmt.Errorf("failed to create an Minio S3 client")
		return nil, err
	}

	s3SplunkClient = cl.(*minio.Client)

	return &MinioClient{
		BucketName:        bucketName,
		S3AccessKeyID:     accessKeyID,
		S3SecretAccessKey: secretAccessKey,
		Prefix:            prefix,
		StartAfter:        startAfter,
		Endpoint:          endpoint,
		Client:            s3SplunkClient,
	}, nil
}

//RegisterMinioClient will add the corresponding function pointer to the map
func RegisterMinioClient() {
	wrapperObject := GetRemoteDataClientWrapper{GetRemoteDataClient: NewMinioClient, GetInitFunc: InitMinioClientWrapper}
	RemoteDataClientsMap["minio"] = wrapperObject
}

// InitMinioClientWrapper is a wrapper around InitMinioClientSession
func InitMinioClientWrapper(ctx context.Context, appS3Endpoint string, accessKeyID string, secretAccessKey string) interface{} {
	return InitMinioClientSession(ctx, appS3Endpoint, accessKeyID, secretAccessKey)
}

// InitMinioClientSession initializes and returns a client session object
func InitMinioClientSession(ctx context.Context, appS3Endpoint string, accessKeyID string, secretAccessKey string) SplunkMinioClient {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("InitMinioClientSession")

	// Check if SSL is needed
	useSSL := true
	if strings.HasPrefix(appS3Endpoint, "http://") {
		// We should always use a secure SSL endpoint, so we won't set useSSL = false
		scopedLog.Info("Using insecure endpoint, useSSL=false for Minio Client Session", "appS3Endpoint", appS3Endpoint)
		appS3Endpoint = strings.TrimPrefix(appS3Endpoint, "http://")
		useSSL = false
	} else if strings.HasPrefix(appS3Endpoint, "https://") {
		appS3Endpoint = strings.TrimPrefix(appS3Endpoint, "https://")
	} else {
		// Unsupported endpoint
		scopedLog.Info("Unsupported endpoint for Minio S3 client", "appS3Endpoint", appS3Endpoint)
		return nil
	}

	// New returns an Minio compatible client object. API compatibility (v2 or v4) is automatically
	// determined based on the Endpoint value.
	scopedLog.Info("Connecting to Minio S3 for apps", "appS3Endpoint", appS3Endpoint)
	var s3Client *minio.Client
	var err error

	options := &minio.Options{
		Secure: useSSL,
	}
	if accessKeyID != "" && secretAccessKey != "" {
		options.Creds = credentials.NewStaticV4(accessKeyID, secretAccessKey, "")
	} else {
		scopedLog.Info("No Access/Secret Keys, attempt connection without them using IAM", "appS3Endpoint", appS3Endpoint)
		options.Creds = credentials.NewIAM("")
	}
	s3Client, err = minio.New(appS3Endpoint, options)
	if err != nil {
		scopedLog.Info("Error creating new Minio Client Session", "err", err)
		return nil
	}

	return s3Client
}

// GetAppsList get the list of apps from remote storage
func (client *MinioClient) GetAppsList(ctx context.Context) (RemoteDataListResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetAppsList")

	scopedLog.Info("Getting Apps list", " S3 Bucket", client.BucketName, "Prefix", client.Prefix)
	remoteDataClientResponse := RemoteDataListResponse{}
	s3Client := client.Client

	// Create a bucket list command for all files in bucket
	opts := minio.ListObjectsOptions{
		UseV1:     true,
		Prefix:    client.Prefix,
		Recursive: false,
	}

	// List all objects from a bucket-name with a matching prefix.
	for object := range s3Client.ListObjects(context.Background(), client.BucketName, opts) {
		if object.Err != nil {
			err := fmt.Errorf("got an object error: %v for bucket: %s", object.Err, client.BucketName)
			return remoteDataClientResponse, err
		}
		scopedLog.Info("Got an object", "object", object)

		// Create a new object to add to append to the response
		newETag := object.ETag
		newKey := object.Key
		newLastModified := object.LastModified
		newSize := object.Size
		newStorageClass := object.StorageClass
		newRemoteObject := RemoteObject{Etag: &newETag, Key: &newKey, LastModified: &newLastModified, Size: &newSize, StorageClass: &newStorageClass}
		remoteDataClientResponse.Objects = append(remoteDataClientResponse.Objects, &newRemoteObject)
	}

	return remoteDataClientResponse, nil
}

// DownloadApp downloads an app package from remote storage
func (client *MinioClient) DownloadApp(ctx context.Context, downloadRequest RemoteDataDownloadRequest) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("DownloadApp").WithValues("remoteFile", downloadRequest.RemoteFile,
		downloadRequest.LocalFile, downloadRequest.Etag)

	file, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		scopedLog.Error(err, "Unable to create local file")
		return false, err
	}
	defer file.Close()

	s3Client := client.Client

	options := minio.GetObjectOptions{}
	// set the option to match the specified etag on remote storage
	options.SetMatchETag(downloadRequest.Etag)

	err = s3Client.FGetObject(ctx, client.BucketName, downloadRequest.RemoteFile, downloadRequest.LocalFile, options)
	if err != nil {
		scopedLog.Error(err, "Unable to download remote file")
		return false, err
	}

	scopedLog.Info("File downloaded")

	return true, nil
}
