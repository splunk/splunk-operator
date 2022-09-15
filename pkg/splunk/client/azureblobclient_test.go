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
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestInitAzureBlobClientWrapper(t *testing.T) {
	ctx := context.TODO()
	azureBlobClientSession := InitAzureBlobClientWrapper(ctx, "https://mystorageaccount.blob.core.windows.net", "abcd", "1234")
	if azureBlobClientSession == nil {
		t.Errorf("We should not have got a nil Azure Blob Client")
	}
}

func TestNewAzureBlobClient(t *testing.T) {
	ctx := context.TODO()
	fn := InitAzureBlobClientWrapper

	azureBlobClient, err := NewAzureBlobClient(ctx, "sample_bucket", "abcd", "xyz", "admin/", "admin", "us-west-2", "https://mystorageaccount.blob.core.windows.net", fn)
	if azureBlobClient == nil || err != nil {
		t.Errorf("NewAzureBlobClient should have returned a valid Azure Blob client.")
	}
}

func TestAzureBlobGetAppsListShouldNotFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "azure_vol1",
			Scope:   enterpriseApi.ScopeLocal,
		},
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "azure_vol1",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "appscontainer1",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "azure",
			},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{
				Name:     "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "azure_vol1",
					Scope:   enterpriseApi.ScopeLocal,
				},
			},
		},
	}

	// Initialize clients
	azureBlobClient := &AzureBlobClient{}
	mclient := spltest.MockHTTPClient{}

	// Add handler for mock client(handles secrets case initially)
	wantRequest, _ := http.NewRequest("GET", "https://mystorageaccount.blob.core.windows.net/appscontainer1?prefix=adminAppsRepo&restype=container&comp=list&include=snapshots&include=metadata", nil)
	respdata := &EnumerationResults{
		Blobs: Blobs{
			Blob: []Blob{
				{
					Properties: ContainerProperties{
						CreationTime:  time.Now().UTC().Format(http.TimeFormat),
						LastModified:  time.Now().UTC().Format(http.TimeFormat),
						ETag:          "abcd",
						ContentLength: fmt.Sprint(64),
					},
				},
			},
		},
	}
	mrespdata, _ := xml.Marshal(respdata)
	mclient.AddHandler(wantRequest, 200, string(mrespdata), nil)

	// Get App source and volume from spec
	appSource := appFrameworkRef.AppSources[0]
	vol, err := GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get volume for app source : %s", appSource.Name)
	}

	// Update the GetRemoteDataClient function pointer
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAzureBlobClient)

	// Update the GetRemoteDataClientInit function pointer
	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		return &mclient
	}
	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	// Init azure blob client
	getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	azureBlobClient.HTTPClient = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234").(*spltest.MockHTTPClient)
	azureBlobClient.BucketName = vol.Path
	azureBlobClient.Prefix = appSource.Location
	azureBlobClient.Endpoint = vol.Endpoint

	// Test Listing apps with secrets
	azureBlobClient.StorageAccountName = vol.Path
	azureBlobClient.SecretAccessKey = "abcd"

	respList, err := azureBlobClient.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not return nil")
	}

	if len(respList.Objects) != 1 {
		t.Errorf("GetAppsList should have returned 1 blob object")
	}

	// Out of two blobs one has Incorrect last modified time so the
	// list should return only one blob
	respdata = &EnumerationResults{
		Blobs: Blobs{
			Blob: []Blob{
				{
					Properties: ContainerProperties{
						CreationTime:  time.Now().UTC().Format(http.TimeFormat),
						LastModified:  fmt.Sprint(time.Now()),
						ETag:          "etag1",
						ContentLength: fmt.Sprint(64),
					},
				},
				{
					Properties: ContainerProperties{
						CreationTime:  time.Now().UTC().Format(http.TimeFormat),
						LastModified:  time.Now().UTC().Format(http.TimeFormat),
						ETag:          "etag2",
						ContentLength: fmt.Sprint(64),
					},
				},
			},
		},
	}
	mrespdata, _ = xml.Marshal(respdata)
	mclient.AddHandler(wantRequest, 200, string(mrespdata), nil)
	// GetAppsList doesn't return error as we move onto the next blob
	resp, err := azureBlobClient.GetAppsList(ctx)

	if err != nil {
		t.Errorf("Did not expect error but one blob should have been returned")
	}

	//check only one blob is returned as it has correct lastmodified date

	if len(resp.Objects) != 1 {
		t.Errorf("Expected only one blob to be returned")
	}

	// Test Listing Apps with IAM
	azureBlobClient.StorageAccountName = ""
	azureBlobClient.SecretAccessKey = ""
	wantRequest, _ = http.NewRequest("GET", "http://169.254.169.254/metadata/identity/oauth2/token?api-version=2021-10-01&resource=https%3A%2F%2Fstorage.azure.com%2F", nil)
	respTokenData := &TokenResponse{
		AccessToken: "acctoken",
		ClientID:    "ClientId",
	}
	mrespdata, _ = json.Marshal(respTokenData)
	mclient.AddHandler(wantRequest, 200, string(mrespdata), nil)

	_, err = azureBlobClient.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not return nil")
	}

}

func TestAzureBlobGetAppsListShouldFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "azure_vol1",
			Scope:   enterpriseApi.ScopeLocal,
		},
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "azure_vol1",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "appscontainer1",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "azure",
			},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{
				Name:     "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "azure_vol1",
					Scope:   enterpriseApi.ScopeLocal,
				},
			},
		},
	}

	// Initialize clients
	azureBlobClient := &AzureBlobClient{}
	mclient := spltest.MockHTTPClient{}

	// Add handler for mock client(handles secrets case initially)
	wantRequest, _ := http.NewRequest("GET", "https://mystorageaccount.blob.core.windows.net/appscontainer1?prefix=adminAppsRepo&restype=container&comp=list&include=snapshots&include=metadata", nil)
	respdata := &EnumerationResults{
		Blobs: Blobs{
			Blob: []Blob{
				{
					Properties: ContainerProperties{
						CreationTime:  time.Now().UTC().Format(http.TimeFormat),
						LastModified:  time.Now().UTC().Format(http.TimeFormat),
						ETag:          "abcd",
						ContentLength: fmt.Sprint(64),
					},
				},
			},
		},
	}
	mrespdata, _ := xml.Marshal(respdata)
	mclient.AddHandler(wantRequest, 200, string(mrespdata), nil)

	// Get App source and volume from spec
	appSource := appFrameworkRef.AppSources[0]
	vol, err := GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get volume for app source : %s", appSource.Name)
	}

	// Update the GetRemoteDataClient function pointer
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAzureBlobClient)

	// Update the GetRemoteDataClientInit function pointer
	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		return &mclient
	}
	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	// Init azure blob client
	getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	azureBlobClient.HTTPClient = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234").(*spltest.MockHTTPClient)
	azureBlobClient.BucketName = vol.Path
	azureBlobClient.Prefix = appSource.Location

	// Test Listing apps with secrets but bad end point
	azureBlobClient.StorageAccountName = vol.Path
	azureBlobClient.SecretAccessKey = "abcd"
	azureBlobClient.Endpoint = "not-a-valid-end-point"
	_, err = azureBlobClient.GetAppsList(ctx)
	if err == nil {
		t.Errorf("Expected error for invalid endpoint")
	}
	azureBlobClient.Endpoint = vol.Endpoint
	// Test error conditions

	// Test error for Ouath request
	azureBlobClient.StorageAccountName = ""
	azureBlobClient.SecretAccessKey = ""

	_, err = azureBlobClient.GetAppsList(ctx)
	if err == nil {
		t.Errorf("Expected error for incorrect oauth request")
	}

	// Test error for get app list request
	mclient.AddHandler(wantRequest, 200, string(mrespdata), nil)
	_, err = azureBlobClient.GetAppsList(ctx)
	if err == nil {
		t.Errorf("Expected error for incorrect get apps list request")
	}

	// Test error for extract response
	wantRequest, _ = http.NewRequest("GET", "https://mystorageaccount.blob.core.windows.net/appscontainer1?prefix=adminAppsRepo&restype=container&comp=list&include=snapshots&include=metadata", nil)
	mclient.AddHandler(wantRequest, 200, string("FailToUnmarshal"), nil)
	_, err = azureBlobClient.GetAppsList(ctx)
	if err == nil {
		t.Errorf("Expected error for incorrect http response from get apps list, unable to unmarshal")
	}

}

func TestAzureBlobDownloadAppShouldNotFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "azure_vol1",
			Scope:   enterpriseApi.ScopeLocal,
		},
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "azure_vol1",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "appscontainer1",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "azure",
			},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{
				Name:     "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "azure_vol1",
					Scope:   enterpriseApi.ScopeLocal,
				},
			},
		},
	}

	// Initialize clients
	azureBlobClient := &AzureBlobClient{}
	mclient := spltest.MockHTTPClient{}

	// Add handler for mock client(handles secrets case initially)
	wantRequest, _ := http.NewRequest("GET", "https://mystorageaccount.blob.core.windows.net/appscontainer1/adminAppsRepo/app1.tgz", nil)
	respdata := "This is a test body of an app1.tgz package. In real use it would be a binary file but for test it is just a string data"

	mclient.AddHandler(wantRequest, 200, respdata, nil)

	// Get App source and volume from spec
	appSource := appFrameworkRef.AppSources[0]
	vol, err := GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get volume for app source : %s", appSource.Name)
	}

	// Update the GetRemoteDataClient function pointer
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAzureBlobClient)

	// Update the GetRemoteDataClientInit function pointer
	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		return &mclient
	}
	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	// Init azure blob client
	getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	azureBlobClient.HTTPClient = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234").(*spltest.MockHTTPClient)
	azureBlobClient.BucketName = vol.Path
	azureBlobClient.Prefix = appSource.Location
	azureBlobClient.Endpoint = vol.Endpoint

	// Test Download App package with secret
	azureBlobClient.StorageAccountName = vol.Path
	azureBlobClient.SecretAccessKey = "abcd"

	// Create RemoteDownload request
	downloadRequest := RemoteDataDownloadRequest{
		LocalFile:  "app1.tgz",
		RemoteFile: "adminAppsRepo/app1.tgz",
	}
	_, err = azureBlobClient.DownloadApp(ctx, downloadRequest)
	if err != nil {
		t.Errorf("DownloadApps should not return nil")
	}

	downloadedAppData, err := os.ReadFile(downloadRequest.LocalFile)
	if err != nil {
		t.Errorf("DownloadApps failed reading downloaded file. Error is: %s", err.Error())
	}

	if strings.Compare(respdata, string(downloadedAppData)) != 0 {
		t.Errorf("DownloadApps failed as it did not download correct data")
	}

	os.Remove(downloadRequest.LocalFile)

	// Test Download App package with IAM
	azureBlobClient.StorageAccountName = ""
	azureBlobClient.SecretAccessKey = ""
	wantRequest, _ = http.NewRequest("GET", "http://169.254.169.254/metadata/identity/oauth2/token?api-version=2021-10-01&resource=https%3A%2F%2Fstorage.azure.com%2F", nil)
	respTokenData := &TokenResponse{
		AccessToken: "acctoken",
		ClientID:    "ClientId",
	}
	mrespdata, _ := json.Marshal(respTokenData)
	mclient.AddHandler(wantRequest, 200, string(mrespdata), nil)

	_, err = azureBlobClient.DownloadApp(ctx, downloadRequest)
	if err != nil {
		t.Errorf("DownloadApps should not return nil")
	}

	if strings.Compare(respdata, string(downloadedAppData)) != 0 {
		t.Errorf("DownloadApps failed usign IAM as it did not download correct data")
	}

	os.Remove(downloadRequest.LocalFile)
}

func TestAzureBlobDownloadAppShouldFail(t *testing.T) {
	ctx := context.TODO()
	appFrameworkRef := enterpriseApi.AppFrameworkSpec{
		Defaults: enterpriseApi.AppSourceDefaultSpec{
			VolName: "azure_vol1",
			Scope:   enterpriseApi.ScopeLocal,
		},
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "azure_vol1",
				Endpoint:  "https://mystorageaccount.blob.core.windows.net",
				Path:      "appscontainer1",
				SecretRef: "blob-secret",
				Type:      "blob",
				Provider:  "azure",
			},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{
				Name:     "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "azure_vol1",
					Scope:   enterpriseApi.ScopeLocal,
				},
			},
		},
	}

	// Initialize clients
	azureBlobClient := &AzureBlobClient{}
	mclient := spltest.MockHTTPClient{}

	// Add handler for mock client(handles secrets case initially)
	wantRequest, _ := http.NewRequest("GET", "https://mystorageaccount.blob.core.windows.net/appscontainer1/adminAppsRepo/app1.tgz", nil)
	respdata := "This is a test body of an app1.tgz package. In real use it would be a binary file but for test it is just a string data"

	mclient.AddHandler(wantRequest, 200, respdata, nil)

	// Get App source and volume from spec
	appSource := appFrameworkRef.AppSources[0]
	vol, err := GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get volume for app source : %s", appSource.Name)
	}

	// Update the GetRemoteDataClient function pointer
	getClientWrapper := RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, NewMockAzureBlobClient)

	// Update the GetRemoteDataClientInit function pointer
	initFn := func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		return &mclient
	}
	getClientWrapper.SetRemoteDataClientInitFuncPtr(ctx, vol.Provider, initFn)

	// Init azure blob client
	getRemoteDataClientFn := getClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	azureBlobClient.HTTPClient = getRemoteDataClientFn(ctx, "us-west-2", "abcd", "1234").(*spltest.MockHTTPClient)
	azureBlobClient.BucketName = vol.Path
	azureBlobClient.Prefix = appSource.Location
	azureBlobClient.Endpoint = vol.Endpoint

	// Test Download App package with secret
	azureBlobClient.StorageAccountName = vol.Path
	azureBlobClient.SecretAccessKey = "abcd"

	// Create RemoteDownload request
	downloadRequest := RemoteDataDownloadRequest{
		LocalFile:  "app1.tgz",
		RemoteFile: "adminAppsRepo/app1.tgz",
	}

	// Test error conditions

	// Test error for http request to download
	azureBlobClient.Endpoint = "dummy"
	_, err = azureBlobClient.DownloadApp(ctx, downloadRequest)
	if err == nil {
		t.Errorf("Expected error for incorrect http request")
	}

	// Test invalid oauth request
	// Test Download App package with secret
	azureBlobClient.StorageAccountName = ""
	azureBlobClient.SecretAccessKey = ""
	mclient.RemoveHandlers()
	azureBlobClient.Endpoint = vol.Endpoint
	_, err = azureBlobClient.DownloadApp(ctx, downloadRequest)
	if err == nil {
		t.Errorf("Expected error for incorrect oauth request")
	}

	// Test empty local file
	downloadRequest.LocalFile = ""
	_, err = azureBlobClient.DownloadApp(ctx, downloadRequest)
	if err == nil {
		t.Errorf("Expected error for incorrect oauth request")
	}
}
