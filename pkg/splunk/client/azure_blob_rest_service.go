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
)

// Get the list of apps from a given
// 1. storageaccount URL
// 2. container name (container in azure is same as bucket in aws)
// 3. path with the container name
// 4. Max number of apps to return in the call
// 5. Config object
// 	  Timeout - time in ms that a http rest call should wait before terminating
//    MaxRetries - number of retries for the rest call if it fails for first time.
//    http transport related parameters
// Output:
//  ListBlobOutput
//     RemoteDataClientResponse
//     ResponseNextMarker  if total apps in the bucket were more that max number of apps requested

// Download a given app with it's full path
// 1. URL of the app to be downloaded with full path including app name
// 2. Max number of bytes to return
// 3. Starting byte
// 5. Config object
// 	  Timeout - time in ms that a http rest call should wait before terminating
//    MaxRetries - number of retries for the rest call if it fails for first time.
//    http transport related parameters
// Output:
//  DownloadBlobOutput
//    Response Body
//    Response Code
//    Total Number of bytes returned

type azureRestService interface {
	NewSession(ctx context.Context, appAzureBlobEndPoint string, storageAccountName string, secretAccessKey string, config Config)
	ListApps(ctx context.Context, bucketName string, opts ListBlobsOptions)
	DownloadApp(ctx context.Context, bucketName string, remoteFile string, localFile string, opts DownloadOptions)
}

// NewSession ... : TODO: placeholder for next sprint
func NewSession(ctx context.Context, appAzureBlobEndPoint string, storageAccountName string, secretAccessKey string, config Config) {

}

// ListApps ... TODO: placeholder for next sprint
func ListApps(ctx context.Context, bucketName string, opts ListBlobsOptions) {
}

// DownloadApp ... TODO: placeholder for next sprint
func DownloadApp(ctx context.Context, bucketName string, remoteFile string, localFile string, opts DownloadOptions) {

}

// func (azureblobClient *AzureBlobClient) DownloadBlob(ctx context.Context, downloadRequest RemoteDataDownloadRequest) (bool, error) {
// 	reqLogger := log.FromContext(ctx)
// 	scopedLog := reqLogger.WithName("DownloadApp").WithValues("remoteFile", downloadRequest.RemoteFile, "localFile",
// 													downloadRequest.LocalFile, "etag", downloadRequest.Etag)
// 	// var numBytes int64
// 	// file, err := os.Create(localFile)
// 	// if err != nil {
// 	// 	scopedLog.Error(err, "Unable to open local file")
// 	// 	return false, err
// 	// }
// 	// defer file.Close()

// 	tokenFetchUrl := "http://169.254.169.254/metadata/identity/oauth2/token"

// 	httpclient := &http.Client{}

// 	request, err := http.NewRequest("GET", tokenFetchUrl, nil)
// 	if err != nil {
// 		scopedLog.Error(err, "Azure Blob Failed to create new token request")
// 	}
// 	request.Header.Set("Metadata", "true")
// 	values := request.URL.Query()

// 	values.Add("api-version", "2018-02-01")
// 	values.Add("resource", "https://storage.azure.com/")
// 	request.URL.RawQuery = values.Encode()

// 	resp, err := httpclient.Do(request)
// 	if err != nil {
// 		scopedLog.Error(err, "Azure blob,Errored when sending request to the server")
// 	}

// 	defer resp.Body.Close()
// 	responseBody, err := ioutil.ReadAll(resp.Body)
// 	if err != nil {
// 		scopedLog.Error(err, "Azure blob,Errored when reading resp body")
// 	}

// 	scopedLog.Info("Azure blob", "Resp Status", resp.Status)
// 	scopedLog.Info("Azure blob", "resp body", string(responseBody))

// 	//2022-07-11T19:36:59.657252739Z  INFO    controller.standalone.ApplyStandalone   Azure blob
// 	//{"reconciler group": "enterprise.splunk.com", "reconciler kind": "Standalone", "name": "test", "namespace": "splunk-operator",
// 	//"resp body":
// 	//"{\"access_token\":\"eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6IjJaUXBKM1VwYmpBWVhZR2FYRUpsOGxWMFRPSSIsImtpZCI6IjJaUXBKM1VwYmp
// 	//...30ydK_KIc6ndk0pw16CGblxm7EhG3B3zSRia6CaVLzhfJcFQbdSUU6ZhQbwrR4pMCoPg\"
// 	//,\"client_id\":\"d085d34e-f682-45d2-ae40-d576928d04ca\"
// 	//,\"expires_in\":\"86399\",\"expires_on\":\"1657654618\",\"ext_expires_in\":\"86399\",\"not_before\":\"1657567918\",
// 	//\"resource\":\"https://storage.azure.com/\",\"token_type\":\"Bearer\"}"}

// 	// Extract the token

// 	type TokenResponse struct {
// 		AccessToken string `json:"access_token"`
// 		ClientId    string `json:"client_id"`
// 	}

// 	var myTokenResponse TokenResponse

// 	err1 := json.Unmarshal(responseBody, &myTokenResponse)
// 	if err1 != nil {
// 		scopedLog.Error(err, "Azure blob token error")
// 	}

// 	scopedLog.Info("Azure blob token found", "Token is:", myTokenResponse.AccessToken)
// 	scopedLog.Info("Azure blob client id", "Client id is:", myTokenResponse.ClientId)

// 	appFetchUrl := "https://cmpoperatorteam.blob.core.windows.net/testapps/" + remoteFile

// 	requestAppDownload, err := http.NewRequest("GET", appFetchUrl, nil)
// 	if err != nil {
// 		scopedLog.Error(err, "Azure Blob Failed to get App fetch URL")
// 	}

// 	requestAppDownload.Header.Set("x-ms-version", "2017-11-09")
// 	requestAppDownload.Header.Set("Authorization", "Bearer "+myTokenResponse.AccessToken)

// 	scopedLog.Info("Azure blob app download requesting", "App  to download", appFetchUrl)

// 	respAppDownload, err := httpclient.Do(requestAppDownload)
// 	if err != nil {
// 		scopedLog.Error(err, "Azure blob,Errored when request get for the app")
// 	}

// 	defer respAppDownload.Body.Close()
// 	responseAppDownloadBody, err := ioutil.ReadAll(respAppDownload.Body)
// 	if err != nil {
// 		scopedLog.Error(err, "Azure blob,Errored when reading resp body for app download")
// 	}

// 	ioutil.WriteFile(downloadRequest.LocalFile, responseAppDownloadBody, 0777)

// 	scopedLog.Info("Azure blob app download", "Resp Status", respAppDownload.Status)
// 	scopedLog.Info("Azure blob app download", "Contenth Length", respAppDownload.ContentLength)

// 	if err != nil {
// 		scopedLog.Error(err, "Unable to download item %s", downloadRequest.RemoteFile)
// 		os.Remove(downloadRequest.LocalFile)
// 		return false, err
// 	}

// 	scopedLog.Info("File downloaded", "numBytes: ", respAppDownload.ContentLength)

// 	return true, err
// }
