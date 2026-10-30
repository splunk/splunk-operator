// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validation

import (
	"reflect"
	"testing"

	v3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	v4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPostgresDatabaseMountsVersionSurface(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[v3.StandaloneSpec](), reflect.TypeFor[v3.SearchHeadClusterSpec]()} {
		_, present := typ.FieldByName("PostgresDatabaseMounts")
		assert.False(t, present, "%s must not expose a v4-only field", typ)
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[v4.StandaloneSpec](), reflect.TypeFor[v4.SearchHeadClusterSpec]()} {
		assert.Equal(t, "PostgresDatabaseMounts", typ.Field(typ.NumField()-1).Name)
	}
}

func TestPostgresDatabaseMountsV3WriteGuard(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v4.AddToScheme(scheme))
	mounts := []v4.PostgresDatabaseMountSpec{{Name: "app", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}, Database: "app"}}
	for _, kind := range []string{"Standalone", "SearchHeadCluster"} {
		t.Run(kind, func(t *testing.T) {
			var object client.Object
			resource := StandaloneV3GVR
			if kind == "Standalone" {
				object = &v4.Standalone{Name: "search", Namespace: "ns", ResourceVersion: "7", Spec: v4.StandaloneSpec{PostgresDatabaseMounts: mounts}}
			} else {
				object = &v4.SearchHeadCluster{Name: "search", Namespace: "ns", ResourceVersion: "7", Spec: v4.SearchHeadClusterSpec{PostgresDatabaseMounts: mounts}}
				resource = SearchHeadClusterV3GVR
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(object).Build()
			request := &admissionv1.AdmissionRequest{
				Resource:        metav1.GroupVersionResource{Group: resource.Group, Version: "v4", Resource: resource.Resource},
				RequestResource: &metav1.GroupVersionResource{Group: resource.Group, Version: resource.Version, Resource: resource.Resource},
				Name:            "search", Namespace: "ns", Operation: admissionv1.Update,
				OldObject: runtime.RawExtension{Raw: []byte(`{"metadata":{"name":"search","namespace":"ns","resourceVersion":"7"}}`)},
			}
			guard := NewCrossVersionFieldGuard(c)
			for _, subresource := range []string{"", "status", "scale"} {
				request.SubResource = subresource
				err := guard.Validate(t.Context(), request)
				require.ErrorContains(t, err, "spec.postgresDatabaseMounts set but unsupported in v3")
			}
			request.Operation = admissionv1.Delete
			request.SubResource = ""
			request.Options = runtime.RawExtension{Raw: []byte(`{"propagationPolicy":"Foreground"}`)}
			require.ErrorContains(t, guard.Validate(t.Context(), request), "spec.postgresDatabaseMounts set but unsupported in v3")
		})
	}
	require.Empty(t, unrepresentableStandaloneFields(&v4.StandaloneSpec{}))
}
