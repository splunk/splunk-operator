package impl

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/storage"
	storagemodel "github.com/splunk/splunk-operator/pkg/gateway/storage/model"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type awsGatewayFactory struct {
	log logr.Logger
	//credentials to log on to aws
	credentials *storagemodel.Credentials
	// client for talking to aws
	client *resty.Client
}

// NewGatewayFactory  new gateway factory to create gateway interface
func NewGatewayFactory() gateway.Factory {
	factory := awsGatewayFactory{}
	err := factory.init()
	if err != nil {
		return nil // FIXME we have to throw some kind of exception or error here
	}
	return factory
}

func (f *awsGatewayFactory) init() error {
	return nil
}

func (f awsGatewayFactory) awsGateway(ctx context.Context, sad *storagemodel.Credentials, publisher storagemodel.EventPublisher) (*awsGateway, error) {
	reqLogger := log.FromContext(ctx)
	f.log = reqLogger.WithName("awsGateway")

	var s3SplunkClient SplunkAWSS3Client
	var err error

	// for backward compatibility, if `region` is not specified in the CR,
	// then derive the region from the endpoint.
	if sad.Region == "" {
		err = GetRegion(ctx, sad.Endpoint, &sad.Region)
		if err != nil {
			return nil, err
		}
	}

	cl := sad.InitFunc(ctx, sad.Region, sad.AccessKeyID, sad.SecretAccessKey)
	if cl == nil {
		err = fmt.Errorf("failed to create an AWS S3 client")
		return nil, err
	}

	s3SplunkClient = cl.(*s3.S3)
	downloader := s3manager.NewDownloaderWithClient(cl.(*s3.S3))

	return &awsGateway{
		credentials: &storagemodel.Credentials{
			Region:          sad.Region,
			BucketName:      sad.BucketName,
			AccessKeyID:     sad.AccessKeyID,
			SecretAccessKey: sad.SecretAccessKey,
			Prefix:          sad.Prefix,
			StartAfter:      sad.StartAfter,
		},
		client:     s3SplunkClient,
		downloader: downloader,
		log:        f.log,
	}, nil
}

// NewGateway returns a new Splunk Gateway using global
// configuration for finding the Splunk services.
func (f awsGatewayFactory) NewStorageGateway(ctx context.Context, sad *storagemodel.Credentials, publisher storagemodel.EventPublisher) (gateway.StorageGateway, error) {
	return f.awsGateway(ctx, sad, publisher)
}
