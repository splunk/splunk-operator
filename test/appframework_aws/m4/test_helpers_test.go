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
package m4appfw

import (
	"context"

	"github.com/splunk/splunk-operator/test/testenv"
)

// verifyM4ClusterReady verifies indexers are ready, cluster is configured as multisite, and SHC is ready.
func verifyM4ClusterReady(ctx context.Context, testcaseEnvInst *testenv.TestCaseEnv, deployment *testenv.Deployment, siteCount int) {
	testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)
}

// verifyM4IndexersAndSHCReady verifies indexers are ready and SHC is ready (without multisite check).
func verifyM4IndexersAndSHCReady(ctx context.Context, testcaseEnvInst *testenv.TestCaseEnv, deployment *testenv.Deployment, siteCount int) {
	testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)
}
