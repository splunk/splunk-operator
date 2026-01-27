package objectstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

type azureProvider struct {
	cfg     Config
	client  *container.Client
	baseURL string
}

func newAzureProvider(ctx context.Context, cfg Config) (Provider, error) {
	containerURL, err := buildAzureContainerURL(cfg)
	if err != nil {
		return nil, err
	}
	var client *container.Client
	if strings.TrimSpace(cfg.AzureSASToken) != "" {
		client, err = container.NewClientWithNoCredential(containerURL, nil)
	} else if strings.TrimSpace(cfg.AzureKey) != "" {
		if strings.TrimSpace(cfg.AzureAccount) == "" {
			return nil, fmt.Errorf("azure account name is required for shared key auth")
		}
		credential, err := azblob.NewSharedKeyCredential(cfg.AzureAccount, cfg.AzureKey)
		if err != nil {
			return nil, err
		}
		client, err = container.NewClientWithSharedKeyCredential(containerURL, credential, nil)
	} else {
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, err
		}
		client, err = container.NewClient(containerURL, credential, nil)
	}
	if err != nil {
		return nil, err
	}
	return &azureProvider{cfg: cfg, client: client, baseURL: containerURL}, nil
}

func buildAzureContainerURL(cfg Config) (string, error) {
	serviceURL := strings.TrimRight(strings.TrimSpace(cfg.AzureEndpoint), "/")
	if serviceURL == "" {
		if strings.TrimSpace(cfg.AzureAccount) == "" {
			return "", fmt.Errorf("azure endpoint or account name is required")
		}
		serviceURL = fmt.Sprintf("https://%s.blob.core.windows.net", cfg.AzureAccount)
	}
	containerURL := fmt.Sprintf("%s/%s", serviceURL, cfg.Bucket)
	if strings.TrimSpace(cfg.AzureSASToken) != "" {
		token := strings.TrimPrefix(strings.TrimSpace(cfg.AzureSASToken), "?")
		containerURL = containerURL + "?" + token
	}
	return containerURL, nil
}

func (p *azureProvider) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	remotePrefix := ResolveKey(p.cfg.Prefix, prefix)
	options := &container.ListBlobsFlatOptions{}
	if remotePrefix != "" {
		options.Prefix = &remotePrefix
	}
	pager := p.client.NewListBlobsFlatPager(options)
	var objects []ObjectInfo
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		if resp.Segment == nil {
			continue
		}
		for _, item := range resp.Segment.BlobItems {
			if item == nil || item.Name == nil {
				continue
			}
			info := ObjectInfo{Key: *item.Name}
			if item.Properties != nil {
				if item.Properties.ContentLength != nil {
					info.Size = *item.Properties.ContentLength
				}
				if item.Properties.LastModified != nil {
					info.LastModified = *item.Properties.LastModified
				}
				if item.Properties.ETag != nil {
					info.ETag = string(*item.Properties.ETag)
				}
			}
			objects = append(objects, info)
		}
	}
	return objects, nil
}

func (p *azureProvider) Upload(ctx context.Context, key string, localPath string) (ObjectInfo, error) {
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
	blobClient := p.client.NewBlockBlobClient(remoteKey)
	if _, err := blobClient.UploadFile(ctx, file, nil); err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: remoteKey, Size: stat.Size()}, nil
}

func (p *azureProvider) Download(ctx context.Context, key string, localPath string) (ObjectInfo, error) {
	remoteKey := ResolveKey(p.cfg.Prefix, key)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return ObjectInfo{}, err
	}
	file, err := os.Create(localPath)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer file.Close()
	blobClient := p.client.NewBlockBlobClient(remoteKey)
	written, err := blobClient.DownloadFile(ctx, file, nil)
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: remoteKey, Size: written}, nil
}

func (p *azureProvider) Delete(ctx context.Context, key string) error {
	remoteKey := ResolveKey(p.cfg.Prefix, key)
	blobClient := p.client.NewBlobClient(remoteKey)
	_, err := blobClient.Delete(ctx, nil)
	return err
}

func (p *azureProvider) Close() error {
	return nil
}
