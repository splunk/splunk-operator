package client

import (
	"fmt"
	"time"
)

//GetS3Client gets the required S3Client based on the provider
type GetS3Client func(string /* bucket */, string, /* AWS access key ID */
	string /* AWS secret access key */, string /* Prefix */, string /* StartAfter */, string /* Endpoint */) S3Client

// S3Clients is a map of provider name to init functions
var S3Clients = make(map[string]GetS3Client)

// S3Client is an interface to implement different S3 client APIs
type S3Client interface {
	GetAppsList() (S3Response, error)
	GetInitContainerImage() string
	GetInitContainerCmd(string /* endpoint */, string /* bucket */, string /* path */, string /* app src name */, string /* app mnt */) []string
}

// S3Response struct contains list of RemoteObject objects as part of S3 response
type S3Response struct {
	Objects []*RemoteObject
}

// RemoteObject struct contains contents returned as part of S3 response
type RemoteObject struct {
	Etag         *string
	Key          *string
	LastModified *time.Time
	Size         *int64
	StorageClass *string
}

//RegisterS3Client registers the respective Client
func RegisterS3Client(provider string) {
	switch provider {
	case "aws":
		RegisterAWSS3Client()
	case "minio":
		RegisterMinioClient()
	default:
		fmt.Println("ERROR: Invalid provider specified: ", provider)
	}
}
