package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	ai "github.com/splunk/splunk-operator/api/v4"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StorageClient abstracts listing objects and building loader/workingDir snippets.
type StorageClient interface {
	// ListObjects returns all object keys under the volume’s prefix.
	ListObjects(ctx context.Context) ([]string, error)
	// BuildLoaderBlock returns the YAML snippet for model_loader based on the URI.
	BuildLoaderBlock(uri string) string
	// BuildWorkingDir returns the working_dir URI for an application package.
	BuildWorkingDir(modelName string) string
	// BuildArtifactURI builds the full URI (e.g. "s3://bucket/prefix/key") for an arbitrary object key.
	BuildArtifactURI(key string) string
	Exists(ctx context.Context, key string) (bool, error)
	GetProvider() string
	GetBucket() string
	GetPrefix() string
}

func NewStorageClient(
	k8sClient client.Client,
	namespace string,
	vs ai.AiVolumeSpec,
) (StorageClient, error) {
	u, err := url.Parse(vs.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid volume URI %q: %w", vs.Path, err)
	}

	// strip leading slash from path
	// e.g. u.Path="/prefix/..." → "prefix/..."
	prefix := strings.TrimPrefix(u.Path, "/")

	switch u.Scheme {
	case "s3":
		return NewS3Client(k8sClient, namespace, u.Host, prefix, vs)
	case "gs", "gcs":
		return NewGCSClient(k8sClient, namespace, u.Host, prefix, vs)
	case "azure":
		return NewAzureClient(k8sClient, namespace, u.Host, prefix, vs)
	case "minio":
		// everything after "//" is host (bucket) and path.  We treat u.Host as bucket,
		// vs.Endpoint *must* be set to your MinIO URL for this case.
		return NewMinioClient(k8sClient, namespace, u.Host, prefix, vs)
	default:
		return nil, fmt.Errorf("unsupported storage scheme %q", u.Scheme)
	}
}
