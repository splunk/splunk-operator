/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	enterpriseApiV4 "github.com/splunk/splunk-operator/api/enterprise/v4"
)

var (
	crossVersionOldIndexer = runtime.RawExtension{
		Raw: []byte(`{"metadata":{"name":"idx","namespace":"splunk","resourceVersion":"7"}}`),
	}
	crossVersionOldIndexerWithFinalizer = runtime.RawExtension{
		Raw: []byte(`{"metadata":{"name":"idx","namespace":"splunk","resourceVersion":"7","finalizers":["enterprise.splunk.com/protect"]}}`),
	}
	crossVersionOldSearchHead = runtime.RawExtension{
		Raw: []byte(`{"metadata":{"name":"shc","namespace":"splunk","resourceVersion":"7"}}`),
	}
)

func TestIsCrossVersionGuarded(t *testing.T) {
	tests := []struct {
		desc     string
		in       schema.GroupVersionResource
		expected bool
	}{
		{
			desc:     "returns true for v3 indexerclusters",
			in:       IndexerClusterV3GVR,
			expected: true,
		},
		{
			desc:     "returns true for v3 searchheadclusters",
			in:       SearchHeadClusterV3GVR,
			expected: true,
		},
		{
			desc:     "returns false for v4 indexerclusters",
			in:       IndexerClusterGVR,
			expected: false,
		},
		{
			desc:     "returns false for unrelated resources",
			in:       StandaloneGVR,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.expected, isCrossVersionGuarded(tt.in))
		})
	}
}

func TestNewCrossVersionFieldGuard(t *testing.T) {
	tests := []struct {
		name   string
		reader client.Reader
	}{
		{
			name:   "stores the reader",
			reader: fake.NewClientBuilder().WithScheme(scheme).Build(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := NewCrossVersionFieldGuard(tt.reader)

			require.NotNil(t, guard)
			assert.Same(t, tt.reader, guard.reader)
		})
	}
}

