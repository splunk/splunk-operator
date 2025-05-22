// controllers/storage/azure.go
package storage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	ai "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type azureClient struct {
	cli       *azblob.Client
	endpoint  string
	container string
	prefix    string
}

// NewAzureClient optionally reads client ID/secret/tenant from SecretRef.
// If SecretRef is empty, it uses DefaultAzureCredential (MSI/pod-identity).
func NewAzureClient(
	k8sClient client.Client,
	namespace, container, prefix string,
	vs ai.AiVolumeSpec,
) (StorageClient, error) {
	var cred azcore.TokenCredential
	var err error

	if vs.SecretRef != "" {
		secret := &corev1.Secret{}
		if err := k8sClient.Get(context.TODO(),
			client.ObjectKey{Namespace: namespace, Name: vs.SecretRef},
			secret,
		); err != nil {
			return nil, fmt.Errorf("fetch Azure secret: %w", err)
		}

		tenantID := string(secret.Data["azure_tenant_id"])
		clientID := string(secret.Data["azure_client_id"])
		clientSecret := string(secret.Data["azure_client_secret"])
		cred, err = azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
		if err != nil {
			return nil, fmt.Errorf("client-secret credential: %w", err)
		}
	} else {
		cred, err = azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("default azure credential: %w", err)
		}
	}

	cli, err := azblob.NewClient(vs.Endpoint, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("new Azure blob client: %w", err)
	}
	return &azureClient{
		cli:       cli,
		endpoint:  vs.Endpoint,
		container: container,
		prefix:    prefix,
	}, nil
}

func (c *azureClient) ListObjects(ctx context.Context) ([]string, error) {
	pager := c.cli.NewListBlobsFlatPager(c.container, &azblob.ListBlobsFlatOptions{Prefix: &c.prefix})
	var keys []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, b := range page.Segment.BlobItems {
			keys = append(keys, *b.Name)
		}
	}
	return keys, nil
}

func (c *azureClient) BuildLoaderBlock(uri string) string {
	// uri e.g. "https://account.blob.core.windows.net/container/prefix/file.ext"
	trim := fmt.Sprintf("%s/%s/", c.endpoint, c.container)
	p := strings.TrimPrefix(uri, trim)
	dir := path.Dir(p)
	return fmt.Sprintf(`        azure_blob:
          url: %s
          container: %s
          blob_prefix: %s
`, c.endpoint, c.container, dir)
}

func (c *azureClient) BuildWorkingDir(modelName string) string {
	// assemble https://endpoint/container/prefix/applications/<modelName>.zip
	base := fmt.Sprintf("%s/%s", c.endpoint, c.container)
	if c.prefix == "" {
		return fmt.Sprintf("%s/%s", base, modelName)
	}
	return fmt.Sprintf("%s/%s/%s", base, c.prefix, modelName)
}

// BuildArtifactURI constructs the full URI for an object key in Azure Blob Storage:
// e.g. https://account.blob.core.windows.net/container/prefix/key
func (c *azureClient) BuildArtifactURI(key string) string {
	k := strings.TrimPrefix(key, "/")
	if c.prefix != "" {
		return fmt.Sprintf("%s/%s/%s/%s", c.endpoint, c.container, c.prefix, k)
	}
	return fmt.Sprintf("%s/%s/%s", c.endpoint, c.container, k)
}

func (c *azureClient) Exists(ctx context.Context, blobName string) (bool, error) {
	// If you have a prefix, stitch it back onto the blob name:
	key := blobName
	if c.prefix != "" {
		key = path.Join(c.prefix, blobName)
	}

	// 1. Turn your azblob.Client into a service client
	svc := c.cli.ServiceClient() // (c) ServiceClient() :contentReference[oaicite:0]{index=0}

	// 2. From the service, grab the container client
	containerClient := svc.NewContainerClient(c.container) // NewContainerClient :contentReference[oaicite:1]{index=1}

	// 3. From the container, grab a blob‐level client
	blobClient := containerClient.NewBlobClient(key) // NewBlobClient :contentReference[oaicite:2]{index=2}

	// 4. Head the blob
	_, err := blobClient.GetProperties(ctx, nil) // GetProperties :contentReference[oaicite:3]{index=3}
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == 404 {
			// not found
			return false, nil
		}
		return false, fmt.Errorf("blob GetProperties: %w", err)
	}
	return true, nil
}

func (c *azureClient) GetProvider() string { return "azure" }
func (c *azureClient) GetBucket() string   { return c.container }
func (c *azureClient) GetPrefix() string   { return c.prefix }
