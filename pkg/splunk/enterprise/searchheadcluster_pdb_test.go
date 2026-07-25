// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestSearchHeadPodDisruptionBudgetContract(t *testing.T) {
	replicas := int32(3)
	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterpriseApi.GroupVersion.String(),
			Kind:       "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "test",
			UID:       types.UID("shc-uid"),
		},
	}
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-example-search-head",
			Namespace: "test",
			Labels:    map[string]string{"app": "search-head"},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "search-head"},
			},
		},
	}

	pdb := getSearchHeadPodDisruptionBudget(cr, statefulSet)

	if pdb.GetName() != "splunk-example-search-head-pdb" {
		t.Fatalf("PDB name = %q", pdb.GetName())
	}
	if pdb.Spec.MaxUnavailable == nil ||
		*pdb.Spec.MaxUnavailable != intstr.FromInt32(1) {
		t.Fatalf("maxUnavailable = %#v, want 1", pdb.Spec.MaxUnavailable)
	}
	if pdb.Spec.MinAvailable != nil {
		t.Fatalf("minAvailable must remain unset: %#v", pdb.Spec.MinAvailable)
	}
	if pdb.Spec.Selector == statefulSet.Spec.Selector ||
		pdb.Spec.Selector.MatchLabels["app"] != "search-head" {
		t.Fatalf("selector = %#v, want an independent Search Head selector", pdb.Spec.Selector)
	}
	if len(pdb.OwnerReferences) != 1 ||
		pdb.OwnerReferences[0].UID != cr.GetUID() ||
		pdb.OwnerReferences[0].Controller == nil ||
		!*pdb.OwnerReferences[0].Controller {
		t.Fatalf("owner references = %#v", pdb.OwnerReferences)
	}
}

func TestApplySearchHeadPodDisruptionBudget(t *testing.T) {
	ctx := context.Background()
	replicas := int32(3)
	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterpriseApi.GroupVersion.String(),
			Kind:       "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "test",
			UID:       types.UID("shc-uid"),
		},
	}
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-example-search-head",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "search-head"},
			},
		},
	}

	t.Run("creates and then remains idempotent", func(t *testing.T) {
		scheme := runtime.NewScheme()
		if err := enterpriseApi.AddToScheme(scheme); err != nil {
			t.Fatalf("add enterprise scheme: %v", err)
		}
		if err := appsv1.AddToScheme(scheme); err != nil {
			t.Fatalf("add apps scheme: %v", err)
		}
		if err := policyv1.AddToScheme(scheme); err != nil {
			t.Fatalf("add policy scheme: %v", err)
		}
		client := newFakeClientBuilder(scheme).Build()
		if err := applySearchHeadPodDisruptionBudget(ctx, client, cr, statefulSet); err != nil {
			t.Fatalf("create PDB: %v", err)
		}
		if err := applySearchHeadPodDisruptionBudget(ctx, client, cr, statefulSet); err != nil {
			t.Fatalf("repeat PDB apply: %v", err)
		}
		pdb := &policyv1.PodDisruptionBudget{}
		if err := client.Get(
			ctx,
			types.NamespacedName{
				Namespace: "test",
				Name:      "splunk-example-search-head-pdb",
			},
			pdb,
		); err != nil {
			t.Fatalf("read PDB: %v", err)
		}
		if pdb.Spec.MaxUnavailable == nil ||
			*pdb.Spec.MaxUnavailable != intstr.FromInt32(1) {
			t.Fatalf("stored maxUnavailable = %#v", pdb.Spec.MaxUnavailable)
		}
	})

	t.Run("does not take over a user-owned object", func(t *testing.T) {
		scheme := runtime.NewScheme()
		if err := enterpriseApi.AddToScheme(scheme); err != nil {
			t.Fatalf("add enterprise scheme: %v", err)
		}
		if err := appsv1.AddToScheme(scheme); err != nil {
			t.Fatalf("add apps scheme: %v", err)
		}
		if err := policyv1.AddToScheme(scheme); err != nil {
			t.Fatalf("add policy scheme: %v", err)
		}
		pdb := getSearchHeadPodDisruptionBudget(cr, statefulSet)
		pdb.OwnerReferences = nil
		client := newFakeClientBuilder(scheme).WithObjects(pdb).Build()

		err := applySearchHeadPodDisruptionBudget(ctx, client, cr, statefulSet)
		if err == nil {
			t.Fatal("expected owner conflict")
		}
	})

	t.Run("repairs an owned policy drift", func(t *testing.T) {
		scheme := runtime.NewScheme()
		if err := enterpriseApi.AddToScheme(scheme); err != nil {
			t.Fatalf("add enterprise scheme: %v", err)
		}
		if err := appsv1.AddToScheme(scheme); err != nil {
			t.Fatalf("add apps scheme: %v", err)
		}
		if err := policyv1.AddToScheme(scheme); err != nil {
			t.Fatalf("add policy scheme: %v", err)
		}
		pdb := getSearchHeadPodDisruptionBudget(cr, statefulSet)
		unsafeMaxUnavailable := intstr.FromInt32(2)
		pdb.Spec.MaxUnavailable = &unsafeMaxUnavailable
		client := newFakeClientBuilder(scheme).WithObjects(pdb).Build()

		if err := applySearchHeadPodDisruptionBudget(ctx, client, cr, statefulSet); err != nil {
			t.Fatalf("repair PDB: %v", err)
		}
		repaired := &policyv1.PodDisruptionBudget{}
		if err := client.Get(
			ctx,
			types.NamespacedName{
				Namespace: "test",
				Name:      "splunk-example-search-head-pdb",
			},
			repaired,
		); err != nil {
			t.Fatalf("read repaired PDB: %v", err)
		}
		if repaired.Spec.MaxUnavailable == nil ||
			*repaired.Spec.MaxUnavailable != intstr.FromInt32(1) {
			t.Fatalf("repaired maxUnavailable = %#v, want 1", repaired.Spec.MaxUnavailable)
		}
	})
}
