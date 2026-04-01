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

	gomega "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// DeployQueueAndObjectStorage deploys a Queue and ObjectStorage CR and returns both.
func DeployQueueAndObjectStorage(ctx context.Context, deployment *Deployment, qSpec enterpriseApi.QueueSpec, osSpec enterpriseApi.ObjectStorageSpec) (*enterpriseApi.Queue, *enterpriseApi.ObjectStorage) {
	q, err := deployment.DeployQueue(ctx, "queue", qSpec)
	gomega.Expect(err).To(gomega.Succeed(), "Unable to deploy Queue")

	objStorage, err := deployment.DeployObjectStorage(ctx, "os", osSpec)
	gomega.Expect(err).To(gomega.Succeed(), "Unable to deploy ObjectStorage")

	return q, objStorage
}

// SetupIngestorStack deploys the full Queue/ObjectStorage/IngestorCluster/ClusterManager/IndexerCluster stack
// and verifies each component reaches the Ready phase.
func SetupIngestorStack(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, qSpec enterpriseApi.QueueSpec, osSpec enterpriseApi.ObjectStorageSpec, cmSpec enterpriseApi.ClusterManagerSpec) {
	volumeSpec := []enterpriseApi.SQSVolumeSpec{GenerateQueueVolumeSpec(
		"queue-secret-ref-volume",
		testcaseEnvInst.GetIndexIngestSepSecretName(),
	)}
	qSpec.SQS.VolList = volumeSpec

	q, objStorage := DeployQueueAndObjectStorage(ctx, deployment, qSpec, osSpec)

	_, err := deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, v1.ObjectReference{Name: q.Name}, v1.ObjectReference{Name: objStorage.Name}, "")
	gomega.Expect(err).To(gomega.Succeed(), "Unable to deploy Ingestor Cluster")

	_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
	gomega.Expect(err).To(gomega.Succeed(), "Unable to deploy Cluster Manager")

	_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", v1.ObjectReference{Name: q.Name}, v1.ObjectReference{Name: objStorage.Name}, "")
	gomega.Expect(err).To(gomega.Succeed(), "Unable to deploy Indexer Cluster")

	testcaseEnvInst.VerifyIngestorReady(ctx, deployment)
	testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)
}

// DeleteIngestorStack tears down the full Queue/ObjectStorage/IngestorCluster/IndexerCluster stack.
func DeleteIngestorStack(ctx context.Context, deployment *Deployment) {
	// Delete the Indexer Cluster
	DeleteCRWithExpect(ctx, deployment, &enterpriseApi.IndexerCluster{}, deployment.GetName()+"-idxc", "Unable to get Indexer Cluster instance", "Unable to delete Indexer Cluster instance")

	// Delete the Ingestor Cluster
	DeleteCRWithExpect(ctx, deployment, &enterpriseApi.IngestorCluster{}, deployment.GetName()+"-ingest", "Unable to get Ingestor Cluster instance", "Unable to delete Ingestor Cluster instance")

	// Delete the Queue
	DeleteCRWithExpect(ctx, deployment, &enterpriseApi.Queue{}, "queue", "Unable to get Queue instance", "Unable to delete Queue")

	// Delete the ObjectStorage
	DeleteCRWithExpect(ctx, deployment, &enterpriseApi.ObjectStorage{}, "os", "Unable to get ObjectStorage instance", "Unable to delete ObjectStorage")
}
