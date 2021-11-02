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

package test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/minio/minio-go/v7"
	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
)

// MockMinioS3Client is used to store all the objects for an app source
type MockMinioS3Client struct {
	Objects []*MockS3Object
}

// MockMinioS3Handler is used for checking response received
type MockMinioS3Handler struct {
	WantSourceAppListResponseMap map[string]MockMinioS3Client
	GotSourceAppListResponseMap  map[string]MockMinioS3Client
}

// AddObjects adds mock AWS S3 Objects to handler
func (c *MockMinioS3Handler) AddObjects(appFrameworkRef enterpriseApi.AppFrameworkSpec, objects ...MockMinioS3Client) {
	for n := range objects {
		mockMinioS3Client := objects[n]
		appSource := appFrameworkRef.AppSources[n]
		if c.WantSourceAppListResponseMap == nil {
			c.WantSourceAppListResponseMap = make(map[string]MockMinioS3Client)
		}
		c.WantSourceAppListResponseMap[appSource.Name] = mockMinioS3Client
	}
}

// CheckMinioS3Response checks if the received objects are same as the one we expect
func (c *MockMinioS3Handler) CheckMinioS3Response(t *testing.T, testMethod string) {

	if len(c.WantSourceAppListResponseMap) != len(c.GotSourceAppListResponseMap) {
		t.Fatalf("%s got %d Responses; want %d", testMethod, len(c.GotSourceAppListResponseMap), len(c.WantSourceAppListResponseMap))
	}

	for appSourceName, gotObjects := range c.GotSourceAppListResponseMap {
		wantObjects := c.WantSourceAppListResponseMap[appSourceName]
		checkS3Response(t, testMethod, gotObjects.Objects, wantObjects.Objects, appSourceName)
	}
}

// ListObjects is a mock call to minio client ListObjects
func (mockClient MockMinioS3Client) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	objectCh := make(chan minio.ObjectInfo, 1)

	go func(objectStatCh chan<- minio.ObjectInfo) {
		defer close(objectCh)
		if len(mockClient.Objects) == 0 {
			objectCh <- minio.ObjectInfo{
				Err: fmt.Errorf("empty objects list"),
			}
			return
		}
		for _, obj := range mockClient.Objects {

			object := minio.ObjectInfo{
				ETag:         *obj.Etag,
				Key:          *obj.Key,
				LastModified: *obj.LastModified,
				Size:         *obj.Size,
				StorageClass: *obj.StorageClass,
			}
			objectCh <- object
		}
		return
	}(objectCh)

	return objectCh
}

// FGetObject is a mock call to download file/app from minio client.
// It just does some error checking.
func (mockClient MockMinioS3Client) FGetObject(ctx context.Context, bucketName string, remoteFileName string, localFileName string, opts minio.GetObjectOptions) error {

	var err error
	headers := opts.Header()
	etag := headers[http.CanonicalHeaderKey("If-Match")]
	if remoteFileName == "" || localFileName == "" || etag[0] == "" {
		err = fmt.Errorf("empty remoteFileName/localFileName/etag. remoteFileName=%s, localFileName=%s, etag=%s", remoteFileName, localFileName, etag)
	}
	return err
}
