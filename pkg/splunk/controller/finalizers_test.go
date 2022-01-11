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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestCheckForDeletion(t *testing.T) {
	ctx := context.TODO()
	cr := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	c := spltest.NewMockClient()
	currentTime := metav1.NewTime(time.Now())
	dummyFinalizer := "test.splunk.com/dummy"
	cr.ObjectMeta.DeletionTimestamp = &currentTime
	cr.ObjectMeta.Finalizers = []string{dummyFinalizer}
	c.AddObject(&cr)

	gotCallback := false
	SplunkFinalizerRegistry[dummyFinalizer] = func(ctx context.Context, cr splcommon.MetaObject, c splcommon.ControllerClient) error {
		gotCallback = true
		return nil
	}

	mockCalls := make(map[string][]spltest.MockFuncCall)
	mockCalls["Update"] = []spltest.MockFuncCall{{MetaName: "*v1.ConfigMap-test-defaults"}}
	_, err := CheckForDeletion(ctx, &cr, c)
	if err != nil {
		t.Errorf("TestCheckForDeletion() returned %v; want nil", err)
	}
	c.CheckCalls(t, "TestCheckForDeletion", mockCalls)
	if !gotCallback {
		t.Errorf("TestCheckForDeletion() did not call finalizer method")
	}
}
