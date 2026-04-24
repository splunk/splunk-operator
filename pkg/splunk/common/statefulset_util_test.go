// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package common

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Helper function to create a base StatefulSet for testing
func newTestStatefulSet(name, namespace string) *appsv1.StatefulSet {
	var replicas int32 = 1
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      make(map[string]string),
					Annotations: make(map[string]string),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "splunk",
							Image: "splunk/splunk:8.2.0",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestMergeStatefulSetMetaUpdates(t *testing.T) {
	tests := []struct {
		name                string
		current             func() *metav1.ObjectMeta
		revised             func() *metav1.ObjectMeta
		expectedReturn      bool
		expectedLabels      map[string]string
		expectedAnnotations map[string]string
	}{
		{
			name: "No changes - same labels and annotations",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "x"},
					Annotations: map[string]string{"note": "value"},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "x"},
					Annotations: map[string]string{"note": "value"},
				}
			},
			expectedReturn:      false,
			expectedLabels:      map[string]string{"app": "splunk", "team": "x"},
			expectedAnnotations: map[string]string{"note": "value"},
		},
		{
			name: "Label added",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: map[string]string{},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "x"},
					Annotations: map[string]string{},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"app": "splunk", "team": "x"},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "Label changed",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "x"},
					Annotations: map[string]string{},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "y"},
					Annotations: map[string]string{},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"app": "splunk", "team": "y"},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "Label removed",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "x", "env": "prod"},
					Annotations: map[string]string{},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "x"},
					Annotations: map[string]string{},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"app": "splunk", "team": "x"},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "Annotation added",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: map[string]string{},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: map[string]string{"foo": "bar"},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"app": "splunk"},
			expectedAnnotations: map[string]string{"foo": "bar"},
		},
		{
			name: "Annotation changed",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: map[string]string{"foo": "bar"},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: map[string]string{"foo": "baz"},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"app": "splunk"},
			expectedAnnotations: map[string]string{"foo": "baz"},
		},
		{
			name: "Both labels and annotations changed",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "x"},
					Annotations: map[string]string{"foo": "bar"},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk", "team": "y"},
					Annotations: map[string]string{"foo": "baz", "new": "annotation"},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"app": "splunk", "team": "y"},
			expectedAnnotations: map[string]string{"foo": "baz", "new": "annotation"},
		},
		{
			name: "Nil labels in current - handles gracefully",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      nil,
					Annotations: map[string]string{},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"team": "x"},
					Annotations: map[string]string{},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"team": "x"},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "Nil annotations in current - handles gracefully",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: nil,
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: map[string]string{"foo": "bar"},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"app": "splunk"},
			expectedAnnotations: map[string]string{"foo": "bar"},
		},
		{
			name: "Nil labels in revised - handles gracefully",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"team": "x"},
					Annotations: map[string]string{},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      nil,
					Annotations: map[string]string{},
				}
			},
			expectedReturn:      true,
			expectedLabels:      nil,
			expectedAnnotations: map[string]string{},
		},
		{
			name: "Both nil in current and revised - no change",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      nil,
					Annotations: nil,
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      nil,
					Annotations: nil,
				}
			},
			expectedReturn:      false,
			expectedLabels:      nil,
			expectedAnnotations: nil,
		},
		{
			name: "Empty maps vs nil - now considered equal (avoids false positive changes)",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{},
					Annotations: map[string]string{},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      nil,
					Annotations: nil,
				}
			},
			expectedReturn:      false,               // nil and empty map are semantically equivalent
			expectedLabels:      map[string]string{}, // current not modified when no real change
			expectedAnnotations: map[string]string{}, // current not modified when no real change
		},
		{
			name: "Multiple annotations added and removed",
			current: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: map[string]string{"old1": "val1", "old2": "val2"},
				}
			},
			revised: func() *metav1.ObjectMeta {
				return &metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "default",
					Labels:      map[string]string{"app": "splunk"},
					Annotations: map[string]string{"new1": "val1", "new2": "val2"},
				}
			},
			expectedReturn:      true,
			expectedLabels:      map[string]string{"app": "splunk"},
			expectedAnnotations: map[string]string{"new1": "val1", "new2": "val2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			current := tt.current()
			revised := tt.revised()

			result := MergeStatefulSetMetaUpdates(ctx, current, revised, "test-sts")

			if result != tt.expectedReturn {
				t.Errorf("MergeStatefulSetMetaUpdates() returned %v, want %v", result, tt.expectedReturn)
			}

			if !reflect.DeepEqual(current.Labels, tt.expectedLabels) {
				t.Errorf("After merge, Labels = %v, want %v", current.Labels, tt.expectedLabels)
			}

			if !reflect.DeepEqual(current.Annotations, tt.expectedAnnotations) {
				t.Errorf("After merge, Annotations = %v, want %v", current.Annotations, tt.expectedAnnotations)
			}
		})
	}
}
