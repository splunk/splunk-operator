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
package azurec3appfw

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// verifyC3ClusterReady verifies SHC is ready and single-site indexers are ready.
func verifyC3ClusterReady(ctx context.Context, testcaseEnvInst *testenv.TestCaseEnv, deployment *testenv.Deployment) {
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)
}

// verifyMCVersionChangedAndReady waits for the MC resource version to change then verifies MC is ready.
func verifyMCVersionChangedAndReady(ctx context.Context, testcaseEnvInst *testenv.TestCaseEnv, deployment *testenv.Deployment, mc *enterpriseApi.MonitoringConsole, resourceVersion string) {
	testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)
}
