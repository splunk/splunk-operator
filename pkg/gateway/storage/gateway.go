package gateway

import (
	"context"
	storagemodel "github.com/splunk/splunk-operator/pkg/gateway/storage/model"
	"time"
)

// GetS3Client gets the required S3Client based on the provider
type GetS3Client func(context.Context, string /* bucket */, string, /* AWS access key ID */
	string /* AWS secret access key */, string /* Prefix */, string /* StartAfter */, string /* Region */, string /* Endpoint */, storagemodel.GetInitFunc) (StorageGateway, error)

type GetS3ClientWrapper struct {
	GetS3Client
	storagemodel.GetInitFunc
}

type SplunkS3Client struct {
	Client StorageGateway
}

type RemoteObject struct {
	Etag         *string
	Key          *string
	LastModified *time.Time
	Size         *int64
	StorageClass *string
}

// S3Response struct contains list of RemoteObject objects as part of S3 response
type S3Response struct {
	Objects []*RemoteObject
}

// Factory is the interface for creating new Gateway objects.
type Factory interface {
	NewStorageGateway(ctx context.Context, sad *storagemodel.Credentials, publisher storagemodel.EventPublisher) (StorageGateway, error)
}

// StorageGateway holds the state information for talking to
// external storage interface gateway.
type StorageGateway interface {
	GetAppsList(context.Context) (storagemodel.StorageResponse, error)
	GetInitContainerImage(context.Context) string
	GetInitContainerCmd(context.Context, string /* endpoint */, string /* bucket */, string /* path */, string /* app src name */, string /* app mnt */) []string
	DownloadApp(context.Context, string, string, string) (bool, error)
}
