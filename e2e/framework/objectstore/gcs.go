package objectstore

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type gcsProvider struct {
	cfg    Config
	client *storage.Client
}

func newGCSProvider(ctx context.Context, cfg Config) (Provider, error) {
	options := []option.ClientOption{}
	if strings.TrimSpace(cfg.GCPCredentialsJSON) != "" {
		options = append(options, option.WithCredentialsJSON([]byte(cfg.GCPCredentialsJSON)))
	} else if strings.TrimSpace(cfg.GCPCredentialsFile) != "" {
		options = append(options, option.WithCredentialsFile(cfg.GCPCredentialsFile))
	}
	client, err := storage.NewClient(ctx, options...)
	if err != nil {
		return nil, err
	}
	return &gcsProvider{cfg: cfg, client: client}, nil
}

func (p *gcsProvider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	remotePrefix := ResolveKey(p.cfg.Prefix, prefix)
	it := p.client.Bucket(p.cfg.Bucket).Objects(ctx, &storage.Query{Prefix: remotePrefix})
	var objects []ObjectInfo
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		objects = append(objects, ObjectInfo{
			Key:          attrs.Name,
			Size:         attrs.Size,
			ETag:         attrs.Etag,
			LastModified: attrs.Updated,
		})
	}
	return objects, nil
}

func (p *gcsProvider) Upload(ctx context.Context, key string, localPath string) (ObjectInfo, error) {
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
	writer := p.client.Bucket(p.cfg.Bucket).Object(remoteKey).NewWriter(ctx)
	if _, err := io.Copy(writer, file); err != nil {
		closeErr := writer.Close()
		if closeErr != nil {
			return ObjectInfo{}, closeErr
		}
		return ObjectInfo{}, err
	}
	if err := writer.Close(); err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: remoteKey, Size: stat.Size()}, nil
}

func (p *gcsProvider) Download(ctx context.Context, key string, localPath string) (ObjectInfo, error) {
	remoteKey := ResolveKey(p.cfg.Prefix, key)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return ObjectInfo{}, err
	}
	reader, err := p.client.Bucket(p.cfg.Bucket).Object(remoteKey).NewReader(ctx)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer reader.Close()
	file, err := os.Create(localPath)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer file.Close()
	written, err := file.ReadFrom(reader)
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: remoteKey, Size: written}, nil
}

func (p *gcsProvider) Delete(ctx context.Context, key string) error {
	remoteKey := ResolveKey(p.cfg.Prefix, key)
	return p.client.Bucket(p.cfg.Bucket).Object(remoteKey).Delete(ctx)
}

func (p *gcsProvider) Close() error {
	return p.client.Close()
}
