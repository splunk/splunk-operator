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
	"encoding/json"
	"fmt"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

// NewMockAWSS3Client returns an AWS S3 mock client for testing
// Ideally this function should live in test package but due to
// dependency of some variables in client package and to avoid
// cyclic dependency this has to live here.
func NewMockAWSS3Client(bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string, endpoint string, fn GetInitFunc) (S3Client, error) {
	var s3SplunkClient SplunkAWSS3Client
	var err error
	region := GetRegion(endpoint)

	cl := fn(region, accessKeyID, secretAccessKey)
	if cl == nil {
		err = fmt.Errorf("Failed to create an AWS S3 client")
		return nil, err
	}

	s3SplunkClient = cl.(SplunkAWSS3Client)

	return &AWSS3Client{
		Region:             region,
		BucketName:         bucketName,
		AWSAccessKeyID:     accessKeyID,
		AWSSecretAccessKey: secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Endpoint:           endpoint,
		Client:             s3SplunkClient,
	}, nil
}

// ConvertS3Response converts S3 Response to a mock client response
func ConvertS3Response(s3Response S3Response) (spltest.MockAWSS3Client, error) {
	scopedLog := log.WithName("ConvertS3Response")

	var mockResponse spltest.MockAWSS3Client

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
func CheckIfVolumeExists(volumeList []enterprisev1.VolumeSpec, volName string) (int, error) {
	for i, volume := range volumeList {
		if volume.Name == volName {
			return i, nil
		}
	}

	return -1, fmt.Errorf("Volume: %s, doesn't exist", volName)
}

// GetVolume gets the volume defintion for an app source
func GetVolume(appSource enterprisev1.AppSourceSpec, appFrameworkRef *enterprisev1.AppFrameworkSpec) (enterprisev1.VolumeSpec, error) {
	var volName string
	var index int
	var err error
	var vol enterprisev1.VolumeSpec

	scopedLog := log.WithName("GetVolume")

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
