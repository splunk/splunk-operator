package testenv

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Set Azure Variables
var (
	AzureRegion            = os.Getenv("AZURE_REGION")
	AzureStorageAccount    = os.Getenv("AZURE_STORAGE_ACCOUNT")
	AzureStorageAccountKey = os.Getenv("AZURE_STORAGE_ACCOUNT_KEY")
	testAzureContainer     = os.Getenv("TEST_AZURE_CONTAINER")
	//testIndexesAzureContainer      = os.Getenv("INDEXES_AZURE_CONTAINER")
	enterpriseAzureLicenseLocation = os.Getenv("ENTERPRISE_LICENSE_AZURE_LOCATION")
)

// GetDefaultAzureRegion returns default Azure Region
func GetDefaultAzureRegion() string {
	return AzureRegion
}

// GetAzureEndpoint returns Azure endpoint
func GetAzureEndpoint() string {
	return "https://" + AzureStorageAccount + ".blob.core.windows.net"
}

// DownloadLicenseFromAzure downloads license file from Azure
func DownloadLicenseFromAzure() bool {
	licenseLocation := enterpriseAzureLicenseLocation
	licenseName := "enterprise.lic"
	licenseFullPath := testAzureContainer + licenseLocation + licenseName
	//dataBucket := testAzureContainer
	status := DownloadFileFromAzure(AzureStorageAccount, AzureStorageAccountKey, licenseFullPath, licenseName)
	return status
}

// DownloadFileFromAzure downloads a file from Azure Storage account container
func DownloadFileFromAzure(accountName, accountKey, appPackageFullPath, localFileName string) bool {

	requestAppDownload, err := http.NewRequest("GET", appPackageFullPath, nil)

	if err != nil {
		logf.Log.Error(err, "Azure Blob Failed to get App fetch URL")
		return false
	}

	setSecretHeaders(accountName, accountKey, requestAppDownload)

	logf.Log.Info("Download from Azure", "App to download", appPackageFullPath)

	httpclient := &http.Client{}
	respAppDownload, err := httpclient.Do(requestAppDownload)
	if err != nil {
		logf.Log.Error(err, "Download from Azure, Errored when request get for the file")
	}

	defer respAppDownload.Body.Close()
	responseAppDownloadBody, err := ioutil.ReadAll(respAppDownload.Body)
	if err != nil {
		logf.Log.Error(err, "Download from Azure, Errored when reading resp body for file download")
	}

	err = ioutil.WriteFile(localFileName, responseAppDownloadBody, 0777)

	if err != nil {
		logf.Log.Error(err, "Writing to ", localFileName, " failed")
		return false
	}
	logf.Log.Info("Azure blob app download", "Resp Status", respAppDownload.Status)
	logf.Log.Info("Azure blob app download", "Resp Status", respAppDownload.Status)
	logf.Log.Info("Azure blob app download", "Contenth Length", respAppDownload.ContentLength)
	return true
}

