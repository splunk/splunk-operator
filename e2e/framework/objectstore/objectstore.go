package objectstore

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Config describes how to connect to an object store provider.
type Config struct {
	Provider           string
	Bucket             string
	Prefix             string
	Region             string
	Endpoint           string
	AccessKey          string
	SecretKey          string
	SessionToken       string
	S3PathStyle        bool
	GCPProject         string
	GCPCredentialsFile string
	GCPCredentialsJSON string
	AzureAccount       string
	AzureKey           string
	AzureEndpoint      string
	AzureSASToken      string
}

// ObjectInfo captures metadata about a remote object.
type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

// Provider is a generic object store client.
type Provider interface {
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
	Upload(ctx context.Context, key string, localPath string) (ObjectInfo, error)
	Download(ctx context.Context, key string, localPath string) (ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	Close() error
}

// NewProvider creates a provider client based on config.
func NewProvider(ctx context.Context, cfg Config) (Provider, error) {
	provider := NormalizeProvider(cfg.Provider)
	if provider == "" {
		return nil, fmt.Errorf("objectstore provider is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("objectstore bucket is required")
	}
	cfg.Provider = provider
	switch provider {
	case "s3":
		return newS3Provider(ctx, cfg)
	case "gcs":
		return newGCSProvider(ctx, cfg)
	case "azure":
		return newAzureProvider(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported objectstore provider: %s", cfg.Provider)
	}
}

// NormalizeProvider maps known aliases to provider names.
func NormalizeProvider(value string) string {
	provider := strings.ToLower(strings.TrimSpace(value))
	switch provider {
	case "aws", "s3", "minio":
		return "s3"
	case "gcp", "gcs":
		return "gcs"
	case "azure", "blob":
		return "azure"
	default:
		return provider
	}
}

// ResolveKey joins a base prefix with a key without introducing double slashes.
func ResolveKey(prefix string, key string) string {
	cleanPrefix := strings.TrimPrefix(prefix, "/")
	cleanKey := strings.TrimPrefix(key, "/")
	if cleanPrefix == "" {
		return cleanKey
	}
	if cleanKey == "" {
		return cleanPrefix
	}
	if strings.HasSuffix(cleanPrefix, "/") {
		return cleanPrefix + cleanKey
	}
	return cleanPrefix + "/" + cleanKey
}
