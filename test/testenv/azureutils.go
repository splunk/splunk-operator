package testenv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Set Azure Variables
var (
	Region                         = os.Getenv("REGION")
	StorageAccount                 = os.Getenv("STORAGE_ACCOUNT")
	StorageAccountKey              = os.Getenv("STORAGE_ACCOUNT_KEY")
	TestContainer                  = os.Getenv("TEST_CONTAINER")
	azureIndexesContainer          = os.Getenv("INDEXES_CONTAINER")
	enterpriseAzureLicenseLocation = os.Getenv("ENTERPRISE_LICENSE_LOCATION")
)

const (
	// Azure URL for downloading an app package
	// URL format is {azure_end_point}/{containerName}/{pathToAppPackage}
	// For example : https://mystorageaccount.blob.core.windows.net/myappsbucket/standlone/myappsteamapp.tgz
	azureBlobDownloadAppFetchURL = "%s%s"
	// Azure http header XMS version
	// https://docs.microsoft.com/en-us/rest/api/storageservices/versioning-for-the-azure-storage-services
	azureHTTPHeaderXmsVersion = "2021-06-08"
)

// AzureBlobClient is a client to implement Azure Blob specific APIs
type AzureBlobClient struct {
	//containerName      string
	StorageAccountName string
	SecretAccessKey    string
	Prefix             string
	StartAfter         string
	Endpoint           string
	HTTPClient         SplunkHTTPClient
}

// RemoteDataDownloadRequest struct specifies the remote data file path,
// local file path where the downloaded data should be written as well as
// the etag data if available
type RemoteDataDownloadRequest struct {
	LocalFile  string // file path where the remote data will be written
	RemoteFile string // file name with path relative to the bucket
	Etag       string // unique tag of the object
}

// SplunkHTTPClient defines the interface used by SplunkClient.
// It is used to mock alternative implementations used for testing.
type SplunkHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type containerProperties struct {
	CreationTime  string `xml:"Creation-Time"`
	LastModified  string `xml:"Last-Modified"`
	ETag          string `xml:"Etag"`
	ContentLength string `xml:"Content-Length"`
}

type myBlob struct {
	XMLName xml.Name `xml:"Blob"`

	Name       string              `xml:"Name"`
	Properties containerProperties `xml:"Properties"`
}

type blobs struct {
	XMLName xml.Name `xml:"Blobs"`

	Blob []myBlob `xml:"Blob"`
}

type enumerationResults struct {
	XMLName xml.Name `xml:"EnumerationResults"`
	Blobs   blobs    `xml:"Blobs"`
}

// Constants ensuring that header names are correctly spelled and consistently cased.
const (
	headerAuthorization             = "Authorization"
	headerCacheControl              = "Cache-Control"
	headerContentEncoding           = "Content-Encoding"
	headerContentDisposition        = "Content-Disposition"
	headerContentLanguage           = "Content-Language"
	headerContentLength             = "Content-Length"
	headerContentMD5                = "Content-MD5"
	headerContentType               = "Content-Type"
	headerDate                      = "Date"
	headerIfMatch                   = "If-Match"
	headerIfModifiedSince           = "If-Modified-Since"
	headerIfNoneMatch               = "If-None-Match"
	headerIfUnmodifiedSince         = "If-Unmodified-Since"
	headerRange                     = "Range"
	headerUserAgent                 = "User-Agent"
	headerXmsDate                   = "x-ms-date"
	headerXmsVersion                = "x-ms-version"
	headerXmsBlobType               = "x-ms-blob-type"
	headerXmsBlobContentDisposition = "x-ms-blob-content-disposition"
	headerXmsMetaM1                 = "x-ms-meta-m1"
	headerXmsMetaM2                 = "x-ms-meta-m2"
)

