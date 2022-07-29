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
	"testing"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestApplyServiceAccount(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{{MetaName: "*v1.ServiceAccount-test-defaults"}}
	updateFunCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.ServiceAccount-test-defaults"},
		{MetaName: "*v1.ServiceAccount-test-defaults"}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFunCalls, "Update": funcCalls}
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.ResourceVersion = "dummy"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		err := ApplyServiceAccount(context.TODO(), c, cr.(*corev1.ServiceAccount))
		return err
	}
	spltest.ReconcileTester(t, "TestApplyServiceAccount", &current, revised, createCalls, updateCalls, reconcile, false)
}

func TestGetServiceAccount(t *testing.T) {
	ctx := context.TODO()
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}

	client := spltest.NewMockClient()
	namespacedName := types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()}

	// serviceAccount doesn't exist
	_, err := GetServiceAccount(ctx, client, namespacedName)
	if err == nil {
		t.Errorf("Should return an error, when the serviceAccount doesn't exist")
	}

	// Create serviceAccount
	err = ApplyServiceAccount(context.TODO(), client, &current)
	if err != nil {
		t.Errorf("Failed to create the serviceAccount. Error: %s", err.Error())
	}

	// Make sure serviceAccount exists
	got, err := GetServiceAccount(ctx, client, namespacedName)
	if err != nil {
		if got.GetName() != current.GetName() {
			t.Errorf("Incorrect service account retrieved got %s want %s", got.GetName(), current.GetName())
		}
		t.Errorf("Should not return an error, when the serviceAccount exists")
	}

	var dummySaName string = "dummy_sa"

	current.Name = dummySaName
	// Update serviceAccount
	err = ApplyServiceAccount(ctx, client, &current)
	if err != nil {
		t.Errorf("Failed to create the serviceAccount. Error: %s", err.Error())
	}

	// Make sure serviceAccount is updated
	got, err = GetServiceAccount(ctx, client, namespacedName)
	if err != nil {
		if got.GetName() != dummySaName {
			t.Errorf("Incorrect service account retrieved got %s want %s", got.GetName(), current.GetName())
		}
		t.Errorf("Should not return an error, when the serviceAccount exists")
	}
}
