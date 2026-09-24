package testenv

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
)

// CloudStorageBackend abstracts cloud-specific upload/delete/download
// operations so that app framework integration tests can be written
// once and parameterized by provider.
type CloudStorageBackend interface {
	UploadFiles(ctx context.Context, testDir string, appFileList []string, localDir string) ([]string, error)
	DeleteFiles(ctx context.Context, uploadedFiles []string) error
	DeleteFile(ctx context.Context, filePath string) error
	DownloadFiles(ctx context.Context, remoteDir string, localDir string, fileList []string) error
	DisableApps(ctx context.Context, downloadDir string, appFileList []string, testDir string) error
	GetCloudProvider() string
	GetFakeSecretData() map[string][]byte
}

// NewCloudStorageBackend returns the correct CloudStorageBackend implementation
// based on the current ClusterProvider. The bucket and dataBucket parameters
// are used by S3 and GCP; Azure ignores them.
func NewCloudStorageBackend(bucket, dataBucket string) CloudStorageBackend {
	switch ClusterProvider {
	case "eks":
		return NewS3Backend(bucket, dataBucket)
	case "azure":
		return NewAzureBackend()
	case "gcp":
		return NewGCPBackend(bucket, dataBucket)
	default:
		Fail(fmt.Sprintf("unsupported cluster provider: %s", ClusterProvider))
		return nil
	}
}

// S3Backend implements CloudStorageBackend for AWS S3.
type S3Backend struct {
	Bucket     string
	DataBucket string
}

func NewS3Backend(bucket, dataBucket string) *S3Backend {
	return &S3Backend{Bucket: bucket, DataBucket: dataBucket}
}

func (b *S3Backend) UploadFiles(_ context.Context, testDir string, appFileList []string, localDir string) ([]string, error) {
	return UploadFilesToS3(b.Bucket, testDir, appFileList, localDir)
}

func (b *S3Backend) DeleteFiles(_ context.Context, uploadedFiles []string) error {
	return DeleteFilesOnS3(b.Bucket, uploadedFiles)
}

func (b *S3Backend) DownloadFiles(_ context.Context, remoteDir string, localDir string, fileList []string) error {
	return DownloadFilesFromS3(b.DataBucket, remoteDir, localDir, fileList)
}

func (b *S3Backend) DeleteFile(_ context.Context, filePath string) error {
	return DeleteFileOnS3(b.Bucket, filePath)
}

func (b *S3Backend) DisableApps(_ context.Context, downloadDir string, appFileList []string, testDir string) error {
	return DisableAppsToS3(downloadDir, appFileList, testDir)
}

func (b *S3Backend) GetCloudProvider() string { return "eks" }

func (b *S3Backend) GetFakeSecretData() map[string][]byte {
	return map[string][]byte{"s3_access_key": []byte(RandomDNSName(5)), "s3_secret_key": []byte(RandomDNSName(5))}
}

// AzureBackend implements CloudStorageBackend for Azure Blob Storage.
type AzureBackend struct{}

func NewAzureBackend() *AzureBackend {
	return &AzureBackend{}
}

func (b *AzureBackend) UploadFiles(ctx context.Context, testDir string, appFileList []string, localDir string) ([]string, error) {
	return UploadFilesToAzure(ctx, StorageAccount, StorageAccountKey, localDir, testDir, appFileList)
}

func (b *AzureBackend) DeleteFiles(ctx context.Context, uploadedFiles []string) error {
	client := &AzureBlobClient{}
	return client.DeleteFilesOnAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, uploadedFiles)
}

func (b *AzureBackend) DownloadFiles(ctx context.Context, remoteDir string, localDir string, fileList []string) error {
	containerName := "/test-data/" + remoteDir
	return DownloadFilesFromAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, localDir, containerName, fileList)
}

func (b *AzureBackend) DeleteFile(ctx context.Context, filePath string) error {
	client := &AzureBlobClient{}
	return client.DeleteFileOnAzure(ctx, filePath, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount)
}

func (b *AzureBackend) DisableApps(ctx context.Context, downloadDir string, appFileList []string, testDir string) error {
	return DisableAppsOnAzure(ctx, downloadDir, appFileList, testDir)
}

func (b *AzureBackend) GetCloudProvider() string { return "azure" }

func (b *AzureBackend) GetFakeSecretData() map[string][]byte {
	return map[string][]byte{"azure_sa_name": []byte(RandomDNSName(5)), "azure_sa_secret_key": []byte(RandomDNSName(5))}
}

// GCPBackend implements CloudStorageBackend for Google Cloud Storage.
type GCPBackend struct {
	Bucket     string
	DataBucket string
}

func NewGCPBackend(bucket, dataBucket string) *GCPBackend {
	return &GCPBackend{Bucket: bucket, DataBucket: dataBucket}
}

func (b *GCPBackend) UploadFiles(_ context.Context, testDir string, appFileList []string, localDir string) ([]string, error) {
	return UploadFilesToGCP(b.Bucket, testDir, appFileList, localDir)
}

func (b *GCPBackend) DeleteFiles(_ context.Context, uploadedFiles []string) error {
	return DeleteFilesOnGCP(b.Bucket, uploadedFiles)
}

func (b *GCPBackend) DownloadFiles(_ context.Context, remoteDir string, localDir string, fileList []string) error {
	return DownloadFilesFromGCP(b.DataBucket, remoteDir, localDir, fileList)
}

func (b *GCPBackend) DeleteFile(_ context.Context, filePath string) error {
	return DeleteFileOnGCP(b.Bucket, filePath)
}

func (b *GCPBackend) DisableApps(_ context.Context, downloadDir string, appFileList []string, testDir string) error {
	return DisableAppsToGCP(downloadDir, appFileList, testDir)
}

func (b *GCPBackend) GetCloudProvider() string { return "gcp" }

func (b *GCPBackend) GetFakeSecretData() map[string][]byte {
	return map[string][]byte{"key.json": []byte(RandomDNSName(5))}
}

// CloudCleanup returns a func() that deletes the given files using the backend,
// matching the signature expected by TeardownAppFrameworkTestCaseEnv.
func CloudCleanup(ctx context.Context, backend CloudStorageBackend, uploadedApps []string) func() {
	return func() {
		backend.DeleteFiles(ctx, uploadedApps)
	}
}
