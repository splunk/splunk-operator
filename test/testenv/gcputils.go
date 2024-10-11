package testenv

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Set GCP Variables
var (
	gcpProjectID                 = os.Getenv("GCP_PROJECT_ID")
	gcpRegion                    = os.Getenv("GCP_REGION")
	testGCPBucket                = os.Getenv("TEST_BUCKET")
	testIndexesGCPBucket         = os.Getenv("TEST_INDEXES_GCP_BUCKET")
	enterpriseLicenseLocationGCP = os.Getenv("ENTERPRISE_LICENSE_LOCATION")
)

// GetSmartStoreIndexesBucket returns the SmartStore test bucket name
func GetSmartStoreIndexesBucket() string {
	return testIndexesGCPBucket
}

// GetDefaultGCPRegion returns the default GCP Region
func GetDefaultGCPRegion() string {
	return gcpRegion
}

// GetGCPEndpoint returns GCP Storage endpoint
func GetGCPEndpoint() string {
	return "https://storage.googleapis.com"
}

// GCPClient wraps the GCP Storage client
type GCPClient struct {
	Client *storage.Client
	Ctx    context.Context
}

// NewGCPClient initializes and returns a GCPClient
func NewGCPClient() (*GCPClient, error) {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		logf.Log.Error(err, "Failed to create GCP Storage client")
		return nil, err
	}
	return &GCPClient{
		Client: client,
		Ctx:    ctx,
	}, nil
}

// CheckPrefixExistsOnGCP checks if a prefix exists in a GCP bucket
func CheckPrefixExistsOnGCP(prefix string) bool {
	dataBucket := testIndexesGCPBucket
	client, err := NewGCPClient()
	if err != nil {
		return false
	}
	defer client.Client.Close()

	it := client.Client.Bucket(dataBucket).Objects(client.Ctx, &storage.Query{
		Prefix: prefix,
		// You can set other query parameters if needed
	})

	for {
		objAttrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			logf.Log.Error(err, "Error listing objects in GCP bucket")
			return false
		}
		logf.Log.Info("CHECKING OBJECT", "OBJECT", objAttrs.Name)
		if strings.Contains(objAttrs.Name, prefix) {
			logf.Log.Info("Prefix found in bucket", "Prefix", prefix, "Object", objAttrs.Name)
			return true
		}
	}
	return false
}

// DownloadLicenseFromGCPBucket downloads the license file from GCP
func DownloadLicenseFromGCPBucket() (string, error) {
	location := enterpriseLicenseLocationGCP
	item := "enterprise.lic"
	dataBucket := testGCPBucket
	filename, err := DownloadFileFromGCP(dataBucket, item, location, ".")
	return filename, err
}

// DownloadFileFromGCP downloads a file from a GCP bucket to a local directory
func DownloadFileFromGCP(bucketName, objectName, gcpFilePath, downloadDir string) (string, error) {
	// Ensure the download directory exists
	if _, err := os.Stat(downloadDir); errors.Is(err, os.ErrNotExist) {
		err := os.MkdirAll(downloadDir, os.ModePerm)
		if err != nil {
			logf.Log.Error(err, "Unable to create download directory")
			return "", err
		}
	}

	client, err := NewGCPClient()
	if err != nil {
		return "", err
	}
	defer client.Client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, time.Minute*10)
	defer cancel()

	objectPath := filepath.Join(gcpFilePath, objectName)
	rc, err := client.Client.Bucket(bucketName).Object(objectPath).NewReader(ctx)
	if err != nil {
		logf.Log.Error(err, "Failed to create reader for object", "Object", objectName)
		return "", err
	}
	defer rc.Close()

	localPath := filepath.Join(downloadDir, objectName)
	file, err := os.Create(localPath)
	if err != nil {
		logf.Log.Error(err, "Failed to create local file", "Filename", localPath)
		return "", err
	}
	defer file.Close()

	written, err := io.Copy(file, rc)
	if err != nil {
		logf.Log.Error(err, "Failed to download object", "Object", objectName)
		return "", err
	}

	logf.Log.Info("Downloaded", "filename", localPath, "bytes", written)
	return localPath, nil
}

// UploadFileToGCP uploads a file to a GCP bucket
func UploadFileToGCP(bucketName, objectName, path string, file *os.File) (string, error) {
	client, err := NewGCPClient()
	if err != nil {
		return "", err
	}
	defer client.Client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, time.Minute*10)
	defer cancel()

	objectPath := filepath.Join(path, objectName)
	wc := client.Client.Bucket(bucketName).Object(objectPath).NewWriter(ctx)
	defer wc.Close()

	written, err := io.Copy(wc, file)
	if err != nil {
		logf.Log.Error(err, "Failed to upload file to GCP", "Filename", objectName)
		return "", err
	}

	if err := wc.Close(); err != nil {
		logf.Log.Error(err, "Failed to finalize upload to GCP", "Filename", objectName)
		return "", err
	}

	logf.Log.Info("Uploaded", "filename", file.Name(), "bytes", written)
	return objectPath, nil
}

