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

package spark

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func init() {
	spltest.MockObjectCopiers = append(spltest.MockObjectCopiers, sparkObjectCopier)
}

// sparkObjectCopier is used to copy enterprisev1.Spark runtime.Objects
func sparkObjectCopier(dst, src runtime.Object) bool {
	switch src.(type) {
	case *enterprisev1.Spark:
		*dst.(*enterprisev1.Spark) = *src.(*enterprisev1.Spark)
	default:
		return false
	}
	return true
}

func TestApplySpark(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Service-test-splunk-stack1-spark-master-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-spark-worker-headless"},
		{MetaName: "*v1.Deployment-test-splunk-stack1-spark-master"},
		{MetaName: "*v1.Deployment-test-splunk-stack1-spark-worker"},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[2], funcCalls[3]}}
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
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySpark(c, cr.(*enterprisev1.Spark))
		return err
	}
	spltest.ReconcileTester(t, "TestApplySpark", &current, revised, createCalls, updateCalls, reconcile, false)
}

func TestDeleteSpark(t *testing.T) {
	cr := enterprisev1.Spark{
		TypeMeta: metav1.TypeMeta{
			Kind: "Spark",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	c := spltest.NewMockClient()
	currentTime := metav1.NewTime(time.Now())
	dummyFinalizer := "test.splunk.com/dummy"
	cr.ObjectMeta.DeletionTimestamp = &currentTime
	cr.ObjectMeta.Finalizers = []string{dummyFinalizer}
	c.AddObject(&cr)

	splctrl.SplunkFinalizerRegistry[dummyFinalizer] = func(cr splcommon.MetaObject, c splcommon.ControllerClient) error {
		return nil
	}

	apiVersion, _ := schema.ParseGroupVersion(enterprisev1.APIVersion)
	mockCalls := make(map[string][]spltest.MockFuncCall)
	mockCalls["Update"] = []spltest.MockFuncCall{{MetaName: fmt.Sprintf("*%s.Spark-test-stack1", apiVersion.Version)}}
	_, err := ApplySpark(c, &cr)
	if err != nil {
		t.Errorf("TestDeleteSpark() returned %v; want nil", err)
	}
	c.CheckCalls(t, "TestDeleteSpark", mockCalls)
}
