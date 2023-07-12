package impl

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/go-logr/logr"
	storagemodel "github.com/splunk/splunk-operator/pkg/gateway/storage/model"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type SplunkAWSS3Client interface {
	ListObjectsV2(options *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error)
}

// SplunkAWSDownloadClient is used to download the apps from remote storage
type SplunkAWSDownloadClient interface {
	Download(w io.WriterAt, input *s3.GetObjectInput, options ...func(*s3manager.Downloader)) (n int64, err error)
}

var regionRegex = ".*.s3[-,.](?P<region>.*).amazonaws.com"

// GetRegion extracts the region from the endpoint field
func GetRegion(ctx context.Context, endpoint string, region *string) error {
	var err error
	pattern := regexp.MustCompile(regionRegex)
	if len(pattern.FindStringSubmatch(endpoint)) > 0 {
		*region = pattern.FindStringSubmatch(endpoint)[1]
	} else {
		err = fmt.Errorf("unable to extract region from the endpoint")
	}
	return err
}

// InitAWSClientWrapper is a wrapper around InitClientSession
func InitAWSClientWrapper(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
	return InitAWSClientSession(ctx, region, accessKeyID, secretAccessKey)
}

func getTLSVersion(tr *http.Transport) string {
	switch tr.TLSClientConfig.MinVersion {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	}

	return "Unknown"
}

// InitAWSClientSession initializes and returns a client session object
func InitAWSClientSession(ctx context.Context, region, accessKeyID, secretAccessKey string) SplunkAWSS3Client {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("InitAWSClientSession")

	// Enforcing minimum version TLS1.2
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	tr.ForceAttemptHTTP2 = true
	httpClient := http.Client{Transport: tr}

	var err error
	var sess *session.Session
	config := &aws.Config{
		Region:     aws.String(region),
		MaxRetries: aws.Int(3),
		HTTPClient: &httpClient,
	}

	if accessKeyID != "" && secretAccessKey != "" {
		config.WithCredentials(credentials.NewStaticCredentials(
			accessKeyID,     // id
			secretAccessKey, // secret
			""))
	} else {
		scopedLog.Info("No valid access/secret keys.  Attempt to connect without them")
	}

	sess, err = session.NewSession(config)
	if err != nil {
		scopedLog.Error(err, "Failed to initialize an AWS S3 session.")
		return nil
	}

	// Create the s3Client
	s3Client := s3.New(sess)

	// Validate transport
	tlsVersion := "Unknown"
	if tr, ok := s3Client.Config.HTTPClient.Transport.(*http.Transport); ok {
		tlsVersion = getTLSVersion(tr)
	}

	scopedLog.Info("AWS Client Session initialization successful.", "region", region, "TLS Version", tlsVersion)

	return s3Client
}

type awsGateway struct {
	// a logger configured for this host
	log logr.Logger
	// a debug logger configured for this host
	debugLog logr.Logger
	// an event publisher for recording significant events
	publisher storagemodel.EventPublisher
	// credentials
	credentials *storagemodel.Credentials
	//Client
	client SplunkAWSS3Client
	//Downloader
	downloader SplunkAWSDownloadClient
}

// GetAppsList get the list of apps from remote storage
func (awsclient *awsGateway) GetAppsList(ctx context.Context) (storagemodel.StorageResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetAppsList")

	scopedLog.Info("Getting Apps list", "AWS S3 Bucket", awsclient.credentials.BucketName)
	s3Resp := storagemodel.StorageResponse{}

	options := &s3.ListObjectsV2Input{
		Bucket:     aws.String(awsclient.credentials.BucketName),
		Prefix:     aws.String(awsclient.credentials.Prefix),
		StartAfter: aws.String(awsclient.credentials.StartAfter), // exclude the directory itself from listing
		MaxKeys:    aws.Int64(4000),                              // return upto 4K keys from S3
		Delimiter:  aws.String("/"),                              // limit the listing to 1 level only
	}

	client := awsclient.client
	resp, err := client.ListObjectsV2(options)
	if err != nil {
		scopedLog.Error(err, "Unable to list items in bucket", "AWS S3 Bucket", awsclient.credentials.BucketName)
		return s3Resp, err
	}

	if resp.Contents == nil {
		scopedLog.Info("empty objects list in bucket. No apps to install", "bucketName", awsclient.credentials.BucketName)
		return s3Resp, nil
	}

	tmp, err := json.Marshal(resp.Contents)
	if err != nil {
		scopedLog.Error(err, "Failed to marshal s3 response", "AWS S3 Bucket", awsclient.credentials.BucketName)
		return s3Resp, err
	}

	err = json.Unmarshal(tmp, &(s3Resp.Objects))
	if err != nil {
		scopedLog.Error(err, "Failed to unmarshal s3 response", "AWS S3 Bucket", awsclient.credentials.BucketName)
		return s3Resp, err
	}

	return s3Resp, nil
}

// DownloadApp downloads the app from remote storage to local file system
func (awsclient *awsGateway) DownloadApp(ctx context.Context, remoteFile, localFile, etag string) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("DownloadApp").WithValues("remoteFile", remoteFile, "localFile", localFile)

	var numBytes int64
	file, err := os.Create(localFile)
	if err != nil {
		scopedLog.Error(err, "Unable to open local file")
		return false, err
	}
	defer file.Close()

	downloader := awsclient.downloader
	numBytes, err = downloader.Download(file,
		&s3.GetObjectInput{
			Bucket:  aws.String(awsclient.credentials.BucketName),
			Key:     aws.String(remoteFile),
			IfMatch: aws.String(etag),
		})
	if err != nil {
		scopedLog.Error(err, "Unable to download item %s", remoteFile)
		os.Remove(localFile)
		return false, err
	}

	scopedLog.Info("File downloaded", "numBytes: ", numBytes)

	return true, err
}

// GetInitContainerImage returns the initContainer image to be used with this s3 client
func (awsclient *awsGateway) GetInitContainerImage(ctx context.Context) string {
	return ("amazon/aws-cli")
}

// GetInitContainerCmd returns the init container command on a per app source basis to be used by the initContainer
func (awsclient *awsGateway) GetInitContainerCmd(ctx context.Context, endpoint string, bucket string, path string, appSrcName string, appMnt string) []string {
	s3AppSrcPath := filepath.Join(bucket, path) + "/"
	podSyncPath := filepath.Join(appMnt, appSrcName) + "/"

	return ([]string{fmt.Sprintf("--endpoint-url=%s", endpoint), "s3", "sync", fmt.Sprintf("s3://%s", s3AppSrcPath), podSyncPath})
}
