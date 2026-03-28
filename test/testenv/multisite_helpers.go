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
)

// VerifyIndexOnAllSites verifies that an index exists on all indexer pods across all sites
func (testcaseenv *TestCaseEnv) VerifyIndexOnAllSites(ctx context.Context, deployment *Deployment, deploymentName string, siteCount int, indexName string) {
	for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
		podName := fmt.Sprintf(MultiSiteIndexerPod, deploymentName, siteNumber, 0)
		testcaseenv.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)
	}
}

// IngestDataOnAllSites ingests data to an index on all indexer pods across all sites
func IngestDataOnAllSites(ctx context.Context, deployment *Deployment, deploymentName string, siteCount int, indexName string, logLineCount int) {
	for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
		podName := fmt.Sprintf(MultiSiteIndexerPod, deploymentName, siteNumber, 0)
		logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
		CreateMockLogfile(logFile, logLineCount)
		IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)
	}
}

// RollHotToWarmOnAllSites rolls hot buckets to warm on all indexer pods across all sites
func RollHotToWarmOnAllSites(ctx context.Context, deployment *Deployment, deploymentName string, siteCount int, indexName string) {
	for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
		podName := fmt.Sprintf(MultiSiteIndexerPod, deploymentName, siteNumber, 0)
		RollHotToWarm(ctx, deployment, podName, indexName)
	}
}

// VerifyIndexOnS3AllSites verifies that an index exists on S3 for all indexer pods across all sites
func (testcaseenv *TestCaseEnv) VerifyIndexOnS3AllSites(ctx context.Context, deployment *Deployment, deploymentName string, siteCount int, indexName string) {
	for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
		podName := fmt.Sprintf(MultiSiteIndexerPod, deploymentName, siteNumber, 0)
		testcaseenv.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)
	}
}

// VerifyCPULimitsOnAllSites verifies CPU limits on all indexer pods across all sites
func (testcaseenv *TestCaseEnv) VerifyCPULimitsOnAllSites(deployment *Deployment, deploymentName string, siteCount int, expectedCPULimit string) {
	for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
		podName := fmt.Sprintf(MultiSiteIndexerPod, deploymentName, siteNumber, 0)
		testcaseenv.VerifyCPULimits(deployment, podName, expectedCPULimit)
	}
}

// MultisiteIndexerWorkflow encapsulates the common workflow for multisite indexer operations:
// verify index, ingest data, roll to warm, verify on S3
func (testcaseenv *TestCaseEnv) MultisiteIndexerWorkflow(ctx context.Context, deployment *Deployment, deploymentName string, siteCount int, indexName string, logLineCount int) {
	// Verify index exists on all sites
	testcaseenv.VerifyIndexOnAllSites(ctx, deployment, deploymentName, siteCount, indexName)

	// Ingest data on all sites
	IngestDataOnAllSites(ctx, deployment, deploymentName, siteCount, indexName, logLineCount)

	// Roll hot to warm on all sites
	RollHotToWarmOnAllSites(ctx, deployment, deploymentName, siteCount, indexName)

	// Verify index on S3 for all sites
	testcaseenv.VerifyIndexOnS3AllSites(ctx, deployment, deploymentName, siteCount, indexName)
}
