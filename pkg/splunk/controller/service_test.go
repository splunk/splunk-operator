// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplyService(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{{MetaName: "*v1.Service-test-svc"}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}
	current := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Spec.ClusterIP = "8.8.8.8"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		return ApplyService(context.TODO(), c, cr.(*corev1.Service))
	}
	spltest.ReconcileTester(t, "TestApplyService", &current, revised, createCalls, updateCalls, reconcile, false)
}
