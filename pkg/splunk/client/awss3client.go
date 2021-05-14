package client

import (
	"encoding/json"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// blank assignment to verify that AWSS3Client implements S3Client
var _ S3Client = &AWSS3Client{}

// AWSS3Client is a client to implement S3 specific APIs
type AWSS3Client struct {
	Region             string
	BucketName         string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	Prefix             string
	StartAfter         string
}

// GetAWSS3Client returns an AWS S3 client
func GetAWSS3Client(region string, bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string) S3Client {
	return &AWSS3Client{
		Region:             region,
		BucketName:         bucketName,
		AWSAccessKeyID:     accessKeyID,
		AWSSecretAccessKey: secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
	}
}

//RegisterAWSS3Client will add the corresponding function pointer to the map
func RegisterAWSS3Client() {
	S3Clients["aws"] = GetAWSS3Client
}

// GetAppsList get the list of apps from remote storage
func (awsclient *AWSS3Client) GetAppsList() (*S3Response, error) {
	scopedLog := log.WithName("GetRemoteStorageClient")

	scopedLog.Info("Getting Apps list", "AWS S3 Bucket", awsclient.BucketName)

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(awsclient.Region),
		Credentials: credentials.NewStaticCredentials(
			awsclient.AWSAccessKeyID,     // id
			awsclient.AWSSecretAccessKey, // secret
			"")},                         //token
	)

	// Create S3 service client
	svc := s3.New(sess)

	options := &s3.ListObjectsV2Input{
		Bucket:     aws.String(awsclient.BucketName),
		Prefix:     aws.String(awsclient.Prefix),
		StartAfter: aws.String(awsclient.StartAfter), //exclude the directory itself from listing
	}

	// List the bucket contents
	resp, err := svc.ListObjectsV2(options)
	if err != nil {
		scopedLog.Error(err, "Unable to list items in bucket ", awsclient.BucketName)
		return nil, err
	}

	tmp, err := json.Marshal(resp.Contents)
	if err != nil {
		scopedLog.Error(err, "Failed to marshal s3 response")
		return nil, err
	}

	s3Resp := S3Response{}
	err = json.Unmarshal(tmp, &(s3Resp.Objects))
	if err != nil {
		scopedLog.Error(err, "Failed to unmarshal s3 response")
		return nil, err
	}

	return &s3Resp, nil
}
