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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
)

func TestReconcileLicenseMaster(t *testing.T) {
	funcCalls := []mockFuncCall{
		{metaName: "*v1.Secret-test-splunk-stack1-license-master-secrets"},
		{metaName: "*v1.Service-test-splunk-stack1-license-master-service"},
		{metaName: "*v1.StatefulSet-test-splunk-stack1-license-master"},
	}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls, "Update": []mockFuncCall{funcCalls[2]}}
	current := enterprisev1.LicenseMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *mockClient, cr interface{}) error {
		_, err := ReconcileLicenseMaster(c, cr.(*enterprisev1.LicenseMaster))
		return err
	}
	reconcileTester(t, "TestReconcileLicenseMaster", &current, revised, createCalls, updateCalls, reconcile)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr enterprisev1.MetaObject, c ControllerClient) (bool, error) {
		_, err := ReconcileLicenseMaster(c, cr.(*enterprisev1.LicenseMaster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}
