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
	//"crypto/tls"
	"encoding/json"
	"io"
	//"net/http"
	"os"

	//"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// blank assignment to verify that GCSClient implements RemoteDataClient
var _ RemoteDataClient = &GCSClient{}

// SplunkGCSClient is an interface to GCS client
type SplunkGCSClient interface {
	ListObjects(ctx context.Context, query *storage.Query) *storage.ObjectIterator
}

// SplunkGCSDownloadClient is used to download the apps from remote storage
type SplunkGCSDownloadClient interface {
	Download(ctx context.Context, w io.WriterAt, obj *storage.ObjectHandle) error
}

// GCSClient is a client to implement GCS specific APIs
type GCSClient struct {
	BucketName     string
	GCPCredentials string
	Prefix         string
	StartAfter     string
	Client         *storage.Client
}

// InitGCSClient initializes and returns a GCS client
func InitGCSClient(ctx context.Context, gcpCredentials string) (*storage.Client, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("InitGCSClient")

	// Enforcing minimum version TLS1.2
	//tr := &http.Transport{
	//	TLSClientConfig: &tls.Config{
	//		MinVersion: tls.VersionTLS12,
	//	},
	//}
	//tr.ForceAttemptHTTP2 = true
	//httpClient := &http.Client{
	//	Transport: tr,
	//	Timeout:   appFrameworkHttpclientTimeout * time.Second,
	//}

	var client *storage.Client
	var err error
	if len(gcpCredentials) == 0 {
		client, err = storage.NewClient(ctx)
	} else  {
		//client, err = storage.NewClient(ctx, option.WithCredentialsFile(gcpCredentials), option.WithHTTPClient(httpClient))
		var creds google.Credentials
		err = json.Unmarshal([]byte(gcpCredentials), &creds)
		if err != nil {
			scopedLog.Error(err, "secret key.json value is not json parsable")
			return nil, err
		}
		client, err = storage.NewClient(ctx, option.WithCredentials(&creds))
	}
	if err != nil {
		scopedLog.Error(err, "Failed to initialize a GCS client.")
		return nil, err
	}

	scopedLog.Info("GCS Client initialization successful.")
	return client, nil
}

// InitAWSClientWrapper is a wrapper around InitClientSession
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

	return &GCSClient{
		BucketName:     bucketName,
		GCPCredentials: secretAccessKey,
		Prefix:         prefix,
		StartAfter:     startAfter,
		Client:         client,
	}, nil
}

// RegisterGCSClient will add the corresponding function pointer to the map
func RegisterGCSClient() {
	wrapperObject := GetRemoteDataClientWrapper{GetRemoteDataClient: NewGCSClient, GetInitFunc: InitGcloudClientWrapper}
	RemoteDataClientsMap["gcloud"] = wrapperObject
}

// GetAppsList get the list of apps from remote storage
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
	it := gcsClient.Client.Bucket(gcsClient.BucketName).Objects(ctx, query)
	//var objects []*storage.ObjectAttrs
	var objects []RemoteObject
	maxKeys := 4000 // Limit the number of objects manually

	for count := 0; count < maxKeys; {
		obj, err := it.Next()
		if err != iterator.Done {
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

// DownloadApp downloads the app from remote storage to local file system
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
