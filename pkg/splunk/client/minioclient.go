package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// blank assignment to verify that MinioClient implements S3Client
var _ S3Client = &MinioClient{}

// MinioClient is a client to implement S3 specific APIs
type MinioClient struct {
	BucketName        string
	S3AccessKeyID     string
	S3SecretAccessKey string
	Prefix            string
	StartAfter        string
	Endpoint          string
}

// NewMinioClient returns an Minio client
func NewMinioClient(bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string, endpoint string) S3Client {
	return &MinioClient{
		BucketName:        bucketName,
		S3AccessKeyID:     accessKeyID,
		S3SecretAccessKey: secretAccessKey,
		Prefix:            prefix,
		StartAfter:        startAfter,
		Endpoint:          endpoint,
	}
}

//RegisterMinioClient will add the corresponding function pointer to the map
func RegisterMinioClient() {
	S3Clients["minio"] = NewMinioClient
}

// InitMinioClientSession initializes and returns a client session object
func InitMinioClientSession(appS3Endpoint string, accessKeyID string, secretAccessKey string, isNotInTestContext *bool) (*minio.Client, error) {
	scopedLog := log.WithName("InitMinioClientSession")
	*isNotInTestContext = true

	// Check if SSL is needed
	useSSL := true
	if strings.HasPrefix(appS3Endpoint, "http://") {
		useSSL = false
		appS3Endpoint = strings.TrimPrefix(appS3Endpoint, "http://")
	} else if strings.HasPrefix(appS3Endpoint, "https://") {
		appS3Endpoint = strings.TrimPrefix(appS3Endpoint, "https://")
	} else {
		// Unsupported endpoint
		scopedLog.Info("Unsupported endpoint for Minio S3 client", "appS3Endpoint", appS3Endpoint)
		return nil, nil
	}

	// New returns an Minio compatible client object. API compatibility (v2 or v4) is automatically
	// determined based on the Endpoint value.
	scopedLog.Info("Connecting to Minio S3 for apps", "appS3Endpoint", appS3Endpoint)
	s3Client, err := minio.New(appS3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		scopedLog.Info("Error creating new Minio Client Session", "err", err)
	}

	return s3Client, err
}

// GetAppsList get the list of apps from remote storage
func (client *MinioClient) GetAppsList() (S3Response, error) {
	scopedLog := log.WithName("GetAppsList")

	scopedLog.Info("Getting Apps list", " S3 Bucket", client.BucketName)
	s3Resp := S3Response{}
	appS3Endpoint := client.Endpoint

	isNotInTestContext := true
	s3Client, err := InitMinioClientSession(appS3Endpoint, client.S3AccessKeyID, client.S3SecretAccessKey, &isNotInTestContext)
	if err != nil {
		scopedLog.Info("Error creating new Minio Client Session", "err", err)
		return s3Resp, err
	}

	// Create a bucket list command for all files in bucket
	opts := minio.ListObjectsOptions{
		UseV1:     true,
		Prefix:    client.Prefix,
		Recursive: false,
	}

	// List all objects from a bucket-name with a matching prefix.
	for object := range s3Client.ListObjects(context.Background(), client.BucketName, opts) {
		if object.Err != nil {
			scopedLog.Info("Got an object error", "object.Err", object.Err, "client.BucketName", client.BucketName)
			return s3Resp, nil
		}
		scopedLog.Info("Got an object", "object", object)
		s3Resp.Objects = append(s3Resp.Objects, &RemoteObject{Etag: &object.ETag, Key: &object.Key, LastModified: &object.LastModified, Size: &object.Size, StorageClass: &object.StorageClass})
	}

	return s3Resp, nil
}

// GetInitContainerImage returns the initContainer image to be used with this s3 client
func (client *MinioClient) GetInitContainerImage() string {
	return ("amazon/aws-cli")
}

// GetInitContainerCmd returns the init container command on a per app source basis to be used by the initContainer
func (client *MinioClient) GetInitContainerCmd(endpoint string, bucket string, path string, appSrcName string, appMnt string) []string {
	return ([]string{fmt.Sprintf("--endpoint-url=%s", endpoint), "s3", "sync", fmt.Sprintf("s3://%s/%s", bucket, path), fmt.Sprintf("%s/%s", appMnt, appSrcName)})
}