// ComputeHMACSHA256 generates a hash signature for an HTTP request or for a SAS
func ComputeHMACSHA256(ctx context.Context, message string, base64DecodedAccountKey []byte) (base64String string) {
	//	Signature=Base64(HMAC-SHA256(UTF8(StringToSign), Base64.decode(<your_azure_storage_account_shared_key>)))
	h := hmac.New(sha256.New, base64DecodedAccountKey)
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func buildStringToSign(ctx context.Context, request http.Request, accountName string) (string, error) {
	// https://docs.microsoft.com/en-us/rest/api/storageservices/authentication-for-the-azure-storage-services
	headers := request.Header
	contentLength := headers.Get(headerContentLength)
	if contentLength == "0" {
		contentLength = ""
	}

	canonicalizedResource, err := buildCanonicalizedResource(ctx, request.URL, accountName)
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
		buildCanonicalizedHeader(ctx, headers),
		canonicalizedResource,
	}, "\n")
	return stringToSign, nil
}

func buildCanonicalizedHeader(ctx context.Context, headers http.Header) string {
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

func buildCanonicalizedResource(ctx context.Context, u *url.URL, accountName string) (string, error) {
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

func setUploadHeaders(ctx context.Context, fileName string, request *http.Request, contentLength int64) {
	info, err := os.Stat(fileName)
	if err != nil {
		fmt.Println(err.Error(), "Could not get sizeof file :", fileName)

		return
	}
	x := info.Size()
	contentDispositionHeader := fmt.Sprintf("attachment; filename=%s", fileName)
	request.Header[headerContentDisposition] = []string{contentDispositionHeader}
	request.Header[headerContentType] = []string{"application/gzip"}
	request.Header[headerXmsBlobType] = []string{"BlockBlob"}
	request.Header[headerXmsMetaM1] = []string{"v1"}
	request.Header[headerXmsMetaM2] = []string{"v2"}

	request.Header[headerContentLength] = []string{strconv.FormatInt(x, 10)}
}

func setSecretHeaders(ctx context.Context, accountName, accountKey string, request *http.Request) {
	request.Header[headerXmsDate] = []string{time.Now().UTC().Format(http.TimeFormat)}
	request.Header[headerXmsVersion] = []string{azureHTTPHeaderXmsVersion}

	authHeader := getSecretAuthHeader(ctx, accountName, accountKey, request)
	request.Header[headerAuthorization] = []string{authHeader}
}

func getSecretAuthHeader(ctx context.Context, accountName, accountKey string, request *http.Request) string {
	stringToSign, err := buildStringToSign(ctx, *request, accountName)
	if err != nil {
		fmt.Println(err.Error(), "Failed to build string to sign")
		return ""
	}

	decodedAccountKey, err := base64.StdEncoding.DecodeString(accountKey)
	if err != nil {
		// failed to decode
		fmt.Println(err.Error(), "Failed to decode accountKey")
		return ""
	}

	signature := ComputeHMACSHA256(ctx, stringToSign, decodedAccountKey)

	//fmt.Println("Azure with secrets", "signature:", signature)
	authHeader := strings.Join([]string{"SharedKey ", accountName, ":", signature}, "")

	return authHeader
}

// GetDefaultAzureRegion returns default Azure Region
func GetDefaultAzureRegion(ctx context.Context) string {
	return Region
}

// GetAzureEndpoint returns Azure endpoint
func GetAzureEndpoint(ctx context.Context) string {
	return "https://" + StorageAccount + ".blob.core.windows.net"
}

// DownloadFileFromAzure downloads a file from an Azure Storage Container
func (client *AzureBlobClient) DownloadFileFromAzure(ctx context.Context, downloadRequest RemoteDataDownloadRequest, endPoint, accountKey, accountName string) (string, error) {
	logf.Log.Info("Downloading from Azure", "File:", downloadRequest.RemoteFile)
	index := strings.LastIndex(downloadRequest.LocalFile, "/")
	downloadDir := downloadRequest.LocalFile[0:index]

	// Check if directory to download file exists
	if _, err := os.Stat(downloadDir); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(downloadDir, os.ModePerm)
		if err != nil {
			logf.Log.Error(err, "Unable to create directory to download file")
			return "", err
		}
	}

	// Create REST request URL
	appPackageFetchURL := fmt.Sprintf(azureBlobDownloadAppFetchURL, endPoint, downloadRequest.RemoteFile)

	// Create HTTP request with the URL
	httpRequest, err := http.NewRequest("GET", appPackageFetchURL, nil)
	if err != nil {
		logf.Log.Error(err, "Failed to create HTTP request for REST request URL")
		return "", err
	}

	// Set secrets
	setSecretHeaders(ctx, accountName, accountKey, httpRequest)

	// Download the file
	httpClient := &http.Client{}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		logf.Log.Error(err, "Unable to execute download file HTTP request")
		return "", err
	}
	defer httpResponse.Body.Close()

	// Create local file on operator
	localFile, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		logf.Log.Error(err, "Unable to open local file")
		return "", err
	}
	defer localFile.Close()

	// Copy the http response (file packages to the local file path)
	_, err = io.Copy(localFile, httpResponse.Body)
	if err != nil {
		fmt.Println(err.Error(), "Failed when copying response body")
		return "", err
	}

	logf.Log.Info("Download from Azure successful:", "File", downloadRequest.RemoteFile)

	return localFile.Name(), err
}

