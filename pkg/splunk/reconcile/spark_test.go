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

package reconcile

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
)

func TestApplySpark(t *testing.T) {
	funcCalls := []mockFuncCall{
		{metaName: "*v1.Service-test-splunk-stack1-spark-master-service"},
		{metaName: "*v1.Service-test-splunk-stack1-spark-worker-headless"},
		{metaName: "*v1.Deployment-test-splunk-stack1-spark-master"},
		{metaName: "*v1.Deployment-test-splunk-stack1-spark-worker"},
	}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls, "Update": {funcCalls[2], funcCalls[3]}}
	current := enterprisev1.Spark{
		TypeMeta: metav1.TypeMeta{
			Kind: "Spark",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *mockClient, cr interface{}) error {
		_, err := ApplySpark(c, cr.(*enterprisev1.Spark))
		return err
	}
	reconcileTester(t, "TestApplySpark", &current, revised, createCalls, updateCalls, reconcile)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr enterprisev1.MetaObject, c ControllerClient) (bool, error) {
		_, err := ApplySpark(c, cr.(*enterprisev1.Spark))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}
