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

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
)

// MockAzureBlobClient is used to store all the objects for an app source
type MockAzureBlobClient struct {
	Objects []*MockS3Object
}

// MockAzureBlobHandler is used for checking response received
type MockAzureBlobHandler struct {
	WantSourceAppListResponseMap map[string]MockAzureBlobClient
	GotSourceAppListResponseMap  map[string]MockAzureBlobClient
}

// AddObjects adds mock AWS S3 Objects to handler
func (c *MockAzureBlobHandler) AddObjects(appFrameworkRef enterpriseApi.AppFrameworkSpec, objects ...MockAzureBlobClient) {
	for n := range objects {
		mockAzureBlobClient := objects[n]
		appSource := appFrameworkRef.AppSources[n]
		if c.WantSourceAppListResponseMap == nil {
			c.WantSourceAppListResponseMap = make(map[string]MockAzureBlobClient)
		}
		c.WantSourceAppListResponseMap[appSource.Name] = mockAzureBlobClient
	}
}

// CheckAzureBlobRemoteDataListResponse checks if the received objects are same as the one we expect
func (c *MockAzureBlobHandler) CheckAzureBlobRemoteDataListResponse(t *testing.T, testMethod string) {

	if len(c.WantSourceAppListResponseMap) != len(c.GotSourceAppListResponseMap) {
		t.Fatalf("%s got %d Responses; want %d", testMethod, len(c.GotSourceAppListResponseMap), len(c.WantSourceAppListResponseMap))
	}

	for appSourceName, gotObjects := range c.GotSourceAppListResponseMap {
		wantObjects := c.WantSourceAppListResponseMap[appSourceName]
		checkRemoteDataListResponse(t, testMethod, gotObjects.Objects, wantObjects.Objects, appSourceName)
	}
}

//TODO : refine this method in next sprint as part of list and download azure rest apis
func (mockClient MockAzureBlobClient) ListApps(ctx context.Context, bucketName string, listAppsOpts map[string]string) ([]byte, error) {

	tmp, err := json.Marshal(mockClient.Objects)
	if err != nil {
		return tmp, err
	}

	return tmp, nil
}

//DownloadApp is a mock call to download file/app from azure blob client.
//It just does some error checking.
//TODO : refine this method in next sprint as part of list and download azure rest apis
func (mockClient MockAzureBlobClient) DownloadApp(ctx context.Context, bucketName string, remoteFileName string, localFileName string, downloadOpts map[string]string) error {

	var err error

	if remoteFileName == "" || localFileName == "" {
		err = fmt.Errorf("empty remoteFileName/localFileName. remoteFileName=%s, localFileName=%s", remoteFileName, localFileName)
	}
	return err
}
