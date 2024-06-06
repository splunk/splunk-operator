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
	"crypto/tls"
	"net/http"
	"os"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestInitAWSClientWrapper(t *testing.T) {
	ctx := context.TODO()
	awsS3ClientSession := InitAWSClientWrapper(ctx, "us-west-2|https://s3.amazon.com", "abcd", "1234", false)
	if awsS3ClientSession == nil {
		t.Errorf("We should have got a valid AWS S3 client session object")
	}

	awsS3ClientSession = InitAWSClientWrapper(ctx, "us-west-2|https://s3.amazon.com", "", "", false)
	if awsS3ClientSession == nil {
		t.Errorf("Case: Invalid secret/access keys, still returns a session")
	}

	awsS3ClientSession = InitAWSClientWrapper(ctx, "us-west-2", "", "", false)
	if awsS3ClientSession != nil {
		t.Errorf("Endpoint not resolved, should receive a nil session")
	}

	// Invalid session test
	os.Setenv("AWS_STS_REGIONAL_ENDPOINTS", "abcde")
	awsS3ClientSession = InitAWSClientWrapper(ctx, "us-west-2|https://s3.amazon.com", "abcd", "1234", false)
	os.Unsetenv("AWS_STS_REGIONAL_ENDPOINTS")
}

func TestGetTLSVersion(t *testing.T) {
	tr := http.Transport{
		TLSClientConfig: &tls.Config{},
	}

	versions := []uint16{
		tls.VersionTLS10,
		tls.VersionTLS11,
		tls.VersionTLS12,
		tls.VersionTLS13,
		14,
	}

	for _, val := range versions {
		tr.TLSClientConfig.MinVersion = val
		getTLSVersion(&tr)
	}
}
func TestNewAWSS3Client(t *testing.T) {
	ctx := context.TODO()
	fn := InitAWSClientWrapper
	awsS3Client, err := NewAWSS3Client(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "https://s3.us-west-2.amazonaws.com", false, fn)
	if awsS3Client == nil || err != nil {
		t.Errorf("NewAWSS3Client should have returned a valid AWS S3 client.")
	}

	// just test the backward compatibility where we do not pass a region explicitly
	awsS3Client, err = NewAWSS3Client(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "", "https://s3.us-west-2.amazonaws.com", false, fn)
	if awsS3Client == nil || err != nil {
		t.Errorf("NewAWSS3Client should have returned a valid AWS S3 client.")
	}

	// test the invalid scenario where we cannot extract region from endpoint
	awsS3Client, err = NewAWSS3Client(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "", "https://s3.us-west-2.dummyprovider.com", false, fn)
	if awsS3Client != nil || err == nil {
		t.Errorf("NewAWSS3Client should have returned a valid AWS S3 client.")
	}

	// Test for invalid scenario, where we return nil client
	fn = func(context.Context, string, string, string, bool) interface{} {
		return nil
	}
	_, err = NewAWSS3Client(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "https://s3.us-west-2.amazonaws.com", false, fn)
	if err == nil {
		t.Errorf("NewAWSS3Client should have returned error.")
	}

	// Test for invalid scenario, where we return invalid client
	fn = func(context.Context, string, string, string, bool) interface{} {
		return "abcd"
	}
	_, err = NewAWSS3Client(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "https://s3.us-west-2.amazonaws.com", false, fn)
	if err == nil {
		t.Errorf("NewAWSS3Client should have returned error.")
	}
}

func TestAWSGetAppsListShouldNotFail(t *testing.T) {
	ctx := context.TODO()

	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "msos_s2s3_vol2",
			Scope:   enterpriseApi.ScopeLocal,
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
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "securityApps",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
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
			Objects: []*spltest.MockRemoteDataObject{
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
			Objects: []*spltest.MockRemoteDataObject{
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
			Objects: []*spltest.MockRemoteDataObject{
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

		vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
		getClientWrapper := RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAWSS3Client)

		initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
			cl := spltest.MockAWSS3Client{}
			cl.Objects = mockAwsObjects[index].Objects
			return cl
		}

		getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

		getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
		awsClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockAWSS3Client)

		RemoteDataListResponse, err := awsClient.GetAppsList(ctx)
		if err != nil {
			allSuccess = false
			continue
		}

		var mockResponse spltest.MockRemoteDataClient
		mockResponse, err = ConvertRemoteDataListResponse(ctx, RemoteDataListResponse)
		if err != nil {
			allSuccess = false
			continue
		}

		if mockAwsHandler.GotSourceAppListResponseMap == nil {
			mockAwsHandler.GotSourceAppListResponseMap = make(map[string]spltest.MockAWSS3Client)
		}

		mockAwsHandler.GotSourceAppListResponseMap[appSource.Name] = spltest.MockAWSS3Client(mockResponse)
	}

	if allSuccess == false {
		t.Errorf("Unable to get apps list for all the app sources")
	}
	method := "GetAppsList"
	mockAwsHandler.CheckAWSRemoteDataListResponse(t, method)
}

