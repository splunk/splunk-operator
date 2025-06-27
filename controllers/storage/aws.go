// controllers/storage/s3.go
package storage

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	ai "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// s3Client implements StorageClient for AWS S3.
type s3Client struct {
	cli    *s3.S3
	bucket string
	prefix string
}

func NewS3Client(
	k8sClient client.Client,
	namespace, bucket, prefix string,
	vs ai.AiVolumeSpec,
) (StorageClient, error) {
	awsCfg := &aws.Config{
		Endpoint:         aws.String(vs.Endpoint),
		Region:           aws.String(vs.Region),
		S3ForcePathStyle: aws.Bool(true),
	}

	if vs.SecretRef != "" {
		// static creds from k8s secret
		secret := &corev1.Secret{}
		if err := k8sClient.Get(context.TODO(),
			client.ObjectKey{Namespace: namespace, Name: vs.SecretRef},
			secret,
		); err != nil {
			return nil, err
		}
		awsCfg.Credentials = credentials.NewStaticCredentials(
			string(secret.Data["s3_access_key"]),
			string(secret.Data["s3_secret_key"]),
			"",
		)
	}
	// if no SecretRef, AWS SDK will pick up IRSA / env / shared‐config by itself

	sess, err := session.NewSession(awsCfg)
	if err != nil {
		return nil, err
	}
	return &s3Client{cli: s3.New(sess), bucket: bucket, prefix: prefix}, nil
}

// ListObjects returns all object keys under the configured prefix, across all pages.
func (c *s3Client) ListObjects(ctx context.Context) ([]string, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(c.prefix),
		// Note: no Delimiter, so you get every key under that prefix
	}

	var keys []string
	// This will call your function once per page of up to 1000 objects
	err := c.cli.ListObjectsV2PagesWithContext(ctx, input,
		func(page *s3.ListObjectsV2Output, lastPage bool) bool {
			for _, obj := range page.Contents {
				keys = append(keys, aws.StringValue(obj.Key))
			}
			// return true to keep paginating
			return true
		},
	)
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func (c *s3Client) Exists(ctx context.Context, key string) (bool, error) {
	filePath := fmt.Sprintf("%s/%s", c.prefix, key)
	_, err := c.cli.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(filePath),
	})
	if err != nil {
		// 404 → object missing
		if aerr, ok := err.(awserr.RequestFailure); ok && aerr.StatusCode() == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// BuildLoaderBlock returns the `model_loader` YAML snippet for this URI.
func (c *s3Client) BuildLoaderBlock(uri string) string {
	// uri is "s3://bucket/prefix/.../file"
	trim := fmt.Sprintf("s3://%s/", c.bucket)
	p := strings.TrimPrefix(uri, trim)
	dir := path.Dir(p)
	return fmt.Sprintf(`        s3_artifact:
          bucket: %s
          s3_key_prefix: %s
`, c.bucket, dir)
}

// BuildWorkingDir returns the working_dir URI for the application ZIP.
func (c *s3Client) BuildWorkingDir(modelName string) string {
	if c.prefix == "" {
		return fmt.Sprintf("s3://%s/%s", c.bucket, modelName)
	}
	return fmt.Sprintf("s3://%s/%s/%s", c.bucket, c.prefix, modelName)
}

// BuildArtifactURI builds a “s3://bucket[/prefix]/key” URI.
func (c *s3Client) BuildArtifactURI(key string) string {
	// strip any leading slash on key
	k := strings.TrimPrefix(key, "/")
	if c.prefix != "" {
		return fmt.Sprintf("s3://%s/%s/%s", c.bucket, c.prefix, k)
	}
	return fmt.Sprintf("s3://%s/%s", c.bucket, k)
}

func (c *s3Client) GetProvider() string { return "s3" }
func (c *s3Client) GetBucket() string   { return c.bucket }
func (c *s3Client) GetPrefix() string   { return c.prefix }
