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
	DownloadFiles(ctx context.Context, remoteDir string, localDir string, fileList []string) error
	GetCloudProvider() string
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

func (b *S3Backend) GetCloudProvider() string { return "eks" }

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
	return DownloadFilesFromAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, localDir, remoteDir, fileList)
}

func (b *AzureBackend) GetCloudProvider() string { return "azure" }

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

func (b *GCPBackend) GetCloudProvider() string { return "gcp" }
