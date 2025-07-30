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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// blank assignment to verify that AWSS3Client implements RemoteDataClient
var _ RemoteDataClient = &AWSS3Client{}

// SplunkAWSS3Client is an interface to AWS S3 client
type SplunkAWSS3Client interface {
	ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, options ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, input *s3.GetObjectInput, options ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// SplunkAWSDownloadClient is used to download the apps from remote storage
type SplunkAWSDownloadClient interface {
	Download(ctx context.Context, w io.WriterAt, input *s3.GetObjectInput, options ...func(*manager.Downloader)) (n int64, err error)
}

// AWSS3Client is a client to implement S3 specific APIs
type AWSS3Client struct {
	Endpoint           string
	Region             string
	BucketName         string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	Prefix             string
	StartAfter         string
	Client             SplunkAWSS3Client
	Downloader         SplunkAWSDownloadClient
}

var regionRegex = ".*.s3[-,.]([a-z]+-[a-z]+-[0-9]+)\\..*amazonaws.com"

// GetRegion extracts the region from the endpoint field
func GetRegion(ctx context.Context, endpoint string, region *string) error {
	var err error
	pattern := regexp.MustCompile(regionRegex)
	if len(pattern.FindStringSubmatch(endpoint)) > 1 {
		*region = pattern.FindStringSubmatch(endpoint)[1]
	} else {
		err = fmt.Errorf("unable to extract region from the endpoint")
	}
	return err
}

// InitAWSClientWrapper is a wrapper around InitClientConfig
func InitAWSClientWrapper(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
	return InitAWSClientConfig(ctx, region, accessKeyID, secretAccessKey)
}

// InitAWSClientConfig initializes and returns a client config object
func InitAWSClientConfig(ctx context.Context, regionWithEndpoint, accessKeyID, secretAccessKey string) SplunkAWSS3Client {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("InitAWSClientConfig")

	// Enforcing minimum version TLS1.2
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	tr.ForceAttemptHTTP2 = true
	httpClient := http.Client{
		Transport: tr,
		Timeout:   appFrameworkHttpclientTimeout * time.Second,
	}

	var err error
	var cfg aws.Config
	var region, endpoint string

	// Extract region and endpoint
	regEndSl := strings.Split(regionWithEndpoint, awsRegionEndPointDelimiter)
	if len(regEndSl) != 2 || strings.Count(regionWithEndpoint, awsRegionEndPointDelimiter) != 1 {
		scopedLog.Error(err, "Unable to extract region and endpoint correctly for AWS client",
			"regWithEndpoint", regionWithEndpoint)
		return nil
	}
	region = regEndSl[0]
	endpoint = regEndSl[1]

	if accessKeyID != "" && secretAccessKey != "" {
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithRetryMaxAttempts(3),
			config.WithHTTPClient(&httpClient),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				accessKeyID,     // id
				secretAccessKey, // secret
				"")),            // token
		)
	} else {
		scopedLog.Info("No valid access/secret keys.  Attempt to connect without them")
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithRetryMaxAttempts(3),
			config.WithHTTPClient(&httpClient),
		)
	}
	if err != nil {
		scopedLog.Error(err, "Failed to initialize an AWS S3 config.")
		return nil
	}
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	// Validate transport
	tlsVersion := "Unknown"
	if tr, ok := httpClient.Transport.(*http.Transport); ok {
		tlsVersion = getTLSVersion(tr)
	}

	scopedLog.Info("AWS Client Config initialization successful.", "region", region, "TLS Version", tlsVersion)
	return s3Client
}

