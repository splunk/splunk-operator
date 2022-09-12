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
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// blank assignment to verify that AzureBlobClient implements RemoteDataClient
var _ RemoteDataClient = &AzureBlobClient{}

// AzureBlobClient is a client to implement Azure Blob specific APIs
type AzureBlobClient struct {
	BucketName         string
	StorageAccountName string
	SecretAccessKey    string
	Prefix             string
	StartAfter         string
	Endpoint           string
	HTTPClient         SplunkHTTPClient
}

// ContainerProperties represents blob properties
type ContainerProperties struct {
	CreationTime  string `xml:"Creation-Time"`
	LastModified  string `xml:"Last-Modified"`
	ETag          string `xml:"Etag"`
	ContentLength string `xml:"Content-Length"`
}

// Blob represents a single blob
type Blob struct {
	XMLName    xml.Name            `xml:"Blob"`
	Name       string              `xml:"Name"`
	Properties ContainerProperties `xml:"Properties"`
}

// Blobs represents a slice of blobs
type Blobs struct {
	XMLName xml.Name `xml:"Blobs"`
	Blob    []Blob   `xml:"Blob"`
}

// EnumerationResults holds unmarshaled data from listing APIs
type EnumerationResults struct {
	XMLName xml.Name `xml:"EnumerationResults"`
	Blobs   Blobs    `xml:"Blobs"`
}

// TokenResponse holds the unmarshaled oauth token
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ClientID    string `json:"client_id"`
}

