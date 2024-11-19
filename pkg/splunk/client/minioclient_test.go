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
	"os"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestInitMinioClientWrapper(t *testing.T) {
	ctx := context.TODO()
	minioS3ClientSession := InitMinioClientWrapper(ctx, "https://s3.us-east-1.amazonaws.com", "abcd", "1234", false)
	if minioS3ClientSession == nil {
		t.Errorf("We should have got a valid Minio S3 client object")
	}

	// Test case without access and secret keys
	minioS3ClientSession = InitMinioClientWrapper(ctx, "https://s3.us-east-1.amazonaws.com", "", "", false)
	if minioS3ClientSession == nil {
		t.Errorf("We should have got a valid Minio S3 client object")
	}

	// Test erroneous minio session
	minioS3ClientSession = InitMinioClientWrapper(ctx, "https://s3.us-east-1.amazonaws.com:-9000", "", "", false)
	if minioS3ClientSession != nil {
		t.Errorf("Should have gotten a nil session due to error in URL")
	}
}

func TestNewMinioClient(t *testing.T) {
	ctx := context.TODO()
	fn := InitMinioClientWrapper

	// Test1. Test for endpoint with https
	minioS3Client, err := NewMinioClient(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "https://s3.us-west-2.amazonaws.com", false, fn)

	if minioS3Client == nil || err != nil {
		t.Errorf("NewMinioClient should have returned a valid Minio S3 client.")
	}

	// Test2. Test for endpoint with http
	minioS3Client, err = NewMinioClient(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "http://s3.us-west-2.amazonaws.com", false, fn)

	if minioS3Client == nil || err != nil {
		t.Errorf("NewMinioClient should have returned a valid Minio S3 client.")
	}

	// Test3. Test for invalid endpoint
	minioS3Client, err = NewMinioClient(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "random-endpoint.com", false, fn)

	if minioS3Client != nil || err == nil {
		t.Errorf("NewMinioClient should have returned a error.")
	}
}

func TestMinioGetAppsListShouldNotFail(t *testing.T) {
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
				Provider:  "minio",
			},
			{
				Name:      "msos_s2s3_vol2",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london2",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "minio",
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

	minioClient := &MinioClient{}

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}
	Keys := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	Sizes := []int64{10, 20, 30}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockMinioHandler := spltest.MockMinioS3Handler{}

	mockMinioObjects := []spltest.MockMinioS3Client{
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

	mockMinioHandler.AddObjects(appFrameworkRef, mockMinioObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error
	var allSuccess bool = true
	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetRemoteDataClient with our mock call which initializes mock minio client
		getClientWrapper := RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockMinioS3Client)

		initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
			cl := spltest.MockMinioS3Client{}
			cl.Objects = mockMinioObjects[index].Objects
			return cl
		}

		getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

		getS3ClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
		minioClient.Client = getS3ClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockMinioS3Client)

		RemoteDataListResponse, err := minioClient.GetAppsList(ctx)
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

		if mockMinioHandler.GotSourceAppListResponseMap == nil {
			mockMinioHandler.GotSourceAppListResponseMap = make(map[string]spltest.MockMinioS3Client)
		}

		mockMinioHandler.GotSourceAppListResponseMap[appSource.Name] = spltest.MockMinioS3Client(mockResponse)
	}

	if allSuccess == false {
		t.Errorf("Unable to get apps list for all the app sources")
	}

	method := "GetAppsList"
	mockMinioHandler.CheckMinioRemoteDataListResponse(t, method)
}

func TestMinioGetAppsListShouldFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "minio"},
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

	minioClient := &MinioClient{}

	Etag := "cc707187b036405f095a8ebb43a782c1"
	Key := "admin_app.tgz"
	Size := int64(10)
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockMinioHandler := spltest.MockMinioS3Handler{}

	mockMinioObjects := []spltest.MockMinioS3Client{
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

	mockMinioHandler.AddObjects(appFrameworkRef, mockMinioObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error

	appSource := appFrameworkRef.AppSources[0]

	vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetRemoteDataClient with our mock call which initializes mock minio client
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockMinioS3Client)

	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
		cl := spltest.MockMinioS3Client{}
		// return empty objects list here to test the negative scenario
		return cl
	}

	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	getS3ClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	minioClient.Client = getS3ClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockMinioS3Client)

	_, err = minioClient.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error since we have empty objects in the response")
	}
}

