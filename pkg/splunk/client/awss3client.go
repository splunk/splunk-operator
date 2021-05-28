package client

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// blank assignment to verify that AWSS3Client implements S3Client
var _ S3Client = &AWSS3Client{}

// SplunkAWSS3Client is an interface to AWS S3 client
type SplunkAWSS3Client interface {
	ListObjectsV2(options *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error)
}

// AWSS3Client is a client to implement S3 specific APIs
type AWSS3Client struct {
	Region             string
	BucketName         string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	Prefix             string
	StartAfter         string
	Endpoint           string
	Client             SplunkAWSS3Client
}

// regex to extract the region from the s3 endpoint
var regionRegex = ".*.s3[-,.](?P<region>.*).amazonaws.com"

//getRegion extracts the region from the endpoint field
func getRegion(endpoint string) string {
	pattern := regexp.MustCompile(regionRegex)
	return pattern.FindStringSubmatch(endpoint)[1]
}

// InitAWSClientWrapper is a wrapper around InitClientSession
func InitAWSClientWrapper(region, accessKeyID, secretAccessKey string, isNotInTestContext *bool) interface{} {
	return InitAWSClientSession(region, accessKeyID, secretAccessKey, isNotInTestContext)
}

// InitAWSClientSession initializes and returns a client session object
func InitAWSClientSession(region, accessKeyID, secretAccessKey string, isNotInTestContext *bool) SplunkAWSS3Client {
	scopedLog := log.WithName("InitAWSClientSession")

	*isNotInTestContext = true
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
		Credentials: credentials.NewStaticCredentials(
			accessKeyID,     // id
			secretAccessKey, // secret
			"")},            // token
	)
	if err != nil {
		scopedLog.Error(err, "Failed to initialize an AWS S3 session.")
		return nil
	}

	return s3.New(sess)
}

// NewAWSS3Client returns an AWS S3 client
func NewAWSS3Client(bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string, endpoint string, fn func(string, string, string, *bool) interface{}) (S3Client, error) {
	var isNotInTestContext bool
	var s3SplunkClient SplunkAWSS3Client
	var err error
	region := getRegion(endpoint)
	cl := fn(region, accessKeyID, secretAccessKey, &isNotInTestContext)
	if cl == nil {
		err = fmt.Errorf("Failed to create an AWS S3 client")
		return nil, err
	}
	if isNotInTestContext == true {
		s3SplunkClient = cl.(*s3.S3)
	} else {
		s3SplunkClient = cl.(SplunkAWSS3Client)
	}

	return &AWSS3Client{
		Region:             region,
		BucketName:         bucketName,
		AWSAccessKeyID:     accessKeyID,
		AWSSecretAccessKey: secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Endpoint:           endpoint,
		Client:             s3SplunkClient,
	}, nil
}

// RegisterAWSS3Client will add the corresponding function pointer to the map
func RegisterAWSS3Client() {
	wrapperObject := GetS3ClientWrapper{GetS3Client: NewAWSS3Client, GetInitFunc: InitAWSClientWrapper}
	S3Clients["aws"] = wrapperObject
}

// GetAppsList get the list of apps from remote storage
func (awsclient *AWSS3Client) GetAppsList() (S3Response, error) {
	scopedLog := log.WithName("GetAppsList")

	scopedLog.Info("Getting Apps list", "AWS S3 Bucket", awsclient.BucketName)
	s3Resp := S3Response{}

	options := &s3.ListObjectsV2Input{
		Bucket:     aws.String(awsclient.BucketName),
		Prefix:     aws.String(awsclient.Prefix),
		StartAfter: aws.String(awsclient.StartAfter), // exclude the directory itself from listing
		MaxKeys:    aws.Int64(4000),                  // return upto 4K keys from S3
		Delimiter:  aws.String("/"),                  // limit the listing to 1 level only
	}

	client := awsclient.Client
	resp, err := client.ListObjectsV2(options)
	if err != nil {
		scopedLog.Error(err, "Unable to list items in bucket", "AWS S3 Bucket", awsclient.BucketName)
		return s3Resp, err
	}

	tmp, err := json.Marshal(resp.Contents)
	if err != nil {
		scopedLog.Error(err, "Failed to marshal s3 response", "AWS S3 Bucket", awsclient.BucketName)
		return s3Resp, err
	}

	err = json.Unmarshal(tmp, &(s3Resp.Objects))
	if err != nil {
		scopedLog.Error(err, "Failed to unmarshal s3 response", "AWS S3 Bucket", awsclient.BucketName)
		return s3Resp, err
	}

	return s3Resp, nil
}

// GetInitContainerImage returns the initContainer image to be used with this s3 client
func (awsclient *AWSS3Client) GetInitContainerImage() string {
	return ("amazon/aws-cli")
}

// GetInitContainerCmd returns the init container command on a per app source basis to be used by the initContainer
func (awsclient *AWSS3Client) GetInitContainerCmd(endpoint string, bucket string, path string, appSrcName string, appMnt string) []string {
	return ([]string{fmt.Sprintf("--endpoint-url=%s", endpoint), "s3", "sync", fmt.Sprintf("s3://%s/%s", bucket, path), fmt.Sprintf("%s/%s", appMnt, appSrcName)})
}