// ComputeHMACSHA256 generates a hash signature for an HTTP request or for a SAS.
func ComputeHMACSHA256(message string, base64DecodedAccountKey []byte) (base64String string) {
	// Signature=Base64(HMAC-SHA256(UTF8(StringToSign), Base64.decode(<your_azure_storage_account_shared_key>)))
	h := hmac.New(sha256.New, base64DecodedAccountKey)
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// buildStringToSign is a helper API for adding auth signature to HTTP headers
func buildStringToSign(request http.Request, accountName string) (string, error) {
	// https://docs.microsoft.com/en-us/rest/api/storageservices/authentication-for-the-azure-storage-services
	headers := request.Header
	contentLength := headers.Get(headerContentLength)
	if contentLength == "0" {
		contentLength = ""
	}

	canonicalizedResource, err := buildCanonicalizedResource(request.URL, accountName)
	if err != nil {
		return "", err
	}

	stringToSign := strings.Join([]string{
		request.Method,
		headers.Get(headerContentEncoding),
		headers.Get(headerContentLanguage),
		contentLength,
		headers.Get(headerContentMD5),
		headers.Get(headerContentType),
		"", // Empty date because x-ms-date is expected (as per web page above)
		headers.Get(headerIfModifiedSince),
		headers.Get(headerIfMatch),
		headers.Get(headerIfNoneMatch),
		headers.Get(headerIfUnmodifiedSince),
		headers.Get(headerRange),
		buildCanonicalizedHeader(headers),
		canonicalizedResource,
	}, "\n")
	return stringToSign, nil
}

// buildCanonicalizedHeader is a helper API for adding auth signature to HTTP headers
func buildCanonicalizedHeader(headers http.Header) string {
	cm := map[string][]string{}
	for k, v := range headers {
		headerName := strings.TrimSpace(strings.ToLower(k))
		if strings.HasPrefix(headerName, "x-ms-") {
			cm[headerName] = v // NOTE: the value must not have any whitespace around it.
		}
	}
	if len(cm) == 0 {
		return ""
	}

	keys := make([]string, 0, len(cm))
	for key := range cm {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ch := bytes.NewBufferString("")
	for i, key := range keys {
		if i > 0 {
			ch.WriteRune('\n')
		}
		ch.WriteString(key)
		ch.WriteRune(':')
		ch.WriteString(strings.Join(cm[key], ","))
	}
	return ch.String()
}

// buildCanonicalizedResource is a helper API for adding auth signature to HTTP headers
func buildCanonicalizedResource(u *url.URL, accountName string) (string, error) {
	// https://docs.microsoft.com/en-us/rest/api/storageservices/authentication-for-the-azure-storage-services
	cr := bytes.NewBufferString("/")
	cr.WriteString(accountName)

	if len(u.Path) > 0 {
		// Any portion of the CanonicalizedResource string that is derived from
		// the resource's URI should be encoded exactly as it is in the URI.
		// -- https://msdn.microsoft.com/en-gb/library/azure/dd179428.aspx
		cr.WriteString(u.EscapedPath())
	} else {
		// a slash is required to indicate the root path
		cr.WriteString("/")
	}

	// params is a map[string][]string; param name is key; params values is []string
	params, err := url.ParseQuery(u.RawQuery) // Returns URL decoded values
	if err != nil {
		return "", errors.New("parsing query parameters must succeed, otherwise there might be serious problems in the SDK/generated code")
	}

	if len(params) > 0 { // There is at least 1 query parameter
		paramNames := []string{} // We use this to sort the parameter key names
		for paramName := range params {
			paramNames = append(paramNames, paramName) // paramNames must be lowercase
		}
		sort.Strings(paramNames)

		for _, paramName := range paramNames {
			paramValues := params[paramName]
			sort.Strings(paramValues)

			// Join the sorted key values separated by ','
			// Then prepend "keyName:"; then add this string to the buffer
			cr.WriteString("\n" + paramName + ":" + strings.Join(paramValues, ","))
		}
	}
	return cr.String(), nil
}

// NewAzureBlobClient returns an AzureBlob client
func NewAzureBlobClient(ctx context.Context, bucketName string, storageAccountName string, secretAccessKey string, prefix string, startAfter string, region string, endpoint string, fn GetInitFunc) (RemoteDataClient, error) {
	// Get http client
	azureHTTPClient := fn(ctx, endpoint, storageAccountName, secretAccessKey)

	return &AzureBlobClient{
		BucketName:         bucketName,
		StorageAccountName: storageAccountName,
		SecretAccessKey:    secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Endpoint:           endpoint,
		HTTPClient:         azureHTTPClient.(SplunkHTTPClient),
	}, nil
}

// InitAzureBlobClientWrapper is a wrapper around InitAzureBlobClientSession
func InitAzureBlobClientWrapper(ctx context.Context, appAzureBlobEndPoint string, storageAccountName string, secretAccessKey string) interface{} {
	return InitAzureBlobClientSession(ctx)
}

// InitAzureBlobClientSession initializes and returns a client session object
func InitAzureBlobClientSession(ctx context.Context) SplunkHTTPClient {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("InitAzureBlobClientSession")

	// Enforcing minimum version TLS1.2
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	tr.ForceAttemptHTTP2 = true

	httpClient := http.Client{Transport: tr}

	// Validate transport
	tlsVersion := "Unknown"
	if tr, ok := httpClient.Transport.(*http.Transport); ok {
		tlsVersion = getTLSVersion(tr)
	}

	scopedLog.Info("Azure Blob Client Session initialization successful.", "TLS Version", tlsVersion)

	return &httpClient
}

// Update http request header with secrets info
func updateAzureHTTPRequestHeaderWithSecrets(ctx context.Context, client *AzureBlobClient, httpRequest *http.Request) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("updateHttpRequestHeaderWithSecrets")

	scopedLog.Info("Updating Azure Http Request with secrets")

	// Update httpRequest header with data and version
	httpRequest.Header[headerXmsDate] = []string{time.Now().UTC().Format(http.TimeFormat)}
	httpRequest.Header[headerXmsVersion] = []string{azureHTTPHeaderXmsVersion}

	// Get HMAC signature using storage account name and secret access key
	stringToSign, err := buildStringToSign(*httpRequest, client.StorageAccountName)
	if err != nil {
		scopedLog.Error(err, "Azure Blob with secrets Failed to build string to sign")
		return err
	}
	decodedAccountKey, err := base64.StdEncoding.DecodeString(client.SecretAccessKey)
	if err != nil {
		// failed to decode
		scopedLog.Error(err, "Azure Blob with secrets failed to decode accountKey")
		return err
	}
	signature := ComputeHMACSHA256(stringToSign, decodedAccountKey)
	authHeader := strings.Join([]string{"SharedKey ", client.StorageAccountName, ":", signature}, "")

	// Update httpRequest header with the HMAC256 signature
	httpRequest.Header[headerAuthorization] = []string{authHeader}

	return nil
}

// Update http request header with IAM info
func updateAzureHTTPRequestHeaderWithIAM(ctx context.Context, client *AzureBlobClient, httpRequest *http.Request) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("updateHttpRequestHeaderWithIAM")

	scopedLog.Info("Updating Azure Http Request with IAM")

	// Create http request to retrive IAM oauth token from metadata URL
	oauthRequest, err := http.NewRequest("GET", azureTokenFetchURL, nil)
	if err != nil {
		scopedLog.Error(err, "Azure Blob Failed to create new token request")
		return err
	}

	// Mark metadata flag
	oauthRequest.Header.Set("Metadata", "true")

	// Create raw query for http request
	values := oauthRequest.URL.Query()
	values.Add("api-version", azureIMDSApiVersion)
	values.Add("resource", "https://storage.azure.com/")
	oauthRequest.URL.RawQuery = values.Encode()

	// Retrieve oauth token
	resp, err := client.HTTPClient.Do(oauthRequest)
	if err != nil {
		scopedLog.Error(err, "Azure blob,Errored when sending request to the server")
		return err
	}
	defer resp.Body.Close()

	// Read http response
	responseBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		scopedLog.Error(err, "Azure blob,Errored when reading resp body")
		return err
	}

	// Extract the token from the http response
	var azureOauthTokenResponse TokenResponse
	err = json.Unmarshal(responseBody, &azureOauthTokenResponse)
	if err != nil {
		scopedLog.Error(err, "Unable to unmarshal response to token")
		return err
	}

	// Update http request header with IAM access token
	httpRequest.Header.Set(headerXmsVersion, azureHTTPHeaderXmsVersion)
	httpRequest.Header.Set(headerAuthorization, "Bearer "+azureOauthTokenResponse.AccessToken)

	return nil
}

