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
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
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

func TestAzureBlobGetAppsList(t *testing.T) {
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
			Blob: []MyBlob{
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
	vol, _ := GetAppSrcVolume(ctx, appSource, &appFrameworkRef)

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
	_, err := azureBlobClient.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not return nil")
	}

	// Test Listing Apps with IAM
	azureBlobClient.StorageAccountName = ""
	azureBlobClient.SecretAccessKey = ""
	wantRequest, _ = http.NewRequest("GET", "http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https%3A%2F%2Fstorage.azure.com%2F", nil)
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

	// Test error conditions

	// Test error for Ouath request
	mclient.RemoveHandlers()
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

	// Incorrect last modified time
	respdata = &EnumerationResults{
		Blobs: Blobs{
			Blob: []MyBlob{
				{
					Properties: ContainerProperties{
						CreationTime:  time.Now().UTC().Format(http.TimeFormat),
						LastModified:  fmt.Sprint(time.Now()),
						ETag:          "abcd",
						ContentLength: "",
					},
				},
			},
		},
	}
	mrespdata, _ = xml.Marshal(respdata)
	mclient.AddHandler(wantRequest, 200, string(mrespdata), nil)
	// GetAppsList doesn't return error as we move onto the next blob
	_, _ = azureBlobClient.GetAppsList(ctx)
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

/*
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
*/
