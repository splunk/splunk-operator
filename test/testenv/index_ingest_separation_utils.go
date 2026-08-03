// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
package testenv

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// DeployQueueAndObjectStorage deploys a Queue and ObjectStorage CR and returns both.
func DeployQueueAndObjectStorage(ctx context.Context, deployment *Deployment, qSpec enterpriseApi.QueueSpec, osSpec enterpriseApi.ObjectStorageSpec) (*enterpriseApi.Queue, *enterpriseApi.ObjectStorage, error) {
	q, err := deployment.DeployQueue(ctx, "queue", qSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to deploy Queue: %w", err)
	}

	objStorage, err := deployment.DeployObjectStorage(ctx, "os", osSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to deploy ObjectStorage: %w", err)
	}

	return q, objStorage, nil
}

// SetupIngestorStack deploys the full Queue/ObjectStorage/IngestorCluster/ClusterManager/IndexerCluster stack
// and verifies each component reaches the Ready phase.
func (testcaseEnvInst *TestCaseEnv) SetupIngestorStack(ctx context.Context, deployment *Deployment, qSpec enterpriseApi.QueueSpec, osSpec enterpriseApi.ObjectStorageSpec, cmSpec enterpriseApi.ClusterManagerSpec) error {
	secretName := testcaseEnvInst.GetIndexIngestSepSecretName()
	qSpec.SQS.SecretKeyRef = &enterpriseApi.SQSSecretKeyRef{
		AwsAccessKey: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{Name: secretName}, Key: "s3_access_key"},
		AwsSecretKey: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{Name: secretName}, Key: "s3_secret_key"},
	}

	q, objStorage, err := DeployQueueAndObjectStorage(ctx, deployment, qSpec, osSpec)
	if err != nil {
		return err
	}

	if _, err := deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, v1.ObjectReference{Name: q.Name}, v1.ObjectReference{Name: objStorage.Name}, ""); err != nil {
		return fmt.Errorf("unable to deploy Ingestor Cluster: %w", err)
	}

	if _, err := deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec); err != nil {
		return fmt.Errorf("unable to deploy Cluster Manager: %w", err)
	}

	if _, err := deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", v1.ObjectReference{Name: q.Name}, v1.ObjectReference{Name: objStorage.Name}, ""); err != nil {
		return fmt.Errorf("unable to deploy Indexer Cluster: %w", err)
	}

	if err := testcaseEnvInst.VerifyIngestorReady(ctx, deployment); err != nil {
		return fmt.Errorf("ingestor not ready: %w", err)
	}
	if err := testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment); err != nil {
		return fmt.Errorf("cluster manager not ready: %w", err)
	}
	if err := testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment); err != nil {
		return fmt.Errorf("indexers not ready: %w", err)
	}
	return nil
}

// VerifyCredentialSecretVersion checks that a credential secret version is set and valid (not empty or "0").
func VerifyCredentialSecretVersion(version string, label string) error {
	if version == "" {
		return fmt.Errorf("%s queue status credential access secret version is empty", label)
	}
	if version == "0" {
		return fmt.Errorf("%s queue status credential access secret version is 0", label)
	}
	return nil
}

// DeleteIngestorStack tears down the full Queue/ObjectStorage/IngestorCluster/IndexerCluster stack.
func DeleteIngestorStack(ctx context.Context, deployment *Deployment) error {
	// Delete the Indexer Cluster
	if err := GetAndDeleteCR(ctx, deployment, &enterpriseApi.IndexerCluster{}, deployment.GetName()+"-idxc"); err != nil {
		return err
	}

	// Delete the Ingestor Cluster
	if err := GetAndDeleteCR(ctx, deployment, &enterpriseApi.IngestorCluster{}, deployment.GetName()+"-ingest"); err != nil {
		return err
	}

	// Delete the Queue
	if err := GetAndDeleteCR(ctx, deployment, &enterpriseApi.Queue{}, "queue"); err != nil {
		return err
	}

	// Delete the ObjectStorage
	return GetAndDeleteCR(ctx, deployment, &enterpriseApi.ObjectStorage{}, "os")
}