// GetAppsList gets the list of apps from remote storage
func (client *AzureBlobClient) GetAppsList(ctx context.Context) (RemoteDataListResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AzureBlob:GetAppsList").WithValues("Endpoint", client.Endpoint, "Storage Account", client.BucketName,
		"Prefix", client.Prefix)

	scopedLog.Info("Getting Apps list")

	// create rest request URL with storage account name, container, prefix
	appsListFetchURL := fmt.Sprintf(azureBlobListAppFetchURL, client.Endpoint, client.BucketName, client.Prefix)

	// Create a http request with the URL
	httpRequest, err := http.NewRequest("GET", appsListFetchURL, nil)
	if err != nil {
		scopedLog.Error(err, "Azure Blob Failed to create request for App fetch URL")
		return RemoteDataListResponse{}, err
	}

	// Setup the httpRequest with required authentication
	if client.StorageAccountName != "" && client.SecretAccessKey != "" {
		// Use Secrets
		err = updateAzureHTTPRequestHeaderWithSecrets(ctx, client, httpRequest)
	} else {
		// No Secret provided, try using IAM
		err = updateAzureHTTPRequestHeaderWithIAM(ctx, client, httpRequest)
	}
	if err != nil {
		scopedLog.Error(err, "Failed to get http request authenticated")
		return RemoteDataListResponse{}, err
	}

	// List the apps
	httpResponse, err := client.HTTPClient.Do(httpRequest)
	if err != nil {
		scopedLog.Error(err, "Azure blob, unable to execute list apps http request")
		return RemoteDataListResponse{}, err
	}
	defer httpResponse.Body.Close()

	// Extract response
	azureRemoteDataResponse, err := extractResponse(ctx, httpResponse)
	if err != nil {
		scopedLog.Error(err, "Azure blob, unable to extract blob from httpResponse")
		return azureRemoteDataResponse, err
	}

	// Successfully listed apps
	scopedLog.Info("Listing apps successful")

	return azureRemoteDataResponse, err
}