// DownloadFilesFromAzure downloads given list of files from Azure
func DownloadFilesFromAzure(ctx context.Context, endPoint, accountKey, accountName, downloadDir, containerName string, appList []string) error {
	azureBlobClient := &AzureBlobClient{}
	for _, key := range appList {
		appFile := downloadDir + "/" + key
		downloadRequest := RemoteDataDownloadRequest{
			LocalFile:  appFile,
			RemoteFile: containerName + key,
		}
		_, err := azureBlobClient.DownloadFileFromAzure(ctx, downloadRequest, endPoint, StorageAccountKey, StorageAccount)
		if err != nil {
			logf.Log.Error(err, "Unable to download file", "File Name", key)
			return err
		}
	}
	return nil
}

// DownloadLicenseFromAzure downloads an enterprise license file from an Azure Storage Account container
func DownloadLicenseFromAzure(ctx context.Context, downloadDir string) (string, error) {
	licenseName := "enterprise.lic"
	licenseRemotePath := "/" + TestContainer + "/" + enterpriseAzureLicenseLocation + "/" + licenseName
	localfile := downloadDir + "/" + licenseName

	downloadRequest := RemoteDataDownloadRequest{
		LocalFile:  localfile,
		RemoteFile: licenseRemotePath,
	}
	azureBlobClient := &AzureBlobClient{}
	filename, err := azureBlobClient.DownloadFileFromAzure(ctx, downloadRequest, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount)
	if err != nil {
		logf.Log.Error(err, "Unable to download license file", "File", filename)
	}
	return filename, err
}

// UploadFileToAzure uploads a file to an Azure Storage Account container
func UploadFileToAzure(ctx context.Context, accountName, accountKey, fileFullPath, localFileName string) (string, error) {
	fmt.Println("Upload to Azure", "File:", fileFullPath)

	httpclient := &http.Client{}
	data, err := os.Open(localFileName)
	if err != nil {
		fmt.Println(err.Error(), "Error when opening file to upload to Azure for reading")
		return localFileName, err
	}

	defer data.Close()

	requestUploadApp, err := http.NewRequest(http.MethodPut, fileFullPath, data)

	if err != nil {
		fmt.Println(err.Error(), "Upload to Azure, Failed to put App fetch URL")
		return localFileName, err
	}

	info, err := os.Stat(localFileName)
	if err != nil {
		fmt.Println(err.Error(), "Could not get size of file to upload to Azure:", localFileName)
		return localFileName, err
	}
	x := info.Size()

	setUploadHeaders(ctx, localFileName, requestUploadApp, x)
	setSecretHeaders(ctx, accountName, accountKey, requestUploadApp)

	requestUploadApp.ContentLength = x
	respAppDownload, err := httpclient.Do(requestUploadApp)

	if err != nil {
		fmt.Println(err.Error(), "Upload to Azure, Error when request put for the app")
		return localFileName, err
	}
	defer respAppDownload.Body.Close()

	fmt.Println("App upload", "Resp Status", respAppDownload.Status)
	fmt.Println("File successfuly uploaded to Azure Storage Container: " + fileFullPath)
	return localFileName, err
}

