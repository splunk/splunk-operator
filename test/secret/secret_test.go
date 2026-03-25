// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
package secret

import (
	"context"

	. "github.com/onsi/ginkgo/v2"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Secret Test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	// S1 tests — both label variants use identical V4 config
	s1SecretLabels := []string{"mastersecret", "managersecret"}

	for _, label := range s1SecretLabels {
		label := label
		Context("Standalone deployment (S1) with LM and MC", func() {
			BeforeEach(func() {
				testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")
			})

			AfterEach(func() {
				testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
			})

			It(label+", integration, s1: Secret update on a standalone instance with LM and MC", func() {
				config := testenv.NewClusterReadinessConfigV4()
				RunS1SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
			})

			It(label+", integration, s1: Secret Object is recreated on delete and new secrets are applied to Splunk Pods", func() {
				config := testenv.NewClusterReadinessConfigV4()
				RunS1SecretDeleteTest(ctx, deployment, testcaseEnvInst, config)
			})

			It(label+", smoke, s1: Secret Object data is repopulated in secret object on passing empty Data map and new secrets are applied to Splunk Pods", func() {
				config := testenv.NewClusterReadinessConfigV4()
				RunS1SecretDeleteWithMCRefTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}

	// C3 tests — V3 (master) and V4 (manager) variants
	c3SecretConfigs := []struct {
		namePrefix string
		label      string
		newConfig  func() *testenv.ClusterReadinessConfig
	}{
		{"master", "mastersecret", testenv.NewClusterReadinessConfigV3},
		{"", "managersecret", testenv.NewClusterReadinessConfigV4},
	}

	for _, tc := range c3SecretConfigs {
		tc := tc
		Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
			BeforeEach(func() {
				testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, tc.namePrefix)
			})

			AfterEach(func() {
				testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
			})

			It(tc.label+", smoke, c3: secret update on indexers and search head cluster", func() {
				config := tc.newConfig()
				RunC3SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}

	// M4 tests — V3 (master) and V4 (manager) variants
	m4SecretConfigs := []struct {
		namePrefix string
		label      string
		newConfig  func() *testenv.ClusterReadinessConfig
	}{
		{"master", "mastersecret", testenv.NewClusterReadinessConfigV3},
		{"", "managersecret", testenv.NewClusterReadinessConfigV4},
	}

	for _, tc := range m4SecretConfigs {
		tc := tc
		Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster)", func() {
			BeforeEach(func() {
				testenv.SpecifiedTestTimeout = 40000
				testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, tc.namePrefix)
			})

			AfterEach(func() {
				testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
			})

			It(tc.label+", integration, m4: secret update on multisite indexers and search head cluster", func() {
				config := tc.newConfig()
				RunM4SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}
})