// Extract data from httpResponse and fill it in RemoteDataListResponse structs
func extractResponse(ctx context.Context, httpResponse *http.Response) (RemoteDataListResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AzureBlob:extractResponse")

	azureAppsRemoteData := RemoteDataListResponse{}

	// Read response body
	responseDownloadBody, err := ioutil.ReadAll(httpResponse.Body)
	if err != nil {
		scopedLog.Error(err, "Azure blob,Errored when reading resp body for app download")
		return azureAppsRemoteData, err
	}

	// Variable to hold unmarshaled data
	data := &EnumerationResults{}

	// Unmarshal http response
	err = xml.Unmarshal([]byte(responseDownloadBody), data)
	if err != nil {
		scopedLog.Error(err, "Errored  unmarshalling app packages list")
		return azureAppsRemoteData, err
	}

	// Extract data from all blobs
	for count := 0; count < len(data.Blobs.Blob); count++ {
		// Extract blob
		blob := data.Blobs.Blob[count]

		scopedLog.Info("Listing App package details", "Count:", count, "App package name", blob.Name,
			"Etag", blob.Properties.ETag, "Created on", blob.Properties.CreationTime,
			"Modified on", blob.Properties.LastModified, "Content Size", blob.Properties)

		// Extract properties
		newETag := blob.Properties.ETag
		newKey := blob.Name
		newLastModified, errTime := time.Parse(http.TimeFormat, blob.Properties.LastModified)
		if errTime != nil {
			scopedLog.Error(err, "Unable to get lastModifiedTime, not adding to list", "App Package", newKey, "name", blob.Properties.LastModified)
			continue
		}
		newSize, errInt := strconv.ParseInt(blob.Properties.ContentLength, 10, 64)
		if errInt != nil {
			scopedLog.Error(err, "Unable to get newSize, not adding to list", "App package", newKey, "name", blob.Properties.ContentLength)
			continue
		}
		newStorageClass := "standard" //TODO : map to a azure blob field

		// Create new object and append
		newRemoteObject := RemoteObject{Etag: &newETag, Key: &newKey, LastModified: &newLastModified, Size: &newSize, StorageClass: &newStorageClass}
		azureAppsRemoteData.Objects = append(azureAppsRemoteData.Objects, &newRemoteObject)
	}

	return azureAppsRemoteData, nil
}

// DownloadApp downloads an app package from remote storage
func (client *AzureBlobClient) DownloadApp(ctx context.Context, downloadRequest RemoteDataDownloadRequest) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AzureBlob:DownloadApp").WithValues("Endpoint", client.Endpoint, "Storage Account", client.BucketName,
		"Prefix", client.Prefix, "downloadRequest", downloadRequest)

	scopedLog.Info("Download App package")

	// create rest request URL with storage account name, container, prefix
	appPackageFetchURL := fmt.Sprintf(azureBlobDownloadAppFetchURL, client.Endpoint, client.BucketName, downloadRequest.RemoteFile)

	// Create a http request with the URL
	httpRequest, err := http.NewRequest("GET", appPackageFetchURL, nil)
	if err != nil {
		scopedLog.Error(err, "Azure Blob Failed to create request for App package fetch URL")
		return false, err
	}

	// Setup the httpRequest with required authentication
	if client.StorageAccountName != "" && client.SecretAccessKey != "" {
		// Use Secrets
		err = updateAzureHTTPRequestHeaderWithSecrets(ctx, client, httpRequest)
	} else {
		// No Secret provided, try using IAM
		err = updateAzureHTTPRequestHeaderWithIAM(ctx, client, httpRequest)
	}
	if err != nil {
		scopedLog.Error(err, "Failed to get http request authenticated")
		return false, err
	}

	scopedLog.Info("Calling the download rest request")

	// Download the app
	httpResponse, err := client.HTTPClient.Do(httpRequest)
	if err != nil {
		scopedLog.Error(err, "Azure blob, unable to execute download apps http request")
		return false, err
	}
	defer httpResponse.Body.Close()

	// Create local file on operator
	localFile, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		scopedLog.Error(err, "Unable to open local file")
		return false, err
	}
	defer localFile.Close()

	scopedLog.Info("Copying the download response to localFile")

	// Copy the http response (app packages to the local file path)
	_, err = io.Copy(localFile, httpResponse.Body)
	if err != nil {
		fmt.Println(err.Error(), "Failed when copying resp body for app download")
		return false, err
	}

	// Successfully downloaded app package
	scopedLog.Info("Download app package successful")

	return true, err
}

// RegisterAzureBlobClient will add the corresponding function pointer to the map
func RegisterAzureBlobClient() {
	wrapperObject := GetRemoteDataClientWrapper{GetRemoteDataClient: NewAzureBlobClient, GetInitFunc: InitAzureBlobClientWrapper}
	RemoteDataClientsMap["azure"] = wrapperObject
}