// GetFileListOnGCP lists objects in a GCP bucket with the given prefix
func GetFileListOnGCP(bucketName, prefix string) []*storage.ObjectAttrs {
	client, err := NewGCPClient()
	if err != nil {
		return nil
	}
	defer client.Client.Close()

	it := client.Client.Bucket(bucketName).Objects(client.Ctx, &storage.Query{
		Prefix: prefix,
	})

	var objects []*storage.ObjectAttrs
	for {
		objAttrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			logf.Log.Error(err, "Error listing objects in GCP bucket")
			return nil
		}
		objects = append(objects, objAttrs)
	}
	return objects
}

// DeleteFilesOnGCP deletes a list of files from a GCP bucket
func DeleteFilesOnGCP(bucketName string, filenames []string) error {
	client, err := NewGCPClient()
	if err != nil {
		return err
	}
	defer client.Client.Close()

	for _, file := range filenames {
		err := DeleteFileOnGCP(bucketName, file)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteFileOnGCP deletes a single file from a GCP bucket
func DeleteFileOnGCP(bucketName, objectName string) error {
	client, err := NewGCPClient()
	if err != nil {
		return err
	}
	defer client.Client.Close()

	ctx, cancel := context.WithTimeout(client.Ctx, time.Minute*5)
	defer cancel()

	err = client.Client.Bucket(bucketName).Object(objectName).Delete(ctx)
	if err != nil {
		logf.Log.Error(err, "Unable to delete object from bucket", "Object Name", objectName, "Bucket Name", bucketName)
		return err
	}

	// Optionally, verify deletion
	_, err = client.Client.Bucket(bucketName).Object(objectName).Attrs(ctx)
	if err == storage.ErrObjectNotExist {
		logf.Log.Info("Deleted file on GCP", "File Name", objectName, "Bucket", bucketName)
		return nil
	}
	if err != nil {
		logf.Log.Error(err, "Error verifying deletion of object", "Object Name", objectName, "Bucket Name", bucketName)
		return err
	}

	return errors.New("object still exists after deletion")
}

// GetFilesInPathOnGCP retrieves a list of file names under a given path in a GCP bucket
func GetFilesInPathOnGCP(bucketName, path string) []string {
	resp := GetFileListOnGCP(bucketName, path)
	var files []string
	for _, obj := range resp {
		logf.Log.Info("CHECKING OBJECT", "OBJECT", obj.Name)
		if strings.HasPrefix(obj.Name, path) {
			filename := strings.TrimPrefix(obj.Name, path)
			// This condition filters out directories as GCP returns objects with their full paths
			if len(filename) > 1 && !strings.HasSuffix(filename, "/") {
				logf.Log.Info("File found in bucket", "Path", path, "Object", obj.Name)
				files = append(files, filename)
			}
		}
	}
	return files
}

// DownloadFilesFromGCP downloads a list of files from a GCP bucket to a local directory
func DownloadFilesFromGCP(bucketName, gcpAppDir, downloadDir string, appList []string) error {
	for _, key := range appList {
		logf.Log.Info("Downloading file from GCP", "File name", key)
		_, err := DownloadFileFromGCP(bucketName, key, gcpAppDir, downloadDir)
		if err != nil {
			logf.Log.Error(err, "Unable to download file", "File Name", key)
			return err
		}
	}
	return nil
}

// UploadFilesToGCP uploads a list of files to a specified path in a GCP bucket
func UploadFilesToGCP(bucketName, gcpTestDir string, appList []string, uploadDir string) ([]string, error) {
	var uploadedFiles []string
	for _, key := range appList {
		logf.Log.Info("Uploading file to GCP", "File name", key)
		fileLocation := filepath.Join(uploadDir, key)
		fileBody, err := os.Open(fileLocation)
		if err != nil {
			logf.Log.Error(err, "Unable to open file", "File name", key)
			return nil, err
		}
		defer fileBody.Close()

		objectPath, err := UploadFileToGCP(bucketName, key, gcpTestDir, fileBody)
		if err != nil {
			logf.Log.Error(err, "Unable to upload file", "File name", key)
			return nil, err
		}
		logf.Log.Info("File uploaded to GCP", "File name", objectPath)
		uploadedFiles = append(uploadedFiles, objectPath)
	}
	return uploadedFiles, nil
}

// DisableAppsToGCP untars apps, modifies their config files to disable them, re-tars, and uploads the disabled versions to GCP
func DisableAppsToGCP(downloadDir string, appFileList []string, gcpTestDir string) ([]string, error) {
	// Create directories for untarred and disabled apps
	untarredAppsMainFolder := filepath.Join(downloadDir, "untarred_apps")
	disabledAppsFolder := filepath.Join(downloadDir, "disabled_apps")

	err := os.MkdirAll(untarredAppsMainFolder, os.ModePerm)
	if err != nil {
		logf.Log.Error(err, "Unable to create directory for untarred apps")
		return nil, err
	}

	err = os.MkdirAll(disabledAppsFolder, os.ModePerm)
	if err != nil {
		logf.Log.Error(err, "Unable to create directory for disabled apps")
		return nil, err
	}

	for _, key := range appFileList {
		// Create a unique folder for each app to avoid conflicts
		appUniqueID := uuid.New().String()
		untarredCurrentAppFolder := filepath.Join(untarredAppsMainFolder, key+"_"+appUniqueID)
		err := os.MkdirAll(untarredCurrentAppFolder, os.ModePerm)
		if err != nil {
			logf.Log.Error(err, "Unable to create folder for current app", "App", key)
			return nil, err
		}

		// Untar the app
		tarfile := filepath.Join(downloadDir, key)
		err = untarFile(tarfile, untarredCurrentAppFolder)
		if err != nil {
			logf.Log.Error(err, "Failed to untar app", "App", key)
			return nil, err
		}

		// Disable the app by modifying its config file
		appConfFile := filepath.Join(untarredCurrentAppFolder, "default", "app.conf")
		err = disableAppConfig(appConfFile)
		if err != nil {
			logf.Log.Error(err, "Failed to disable app config", "File", appConfFile)
			return nil, err
		}

		// Tar the disabled app
		tarDestination := filepath.Join(disabledAppsFolder, key)
		err = tarGzFolder(untarredCurrentAppFolder, tarDestination)
		if err != nil {
			logf.Log.Error(err, "Failed to tar disabled app", "App", key)
			return nil, err
		}
	}

	// Upload disabled apps to GCP
	uploadedFiles, err := UploadFilesToGCP(testIndexesGCPBucket, gcpTestDir, appFileList, disabledAppsFolder)
	if err != nil {
		logf.Log.Error(err, "Failed to upload disabled apps to GCP")
		return nil, err
	}

	return uploadedFiles, nil
}

// untarFile extracts a tar.gz file to the specified destination
func untarFile(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tarReader := tar.NewReader(gzr)

	for {
		header, err := tarReader.Next()
		if err != nil {
			return err
		}

		// Sanitize the file path to prevent Zip Slip
		targetPath := filepath.Join(dest, header.Name)
		if !strings.HasPrefix(targetPath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", targetPath)
		}

		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return err
		}

		targetPath = filepath.Join(dest, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			// Create Directory
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			// Create File
			err := os.MkdirAll(filepath.Dir(targetPath), os.ModePerm)
			if err != nil {
				return err
			}
			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
			// Set file permissions
			if err := os.Chmod(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		default:
			logf.Log.Info("Unknown type in tar archive", "Type", header.Typeflag, "Name", header.Name)
		}
	}
	return nil
}

// tarGzFolder creates a tar.gz archive from the specified folder
func tarGzFolder(sourceDir, tarGzPath string) error {
	file, err := os.Create(tarGzPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzw := gzip.NewWriter(file)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	err = filepath.Walk(sourceDir, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create header
		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		// Update the name to maintain the folder structure
		relPath, err := filepath.Rel(sourceDir, filePath)
		if err != nil {
			return err
		}
		header.Name = relPath

		// Write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// If not a regular file, skip
		if !info.Mode().IsRegular() {
			return nil
		}

		// Open file for reading
		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()

		// Copy file data to tar writer
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

// disableAppConfig modifies the app.conf file to disable the app
func disableAppConfig(appConfFile string) error {
	input, err := os.ReadFile(appConfFile)
	if err != nil {
		return err
	}
	lines := strings.Split(string(input), "\n")
	var outputLines []string
	inInstallSection := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "[install]") {
			inInstallSection = true
			outputLines = append(outputLines, "[install]")
			outputLines = append(outputLines, "state = disabled")
			continue
		}
		if inInstallSection {
			if strings.HasPrefix(trimmedLine, "state = enabled") {
				// Skip this line
				continue
			}
			// Exit install section on encountering another section
			if strings.HasPrefix(trimmedLine, "[") && strings.HasSuffix(trimmedLine, "]") {
				inInstallSection = false
			}
		}
		outputLines = append(outputLines, line)
	}

	output := strings.Join(outputLines, "\n")
	err = os.WriteFile(appConfFile, []byte(output), 0644)
	if err != nil {
		return err
	}

	return nil
}
