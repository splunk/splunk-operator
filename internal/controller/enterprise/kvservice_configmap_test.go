// Copyright (c) 2026 Splunk Inc. All rights reserved.
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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKVServiceConfigMapReferences(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := enterpriseApi.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	direct := corev1.Volume{Name: "direct", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}}}
	projected := corev1.Volume{Name: "projected", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}}}}}}
	first := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "direct", Namespace: "test"}}
	first.Spec.Volumes = []corev1.Volume{direct, projected}
	second := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "projected", Namespace: "test"}}
	second.Spec.Volumes = []corev1.Volume{projected}
	unrelated := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "test"}}
	otherNamespace := first.DeepCopy()
	otherNamespace.Name = "other"
	otherNamespace.Namespace = "other"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(first, second, unrelated, otherNamespace).Build()
	r := &KVServiceReconciler{Client: c}
	requests := r.mapConfigMapToKVServices(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test"}})
	if len(requests) != 2 {
		t.Fatalf("wanted two deduplicated same-namespace consumers: %v", requests)
	}
	seen := map[string]bool{}
	for _, request := range requests {
		if request.Namespace != "test" || seen[request.Name] {
			t.Fatalf("unexpected or duplicate request: %v", requests)
		}
		seen[request.Name] = true
	}
	if !seen["direct"] || !seen["projected"] {
		t.Fatalf("volume reference missed: %v", requests)
	}
}
