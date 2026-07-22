// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	sqsaws "github.com/splunk/splunk-operator/pkg/splunk/client/queue/aws"
	s3aws "github.com/splunk/splunk-operator/pkg/splunk/client/storage/aws"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// QueueObjectStorageConfig holds resolved Queue and ObjectStorage specs with credentials.
type QueueObjectStorageConfig struct {
	Queue     enterpriseApi.QueueSpec
	OS        enterpriseApi.ObjectStorageSpec
	AccessKey string
	SecretKey string
	Version   string
}

// ResolveQueueAndObjectStorage fetches Queue and ObjectStorage CRs, resolves
// their endpoints, and extracts credentials from the referenced secret.
// Credentials are resolved when the Queue's VolList is non-empty; an empty
// VolList signals IRSA / workload identity.
func ResolveQueueAndObjectStorage(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, queueRef, osRef corev1.ObjectReference) (*QueueObjectStorageConfig, error) {
	cfg := &QueueObjectStorageConfig{}

	if queueRef.Name != "" {
		ns := cr.GetNamespace()
		if queueRef.Namespace != "" {
			ns = queueRef.Namespace
		}
		var queue enterpriseApi.Queue
		if err := c.Get(ctx, types.NamespacedName{Name: queueRef.Name, Namespace: ns}, &queue); err != nil {
			return nil, err
		}
		cfg.Queue = queue.Spec
	}
	if cfg.Queue.Provider == "sqs" || cfg.Queue.Provider == "sqs_cp" {
		if cfg.Queue.SQS.Endpoint == "" && cfg.Queue.SQS.AuthRegion != "" {
			ep, err := sqsaws.ResolveSQSEndpoint(ctx, cfg.Queue.SQS.AuthRegion)
			if err != nil {
				return nil, err
			}
			cfg.Queue.SQS.Endpoint = ep
		}
	}

	if osRef.Name != "" {
		ns := cr.GetNamespace()
		if osRef.Namespace != "" {
			ns = osRef.Namespace
		}
		var os enterpriseApi.ObjectStorage
		if err := c.Get(ctx, types.NamespacedName{Name: osRef.Name, Namespace: ns}, &os); err != nil {
			return nil, err
		}
		cfg.OS = os.Spec
	}
	if cfg.OS.Provider == "s3" {
		if cfg.OS.S3.Endpoint == "" && cfg.Queue.SQS.AuthRegion != "" {
			ep, err := s3aws.ResolveS3Endpoint(ctx, cfg.Queue.SQS.AuthRegion)
			if err != nil {
				return nil, err
			}
			cfg.OS.S3.Endpoint = ep
		}
	}

	if (cfg.Queue.Provider == "sqs" || cfg.Queue.Provider == "sqs_cp") && len(cfg.Queue.SQS.VolList) > 0 {
		for _, vol := range cfg.Queue.SQS.VolList {
			if vol.SecretRef != "" {
				accessKey, secretKey, version, err := getQueueRemoteVolumeSecrets(ctx, vol, c, cr)
				if err != nil {
					return nil, err
				}
				cfg.AccessKey = accessKey
				cfg.SecretKey = secretKey
				cfg.Version = version
			}
		}
	}

	return cfg, nil
}

func getQueueRemoteVolumeSecrets(ctx context.Context, volume enterpriseApi.SQSVolumeSpec, c splcommon.ControllerClient, cr splcommon.MetaObject) (string, string, string, error) {
	secret, err := splutil.GetSecretByName(ctx, c, cr.GetNamespace(), volume.SecretRef)
	if err != nil {
		return "", "", "", err
	}

	accessKey := string(secret.Data["s3_access_key"])
	secretKey := string(secret.Data["s3_secret_key"])

	if accessKey == "" {
		return "", "", "", fmt.Errorf("access Key is missing")
	}
	if secretKey == "" {
		return "", "", "", fmt.Errorf("secret Key is missing")
	}

	return accessKey, secretKey, secret.ResourceVersion, nil
}
