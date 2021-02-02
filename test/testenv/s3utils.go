package testenv

import (
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Set S3 Variables
var (
	s3Region                  = os.Getenv("S3_REGION")
	testS3Bucket              = os.Getenv("TEST_BUCKET")
	testIndexesS3Bucket       = os.Getenv("TEST_INDEXES_S3_BUCKET")
	enterpriseLicenseLocation = os.Getenv("ENTERPRISE_LICENSE_LOCATION")
)

// CheckPrefixExistsOnS3 lists object in a bucket
func CheckPrefixExistsOnS3(prefix string) bool {
	dataBucket := testIndexesS3Bucket
	sess, err := session.NewSession(&aws.Config{Region: aws.String(s3Region)})
	if err != nil {
		logf.Log.Error(err, "Failed to create s3 session")
	}
	svc := s3.New(session.Must(sess, err))
	resp, err := svc.ListObjects(&s3.ListObjectsInput{
		Bucket: aws.String(dataBucket),
		Prefix: aws.String(prefix),
	})

	if err != nil {
		logf.Log.Error(err, "Failed to list objects on s3 bucket")
		return false
	}

	for _, key := range resp.Contents {
		logf.Log.Info("CHECKING KEY ", "KEY", *key.Key)
		if strings.Contains(*key.Key, prefix) {
			logf.Log.Info("Prefix found on bucket", "Prefix", prefix, "KEY", *key.Key)
			return true
		}
	}

	return false
}

// DownloadFromS3Bucket downloads license file from S3
func DownloadFromS3Bucket() (string, error) {
	dataBucket := testS3Bucket
	location := enterpriseLicenseLocation
	item := "enterprise.lic"
	file, err := os.Create(item)
	if err != nil {
		logf.Log.Error(err, "Failed to create license file")
	}
	defer file.Close()
	sess, _ := session.NewSession(&aws.Config{Region: aws.String(s3Region)})
	downloader := s3manager.NewDownloader(sess)
	numBytes, err := downloader.Download(file,
		&s3.GetObjectInput{
			Bucket: aws.String(dataBucket),
			Key:    aws.String(location + "/" + "enterprise.lic"),
		})
	if err != nil {
		logf.Log.Error(err, "Failed to download license file")
	}

	logf.Log.Info("Downloaded", "filename", file.Name(), "bytes", numBytes)
	return file.Name(), err
}
