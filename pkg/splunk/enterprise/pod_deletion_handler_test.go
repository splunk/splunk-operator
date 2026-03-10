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

package enterprise

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHandlePodDeletion_MissingStatefulSetRemovesFinalizer(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	now := metav1.NewTime(time.Now())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-idxc-indexer-0",
			Namespace: "test",
			Finalizers: []string{
				PodCleanupFinalizer,
			},
			DeletionTimestamp: &now,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       "splunk-test-idxc-indexer",
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	if err := HandlePodDeletion(ctx, c, pod); err != nil {
		t.Fatalf("HandlePodDeletion returned unexpected error: %v", err)
	}

	updated := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updated); err != nil {
		if k8serrors.IsNotFound(err) {
			return
		}
		t.Fatalf("failed to fetch updated pod: %v", err)
	}
	if hasFinalizer(updated, PodCleanupFinalizer) {
		t.Fatalf("expected pod finalizer to be removed when StatefulSet is missing")
	}
}

func TestHandlePodDeletion_NoStatefulSetOwnerRemovesFinalizer(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	now := metav1.NewTime(time.Now())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-idxc-indexer-1",
			Namespace: "test",
			Finalizers: []string{
				PodCleanupFinalizer,
			},
			DeletionTimestamp: &now,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	if err := HandlePodDeletion(ctx, c, pod); err != nil {
		t.Fatalf("HandlePodDeletion returned unexpected error: %v", err)
	}

	updated := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updated); err != nil {
		if k8serrors.IsNotFound(err) {
			return
		}
		t.Fatalf("failed to fetch updated pod: %v", err)
	}
	if hasFinalizer(updated, PodCleanupFinalizer) {
		t.Fatalf("expected pod finalizer to be removed when StatefulSet owner is missing")
	}
}
