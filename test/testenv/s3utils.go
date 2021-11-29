package testenv

import (
	"errors"
	"os"
	"path/filepath"
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

	resp := GetFileListOnS3(dataBucket, prefix)
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
	filename, err := DownloadFileFromS3(dataBucket, item, location, ".")
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
func DownloadFileFromS3(dataBucket string, filename string, s3FilePath string, downloadDir string) (string, error) {
	// Check Directory to download files exists
	if _, err := os.Stat(downloadDir); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(downloadDir, os.ModePerm)
		if err != nil {
			logf.Log.Error(err, "Unable to create directory to download apps")
			return "", err
		}
	}

	// Create empty file on OS File System
	file, err := os.Create(filepath.Join(downloadDir, filename))
	if err != nil {
		logf.Log.Error(err, "Failed to create file", "Filename", file)
	}
	defer file.Close()

	sess, err := S3Session()
	if err != nil {
		return "", err
	}

	downloader := s3manager.NewDownloader(sess)
	numBytes, err := downloader.Download(file,
		&s3.GetObjectInput{
			Bucket: aws.String(dataBucket),
			Key:    aws.String(s3FilePath + "/" + filename),
		})

	if err != nil {
		logf.Log.Error(err, "Failed to download file", "Filename", filename)
		return "", err
	}

	logf.Log.Info("Downloaded", "filename", file.Name(), "bytes", numBytes)
	return file.Name(), err
}

// UploadFileToS3 upload file to S3
func UploadFileToS3(dataBucket string, filename string, path string, file *os.File) (string, error) {
	sess, err := S3Session()
	if err == nil {
		uploader := s3manager.NewUploader(sess)
		numBytes, err := uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(dataBucket),
			Key:    aws.String(filepath.Join(path, filename)), // Name of the file to be saved
			Body:   file,
		})
		if err != nil {
			logf.Log.Error(err, "Error in file upload")
		}
		logf.Log.Info("Uploaded", "filename", file.Name(), "bytes", numBytes)
	}
	return filepath.Join(path, filename), err
}

// GetFileListOnS3 lists object in a bucket
func GetFileListOnS3(dataBucket string, path string) []*s3.Object {
	sess, err := S3Session()
	svc := s3.New(session.Must(sess, err))
	resp, err := svc.ListObjects(&s3.ListObjectsInput{
		Bucket: aws.String(dataBucket),
		Prefix: aws.String(path),
	})
	if err != nil {
		logf.Log.Error(err, "Failed to list objects on s3 bucket")
		return nil
	}

	return resp.Contents
}

// DeleteFilesOnS3 Delete a list of file on S3 Bucket
func DeleteFilesOnS3(bucket string, filenames []string) error {
	for _, file := range filenames {
		err := DeleteFileOnS3(bucket, file)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteFileOnS3 Delete a given file on S3 Bucket
func DeleteFileOnS3(bucket string, filename string) error {
	sess, err := S3Session()
	if err != nil {
		return err
	}
	svc := s3.New(sess)
	_, err = svc.DeleteObject(&s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(filename)})
	if err != nil {
		logf.Log.Error(err, "Unable to delete object from bucket", "Object Name", filename, "Bucket Name", bucket)
	}

	err = svc.WaitUntilObjectNotExists(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filename),
	})
	logf.Log.Info("Deleted file on S3", "File Name", filename, "Bucket", bucket)
	return err
}

// GetFilesInPathOnS3 return list of file name under a given path on S3
func GetFilesInPathOnS3(bucket string, path string) []string {
	resp := GetFileListOnS3(bucket, path)
	var files []string
	for _, key := range resp {
		logf.Log.Info("CHECKING KEY ", "KEY", *key.Key)
		if strings.Contains(*key.Key, path) {
			filename := strings.Replace(*key.Key, path, "", -1)
			// This condition filters out directories as GetFileListOnS3 returns files and directories with their absolute path's
			if len(filename) > 1 {
				logf.Log.Info("File found on bucket", "Prefix", path, "KEY", *key.Key)
				files = append(files, filename)
			}
		}
	}
	return files
}

// DownloadFilesFromS3 download given list of files from S3 to the given directory
func DownloadFilesFromS3(testDataS3Bucket string, s3AppDir string, downloadDir string, appList []string) error {
	for _, key := range appList {
		logf.Log.Info("Downloading file from S3", "File name", key)
		_, err := DownloadFileFromS3(testDataS3Bucket, key, s3AppDir, downloadDir)
		if err != nil {
			logf.Log.Error(err, "Unable to download file", "File Name", key)
			return err
		}
	}
	return nil
}

// UploadFilesToS3 upload given list of file to given location on a S3 bucket
func UploadFilesToS3(testS3Bucket string, s3TestDir string, applist []string, downloadDir string) ([]string, error) {
	var uploadedFiles []string
	for _, key := range applist {
		logf.Log.Info("Uploading file to S3", "File name", key)
		fileLocation := filepath.Join(downloadDir, key)
		fileBody, err := os.Open(fileLocation)
		if err != nil {
			logf.Log.Error(err, "Unable to open file", "File name", key)
			return nil, err
		}
		fileName, err := UploadFileToS3(testS3Bucket, key, s3TestDir, fileBody)
		if err != nil {
			logf.Log.Error(err, "Unable to upload file", "File name", key)
			return nil, err
		}
		logf.Log.Info("File upload to test S3", "File name", fileName)
		uploadedFiles = append(uploadedFiles, fileName)
	}
	return uploadedFiles, nil
}