// NewAWSS3Client returns an AWS S3 client
func NewAWSS3Client(ctx context.Context, bucketName string, accessKeyID string, secretAccessKey string, prefix string, startAfter string, region string, endpoint string, fn GetInitFunc) (RemoteDataClient, error) {
	var s3SplunkClient SplunkAWSS3Client
	var err error

	// for backward compatibility, if `region` is not specified in the CR,
	// then derive the region from the endpoint.
	if region == "" {
		err = GetRegion(ctx, endpoint, &region)
		if err != nil {
			return nil, err
		}
	}

	endpointWithRegion := fmt.Sprintf("%s%s%s", region, awsRegionEndPointDelimiter, endpoint)

	cl := fn(ctx, endpointWithRegion, accessKeyID, secretAccessKey)
	if cl == nil {
		err = fmt.Errorf("failed to create an AWS S3 client")
		return nil, err
	}

	s3SplunkClient, ok := cl.(SplunkAWSS3Client)
	if !ok {
		return nil, fmt.Errorf("unable to get s3 client")
	}
	downloader := manager.NewDownloader(cl.(SplunkAWSS3Client))

	return &AWSS3Client{
		Region:             region,
		BucketName:         bucketName,
		AWSAccessKeyID:     accessKeyID,
		AWSSecretAccessKey: secretAccessKey,
		Prefix:             prefix,
		StartAfter:         startAfter,
		Client:             s3SplunkClient,
		Downloader:         downloader,
	}, nil
}

// RegisterAWSS3Client will add the corresponding function pointer to the map
func RegisterAWSS3Client() {
	wrapperObject := GetRemoteDataClientWrapper{GetRemoteDataClient: NewAWSS3Client, GetInitFunc: InitAWSClientWrapper}
	RemoteDataClientsMap["aws"] = wrapperObject
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

// GetAppsList get the list of apps from remote storage
func (awsclient *AWSS3Client) GetAppsList(ctx context.Context) (RemoteDataListResponse, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetAppsList")

	scopedLog.Info("Getting Apps list", "AWS S3 Bucket", awsclient.BucketName)
	remoteDataClientResponse := RemoteDataListResponse{}

	options := &s3.ListObjectsV2Input{
		Bucket:     aws.String(awsclient.BucketName),
		Prefix:     aws.String(awsclient.Prefix),
		StartAfter: aws.String(awsclient.StartAfter), // exclude the directory itself from listing
		MaxKeys:    aws.Int32(4000),                  // return upto 4K keys from S3
		Delimiter:  aws.String("/"),                  // limit the listing to 1 level only
	}

	client := awsclient.Client
	resp, err := client.ListObjectsV2(ctx, options)
	if err != nil {
		scopedLog.Error(err, "Unable to list items in bucket", "AWS S3 Bucket", awsclient.BucketName, "endpoint", awsclient.Endpoint)
		return remoteDataClientResponse, err
	}

	if resp.Contents == nil {
		scopedLog.Info("empty objects list in bucket. No apps to install", "bucketName", awsclient.BucketName)
		return remoteDataClientResponse, nil
	}

	tmp, err := json.Marshal(resp.Contents)
	if err != nil {
		scopedLog.Error(err, "Failed to marshal s3 response", "AWS S3 Bucket", awsclient.BucketName)
		return remoteDataClientResponse, err
	}

	err = json.Unmarshal(tmp, &(remoteDataClientResponse.Objects))
	if err != nil {
		scopedLog.Error(err, "Failed to unmarshal s3 response", "AWS S3 Bucket", awsclient.BucketName)
		return remoteDataClientResponse, err
	}

	return remoteDataClientResponse, nil
}

// DownloadApp downloads the app from remote storage to local file system
func (awsclient *AWSS3Client) DownloadApp(ctx context.Context, downloadRequest RemoteDataDownloadRequest) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("DownloadApp").WithValues("remoteFile", downloadRequest.RemoteFile, "localFile",
		downloadRequest.LocalFile, "etag", downloadRequest.Etag)

	var numBytes int64
	file, err := os.Create(downloadRequest.LocalFile)
	if err != nil {
		scopedLog.Error(err, "Unable to open local file")
		return false, err
	}
	defer file.Close()

	downloader := awsclient.Downloader
	numBytes, err = downloader.Download(ctx, file,
		&s3.GetObjectInput{
			Bucket:  aws.String(awsclient.BucketName),
			Key:     aws.String(downloadRequest.RemoteFile),
			IfMatch: aws.String(downloadRequest.Etag),
		})
	if err != nil {
		scopedLog.Error(err, "Unable to download item", "RemoteFile", downloadRequest.RemoteFile)
		os.Remove(downloadRequest.RemoteFile)
		return false, err
	}

	scopedLog.Info("File downloaded", "numBytes: ", numBytes)

	return true, err
}