// UploadFilesToAzure upload given list of files to given location on an Azure Storage Container
func UploadFilesToAzure(ctx context.Context, accountName, accountKey, uploadFromDir, containerName string, applist []string) ([]string, error) {
	var uploadedFiles []string
	for _, key := range applist {
		fileLocation := filepath.Join(uploadFromDir, key)
		fileFullPath := "https://" + StorageAccount + ".blob.core.windows.net" + "/" + azureIndexesContainer + "/" + containerName + "/" + key
		fileName, err := UploadFileToAzure(ctx, accountName, accountKey, fileFullPath, fileLocation)
		if err != nil {
			logf.Log.Error(err, "Unable to upload file", "File name", key)
			return nil, err
		}
		uploadedFiles = append(uploadedFiles, fileName)
	}
	return uploadedFiles, nil
}

// ListFilesonAzure list the files present in an Azure Storage account container
func ListFilesonAzure(ctx context.Context, accountName, accountKey, containerName, containerPath string) {
	appsListFetchURL := ""
	if containerPath == "" {
		appsListFetchURL = GetAzureEndpoint(ctx) + "/" + containerName + "?restype=container&comp=list&include=snapshots&include=metadata"
	} else {
		appsListFetchURL = GetAzureEndpoint(ctx) + "/" + containerName + "?restype=container&comp=list&include=snapshots&include=metadata&prefix=" + containerPath
	}

	requestDownloadUsingKey, err := http.NewRequest("GET", appsListFetchURL, nil)

	if err != nil {
		fmt.Println(err.Error(), "List apps on Azure: Failed to create request for App fetch URL")
	}
	setSecretHeaders(ctx, accountName, accountKey, requestDownloadUsingKey)
	fmt.Println("List apps on Azure requesting", "App List to download", appsListFetchURL)
	httpclient := &http.Client{}
	respDownloadWithKey, err := httpclient.Do(requestDownloadUsingKey)
	if err != nil && respDownloadWithKey != nil && respDownloadWithKey.StatusCode == http.StatusForbidden {
		// Service failed to authenticate request, log it
		fmt.Println(err.Error(), "List apps on Azure failed auth:", "===== HTTP Forbidden status")
	}

	responseDownloadBody, err := ioutil.ReadAll(respDownloadWithKey.Body)
	if err != nil {
		fmt.Println(err.Error(), "List apps on Azure, Error when reading response body")
	}

	fmt.Println("List apps on Azure", "Response Status", respDownloadWithKey.Status)
	fmt.Println("List apps on Azure", "Response Body", string(responseDownloadBody))

	dataWithKey := &enumerationResults{}

	err = xml.Unmarshal([]byte(responseDownloadBody), dataWithKey)

	if err != nil {
		fmt.Println(err.Error(), "List apps on Azure, Error when unmarshalling list")
	}

	count := 0
	max := len(dataWithKey.Blobs.Blob)

	for count = 0; count < max; count++ {
		blob := dataWithKey.Blobs.Blob[count]
		fmt.Println("List apps on Azure", "Count:", count, "App name:", blob.Name, "Etag:", blob.Properties.ETag,
			"Created on:", blob.Properties.CreationTime, "Modified on:", blob.Properties.LastModified)
	}

	fmt.Println("Listing of files in Azure Storage Container done")
}

// DeleteFileOnAzure deletes a file from Azure Storage Container
func (client *AzureBlobClient) DeleteFileOnAzure(ctx context.Context, filename, endPoint, accountKey, accountName string) error {
	fmt.Printf("File to delete from Azure: %v \n", filename)

	// Create REST request URL
	appPackageFetchURL := fmt.Sprintf(azureBlobDownloadAppFetchURL, endPoint, filename)

	// Create HTTP request with the URL
	httpRequest, err := http.NewRequest("DELETE", appPackageFetchURL, nil)
	if err != nil {
		logf.Log.Error(err, "Failed to create HTTP request for REST request URL")
		return err
	}

	// Set secrets
	setSecretHeaders(ctx, accountName, accountKey, httpRequest)

	// Delete the file
	httpClient := &http.Client{}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		logf.Log.Error(err, "Unable to execute delete file HTTP request")
		return err
	}
	defer httpResponse.Body.Close()

	return err
}

