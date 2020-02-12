// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package deploy

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplyDeployment(t *testing.T) {
	funcCalls := []mockFuncCall{{metaName: "*v1.Deployment-test-splunk-stack1-spark-worker"}}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls, "Update": funcCalls}
	var replicas int32 = 1
	current := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-spark-worker",
			Namespace: "test",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}
	revised := current.DeepCopy()
	*revised.Spec.Replicas = 3
	reconcile := func(c *mockClient, cr interface{}) error {
		return ApplyDeployment(c, cr.(*appsv1.Deployment))
	}
	reconcileTester(t, "TestApplyDeployment", &current, revised, createCalls, updateCalls, reconcile)
}
