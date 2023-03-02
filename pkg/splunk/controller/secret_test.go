// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"errors"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplySecret(t *testing.T) {
	ctx := context.TODO()
	// Re-concile tester
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-secrets"},
		{MetaName: "*v1.Secret-test-secrets"},
	}
	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-secrets"},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Update": {funcCalls[0]}}
	current := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secrets",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Data = map[string][]byte{"a": {'1', '2'}}
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySecret(ctx, c, cr.(*corev1.Secret))
		return err
	}
	spltest.ReconcileTester(t, "TestApplySecret", &current, revised, createCalls, updateCalls, reconcile, false)

	// Test create and update scenarios
	c := spltest.NewMockClient()

	// Test create
	current.Data = map[string][]byte{"a": {'2', '1'}}
	retr, err := ApplySecret(ctx, c, &current)
	if err != nil {
		t.Errorf("ApplySecret failed %s", err.Error())
	}

	if !reflect.DeepEqual(retr, &current) {
		t.Errorf("Incorrect create got %+v want %+v", retr, current)
	}

	// Test Update
	retr.Data = map[string][]byte{"a": {'1', '2'}}
	retr2, err := ApplySecret(ctx, c, retr)
	if err != nil {
		t.Errorf("ApplySecret failed %s", err.Error())
	}

	if !reflect.DeepEqual(retr, retr2) {
		t.Errorf("Incorrect update got %+v want %+v", retr2, retr)
	}

	// Negative testing
	_, err = ApplySecret(ctx, c, nil)
	if err.Error() != splcommon.InvalidSecretObjectError {
		t.Errorf("Didn't catch invalid secret object")
	}

	// Update failure
	c = spltest.NewMockClient()
	current = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secrets",
			Namespace: "test",
		},
	}
	c.Create(ctx, &current)
	current.Data = make(map[string][]byte)
	revised = current.DeepCopy()
	revised.Data["key"] = []byte{'a'}
	c.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = errors.New("randomerror")
	_, err = ApplySecret(ctx, c, revised)
	if err == nil {
		t.Errorf("Expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorCreate] = errors.New("randomerror")
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = k8serrors.NewNotFound(appsv1.Resource("configmap"), current.GetName())
	_, err = ApplySecret(ctx, c, revised)
	if err == nil {
		t.Errorf("Expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorCreate] = nil
	_, err = ApplySecret(ctx, c, revised)
	if err == nil {
		t.Errorf("Expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = errors.New("randomerror")
	_, err = ApplySecret(ctx, c, revised)
	if err == nil {
		t.Errorf("Expected error")
	}
}
