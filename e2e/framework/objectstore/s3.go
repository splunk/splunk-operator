package objectstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Provider struct {
	cfg    Config
	client *s3.Client
}

func newS3Provider(ctx context.Context, cfg Config) (Provider, error) {
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-west-2"
	}
	options := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}
	if cfg.AccessKey != "" || cfg.SecretKey != "" || cfg.SessionToken != "" {
		options = append(options, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
		))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		if cfg.S3PathStyle {
			o.UsePathStyle = true
		}
	})
	return &s3Provider{cfg: cfg, client: client}, nil
}

func (p *s3Provider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	remotePrefix := ResolveKey(p.cfg.Prefix, prefix)
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(p.cfg.Bucket),
	}
	if remotePrefix != "" {
		input.Prefix = aws.String(remotePrefix)
	}
	var objects []ObjectInfo
	for {
		resp, err := p.client.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, obj := range resp.Contents {
			if obj.Key == nil {
				continue
			}
			info := ObjectInfo{Key: *obj.Key}
			if obj.Size != nil {
				info.Size = *obj.Size
			}
			if obj.ETag != nil {
				info.ETag = strings.Trim(*obj.ETag, "\"")
			}
			if obj.LastModified != nil {
				info.LastModified = *obj.LastModified
			}
			objects = append(objects, info)
		}
		if resp.IsTruncated != nil && *resp.IsTruncated && resp.NextContinuationToken != nil {
			input.ContinuationToken = resp.NextContinuationToken
			continue
		}
		break
	}
	return objects, nil
}

func (p *s3Provider) Upload(ctx context.Context, key string, localPath string) (ObjectInfo, error) {
	remoteKey := ResolveKey(p.cfg.Prefix, key)
	file, err := os.Open(localPath)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return ObjectInfo{}, err
	}
	uploader := manager.NewUploader(p.client)
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(p.cfg.Bucket),
		Key:    aws.String(remoteKey),
		Body:   file,
	})
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: remoteKey, Size: stat.Size()}, nil
}

func (p *s3Provider) Download(ctx context.Context, key string, localPath string) (ObjectInfo, error) {
	remoteKey := ResolveKey(p.cfg.Prefix, key)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return ObjectInfo{}, err
	}
	file, err := os.Create(localPath)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer file.Close()
	downloader := manager.NewDownloader(p.client)
	written, err := downloader.Download(ctx, file, &s3.GetObjectInput{
		Bucket: aws.String(p.cfg.Bucket),
		Key:    aws.String(remoteKey),
	})
	if err != nil {
		return ObjectInfo{}, err
	}
	info := ObjectInfo{Key: remoteKey, Size: written}
	return info, nil
}

func (p *s3Provider) Delete(ctx context.Context, key string) error {
	remoteKey := ResolveKey(p.cfg.Prefix, key)
	if remoteKey == "" {
		return fmt.Errorf("object key is required")
	}
	_, err := p.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(p.cfg.Bucket),
		Key:    aws.String(remoteKey),
	})
	return err
}

func (p *s3Provider) Close() error {
	return nil
}
