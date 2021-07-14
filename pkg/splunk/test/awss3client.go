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
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/s3"
	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
)

// MockAWSS3Object struct contains contents returned as part of S3 response
type MockAWSS3Object struct {
	Etag         *string
	Key          *string
	LastModified *time.Time
	Size         *int64
	StorageClass *string
}

// MockAWSS3Client is used to store all the objects for an app source
type MockAWSS3Client struct {
	Objects []*MockAWSS3Object
}

// MockAWSS3Handler is used for checking response received
type MockAWSS3Handler struct {
	WantSourceAppListResponseMap map[string]MockAWSS3Client
	GotSourceAppListResponseMap  map[string]MockAWSS3Client
}

// AddObjects adds mock AWS S3 Objects to handler
func (c *MockAWSS3Handler) AddObjects(appFrameworkRef enterpriseApi.AppFrameworkSpec, objects ...MockAWSS3Client) {
	for n := range objects {
		mockAWSS3Client := objects[n]
		appSource := appFrameworkRef.AppSources[n]
		if c.WantSourceAppListResponseMap == nil {
			c.WantSourceAppListResponseMap = make(map[string]MockAWSS3Client)
		}
		c.WantSourceAppListResponseMap[appSource.Name] = mockAWSS3Client
	}
}

// CheckAWSS3Response checks if the received objects are same as the one we expect
func (c *MockAWSS3Handler) CheckAWSS3Response(t *testing.T, testMethod string) {
	if len(c.WantSourceAppListResponseMap) != len(c.GotSourceAppListResponseMap) {
		t.Fatalf("%s got %d Responses; want %d", testMethod, len(c.GotSourceAppListResponseMap), len(c.WantSourceAppListResponseMap))
	}
	for appSourceName, gotObjects := range c.GotSourceAppListResponseMap {
		wantObjects := c.WantSourceAppListResponseMap[appSourceName]
		if !reflect.DeepEqual(gotObjects.Objects, wantObjects.Objects) {
			for n, gotObject := range gotObjects.Objects {
				if *gotObject.Etag != *wantObjects.Objects[n].Etag {
					t.Errorf("%s GotResponse[%s] Etag=%s; want %s", testMethod, appSourceName, *gotObject.Etag, *wantObjects.Objects[n].Etag)
				}
				if *gotObject.Key != *wantObjects.Objects[n].Key {
					t.Errorf("%s GotResponse[%s] Key=%s; want %s", testMethod, appSourceName, *gotObject.Key, *wantObjects.Objects[n].Key)
				}
				if *gotObject.StorageClass != *wantObjects.Objects[n].StorageClass {
					t.Errorf("%s GotResponse[%s] StorageClass=%s; want %s", testMethod, appSourceName, *gotObject.StorageClass, *wantObjects.Objects[n].StorageClass)
				}
				if *gotObject.Size != *wantObjects.Objects[n].Size {
					t.Errorf("%s GotResponse[%s] Size=%d; want %d", testMethod, appSourceName, *gotObject.Size, *wantObjects.Objects[n].Size)
				}
				if *gotObject.LastModified != *wantObjects.Objects[n].LastModified {
					t.Errorf("%s GotResponse[%s] LastModified=%s; want %s", testMethod, appSourceName, gotObject.LastModified.String(), wantObjects.Objects[n].LastModified.String())
				}
			}
		}
	}
}

// ListObjectsV2 is a mock call to ListObjectsV2
func (mockClient MockAWSS3Client) ListObjectsV2(options *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
	output := &s3.ListObjectsV2Output{}

	tmp, err := json.Marshal(mockClient.Objects)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(tmp, &output.Contents)
	if err != nil {
		return nil, err
	}

	return output, nil
}