// DeleteFilesOnAzure Delete a list of files on Azure
func (client *AzureBlobClient) DeleteFilesOnAzure(ctx context.Context, endPoint, accountKey, accountName string, filenames []string) error {
	azureBlobClient := &AzureBlobClient{}
	for _, file := range filenames {
		err := azureBlobClient.DeleteFileOnAzure(ctx, file, endPoint, StorageAccountKey, StorageAccount)
		if err != nil {
			return err
		}
	}
	return nil
}

// DisableAppsOnAzure untar apps, modify their conf file to disable them, re-tar and upload the disabled version to Azure
func DisableAppsOnAzure(ctx context.Context, downloadDir string, appFileList []string, containerName string) ([]string, error) {

	// Create a folder named 'untarred_apps' to store untarred apps folders
	untarredAppsMainFolder := downloadDir + "/untarred_apps"
	cmd := exec.Command("mkdir", untarredAppsMainFolder)
	cmd.Run()

	// Create a folder named 'disabled_apps' to stored disabled apps tgz files
	disabledAppsFolder := downloadDir + "/disabled_apps"
	cmd = exec.Command("mkdir", disabledAppsFolder)
	cmd.Run()

	for _, key := range appFileList {
		// Create a specific folder for each app in 'untarred_apps'
		tarfile := downloadDir + "/" + key
		lastInd := strings.LastIndex(key, ".")
		untarredCurrentAppFolder := untarredAppsMainFolder + "/" + key[:lastInd]
		cmd := exec.Command("mkdir", untarredCurrentAppFolder)
		cmd.Run()

		// Untar the app
		cmd = exec.Command("tar", "-xf", tarfile, "-C", untarredCurrentAppFolder)
		cmd.Run()

		// Disable the app
		// - Get the name of the untarred app folder (as it could be different from the tgz file)
		wildcardpath := untarredCurrentAppFolder + "/*/./"
		bytepath, _ := exec.Command("/bin/sh", "-c", "cd "+wildcardpath+"; pwd").Output()
		untarredAppRootFolder := string(bytepath)
		untarredAppRootFolder = untarredAppRootFolder[:len(untarredAppRootFolder)-1] //removing \n at the end of folder path

		// - Edit /default/app.conf (add "state = disabled" in [install] stanza)
		appConfFile := untarredAppRootFolder + "/default/app.conf"
		input, err := ioutil.ReadFile(appConfFile)
		if err != nil {
			log.Fatalln(err)
			return nil, err
		}
		lines := strings.Split(string(input), "\n")
		for i, line := range lines {
			if strings.Contains(line, "[install]") {
				lines[i] = "[install]\nstate = disabled"
			}
			if strings.Contains(line, "state = enabled") {
				lines = append(lines[:i], lines[i+1:]...)
			}
		}
		output := strings.Join(lines, "\n")
		err = ioutil.WriteFile(appConfFile, []byte(output), 0644)
		if err != nil {
			log.Fatalln(err)
		}

		// Tar disabled app folder
		lastInd = strings.LastIndex(untarredAppRootFolder, "/")
		appFolderName := untarredAppRootFolder[lastInd+1:]
		tarDestination := disabledAppsFolder + "/" + key
		cmd = exec.Command("tar", "-czf", tarDestination, "--directory", untarredCurrentAppFolder, appFolderName)
		cmd.Run()
	}

	// Upload disabled apps to Azure
	uploadedFiles, _ := UploadFilesToAzure(ctx, StorageAccount, StorageAccountKey, disabledAppsFolder, containerName, appFileList)

	return uploadedFiles, nil
}
