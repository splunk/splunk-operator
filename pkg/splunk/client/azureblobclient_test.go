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

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestInitAzureBlobClientWrapper(t *testing.T) {
	ctx := context.TODO()
	azureBlobClientSession := InitAzureBlobClientWrapper(ctx, "https://mystorageaccount.blob.core.windows.net", "abcd", "1234")
	if azureBlobClientSession == nil {
		t.Errorf("We should have got a valid Azure Blob client object")
	}
}

func TestNewAzureBlobClient(t *testing.T) {
	ctx := context.TODO()
	fn := InitAzureBlobClientWrapper

	// Test1. Test for endpoint with https
	azureBlobClient, err := NewAzureBlobClient(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "https://mystorageaccount.blob.core.windows.net", fn)

	if azureBlobClient == nil || err != nil {
		t.Errorf("NewAzureBlobClient should have returned a valid Azure Blob client.")
	}

	// Test2. Test for endpoint with http
	azureBlobClient, err = NewAzureBlobClient(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "http://mystorageaccount.blob.core.windows.net", fn)

	if azureBlobClient == nil || err != nil {
		t.Errorf("NewAzureBlobClient should have returned a valid Azure Blob client.")
	}

	// Test3. Test for invalid endpoint
	//TODO : revisit and fix it for next sprint
	// azureBlobClient, err = NewAzureBlobClient(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "random-endpoint.com", fn)

	// if azureBlobClient != nil || err == nil {
	// 	t.Errorf("NewAzureBlobClient should have returned a error.")
	// }
}

func TestAzureBlobGetAppsListShouldNotFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "msos_s2s3_vol2",
			Scope:   enterpriseApi.ScopeLocal,
		},
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "testbucket-rs-london",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "azure",
			},
			{
				Name:      "msos_s2s3_vol2",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "testbucket-rs-london2",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "azure",
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

	azureBlobClient := &AzureBlobClient{}

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}
	Keys := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	Sizes := []int64{10, 20, 30}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAzureBlobHandler := spltest.MockAzureBlobHandler{}

	mockAzureBlobObjects := []spltest.MockAzureBlobClient{
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

	mockAzureBlobHandler.AddObjects(appFrameworkRef, mockAzureBlobObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error
	var allSuccess bool = true
	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetRemoteDataClient with our mock call which initializes mock Azure Blob client
		getClientWrapper := RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAzureBlobClient)

		initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
			cl := spltest.MockAzureBlobClient{}
			cl.Objects = mockAzureBlobObjects[index].Objects
			return cl
		}

		getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

		getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
		azureBlobClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234").(spltest.MockAzureBlobClient)

		RemoteDataListResponse, err := azureBlobClient.GetAppsList(ctx)
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

		if mockAzureBlobHandler.GotSourceAppListResponseMap == nil {
			mockAzureBlobHandler.GotSourceAppListResponseMap = make(map[string]spltest.MockAzureBlobClient)
		}

		mockAzureBlobHandler.GotSourceAppListResponseMap[appSource.Name] = spltest.MockAzureBlobClient(mockResponse)

	}

	if allSuccess == false {
		t.Errorf("Unable to get apps list for all the app sources")
	}

	method := "GetAppsList"
	mockAzureBlobHandler.CheckAzureBlobRemoteDataListResponse(t, method)

}

func TestAzureBlobGetAppsListShouldFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "testbucket-rs-london",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "Azure"},
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

	azureBlobClient := &AzureBlobClient{}

	Etag := "cc707187b036405f095a8ebb43a782c1"
	Key := "admin_app.tgz"
	Size := int64(10)
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAzureBlobHandler := spltest.MockAzureBlobHandler{}

	mockAzureBlobObjects := []spltest.MockAzureBlobClient{
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

	mockAzureBlobHandler.AddObjects(appFrameworkRef, mockAzureBlobObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error

	appSource := appFrameworkRef.AppSources[0]

	vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetRemoteDataClient with our mock call which initializes mock Azure Blob client
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAzureBlobClient)

	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		cl := spltest.MockAzureBlobClient{}
		// return empty objects list here to test the negative scenario
		return cl
	}

	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	azureBlobClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234").(spltest.MockAzureBlobClient)
	//TODO: revisit after next sprint
	// _, err = azureBlobClient.GetAppsList(ctx)
	// if err == nil {
	// 	t.Errorf("GetAppsList should have returned error since we have empty objects in the response")
	// }

}

func TestAzureBlobDownloadAppShouldNotFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "msos_s2s3_vol2",
			Scope:   enterpriseApi.ScopeLocal,
		},
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "testbucket-rs-london",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "Azure",
			},
			{
				Name:      "msos_s2s3_vol2",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "testbucket-rs-london2",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "Azure",
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

	azureBlobClient := &AzureBlobClient{}

	RemoteFiles := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	LocalFiles := []string{"/tmp/admin_app.tgz", "/tmp/security_app.tgz", "/tmp/authentication_app.tgz"}
	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}

	mockAzureBlobDownloadHandler := spltest.MockRemDataClntDownloadHandler{}

	mockAzureBlobDownloadObjects := []spltest.MockRemoteDataClientDownloadClient{
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

	mockAzureBlobDownloadHandler.AddObjects(LocalFiles, mockAzureBlobDownloadObjects...)

	var vol enterpriseApi.VolumeSpec
	var err error

	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			t.Errorf("Unable to get volume for app source : %s", appSource.Name)
		}

		// Update the GetRemoteDataClient with our mock call which initializes mock Azure Blob client
		getClientWrapper := RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAzureBlobClient)

		initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
			cl := spltest.MockAzureBlobClient{}
			return cl
		}

		getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

		getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)

		azureBlobClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234").(spltest.MockAzureBlobClient)

		downloadRequest := RemoteDataDownloadRequest{
			LocalFile:  LocalFiles[index],
			RemoteFile: RemoteFiles[index],
			Etag:       Etags[index],
		}

		downloadSuccess, err := azureBlobClient.DownloadApp(ctx, downloadRequest)
		if err != nil {
			t.Errorf("Unable to download app: %s", RemoteFiles[index])
		}

		mockDownloadObject := spltest.MockRemoteDataClientDownloadClient{
			RemoteFile:      RemoteFiles[index],
			DownloadSuccess: downloadSuccess,
		}

		if mockAzureBlobDownloadHandler.GotLocalToRemoteFileMap == nil {
			mockAzureBlobDownloadHandler.GotLocalToRemoteFileMap = make(map[string]spltest.MockRemoteDataClientDownloadClient)
		}

		mockAzureBlobDownloadHandler.GotLocalToRemoteFileMap[LocalFiles[index]] = mockDownloadObject
	}

	method := "DownloadApp"
	mockAzureBlobDownloadHandler.CheckRemDataClntDownloadResponse(t, method)
}

func TestAzureBlobDownloadAppShouldFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "msos_s2s3_vol2",
			Scope:   enterpriseApi.ScopeLocal,
		},
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "testbucket-rs-london",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "Azure",
			},
			{
				Name:      "msos_s2s3_vol2",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "testbucket-rs-london2",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "Azure",
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

	azureBlobClient := &AzureBlobClient{}

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

	// Update the GetRemoteDataClient with our mock call which initializes mock Azure Blob client
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAzureBlobClient)

	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		cl := spltest.MockAzureBlobClient{}
		return cl
	}

	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)

	azureBlobClient.Client = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234").(spltest.MockAzureBlobClient)

	downloadRequest := RemoteDataDownloadRequest{
		LocalFile:  LocalFile[0],
		RemoteFile: RemoteFile,
		Etag:       Etag,
	}
	_, err = azureBlobClient.DownloadApp(ctx, downloadRequest)
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
	_, err = azureBlobClient.DownloadApp(ctx, downloadRequest)
	os.Remove(LocalFile[0])
	if err == nil {
		t.Errorf("DownloadApp should have returned error since remoteFile name is empty")
	}
}