func TestCrossVersionFieldGuardValidate(t *testing.T) {
	tests := []struct {
		name      string
		reader    client.Reader
		request   *admissionv1.AdmissionRequest
		wantError string
	}{
		{
			name: "allows unguarded resources without reading",
			reader: fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return errors.New("unexpected read")
				},
			}).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: StandaloneGVR.Group, Version: StandaloneGVR.Version, Resource: StandaloneGVR.Resource},
				Operation: admissionv1.Update,
				OldObject: runtime.RawExtension{Raw: []byte(`{`)},
			},
		},
		{
			name: "allows non update or delete operations without reading",
			reader: fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return errors.New("unexpected read")
				},
			}).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: IndexerClusterV3GVR.Group, Version: IndexerClusterV3GVR.Version, Resource: IndexerClusterV3GVR.Resource},
				Operation: admissionv1.Create,
				OldObject: runtime.RawExtension{Raw: []byte(`{`)},
			},
		},
		{
			name:   "returns bad request when old object cannot decode",
			reader: fake.NewClientBuilder().WithScheme(scheme).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: IndexerClusterV3GVR.Group, Version: IndexerClusterV3GVR.Version, Resource: IndexerClusterV3GVR.Resource},
				Operation: admissionv1.Update,
				OldObject: runtime.RawExtension{Raw: []byte(`{`)},
			},
			wantError: "decode oldObject: unexpected end of JSON input",
		},
		{
			name: "uses request resource over equivalent relabelled resource",
			reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(&enterpriseApiV4.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "splunk", ResourceVersion: "7"},
				Spec: enterpriseApiV4.IndexerClusterSpec{
					QueueRef: &corev1.ObjectReference{Name: "queue"},
				},
			}).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:        metav1.GroupVersionResource{Group: IndexerClusterGVR.Group, Version: IndexerClusterGVR.Version, Resource: IndexerClusterGVR.Resource},
				RequestResource: &metav1.GroupVersionResource{Group: IndexerClusterV3GVR.Group, Version: IndexerClusterV3GVR.Version, Resource: IndexerClusterV3GVR.Resource},
				Name:            "idx",
				Namespace:       "splunk",
				Operation:       admissionv1.Update,
				SubResource:     "scale",
				OldObject:       crossVersionOldIndexer,
			},
			wantError: "IndexerCluster splunk/idx cannot be represented in apiVersion enterprise.splunk.com/v3: spec.queueRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "falls back to resource when request resource is unset",
			reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(&enterpriseApiV4.SearchHeadCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "splunk", ResourceVersion: "7"},
				Spec:       enterpriseApiV4.SearchHeadClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
			}).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: SearchHeadClusterV3GVR.Group, Version: SearchHeadClusterV3GVR.Version, Resource: SearchHeadClusterV3GVR.Resource},
				Name:      "shc",
				Namespace: "splunk",
				Operation: admissionv1.Update,
				OldObject: crossVersionOldSearchHead,
			},
			wantError: "SearchHeadCluster splunk/shc cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name: "allows delete that does not write back without reading",
			reader: fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return errors.New("unexpected read")
				},
			}).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: IndexerClusterV3GVR.Group, Version: IndexerClusterV3GVR.Version, Resource: IndexerClusterV3GVR.Resource},
				Name:      "idx",
				Namespace: "splunk",
				Operation: admissionv1.Delete,
				OldObject: crossVersionOldIndexer,
			},
		},
		{
			name: "uses old object name for deletecollection items",
			reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(&enterpriseApiV4.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "splunk", ResourceVersion: "7"},
				Spec:       enterpriseApiV4.IndexerClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
			}).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: IndexerClusterV3GVR.Group, Version: IndexerClusterV3GVR.Version, Resource: IndexerClusterV3GVR.Resource},
				Namespace: "splunk",
				Operation: admissionv1.Delete,
				OldObject: crossVersionOldIndexerWithFinalizer,
			},
			wantError: "IndexerCluster splunk/idx cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource",
		},
		{
			name:   "allows when the object was deleted before the guard read it",
			reader: fake.NewClientBuilder().WithScheme(scheme).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: IndexerClusterV3GVR.Group, Version: IndexerClusterV3GVR.Version, Resource: IndexerClusterV3GVR.Resource},
				Name:      "idx",
				Namespace: "splunk",
				Operation: admissionv1.Update,
				OldObject: crossVersionOldIndexer,
			},
		},
		{
			name: "returns internal error when the stored read fails",
			reader: fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return errors.New("apiserver unavailable")
				},
			}).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: IndexerClusterV3GVR.Group, Version: IndexerClusterV3GVR.Version, Resource: IndexerClusterV3GVR.Resource},
				Name:      "idx",
				Namespace: "splunk",
				Operation: admissionv1.Update,
				OldObject: crossVersionOldIndexer,
			},
			wantError: "Internal error occurred: read IndexerCluster splunk/idx: apiserver unavailable",
		},
		{
			name: "returns conflict when the stored object has changed",
			reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(&enterpriseApiV4.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "splunk", ResourceVersion: "8"},
				Spec:       enterpriseApiV4.IndexerClusterSpec{Replicas: 3},
			}).Build(),
			request: &admissionv1.AdmissionRequest{
				Resource:  metav1.GroupVersionResource{Group: IndexerClusterV3GVR.Group, Version: IndexerClusterV3GVR.Version, Resource: IndexerClusterV3GVR.Resource},
				Name:      "idx",
				Namespace: "splunk",
				Operation: admissionv1.Update,
				OldObject: crossVersionOldIndexer,
			},
			wantError: `Operation cannot be fulfilled on indexerclusters.enterprise.splunk.com "idx": the object has been modified; please apply your changes to the latest version and try again`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewCrossVersionFieldGuard(tt.reader).Validate(t.Context(), tt.request)

			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDeleteWritesBack(t *testing.T) {
	tests := []struct {
		name       string
		request    *admissionv1.AdmissionRequest
		finalizers []string
		want       bool
		wantError  string
	}{
		{
			name: "subresources do not write back",
			request: &admissionv1.AdmissionRequest{
				SubResource: "status",
			},
			finalizers: []string{"enterprise.splunk.com/protect"},
			want:       false,
		},
		{
			name:    "plain delete does not write back",
			request: &admissionv1.AdmissionRequest{},
			want:    false,
		},
		{
			name:       "finalizers write back",
			request:    &admissionv1.AdmissionRequest{},
			finalizers: []string{"enterprise.splunk.com/protect"},
			want:       true,
		},
		{
			name: "foreground propagation writes back",
			request: &admissionv1.AdmissionRequest{
				Options: runtime.RawExtension{Raw: []byte(`{"propagationPolicy":"Foreground"}`)},
			},
			want: true,
		},
		{
			name: "orphan propagation writes back",
			request: &admissionv1.AdmissionRequest{
				Options: runtime.RawExtension{Raw: []byte(`{"propagationPolicy":"Orphan"}`)},
			},
			want: true,
		},
		{
			name: "orphan dependents writes back",
			request: &admissionv1.AdmissionRequest{
				Options: runtime.RawExtension{Raw: []byte(`{"orphanDependents":true}`)},
			},
			want: true,
		},
		{
			name: "background propagation does not write back",
			request: &admissionv1.AdmissionRequest{
				Options: runtime.RawExtension{Raw: []byte(`{"propagationPolicy":"Background"}`)},
			},
			want: false,
		},
		{
			name: "invalid delete options return bad request",
			request: &admissionv1.AdmissionRequest{
				Options: runtime.RawExtension{Raw: []byte(`{`)},
			},
			wantError: "decode delete options: unexpected end of JSON input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deleteWritesBack(tt.request, tt.finalizers)

			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