// UploadFileToAzure uploads a file to Azure Storage account container
func UploadFileToAzure(accountName, accountKey, appPackageFullPath, localFileName string) bool {

	httpclient := &http.Client{}
	data, err := os.Open(localFileName)
	if err != nil {
		logf.Log.Error(err, "Error when opening file to upload to Azure for reading")
		return false
	}

	defer data.Close()

	requestUploadApp, err := http.NewRequest(http.MethodPut, appPackageFullPath, data)

	if err != nil {
		logf.Log.Error(err, "Upload to Azure blob, Failed to put App fetch URL")
		return false
	}

	info, err := os.Stat(localFileName)
	if err != nil {
		logf.Log.Error(err, "Could not get size of file to upload to Azure:", localFileName)
		return false
	}
	x := info.Size()

	setUploadHeaders(localFileName, requestUploadApp, x)
	setSecretHeaders(accountName, accountKey, requestUploadApp)

	logf.Log.Info("Upload to Azure blob,", "File to upload:", appPackageFullPath)

	requestUploadApp.ContentLength = x

	respAppDownload, err := httpclient.Do(requestUploadApp)

	if err != nil {
		logf.Log.Error(err, "Upload to Azure blob, Errored when request put for the app")
		return false
	}
	defer respAppDownload.Body.Close()

	logf.Log.Info("Azure blob app upload", "Resp Status", respAppDownload.Status)
	logf.Log.Info("Azure blob app upload", "Resp Status Code", respAppDownload.StatusCode)
	logf.Log.Info("#######################")

	logf.Log.Info("Azure blob successfully uploaded ", "file", localFileName)
	logf.Log.Info("#######################")

	return true
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

// ListAppsonAzure list the files present in an Azure Storage account container
func ListAppsonAzure(accountName, accountKey, bucketName, pathWithInBucket string) bool {

	logf.Log.Info("List apps parameters:", "accountName", accountName, "accountKey:", accountKey, "bucketName:", bucketName, "pathWithInBucket:", pathWithInBucket)

	if !valid("accountName", accountName) {
		return false
	}
	if !valid("accountKey", accountKey) {
		return false
	}
	if !valid("bucketName", bucketName) {
		return false
	}

	endPoint := "https://" + accountName + ".blob.core.windows.net"
	appsListFetchURL := ""
	if pathWithInBucket == "" {
		appsListFetchURL = endPoint + "/" + bucketName + "?restype=container&comp=list&include=snapshots&include=metadata"
	} else {
		appsListFetchURL = endPoint + "/" + bucketName + "?restype=container&comp=list&include=snapshots&include=metadata&prefix=" + pathWithInBucket
	}

	requestDownloadUsingKey, err := http.NewRequest("GET", appsListFetchURL, nil)

	if err != nil {
		logf.Log.Error(err, "List apps on Azure blob: Failed to create request for App fetch URL")
		return false
	}

	setSecretHeaders(accountName, accountKey, requestDownloadUsingKey)
	logf.Log.Info("Azure blob with secrets list download requesting", "App List to download", appsListFetchURL)
	httpclient := &http.Client{}
	respDownloadWithKey, err := httpclient.Do(requestDownloadUsingKey)
	if err != nil && respDownloadWithKey != nil && respDownloadWithKey.StatusCode == http.StatusForbidden {
		// Service failed to authenticate request, log it
		logf.Log.Error(err, "Azure Blob with secrets failed auth:", "===== HTTP Forbidden status")
		return false
	}

	responseDownloadBody, err := ioutil.ReadAll(respDownloadWithKey.Body)
	if err != nil {
		logf.Log.Error(err, "Azure blob with secrets ,Errored when reading resp body for app download")
		return false
	}

	logf.Log.Info("Azure blob with secrets download", "Resp Status", respDownloadWithKey.Status)
	logf.Log.Info("Azure blob with secrets download", "resp body", string(responseDownloadBody))

	dataWithKey := &enumerationResults{}

	err = xml.Unmarshal([]byte(responseDownloadBody), dataWithKey)

	if err != nil {
		logf.Log.Error(err, "Azure blob with secrets, Errored when unmarshalling list")
		return false
	}

	count := 0
	max := len(dataWithKey.Blobs.Blob)

	for count = 0; count < max; count++ {
		blob := dataWithKey.Blobs.Blob[count]
		logf.Log.Info("Azure blob app details", "Count:", count, "App name:", blob.Name, "Etag:", blob.Properties.ETag,
			"Created on:", blob.Properties.CreationTime, "Modified on:", blob.Properties.LastModified)
	}

	logf.Log.Info("Listing of files in Azure Storage Container done")
	return true
}

func valid(keyName, keyValue string) (resp bool) {

	if strings.TrimSpace(keyValue) == "" {
		fmt.Printf("Error, %s in Empty\n", keyName)
		return false
	}
	return true
}

// ComputeHMACSHA256 generates a hash signature for an HTTP request or for a SAS.
func ComputeHMACSHA256(message string, base64DecodedAccountKey []byte) (base64String string) {

	//	Signature=Base64(HMAC-SHA256(UTF8(StringToSign), Base64.decode(<your_azure_storage_account_shared_key>)))

	h := hmac.New(sha256.New, base64DecodedAccountKey)
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

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

func setUploadHeaders(fileName string, request *http.Request, contentLength int64) {

	info, err := os.Stat(fileName)
	if err != nil {
		logf.Log.Error(err, "Azure Blob could not get sizeof fle :", fileName)

		return
	}
	x := info.Size()

	contentDispositionHeader := fmt.Sprintf("attachment; filename=%s", fileName)
	request.Header[headerContentDisposition] = []string{contentDispositionHeader}
	request.Header[headerContentType] = []string{"application/octet-stream"}
	request.Header[headerContentEncoding] = []string{"gzip"}
	request.Header[headerXmsBlobType] = []string{"BlockBlob"}
	request.Header[headerXmsMetaM1] = []string{"v1"}
	request.Header[headerXmsMetaM2] = []string{"v2"}

	request.Header[headerContentLength] = []string{strconv.FormatInt(x, 10)}
}

func setSecretHeaders(accountName, accountKey string, request *http.Request) {
	request.Header[headerXmsDate] = []string{time.Now().UTC().Format(http.TimeFormat)}
	request.Header[headerXmsVersion] = []string{"2021-06-08"}

	authHeader := getSecretAuthHeader(accountName, accountKey, request)
	request.Header[headerAuthorization] = []string{authHeader}

}

func getSecretAuthHeader(accountName, accountKey string, request *http.Request) string {
	stringToSign, err := buildStringToSign(*request, accountName)
	if err != nil {
		logf.Log.Error(err, "Azure Blob with secrets Failed to build string to sign")
		return ""
	}

	decodedAccountKey, err := base64.StdEncoding.DecodeString(accountKey)
	if err != nil {
		// failed to decode
		logf.Log.Error(err, "Azure Blob with secrets failed auth:", "failed to decode accountKey")
		return ""
	}

	signature := ComputeHMACSHA256(stringToSign, decodedAccountKey)

	logf.Log.Info("Azure blob with secrets", "signature:", signature)

	authHeader := strings.Join([]string{"SharedKey ", accountName, ":", signature}, "")

	return authHeader
}
