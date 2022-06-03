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

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NewMockAWSS3Client returns an AWS S3 mock client for testing
// Ideally this function should live in test package but due to
// dependency of some variables in client package and to avoid
// cyclic dependency this has to live here.
func NewMockAWSS3Client(ctx context.Context, bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string, region string, endpoint string, fn GetInitFunc) (S3Client, error) {
	var s3SplunkClient SplunkAWSS3Client
	var err error

	cl := fn(ctx, region, accessKeyID, secretAccessKey)
	if cl == nil {
		err = fmt.Errorf("failed to create an AWS S3 client")
		return nil, err
	}

	s3SplunkClient = cl.(SplunkAWSS3Client)
	downloader := spltest.MockAWSDownloadClient{}

	return &AWSS3Client{
		Region:             region,
		BucketName:         bucketName,
		AWSAccessKeyID:     accessKeyID,
		AWSSecretAccessKey: secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Client:             s3SplunkClient,
		Downloader:         downloader,
	}, nil
}

// NewMockMinioS3Client is mock client for testing minio client
func NewMockMinioS3Client(ctx context.Context, bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string, region string, endpoint string, fn GetInitFunc) (S3Client, error) {
	var s3SplunkClient SplunkMinioClient
	var err error

	cl := fn(ctx, endpoint, accessKeyID, secretAccessKey)
	if cl == nil {
		err = fmt.Errorf("failed to create an AWS S3 client")
		return nil, err
	}

	s3SplunkClient = cl.(SplunkMinioClient)

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

// ConvertS3Response converts S3 Response to a mock client response
func ConvertS3Response(ctx context.Context, s3Response S3Response) (spltest.MockS3Client, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ConvertS3Response")

	var mockResponse spltest.MockS3Client

	tmp, err := json.Marshal(s3Response)
	if err != nil {
		scopedLog.Error(err, "Unable to marshal s3 response")
		return mockResponse, err
	}

	err = json.Unmarshal(tmp, &mockResponse)
	if err != nil {
		scopedLog.Error(err, "Unable to unmarshal s3 response")
		return mockResponse, err
	}

	return mockResponse, err
}

// CheckIfVolumeExists checks if the volume is configured or not
func CheckIfVolumeExists(volumeList []enterpriseApi.VolumeSpec, volName string) (int, error) {
	for i, volume := range volumeList {
		if volume.Name == volName {
			return i, nil
		}
	}

	return -1, fmt.Errorf("volume: %s, doesn't exist", volName)
}

// GetAppSrcVolume gets the volume defintion for an app source
func GetAppSrcVolume(ctx context.Context, appSource enterpriseApi.AppSourceSpec, appFrameworkRef *enterpriseApi.AppFrameworkSpec) (enterpriseApi.VolumeSpec, error) {
	var volName string
	var index int
	var err error
	var vol enterpriseApi.VolumeSpec

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetAppSrcVolume")

	// get the volume spec from the volume name
	if appSource.VolName != "" {
		volName = appSource.VolName
	} else {
		volName = appFrameworkRef.Defaults.VolName
	}

	index, err = CheckIfVolumeExists(appFrameworkRef.VolList, volName)
	if err != nil {
		scopedLog.Error(err, "Invalid volume name provided. Please specify a valid volume name.", "App source", appSource.Name, "Volume name", volName)
		return vol, err
	}

	vol = appFrameworkRef.VolList[index]
	return vol, err
}
