// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// blank assignment to verify that MinioClient implements S3Client
var _ S3Client = &MinioClient{}

// SplunkMinioClient is an interface to Minio S3 client
type SplunkMinioClient interface {
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
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
func NewMinioClient(bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string, endpoint string, fn GetInitFunc) (S3Client, error) {
	var s3SplunkClient SplunkMinioClient
	var err error

	cl := fn(endpoint, accessKeyID, secretAccessKey)
	if cl == nil {
		err = fmt.Errorf("Failed to create an AWS S3 client")
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
	wrapperObject := GetS3ClientWrapper{GetS3Client: NewMinioClient, GetInitFunc: InitMinioClientWrapper}
	S3Clients["minio"] = wrapperObject
}

// InitMinioClientWrapper is a wrapper around InitMinioClientSession
func InitMinioClientWrapper(appS3Endpoint string, accessKeyID string, secretAccessKey string) interface{} {
	return InitMinioClientSession(appS3Endpoint, accessKeyID, secretAccessKey)
}

// InitMinioClientSession initializes and returns a client session object
func InitMinioClientSession(appS3Endpoint string, accessKeyID string, secretAccessKey string) SplunkMinioClient {
	scopedLog := log.WithName("InitMinioClientSession")

	// Check if SSL is needed
	useSSL := true
	if strings.HasPrefix(appS3Endpoint, "http://") {
		// We should always use a secure SSL endpoint, so we won't set useSSL = false
		scopedLog.Info("Using insecure endpoint, forcing useSSL=true for Minio Client Session", "appS3Endpoint", appS3Endpoint)
		appS3Endpoint = strings.TrimPrefix(appS3Endpoint, "http://")
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
	s3Client, err := minio.New(appS3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		scopedLog.Info("Error creating new Minio Client Session", "err", err)
		return nil
	}

	return s3Client
}

// GetAppsList get the list of apps from remote storage
func (client *MinioClient) GetAppsList() (S3Response, error) {
	scopedLog := log.WithName("GetAppsList")

	scopedLog.Info("Getting Apps list", " S3 Bucket", client.BucketName, "Prefix", client.Prefix)
	s3Resp := S3Response{}
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
			scopedLog.Info("Got an object error", "object.Err", object.Err, "client.BucketName", client.BucketName)
			return s3Resp, nil
		}
		scopedLog.Info("Got an object", "object", object)

		// Create a new object to add to append to the response
		newETag := object.ETag
		newKey := object.Key
		newLastModified := object.LastModified
		newSize := object.Size
		newStorageClass := object.StorageClass
		newRemoteObject := RemoteObject{Etag: &newETag, Key: &newKey, LastModified: &newLastModified, Size: &newSize, StorageClass: &newStorageClass}
		s3Resp.Objects = append(s3Resp.Objects, &newRemoteObject)
	}

	return s3Resp, nil
}

// GetInitContainerImage returns the initContainer image to be used with this s3 client
func (client *MinioClient) GetInitContainerImage() string {
	return ("amazon/aws-cli")
}

// GetInitContainerCmd returns the init container command on a per app source basis to be used by the initContainer
func (client *MinioClient) GetInitContainerCmd(endpoint string, bucket string, path string, appSrcName string, appMnt string) []string {
	s3AppSrcPath := filepath.Join(bucket, path) + "/"
	podSyncPath := filepath.Join(appMnt, appSrcName) + "/"

	return ([]string{fmt.Sprintf("--endpoint-url=%s", endpoint), "s3", "sync", fmt.Sprintf("s3://%s", s3AppSrcPath), podSyncPath})
}
