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
	"reflect"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestInitAWSClientWrapper(t *testing.T) {

	awsS3ClientSession := InitAWSClientWrapper("us-west-2", "abcd", "1234")
	if awsS3ClientSession == nil {
		t.Errorf("We should have got a valid AWS S3 client session object")
	}
}

func TestNewAWSS3Client(t *testing.T) {

	fn := InitAWSClientWrapper
	awsS3Client, err := NewAWSS3Client("sample_bucket", "abcd", "xyz", "admin/", "admin", "https://s3.us-west-2.amazonaws.com", fn)
	if awsS3Client == nil || err != nil {
		t.Errorf("NewAWSS3Client should have returned a valid AWS S3 client.")
	}

	// Test for invalid scenario, where we return nil client
	fn = func(string, string, string) interface{} {
		return nil
	}
	awsS3Client, err = NewAWSS3Client("sample_bucket", "abcd", "xyz", "admin/", "admin", "https://s3.us-west-2.amazonaws.com", fn)
	if err == nil {
		t.Errorf("NewAWSS3Client should have returned error.")
	}
}

func TestGetInitContainerImage(t *testing.T) {
	awsClient := &AWSS3Client{}

	if awsClient.GetInitContainerImage() != "amazon/aws-cli" {
		t.Errorf("Got invalid init container image for AWS client.")
	}
}

func TestGetAWSInitContainerCmd(t *testing.T) {
	wantCmd := []string{"--endpoint-url=https://s3.us-west-2.amazonaws.com", "s3", "sync", "s3://sample_bucket/admin/", "/mnt/apps-local/admin/"}

	awsClient := &AWSS3Client{}
	gotCmd := awsClient.GetInitContainerCmd("https://s3.us-west-2.amazonaws.com", "sample_bucket", "admin/", "admin", "/mnt/apps-local/")
	if !reflect.DeepEqual(wantCmd, gotCmd) {
		t.Errorf("Got incorrect Init container cmd")
	}
}

func TestGetAppsListShouldNotFail(t *testing.T) {

	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "msos_s2s3_vol2",
			Scope:   "local",
		},
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "aws",
			},
			{
				Name:      "msos_s2s3_vol2",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london2",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "aws",
			},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{Name: "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   "local"},
			},
			{Name: "securityApps",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   "local"},
			},
			{Name: "authenticationApps",
				Location: "authenticationAppsRepo",
			},
		},
	}

	awsClient := &AWSS3Client{}

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}
	Keys := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	Sizes := []int64{10, 20, 30}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAwsHandler := spltest.MockAWSS3Handler{}

	mockAwsObjects := []spltest.MockAWSS3Client{
		{
			Objects: []*spltest.MockAWSS3Object{
				{
					Etag:         &Etags[0],
					Key:          &Keys[0],
					LastModified: &randomTime,
					Size:         &Sizes[0],
					StorageClass: &StorageClass,
				},
			},
		},
		{
			Objects: []*spltest.MockAWSS3Object{
				{
					Etag:         &Etags[1],
					Key:          &Keys[1],
					LastModified: &randomTime,
					Size:         &Sizes[1],
					StorageClass: &StorageClass,
				},
			},
		},
		{
			Objects: []*spltest.MockAWSS3Object{
				{
					Etag:         &Etags[2],
					Key:          &Keys[2],
					LastModified: &randomTime,
					Size:         &Sizes[2],
					StorageClass: &StorageClass,
				},
			},
		},
	}

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error
	var allSuccess bool = true
	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = GetAppSrcVolume(appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetS3Client with our mock call which initializes mock AWS client
		getClientWrapper := S3Clients[vol.Provider]
		getClientWrapper.SetS3ClientFuncPtr(vol.Provider, NewMockAWSS3Client)

		initFn := func(region, accessKeyID, secretAccessKey string) interface{} {
			cl := spltest.MockAWSS3Client{}
			cl.Objects = mockAwsObjects[index].Objects
			return cl
		}

		getClientWrapper.SetS3ClientInitFuncPtr(vol.Name, initFn)

		getS3ClientFn := getClientWrapper.GetS3ClientInitFuncPtr()
		awsClient.Client = getS3ClientFn("us-west-2", "abcd", "1234").(spltest.MockAWSS3Client)

		s3Response, err := awsClient.GetAppsList()
		if err != nil {
			allSuccess = false
			continue
		}

		var mockResponse spltest.MockAWSS3Client
		mockResponse, err = ConvertS3Response(s3Response)
		if err != nil {
			allSuccess = false
			continue
		}

		if mockAwsHandler.GotSourceAppListResponseMap == nil {
			mockAwsHandler.GotSourceAppListResponseMap = make(map[string]spltest.MockAWSS3Client)
		}

		mockAwsHandler.GotSourceAppListResponseMap[appSource.Name] = mockResponse
	}

	if allSuccess == false {
		t.Errorf("Unable to get apps list for all the app sources")
	}
	method := "GetAppsList"
	mockAwsHandler.CheckAWSS3Response(t, method)
}

func TestGetAppsListShouldFail(t *testing.T) {

	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "aws"},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{Name: "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   "local"},
			},
		},
	}

	awsClient := &AWSS3Client{}

	Etag := "cc707187b036405f095a8ebb43a782c1"
	Key := "admin_app.tgz"
	Size := int64(10)
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAwsHandler := spltest.MockAWSS3Handler{}

	mockAwsObjects := []spltest.MockAWSS3Client{
		{
			Objects: []*spltest.MockAWSS3Object{
				{
					Etag:         &Etag,
					Key:          &Key,
					LastModified: &randomTime,
					Size:         &Size,
					StorageClass: &StorageClass,
				},
			},
		},
	}

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error

	appSource := appFrameworkRef.AppSources[0]

	vol, err = GetAppSrcVolume(appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetS3Client with our mock call which initializes mock AWS client
	getClientWrapper := S3Clients[vol.Provider]
	getClientWrapper.SetS3ClientFuncPtr(vol.Provider, NewMockAWSS3Client)

	initFn := func(region, accessKeyID, secretAccessKey string) interface{} {
		cl := spltest.MockAWSS3Client{}
		// return empty objects list here to test the negative scenario
		return cl
	}

	getClientWrapper.SetS3ClientInitFuncPtr(vol.Name, initFn)

	getS3ClientFn := getClientWrapper.GetS3ClientInitFuncPtr()
	awsClient.Client = getS3ClientFn("us-west-2", "abcd", "1234").(spltest.MockAWSS3Client)

	_, err = awsClient.GetAppsList()
	if err == nil {
		t.Errorf("GetAppsList should have returned error since we have empty objects in the response")
	}

}
