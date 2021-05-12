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

// GetSmartStoreIndexesBucet returns smartstore test bucket name
func GetSmartStoreIndexesBucet() string {
	return testIndexesS3Bucket
}

// GetDefaultS3Region returns default AWS Region
func GetDefaultS3Region() string {
	return s3Region
}

// GetS3Endpoint return s3 endpoint for smartstore test
func GetS3Endpoint() string {
	return "https://s3-" + s3Region + ".amazonaws.com"
}

// CheckPrefixExistsOnS3 lists object in a bucket
func CheckPrefixExistsOnS3(prefix string) bool {
	dataBucket := testIndexesS3Bucket

	resp := GetFileListOnS3(dataBucket)
	for _, key := range resp {
		logf.Log.Info("CHECKING KEY ", "KEY", *key.Key)
		if strings.Contains(*key.Key, prefix) {
			logf.Log.Info("Prefix found on bucket", "Prefix", prefix, "KEY", *key.Key)
			return true
		}
	}

	return false
}

// DownloadLicenseFromS3Bucket downloads license file from S3
func DownloadLicenseFromS3Bucket() (string, error) {
	location := enterpriseLicenseLocation
	item := "enterprise.lic"
	dataBucket := testS3Bucket
	filename, err := DownloadFileFromS3(dataBucket, item, location)
	return filename, err
}

// S3Session Create session object for S3 bucket connection
func S3Session() (*session.Session, error) {
	sess, err := session.NewSession(&aws.Config{Region: aws.String(s3Region)})
	if err != nil {
		logf.Log.Error(err, "Failed to create session to S3")
	}
	return sess, err
}

// DownloadFileFromS3 downloads file from S3
func DownloadFileFromS3(dataBucket string, filename string, filepath string) (string, error) {
	file, err := os.Create(filename)
	if err != nil {
		logf.Log.Error(err, "Failed to create file")
	}
	defer file.Close()
	sess, err := S3Session()
	if err == nil {
		downloader := s3manager.NewDownloader(sess)
		numBytes, err := downloader.Download(file,
			&s3.GetObjectInput{
				Bucket: aws.String(dataBucket),
				Key:    aws.String(filepath + "/" + filename),
			})
		if err != nil {
			logf.Log.Error(err, "Failed to download file")
		}
		logf.Log.Info("Downloaded", "filename", file.Name(), "bytes", numBytes)
	}
	return file.Name(), err
}

// UploadFileToS3 upload file to S3
func UploadFileToS3(dataBucket string, filename string, filepath string, file *os.File) (string, error) {
	sess, err := S3Session()
	if err == nil {
		uploader := s3manager.NewUploader(sess)
		numBytes, err := uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(dataBucket),
			Key:    aws.String(filename), // Name of the file to be saved
			Body:   file,                 // File
		})
		if err != nil {
			logf.Log.Error(err, "Error in file upload")
		}
		logf.Log.Info("Uploaded", "filename", file.Name(), "bytes", numBytes)
	}
	return file.Name(), err
}

// GetFileListOnS3 lists object in a bucket
func GetFileListOnS3(dataBucket string) []*s3.Object {
	sess, err := S3Session()
	svc := s3.New(session.Must(sess, err))
	resp, err := svc.ListObjects(&s3.ListObjectsInput{
		Bucket: aws.String(dataBucket),
	})
	if err != nil {
		logf.Log.Error(err, "Failed to list objects on s3 bucket")
		return nil
	}

	return resp.Contents
}

// DeleteFilesOnS3 Delete a list of file on S3 Bucket
func DeleteFilesOnS3(dataBucket string, filenames []string) bool {
	sess, err := S3Session()
	if err == nil {
		svc := s3.New(session.Must(sess, err))
		objects := make([]*s3.ObjectIdentifier, len(filenames))

		for ind, filename := range filenames {
			objects[ind].Key = aws.String(filename)
		}
		deleteInput := &s3.DeleteObjectsInput{
			Bucket: aws.String(dataBucket),
			Delete: &s3.Delete{
				Objects: objects,
				Quiet:   aws.Bool(false),
			},
		}

		_, err = svc.DeleteObjects(deleteInput)
		if err != nil {
			logf.Log.Error(err, "Failed to delete files on S3 bucket")
			return false
		}
		return true
	}
	return false
}
