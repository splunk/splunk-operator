package testenv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Set Global Variables
var (
	ClusterProvider = os.Getenv("TEST_CLUSTER_PLATFORM")
)

// Set S3 Variables
var (
	s3Region                  = os.Getenv("S3_REGION")
	testS3Bucket              = os.Getenv("TEST_BUCKET")
	testIndexesS3Bucket       = os.Getenv("TEST_INDEXES_S3_BUCKET")
	enterpriseLicenseLocation = os.Getenv("ENTERPRISE_LICENSE_LOCATION")
	s3deleteWaitTime          = 60
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

// S3Config Create config object for S3 bucket connection
func S3Config() (*aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(s3Region))
	if err != nil {
		logf.Log.Error(err, "Failed to create config for S3")
	}
	return &cfg, err
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

	cfg, err := S3Config()
	if err != nil {
		return "", err
	}

	s3Client := s3.NewFromConfig(*cfg)
	downloader := manager.NewDownloader(s3Client)
	numBytes, err := downloader.Download(context.TODO(), file,
		&s3.GetObjectInput{
			Bucket: aws.String(dataBucket),
			Key:    aws.String(s3FilePath + filename),
		})

	if err != nil {
		logf.Log.Error(err, "Failed to download file", "Bucket", dataBucket, "Path", s3FilePath, "Filename", filename)
		return "", err
	}

	logf.Log.Info("Downloaded", "filename", file.Name(), "bytes", numBytes)
	return file.Name(), err
}

// UploadFileToS3 upload file to S3
func UploadFileToS3(dataBucket string, filename string, path string, file *os.File) (string, error) {
	cfg, err := S3Config()
	if err == nil {
		s3Client := s3.NewFromConfig(*cfg)
		uploader := manager.NewUploader(s3Client)
		numBytes, err := uploader.Upload(context.TODO(), &s3.PutObjectInput{
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

func GetFileListOnS3(dataBucket string, path string) []types.Object {
	cfg, err := S3Config()
	svc := s3.NewFromConfig(*cfg)
	resp, err := svc.ListObjects(context.TODO(), &s3.ListObjectsInput{
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
	cfg, err := S3Config()
	if err != nil {
		return err
	}
	svc := s3.NewFromConfig(*cfg)
	_, err = svc.DeleteObject(context.TODO(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(filename)})
	if err != nil {
		logf.Log.Error(err, "Unable to delete object from bucket", "Object Name", filename, "Bucket Name", bucket)
	}

	waiter := s3.NewObjectNotExistsWaiter(svc)
	waiter.Wait(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filename),
	}, time.Duration(time.Duration(s3deleteWaitTime).Seconds()))
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

// DisableAppsToS3 untar apps, modify their conf file to disable them, re-tar and upload the disabled version to S3
func DisableAppsToS3(downloadDir string, appFileList []string, s3TestDir string) ([]string, error) {

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
		// Use filepath.ReadDir to reliably get the first directory
		entries, err := os.ReadDir(untarredCurrentAppFolder)
		if err != nil {
			log.Fatalln(err)
			return nil, err
		}

		var appFolderName string
		for _, entry := range entries {
			if entry.IsDir() {
				appFolderName = entry.Name()
				break
			}
		}

		if appFolderName == "" {
			err := fmt.Errorf("no app directory found in %s", untarredCurrentAppFolder)
			log.Fatalln(err)
			return nil, err
		}

		untarredAppRootFolder := filepath.Join(untarredCurrentAppFolder, appFolderName)

		// - Edit /default/app.conf (add "state = disabled" in [install] stanza)
		appConfFile := untarredAppRootFolder + "/default/app.conf"
		input, err := os.ReadFile(appConfFile)
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
		err = os.WriteFile(appConfFile, []byte(output), 0644)
		if err != nil {
			log.Fatalln(err)
		}

		// Tar disabled app folder
		tarDestination := disabledAppsFolder + "/" + key
		cmd = exec.Command("tar", "-czf", tarDestination, "--directory", untarredCurrentAppFolder, appFolderName)
		err = cmd.Run()
		if err != nil {
			log.Fatalf("Failed to create tar archive for disabled app %s: %v", key, err)
			return nil, err
		}
	}

	// Upload disabled apps to S3
	uploadedFiles, err := UploadFilesToS3(testIndexesS3Bucket, s3TestDir, appFileList, disabledAppsFolder)
	if err != nil {
		log.Fatalf("Failed to upload disabled apps to S3: %v", err)
		return nil, err
	}

	return uploadedFiles, nil
}
