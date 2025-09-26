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
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// MockAWSS3ClientError is used to store all the objects for an app source
type MockAWSS3ClientError struct {
	Objects []*MockRemoteDataObject
}

// MockAWSS3Client is used to store all the objects for an app source
type MockAWSS3Client struct {
	Objects []*MockRemoteDataObject
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

// CheckAWSRemoteDataListResponse checks if the received objects are same as the one we expect
func (c *MockAWSS3Handler) CheckAWSRemoteDataListResponse(t *testing.T, testMethod string) {
	if len(c.WantSourceAppListResponseMap) != len(c.GotSourceAppListResponseMap) {
		t.Fatalf("%s got %d Responses; want %d", testMethod, len(c.GotSourceAppListResponseMap), len(c.WantSourceAppListResponseMap))
	}

	for appSourceName, gotObjects := range c.GotSourceAppListResponseMap {
		wantObjects := c.WantSourceAppListResponseMap[appSourceName]
		checkRemoteDataListResponse(t, testMethod, gotObjects.Objects, wantObjects.Objects, appSourceName)
	}
}

// ListObjectsV2 is a mock call to ListObjectsV2
func (mockClient MockAWSS3Client) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, options ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
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

// GetObject is a mock call to GetObject
func (mockClient MockAWSS3Client) GetObject(ctx context.Context, input *s3.GetObjectInput, options ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	output := &s3.GetObjectOutput{}

	tmp, err := json.Marshal(mockClient.Objects)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(tmp, &output.Body)
	if err != nil {
		return nil, err
	}

	return output, nil
}

// HeadObject is a mock call to HeadObject
func (mockClient MockAWSS3Client) HeadObject(ctx context.Context, input *s3.HeadObjectInput, options ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return &s3.HeadObjectOutput{
		ETag: aws.String(""),
	}, nil
}

// ListObjectsV2 is a mock call to ListObjectsV2
func (mockClient MockAWSS3ClientError) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, options ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &s3.ListObjectsV2Output{}, errors.New("Dummy Error")
}

// GetObject is a mock call to GetObject
func (mockClient MockAWSS3ClientError) GetObject(ctx context.Context, input *s3.GetObjectInput, options ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{}, errors.New("Dummy Error")
}

// HeadObject is a mock call to HeadObject
func (mockClient MockAWSS3ClientError) HeadObject(ctx context.Context, input *s3.HeadObjectInput, options ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return &s3.HeadObjectOutput{}, errors.New("dummy Error")
}

// MockAWSDownloadClient is mock aws client for download
type MockAWSDownloadClient struct{}

// Download is a mock call for aws sdk download api.
// It just does some error checking.
func (mockDownloadClient MockAWSDownloadClient) Download(ctx context.Context, w io.WriterAt, input *s3.GetObjectInput, options ...func(*manager.Downloader)) (size int64, err error) {
	var bytes int64
	remoteFile := *input.Key
	localFile := w.(*os.File).Name()

	if remoteFile == "" || localFile == "" {
		err := fmt.Errorf("empty localFile/remoteFile/eTag. remoteFile=%s, localFile=%s", remoteFile, localFile)
		return bytes, err
	}

	return bytes, nil
}