func TestMinioDownloadAppShouldNotFail(t *testing.T) {
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
				Provider:  "minio",
			},
			{
				Name:      "msos_s2s3_vol2",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london2",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "minio",
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

	minioClient := &MinioClient{}

	RemoteFiles := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	LocalFiles := []string{"/tmp/admin_app.tgz", "/tmp/security_app.tgz", "/tmp/authentication_app.tgz"}
	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}

	mockMinioDownloadHandler := spltest.MockRemoteDataClientDownloadHandler{}

	mockMinioDownloadObjects := []spltest.MockRemoteDataClientDownloadClient{
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

	mockMinioDownloadHandler.AddObjects(LocalFiles, mockMinioDownloadObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error

	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			t.Errorf("Unable to get volume for app source : %s", appSource.Name)
		}

		// Update the GetRemoteDataClient with our mock call which initializes mock minio client
		getClientWrapper := RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockMinioS3Client)

		initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
			cl := spltest.MockMinioS3Client{}
			return cl
		}

		getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

		getS3ClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)

		minioClient.Client = getS3ClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockMinioS3Client)

		downloadRequest := RemoteDataDownloadRequest{
			LocalFile:  LocalFiles[index],
			RemoteFile: RemoteFiles[index],
			Etag:       Etags[index],
		}

		downloadSuccess, err := minioClient.DownloadApp(ctx, downloadRequest)
		if err != nil {
			t.Errorf("Unable to download app: %s", RemoteFiles[index])
		}

		mockDownloadObject := spltest.MockRemoteDataClientDownloadClient{
			RemoteFile:      RemoteFiles[index],
			DownloadSuccess: downloadSuccess,
		}

		if mockMinioDownloadHandler.GotLocalToRemoteFileMap == nil {
			mockMinioDownloadHandler.GotLocalToRemoteFileMap = make(map[string]spltest.MockRemoteDataClientDownloadClient)
		}

		mockMinioDownloadHandler.GotLocalToRemoteFileMap[LocalFiles[index]] = mockDownloadObject
	}

	method := "DownloadApp"
	mockMinioDownloadHandler.CheckRemDataClntDownloadResponse(t, method)
}

func TestMinioDownloadAppShouldFail(t *testing.T) {
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
				Provider:  "minio",
			},
			{
				Name:      "msos_s2s3_vol2",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london2",
				SecretRef: "s3-secret",
				Type:      "s3",
				Provider:  "minio",
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

	minioClient := &MinioClient{}

	RemoteFile := ""
	Etag := ""
	LocalFile := []string{""}

	var vol enterpriseApi.VolumeSpec
	var err error

	appSource := appFrameworkRef.AppSources[0]

	vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get volume for app source : %s", appSource.Name)
	}

	// Update the GetRemoteDataClient with our mock call which initializes mock minio client
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockMinioS3Client)

	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string, pathStyleUrl bool) interface{} {
		cl := spltest.MockMinioS3Client{}
		return cl
	}

	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	getS3ClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)

	minioClient.Client = getS3ClientFn(ctx, "us-west-2", "abcd", "1234", false).(spltest.MockMinioS3Client)

	downloadRequest := RemoteDataDownloadRequest{
		LocalFile:  LocalFile[0],
		RemoteFile: RemoteFile,
		Etag:       Etag,
	}
	_, err = minioClient.DownloadApp(ctx, downloadRequest)
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
	_, err = minioClient.DownloadApp(ctx, downloadRequest)
	os.Remove(LocalFile[0])
	if err == nil {
		t.Errorf("DownloadApp should have returned error since remoteFile name is empty")
	}
}