func TestAWSGetAppsListShouldFail(t *testing.T) {
	ctx := context.TODO()

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
					Scope:   enterpriseApi.ScopeLocal},
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
			Objects: []*spltest.MockRemoteDataObject{
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

	vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAWSS3Client)

	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
		cl := spltest.MockAWSS3Client{}
		// return empty objects list here to test the negative scenario
		return cl
	}

	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	awsClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockAWSS3Client)

	remoteDataClientResponse, err := awsClient.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed")
	}
	if len(remoteDataClientResponse.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty list in response")
	}

	// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAWSS3Client)
	initFn = func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
		cl := spltest.MockAWSS3ClientError{}
		// return empty objects list here to test the negative scenario
		return cl
	}

	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)
	getRemoteDataClientFn = getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	awsClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockAWSS3ClientError)

	remoteDataClientResponse, err = awsClient.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error")
	}
}

func TestAWSDownloadAppShouldNotFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "msos_s2s3_vol2",
			Scope:   enterpriseApi.ScopeLocal,
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
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "securityApps",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "authenticationApps",
				Location: "authenticationAppsRepo",
			},
		},
	}

	awsClient := &AWSS3Client{
		Downloader: spltest.MockAWSDownloadClient{},
	}

	RemoteFiles := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	LocalFiles := []string{"/tmp/admin_app.tgz", "/tmp/security_app.tgz", "/tmp/authentication_app.tgz"}
	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}

	mockAwsDownloadHandler := spltest.MockRemoteDataClientDownloadHandler{}

	mockAwsDownloadObjects := []spltest.MockRemoteDataClientDownloadClient{
		{
			RemoteFile:      RemoteFiles[0],
			DownloadSuccess: true,
		},

		{
			RemoteFile:      RemoteFiles[1],
			DownloadSuccess: true,
		},

		{
			RemoteFile:      RemoteFiles[2],
			DownloadSuccess: true,
		},
	}

	mockAwsDownloadHandler.AddObjects(LocalFiles, mockAwsDownloadObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error

	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			t.Errorf("Unable to get volume for app source : %s", appSource.Name)
		}

		// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
		getClientWrapper := RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAWSS3Client)

		initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
			cl := spltest.MockAWSS3Client{}
			return cl
		}

		getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

		getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)

		awsClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockAWSS3Client)

		downloadRequest := RemoteDataDownloadRequest{
			LocalFile:  LocalFiles[index],
			RemoteFile: RemoteFiles[index],
			Etag:       Etags[index],
		}
		downloadSuccess, err := awsClient.DownloadApp(ctx, downloadRequest)
		if err != nil {
			t.Errorf("Unable to download app: %s", RemoteFiles[index])
		}

		mockDownloadObject := spltest.MockRemoteDataClientDownloadClient{
			RemoteFile:      RemoteFiles[index],
			DownloadSuccess: downloadSuccess,
		}

		if mockAwsDownloadHandler.GotLocalToRemoteFileMap == nil {
			mockAwsDownloadHandler.GotLocalToRemoteFileMap = make(map[string]spltest.MockRemoteDataClientDownloadClient)
		}

		mockAwsDownloadHandler.GotLocalToRemoteFileMap[LocalFiles[index]] = mockDownloadObject
	}

	method := "DownloadApp"
	mockAwsDownloadHandler.CheckRemDataClntDownloadResponse(t, method)
}

func TestAWSDownloadAppShouldFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "msos_s2s3_vol2",
			Scope:   enterpriseApi.ScopeLocal,
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
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}

	awsClient := &AWSS3Client{
		Downloader: spltest.MockAWSDownloadClient{},
	}

	RemoteFile := ""
	LocalFile := []string{""}
	Etag := ""

	var vol enterpriseApi.VolumeSpec
	var err error

	appSource := appFrameworkRef.AppSources[0]

	vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get volume for app source : %s", appSource.Name)
	}

	// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAWSS3Client)

	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
		cl := spltest.MockAWSS3Client{}
		return cl
	}

	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)

	awsClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockAWSS3Client)

	downloadRequest := RemoteDataDownloadRequest{
		LocalFile:  LocalFile[0],
		RemoteFile: RemoteFile,
		Etag:       Etag,
	}
	_, err = awsClient.DownloadApp(ctx, downloadRequest)
	if err == nil {
		t.Errorf("DownloadApp should have returned error since both remoteFile and localFile names are empty")
	}

	// Now make the localFile name non-empty string
	LocalFile[0] = "randomFile"
	downloadRequest = RemoteDataDownloadRequest{
		LocalFile:  LocalFile[0],
		RemoteFile: RemoteFile,
		Etag:       Etag,
	}
	_, err = awsClient.DownloadApp(ctx, downloadRequest)
	os.Remove(LocalFile[0])
	if err == nil {
		t.Errorf("DownloadApp should have returned error since remoteFile name is empty")
	}
}
