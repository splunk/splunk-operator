// controllers/storage/gcs.go
package storage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"google.golang.org/api/iterator"

	"cloud.google.com/go/storage"
	ai "github.com/splunk/splunk-operator/api/v4"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type gcsClient struct {
	cli    *storage.Client
	bucket string
	prefix string
}

func NewGCSClient(
	k8sClient client.Client,
	namespace, bucket, prefix string,
	vs ai.AiVolumeSpec,
) (StorageClient, error) {
	opts := []option.ClientOption{}

	if vs.SecretRef != "" {
		secret := &corev1.Secret{}
		if err := k8sClient.Get(context.TODO(),
			client.ObjectKey{Namespace: namespace, Name: vs.SecretRef},
			secret,
		); err != nil {
			return nil, fmt.Errorf("fetch GCP secret: %w", err)
		}

		keyJSON, ok := secret.Data["service_account.json"]
		if !ok {
			return nil, fmt.Errorf("secret %q missing key 'service_account.json'", vs.SecretRef)
		}
		opts = append(opts, option.WithCredentialsJSON(keyJSON))
	}

	cli, err := storage.NewClient(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("new GCS client: %w", err)
	}
	return &gcsClient{cli: cli, bucket: bucket, prefix: prefix}, nil
}

func (c *gcsClient) ListObjects(ctx context.Context) ([]string, error) {
	it := c.cli.Bucket(c.bucket).Objects(ctx, &storage.Query{Prefix: c.prefix})
	var keys []string
	for {
		obj, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		keys = append(keys, obj.Name)
	}
	return keys, nil
}

func (c *gcsClient) BuildLoaderBlock(uri string) string {
	// uri e.g. "gs://bucket/prefix/file.ext"
	trim := fmt.Sprintf("gs://%s/", c.bucket)
	p := strings.TrimPrefix(uri, trim)
	dir := path.Dir(p)
	return fmt.Sprintf(`        gcs_artifact:
          bucket: %s
          object_key: %s
`, c.bucket, dir)
}

func (c *gcsClient) BuildWorkingDir(modelName string) string {
	// assemble gs://bucket/prefix/applications/<modelName>.zip
	if c.prefix == "" {
		return fmt.Sprintf("gs://%s/%s", c.bucket, modelName)
	}
	return fmt.Sprintf("gs://%s/%s/%s", c.bucket, c.prefix, modelName)
}

func (c *gcsClient) BuildArtifactURI(key string) string {
	// key is "prefix/file.ext"
	trim := fmt.Sprintf("%s/", c.prefix)
	p := strings.TrimPrefix(key, trim)
	return fmt.Sprintf("gs://%s/%s", c.bucket, p)
}

func (c *gcsClient) Exists(ctx context.Context, key string) (bool, error) {
	filePath := fmt.Sprintf("%s/%s", c.prefix, key)
	obj := c.cli.Bucket(c.bucket).Object(filePath)
	_, err := obj.Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("GCS HeadObject(%q): %w", filePath, err)
	}
	return true, nil
}

func (c *gcsClient) GetProvider() string { return "gcs" }
func (c *gcsClient) GetBucket() string   { return c.bucket }
func (c *gcsClient) GetPrefix() string   { return c.prefix }
