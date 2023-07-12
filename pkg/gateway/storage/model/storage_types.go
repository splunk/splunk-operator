package model

import (
	"context"
	"time"
)

// EventPublisher is a function type for publishing events associated
// with gateway functions.
type EventPublisher func(ctx context.Context, eventType, reason, message string)

type RemoteObject struct {
	Etag         *string
	Key          *string
	LastModified *time.Time
	Size         *int64
	StorageClass *string
}

// StorageResponse struct contains list of RemoteObject objects as part of Storage response
type StorageResponse struct {
	Objects []*RemoteObject
}

// GetInitFunc gets the init function pointer which returns the new Storage session client object
type GetInitFunc func(context.Context, string, string, string) interface{}

// Credentials contains the information necessary to communicate with
// the external storage service
type Credentials struct {
	BucketName      string
	AccessKeyID     string
	SecretAccessKey string
	Prefix          string
	StartAfter      string
	Region          string
	Endpoint        string
	InitFunc        GetInitFunc
}
