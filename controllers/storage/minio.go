package storage

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	ai "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewMinioClient(
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
	// no SecretRef → AWS SDK default chain (IRSA, env, etc)
	sess, err := session.NewSession(awsCfg)
	if err != nil {
		return nil, err
	}
	return &s3Client{cli: s3.New(sess), bucket: bucket, prefix: prefix}, nil
}
